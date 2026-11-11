package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
)

func TestEntryCompactsGroupsPostgres(t *testing.T) {
	for _, operation := range []string{"delete middle", "delete last", "move forward", "move backward", "rollback"} {
		t.Run(operation, func(t *testing.T) {
			pool := ratingTestPool(t)
			ctx := context.Background()
			repo := NewEntryRepository(pool)
			shared := insertTestMovie(t, ctx, pool, "Repeated movie")
			first := createTestEntry(t, ctx, repo, shared, 1)
			middle := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Middle"), 2)
			last := createTestEntry(t, ctx, repo, shared, 3)
			fourth := createTestEntry(t, ctx, repo, shared, 4)
			var picker uuid.UUID
			if err := pool.QueryRow(ctx, "SELECT id FROM persons LIMIT 1").Scan(&picker); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, "UPDATE entries SET picked_by_person_id=$1 WHERE id=$2", picker, last.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, "INSERT INTO ratings(person_id,entry_id,score) VALUES($1,$2,8)", picker, last.ID); err != nil {
				t.Fatal(err)
			}
			want := map[uuid.UUID][2]int{first.ID: {1, 1}, middle.ID: {2, 1}, last.ID: {3, 1}, fourth.ID: {4, 1}}
			switch operation {
			case "delete middle":
				if err := repo.Delete(ctx, middle.ID); err != nil {
					t.Fatal(err)
				}
				delete(want, middle.ID)
				want[last.ID] = [2]int{2, 1}
				want[fourth.ID] = [2]int{3, 1}
			case "delete last":
				if err := repo.Delete(ctx, fourth.ID); err != nil {
					t.Fatal(err)
				}
				delete(want, fourth.ID)
			case "move forward", "move backward", "rollback":
				target := 4
				if operation == "move backward" {
					target = 1
				}
				input := model.UpdateEntryInput{GroupNumber: &target}
				if operation == "rollback" {
					unknown := uuid.New()
					input.PickedByPersonID = &unknown
				}
				err := repo.Update(ctx, middle.ID, input)
				if operation == "rollback" {
					if !errors.Is(err, ErrPersonNotFound) {
						t.Fatal(err)
					}
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				want[last.ID] = [2]int{2, 1}
				want[fourth.ID] = [2]int{3, 1}
				want[middle.ID] = [2]int{3, 2}
				if operation == "move backward" {
					want[middle.ID] = [2]int{1, 2}
				}
			}
			for id, expected := range want {
				group, pos := entryGroupAndPosition(t, ctx, pool, id)
				if [2]int{group, pos} != expected {
					t.Fatalf("%s at %d/%d, want %v", id, group, pos, expected)
				}
			}
			got, err := repo.GetByID(ctx, last.ID)
			if err != nil || got.PickedByPersonID == nil || *got.PickedByPersonID != picker || len(got.Ratings) != 1 || got.Ratings[0].Score != 8 {
				t.Fatalf("lost references: %+v, %v", got, err)
			}
			if _, err := pool.Exec(ctx, "DELETE FROM ratings WHERE person_id=$1", picker); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, "DELETE FROM persons WHERE id=$1", picker); err != nil {
				t.Fatal(err)
			}
			got, err = repo.GetByID(ctx, last.ID)
			if err != nil || got.PickedByPersonID != nil {
				t.Fatalf("picker SET NULL lost: %+v %v", got, err)
			}
		})
	}
}

// Hold a compaction uncommitted and verify every writer waits before reading
// group numbers. A stale create is rejected; reorder cannot touch a new group.
func TestEntryCompactionSerializesWritersPostgres(t *testing.T) {
	for _, operation := range []string{"create", "delete", "reorder", "move"} {
		t.Run(operation, func(t *testing.T) {
			pool := ratingTestPool(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			repo := NewEntryRepository(pool)
			createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "First"), 1)
			middle := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Middle"), 2)
			last := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Last"), 3)
			movie := insertTestMovie(t, ctx, pool, "New")
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if err := lockEntryGroups(ctx, tx); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, "DELETE FROM entries WHERE id=$1", middle.ID); err != nil {
				t.Fatal(err)
			}
			if err := compactEntryGroups(ctx, tx, 2); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				switch operation {
				case "create":
					_, err := repo.Create(ctx, model.CreateEntryInput{MovieID: movie, GroupNumber: 4})
					done <- err
				case "delete":
					done <- repo.Delete(ctx, last.ID)
				case "reorder":
					done <- repo.ReorderEntries(ctx, 3, []uuid.UUID{last.ID})
				case "move":
					target := 1
					done <- repo.Update(ctx, last.ID, model.UpdateEntryInput{GroupNumber: &target})
				}
			}()
			waitForBlockedMove(t, ctx, pool, done)
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			err = <-done
			wantErr := map[string]error{"create": ErrGroupNumberingChanged, "move": ErrGroupNumberingChanged, "reorder": ErrReorderMismatch}[operation]
			if !errors.Is(err, wantErr) {
				t.Fatalf("writer error %v, want %v", err, wantErr)
			}
			var maxGroup, groups int
			if err := pool.QueryRow(ctx, "SELECT COALESCE(MAX(group_number),0), COUNT(DISTINCT group_number) FROM entries").Scan(&maxGroup, &groups); err != nil {
				t.Fatal(err)
			}
			if maxGroup != groups {
				t.Fatalf("groups have gap: max=%d count=%d", maxGroup, groups)
			}
		})
	}
}
