package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEntryReorderPostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewEntryRepository(pool)

	first := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "First"), 1)
	second := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Second"), 1)
	third := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Third"), 1)
	other := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Other group"), 2)

	listOrder := func() []uuid.UUID {
		t.Helper()
		entries, err := repo.ListAll(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var ids []uuid.UUID
		for _, entry := range entries {
			if entry.GroupNumber == 1 {
				ids = append(ids, entry.ID)
			}
		}
		return ids
	}
	assertOrder := func(want ...uuid.UUID) {
		t.Helper()
		got := listOrder()
		if len(got) != len(want) {
			t.Fatalf("got %d entries, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("order mismatch at %d: got %v, want %v", i, got, want)
			}
		}
	}

	// Newest first by default.
	assertOrder(third.ID, second.ID, first.ID)

	if err := repo.ReorderEntries(ctx, 1, []uuid.UUID{first.ID, third.ID, second.ID}); err != nil {
		t.Fatal(err)
	}
	assertOrder(first.ID, third.ID, second.ID)
	if _, position := entryGroupAndPosition(t, ctx, pool, first.ID); position != 3 {
		t.Fatalf("first visual entry has position %d, want 3", position)
	}

	// A request that does not list exactly the group's entries changes nothing.
	for name, ids := range map[string][]uuid.UUID{
		"foreign entry": {second.ID, other.ID, first.ID},
		"missing entry": {second.ID, first.ID},
		"duplicate":     {second.ID, first.ID, first.ID},
		"extra entry":   {second.ID, third.ID, first.ID, other.ID},
		"empty":         {},
	} {
		if err := repo.ReorderEntries(ctx, 1, ids); !errors.Is(err, ErrReorderMismatch) {
			t.Fatalf("%s: expected ErrReorderMismatch, got %v", name, err)
		}
	}
	assertOrder(first.ID, third.ID, second.ID)
	if group, position := entryGroupAndPosition(t, ctx, pool, other.ID); group != 2 || position != 1 {
		t.Fatalf("foreign entry changed to group=%d position=%d", group, position)
	}

	if err := repo.ReorderEntries(ctx, 1, nil); !errors.Is(err, ErrReorderMismatch) {
		t.Fatalf("empty reorder of a non-empty group: expected ErrReorderMismatch, got %v", err)
	}
	if err := repo.ReorderEntries(ctx, 7, nil); err != nil {
		t.Fatalf("empty reorder of an empty group: %v", err)
	}

	// New entries still land after reordered ones.
	fourth := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Fourth"), 1)
	if fourth.Position != 4 {
		t.Fatalf("new entry position %d, want 4", fourth.Position)
	}
}
