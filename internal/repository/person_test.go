package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func personColors(t *testing.T, ctx context.Context, repo *PersonRepository) map[string]string {
	t.Helper()
	people, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	colors := make(map[string]string, len(people))
	for _, person := range people {
		colors[person.Initial] = person.Color
	}
	return colors
}

func TestPersonColorMigrationKeepsSeededColours(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewPersonRepository(pool)

	colors := personColors(t, ctx, repo)
	for initial, want := range map[string]string{"D": "cyan", "J": "rose", "C": "amber", "A": "violet"} {
		if colors[initial] != want {
			t.Errorf("seeded %s has colour %q, want %q", initial, colors[initial], want)
		}
	}

	// Replay only the backfill for a database whose people were replaced
	// before this migration: unknown initials cycle through the palette.
	content, err := os.ReadFile("../../migrations/013_add_persons_color.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.Split(string(content), "-- +goose Down")[0]
	backfill := up[strings.Index(up, "UPDATE persons"):]
	for _, initial := range []string{"B", "E"} {
		if _, err := repo.Create(ctx, initial, "Person "+initial, "cyan"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, backfill); err != nil {
		t.Fatalf("replay backfill: %v", err)
	}
	// By initial order: A B C D E J. Known initials keep their colour and
	// the rest take the palette position of their row.
	colors = personColors(t, ctx, repo)
	for initial, want := range map[string]string{"A": "violet", "B": "rose", "C": "amber", "D": "cyan", "E": "violet", "J": "rose"} {
		if colors[initial] != want {
			t.Errorf("after backfill %s has colour %q, want %q", initial, colors[initial], want)
		}
	}
}

func TestPersonRepositoryManagesPeople(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewPersonRepository(pool)

	created, err := repo.Create(ctx, "Z", "Zed", "mint")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == uuid.Nil || created.Initial != "Z" || created.Name != "Zed" || created.Color != "mint" {
		t.Fatalf("unexpected person %+v", created)
	}
	if _, err := repo.Create(ctx, "Z", "Other", "rose"); !errors.Is(err, ErrPersonInitialTaken) {
		t.Fatalf("duplicate initial: got %v, want ErrPersonInitialTaken", err)
	}

	if err := repo.Update(ctx, created.ID, "Zedd", "amber"); err != nil {
		t.Fatal(err)
	}
	people, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, person := range people {
		if person.ID == created.ID {
			found = true
			if person.Name != "Zedd" || person.Color != "amber" || person.Initial != "Z" {
				t.Fatalf("update not stored: %+v", person)
			}
		}
	}
	if !found {
		t.Fatal("created person missing from GetAll")
	}
	if err := repo.Update(ctx, uuid.New(), "Nobody", "rose"); !errors.Is(err, ErrPersonNotFound) {
		t.Fatalf("update unknown: got %v, want ErrPersonNotFound", err)
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete unused person: %v", err)
	}
	if err := repo.Delete(ctx, created.ID); !errors.Is(err, ErrPersonNotFound) {
		t.Fatalf("delete twice: got %v, want ErrPersonNotFound", err)
	}
}

func TestPersonRepositoryKeepsPeopleWithRatingsOrPicks(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewPersonRepository(pool)

	rater, err := repo.Create(ctx, "R", "Rater", "rose")
	if err != nil {
		t.Fatal(err)
	}
	picker, err := repo.Create(ctx, "P", "Picker", "mint")
	if err != nil {
		t.Fatal(err)
	}
	movieID := insertTestMovie(t, ctx, pool, "Kept")
	entry := createTestEntry(t, ctx, NewEntryRepository(pool), movieID, 1)
	if _, err := pool.Exec(ctx, `INSERT INTO ratings (person_id, entry_id, score) VALUES ($1, $2, 7)`, rater.ID, entry.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE entries SET picked_by_person_id = $1 WHERE id = $2`, picker.ID, entry.ID); err != nil {
		t.Fatal(err)
	}

	for _, person := range []uuid.UUID{rater.ID, picker.ID} {
		if err := repo.Delete(ctx, person); !errors.Is(err, ErrPersonInUse) {
			t.Fatalf("delete %s: got %v, want ErrPersonInUse", person, err)
		}
	}

	var pickedBy *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT picked_by_person_id FROM entries WHERE id = $1`, entry.ID).Scan(&pickedBy); err != nil {
		t.Fatal(err)
	}
	if pickedBy == nil || *pickedBy != picker.ID {
		t.Fatalf("pick was cleared: %v", pickedBy)
	}
}
