package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
)

// All numeric targets remain in range after compaction. A range check alone
// would therefore silently direct create/move to the original fourth group.
func TestEntryRejectsRenumberedTargetWhileWaitingPostgres(t *testing.T) {
	for _, commit := range []bool{true, false} {
		for _, operation := range []string{"create", "move", "reorder"} {
			t.Run(fmt.Sprintf("%s/commit=%v", operation, commit), func(t *testing.T) {
				pool := ratingTestPool(t)
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				repo := NewEntryRepository(pool)
				entries := make([]*model.Entry, 4)
				for i := range entries {
					entries[i] = createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Resident"), i+1)
				}
				// Keep the move's source group alive.
				mover := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Mover"), 1)
				movie := insertTestMovie(t, ctx, pool, "New")
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = tx.Rollback(context.Background()) }()
				if err := lockEntryGroups(ctx, tx); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(ctx, "DELETE FROM entries WHERE id=$1", entries[1].ID); err != nil {
					t.Fatal(err)
				}
				if err := compactEntryGroups(ctx, tx, 2); err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() {
					switch operation {
					case "create":
						_, err := repo.Create(ctx, model.CreateEntryInput{MovieID: movie, GroupNumber: 3})
						done <- err
					case "move":
						target := 3
						done <- repo.Update(ctx, mover.ID, model.UpdateEntryInput{GroupNumber: &target})
					case "reorder":
						done <- repo.ReorderEntries(ctx, 3, []uuid.UUID{entries[2].ID})
					}
				}()
				waitForBlockedMove(t, ctx, pool, done)
				if commit {
					err = tx.Commit(ctx)
				} else {
					err = tx.Rollback(ctx)
				}
				if err != nil {
					t.Fatal(err)
				}
				var want error
				if commit {
					want = ErrGroupNumberingChanged
					if operation == "reorder" {
						want = ErrReorderMismatch
					}
				}
				if err := <-done; !errors.Is(err, want) {
					t.Fatalf("writer returned %v, want %v", err, want)
				}
				if commit {
					if group, pos := entryGroupAndPosition(t, ctx, pool, mover.ID); group != 1 || pos != 2 {
						t.Fatalf("rejected move changed source: %d/%d", group, pos)
					}
					if group, pos := entryGroupAndPosition(t, ctx, pool, entries[3].ID); group != 3 || pos != 1 {
						t.Fatalf("replacement target changed: %d/%d", group, pos)
					}
					var count int
					if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM entries WHERE movie_id=$1", movie).Scan(&count); err != nil || count != 0 {
						t.Fatalf("rejected add persisted: count=%d err=%v", count, err)
					}
				}
			})
		}
	}
}

