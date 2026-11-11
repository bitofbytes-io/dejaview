package repository

import (
	"context"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
)

// TestEntryListAllPostgres covers the single dashboard query: newest group
// first, display order within each group, and each entry's movie, picker and
// ratings.
func TestEntryListAllPostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewEntryRepository(pool)

	people := map[string]uuid.UUID{}
	for _, initial := range []string{"A", "C", "D"} {
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO persons (initial, name) VALUES ($1, $2)
			ON CONFLICT (initial) DO UPDATE SET name = EXCLUDED.name RETURNING id`, initial, "Person "+initial).Scan(&id); err != nil {
			t.Fatal(err)
		}
		people[initial] = id
	}

	g1a := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "One A"), 1)
	g1b := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "One B"), 1)
	g2a := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Two A"), 2)
	g3 := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Three"), 3)
	g2b := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Two B"), 2)
	g2c := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Two C"), 2)
	if err := repo.ReorderEntries(ctx, 2, []uuid.UUID{g2b.ID, g2c.ID, g2a.ID}); err != nil {
		t.Fatal(err)
	}
	picker := people["C"]
	if err := repo.Update(ctx, g3.ID, model.UpdateEntryInput{PickedByPersonID: &picker}); err != nil {
		t.Fatal(err)
	}
	for initial, score := range map[string]float64{"D": 8, "A": 6, "C": 7} {
		if _, err := pool.Exec(ctx, `INSERT INTO ratings (person_id, entry_id, score) VALUES ($1, $2, $3)`, people[initial], g2c.ID, score); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []uuid.UUID{g3.ID, g2b.ID, g2c.ID, g2a.ID, g1b.ID, g1a.ID}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries, want %d", len(entries), len(want))
	}
	for i, entry := range entries {
		if entry.ID != want[i] {
			t.Fatalf("entry %d is %s (%s), want %s", i, entry.ID, entry.Movie.Title, want[i])
		}
	}
	if picked := entries[0].PickedByPerson; picked == nil || picked.ID != picker || entries[0].Movie.Title != "Three" {
		t.Fatalf("group 3 entry movie=%q picker=%+v", entries[0].Movie.Title, picked)
	}
	if entries[3].PickedByPerson != nil || entries[3].Ratings != nil {
		t.Fatalf("unpicked, unrated entry has picker=%+v ratings=%v", entries[3].PickedByPerson, entries[3].Ratings)
	}

	initials := func(entry *model.Entry) string {
		var got string
		for _, rating := range entry.Ratings {
			got += rating.Person.Initial
		}
		return got
	}
	if got := initials(entries[2]); got != "ACD" {
		t.Fatalf("listed ratings by %q, want ACD", got)
	}
	single, err := repo.GetByID(ctx, g2c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := initials(single); got != "ACD" || single.Movie.Title != "Two C" {
		t.Fatalf("GetByID ratings by %q for %q, want ACD for Two C", got, single.Movie.Title)
	}
}