func TestEntryRejectsStaleDashboardVersionPostgres(t *testing.T) {
	for _, removed := range []int{2, 4} {
		t.Run(fmt.Sprintf("remove-%d", removed), func(t *testing.T) {
			pool := ratingTestPool(t)
			ctx := context.Background()
			repo := NewEntryRepository(pool)
			entries := make([]*model.Entry, 4)
			for i := range entries {
				entries[i] = createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Resident"), i+1)
			}
			_, state, err := repo.ListDashboard(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.Delete(ctx, entries[removed-1].ID); err != nil {
				t.Fatal(err)
			}
			// Group 3 still exists; group 4 is now the valid next new group. Neither
			// should accept an old selection after its meaning changed.
			target := 3
			if removed == 4 {
				target = 4
			}
			movie := insertTestMovie(t, ctx, pool, "Stale add")
			_, err = repo.Create(ctx, model.CreateEntryInput{MovieID: movie, GroupNumber: target, GroupVersion: &state.Version})
			if !errors.Is(err, ErrGroupNumberingChanged) {
				t.Fatalf("stale create: %v", err)
			}
			err = repo.Update(ctx, entries[0].ID, model.UpdateEntryInput{GroupNumber: &target, GroupVersion: &state.Version})
			if !errors.Is(err, ErrGroupNumberingChanged) {
				t.Fatalf("stale move: %v", err)
			}
			current, err := repo.GroupState(ctx)
			if err != nil || current.Version != state.Version+1 {
				t.Fatalf("state after deletion: %+v, %v", current, err)
			}
			if _, err := repo.Create(ctx, model.CreateEntryInput{MovieID: movie, GroupNumber: target, GroupVersion: &current.Version}); err != nil {
				t.Fatalf("fresh add: %v", err)
			}
		})
	}
}

func TestEntryHistoricalCapacityPostgres(t *testing.T) {
	for _, operation := range []string{"delete first", "move first", "rollback", "delete part of first", "delete middle", "delete only group"} {
		t.Run(operation, func(t *testing.T) {
			pool := ratingTestPool(t)
			ctx := context.Background()
			repo := NewEntryRepository(pool)
			first := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Historical"), 1)
			secondSize := 9
			if operation == "move first" {
				secondSize = 8
			}
			if operation != "delete only group" {
				for i := 0; i < secondSize; i++ {
					createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Normal"), 2)
				}
			}
			wantCapacity, wantVersion := 8, int64(0)
			switch operation {
			case "delete first", "delete only group":
				if err := repo.Delete(ctx, first.ID); err != nil {
					t.Fatal(err)
				}
				wantCapacity, wantVersion = 9, 1
			case "move first", "rollback":
				target := 2
				input := model.UpdateEntryInput{GroupNumber: &target}
				if operation == "rollback" {
					unknown := uuid.New()
					input.PickedByPersonID = &unknown
				}
				err := repo.Update(ctx, first.ID, input)
				if operation == "rollback" {
					if !errors.Is(err, ErrPersonNotFound) {
						t.Fatalf("rollback: %v", err)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					wantCapacity, wantVersion = 9, 1
				}
			case "delete part of first":
				createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Other historical"), 1)
				if err := repo.Delete(ctx, first.ID); err != nil {
					t.Fatal(err)
				}
			case "delete middle":
				middle := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Middle"), 3)
				createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Last"), 4)
				if err := repo.Delete(ctx, middle.ID); err != nil {
					t.Fatal(err)
				}
				wantVersion = 1
			}
			entries, state, err := repo.ListDashboard(ctx)
			if err != nil || state.Capacity(1) != wantCapacity || state.Capacity(2) != 9 || state.Version != wantVersion {
				t.Fatalf("dashboard state %+v, err=%v; want capacity=%d version=%d", state, err, wantCapacity, wantVersion)
			}
			if operation == "delete first" || operation == "move first" {
				if len(entries) != 9 {
					t.Fatalf("compacted first group contains %d movies, want 9", len(entries))
				}
				for _, entry := range entries {
					if entry.GroupNumber != 1 {
						t.Fatalf("group %d was not compacted to 1", entry.GroupNumber)
					}
				}
			}
			if operation == "delete only group" {
				createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "New collection"), 1)
				state, err = repo.GroupState(ctx)
				if err != nil || state.Capacity(1) != 9 {
					t.Fatalf("new collection restored historical capacity: %+v, %v", state, err)
				}
			}
		})
	}
}

// Build the pre-015 schema with legal legacy gaps, then apply the actual Up
// migration. A later compaction must not give a normal group the historical
// eight-pick capacity just because its display number becomes 1.
func TestEntryGroupStateLegacyUpgradePostgres(t *testing.T) {
	migration, err := os.ReadFile("../../migrations/015_add_entry_group_state.sql")
	if err != nil {
		t.Fatal(err)
	}
	sections := strings.SplitN(string(migration), "-- +goose Down", 2)
	for _, tc := range []struct {
		name                       string
		firstGroup, size, capacity int
	}{
		{"historical group already absent", 2, 9, 9},
		{"historical group remains", 1, 8, 8},
		{"fresh database", 0, 0, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := ratingTestPool(t)
			ctx := context.Background()
			// 015 only adds this state table; its Down leaves the pre-upgrade schema.
			if _, err := pool.Exec(ctx, sections[1]); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < tc.size; i++ {
				insertTestEntry(t, ctx, pool, insertTestMovie(t, ctx, pool, "Legacy pick"), tc.firstGroup)
			}
			var last uuid.UUID
			if tc.size > 0 {
				insertTestEntry(t, ctx, pool, insertTestMovie(t, ctx, pool, "Last legacy pick"), 3)
				if err := pool.QueryRow(ctx, "SELECT id FROM entries WHERE group_number=3").Scan(&last); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := pool.Exec(ctx, sections[0]); err != nil {
				t.Fatal(err)
			}
			repo := NewEntryRepository(pool)
			initial, err := repo.GroupState(ctx)
			if err != nil || initial.Version != 0 || initial.Capacity(1) != tc.capacity {
				t.Fatalf("upgrade state=%+v err=%v; want version=0 capacity=%d", initial, err, tc.capacity)
			}
			wantVersion := int64(0)
			if last != uuid.Nil {
				if err := repo.Delete(ctx, last); err != nil {
					t.Fatal(err)
				}
				wantVersion = 1
			}
			entries, state, err := repo.ListDashboard(ctx)
			if err != nil || len(entries) != tc.size || state.Version != wantVersion || state.Capacity(1) != tc.capacity {
				t.Fatalf("after compaction entries=%d state=%+v err=%v; want entries=%d version=%d capacity=%d", len(entries), state, err, tc.size, wantVersion, tc.capacity)
			}
			for _, entry := range entries {
				if entry.GroupNumber != 1 {
					t.Fatalf("legacy group did not compact to 1: %+v", entry)
				}
			}
		})
	}
}
