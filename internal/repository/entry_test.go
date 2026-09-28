package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func insertTestMovie(t *testing.T, ctx context.Context, pool *pgxpool.Pool, title string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO movies(title) VALUES ($1) RETURNING id`, title).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func createTestEntry(t *testing.T, ctx context.Context, repo *EntryRepository, movieID uuid.UUID, group int) *model.Entry {
	t.Helper()
	entry, err := repo.Create(ctx, model.CreateEntryInput{MovieID: movieID, GroupNumber: group})
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func entryGroupAndPosition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (int, int) {
	t.Helper()
	var group, position int
	if err := pool.QueryRow(ctx, `SELECT group_number, position FROM entries WHERE id = $1`, id).Scan(&group, &position); err != nil {
		t.Fatal(err)
	}
	return group, position
}

func TestEntryUpdateMovesGroupPostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewEntryRepository(pool)

	shared := insertTestMovie(t, ctx, pool, "Shared")
	a := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "A"), 1)
	b := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "B"), 1)
	sharedInOne := createTestEntry(t, ctx, repo, shared, 1)
	createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "C"), 2)
	createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "D"), 2)
	createTestEntry(t, ctx, repo, shared, 2)

	// b sits at position 2 in group 1; group 2 already holds positions 1..3.
	target := 2
	if err := repo.Update(ctx, b.ID, model.UpdateEntryInput{GroupNumber: &target}); err != nil {
		t.Fatalf("move entry: %v", err)
	}
	if group, position := entryGroupAndPosition(t, ctx, pool, b.ID); group != 2 || position != 4 {
		t.Fatalf("moved entry at group=%d position=%d, want group=2 position=4", group, position)
	}

	// Moving into a group that already has the movie is a conflict and leaves the entry alone.
	err := repo.Update(ctx, sharedInOne.ID, model.UpdateEntryInput{GroupNumber: &target})
	if !errors.Is(err, ErrEntryExistsInGroup) {
		t.Fatalf("expected ErrEntryExistsInGroup, got %v", err)
	}
	if group, position := entryGroupAndPosition(t, ctx, pool, sharedInOne.ID); group != 1 || position != 3 {
		t.Fatalf("conflicting entry changed to group=%d position=%d", group, position)
	}

	// Updating to the current group keeps the position.
	same := 1
	if err := repo.Update(ctx, a.ID, model.UpdateEntryInput{GroupNumber: &same}); err != nil {
		t.Fatal(err)
	}
	if group, position := entryGroupAndPosition(t, ctx, pool, a.ID); group != 1 || position != 1 {
		t.Fatalf("same-group update moved entry to group=%d position=%d", group, position)
	}

	// Moving to an empty group starts at position 1 and still applies picked_by.
	empty := 7
	var picker uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM persons ORDER BY initial LIMIT 1`).Scan(&picker); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, a.ID, model.UpdateEntryInput{GroupNumber: &empty, PickedByPersonID: &picker}); err != nil {
		t.Fatal(err)
	}
	moved, err := repo.GetByID(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if moved.GroupNumber != 7 || moved.Position != 1 || moved.PickedByPersonID == nil || *moved.PickedByPersonID != picker {
		t.Fatalf("unexpected moved entry: group=%d position=%d picked_by=%v", moved.GroupNumber, moved.Position, moved.PickedByPersonID)
	}

	if err := repo.Update(ctx, uuid.New(), model.UpdateEntryInput{GroupNumber: &target}); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("expected ErrEntryNotFound, got %v", err)
	}
}

func TestEntryUpdateConcurrentMovesPostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewEntryRepository(pool)

	createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Target resident"), 10)
	const movers = 8
	ids := make([]uuid.UUID, movers)
	for i := range ids {
		// Each mover starts at position 1 of its own group, colliding with the target's position 1.
		ids[i] = createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Mover"), i+1).ID
	}

	target := 10
	var wg sync.WaitGroup
	errs := make(chan error, movers)
	for _, id := range ids {
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			errs <- repo.Update(ctx, id, model.UpdateEntryInput{GroupNumber: &target})
		}(id)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent move failed: %v", err)
		}
	}

	var count, distinct, maxPosition int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(DISTINCT position), MAX(position) FROM entries WHERE group_number = 10`).Scan(&count, &distinct, &maxPosition); err != nil {
		t.Fatal(err)
	}
	if count != movers+1 || distinct != count || maxPosition != count {
		t.Fatalf("group 10 has count=%d distinct positions=%d max=%d", count, distinct, maxPosition)
	}
}

func TestEntryCreateAssignsPositionsPostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewEntryRepository(pool)

	movie := insertTestMovie(t, ctx, pool, "First")
	for i, want := range []struct{ group, position int }{{1, 1}, {1, 2}, {2, 1}, {1, 3}} {
		entryMovie := movie
		if i > 0 {
			entryMovie = insertTestMovie(t, ctx, pool, "Next")
		}
		entry := createTestEntry(t, ctx, repo, entryMovie, want.group)
		if entry.GroupNumber != want.group || entry.Position != want.position {
			t.Fatalf("entry %d: got group=%d position=%d, want %+v", i, entry.GroupNumber, entry.Position, want)
		}
	}

	// The handler relies on a unique violation to detect duplicates within a group.
	_, err := repo.Create(ctx, model.CreateEntryInput{MovieID: movie, GroupNumber: 1})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != "entries_movie_group_unique" {
		t.Fatalf("expected entries_movie_group_unique violation, got %v", err)
	}
}

func TestEntryCreateConcurrentPostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewEntryRepository(pool)

	const creators = 10
	movies := make([]uuid.UUID, creators)
	for i := range movies {
		movies[i] = insertTestMovie(t, ctx, pool, "Concurrent")
	}

	var wg sync.WaitGroup
	positions := make(chan int, creators)
	errs := make(chan error, creators)
	for _, movieID := range movies {
		wg.Add(1)
		go func(movieID uuid.UUID) {
			defer wg.Done()
			entry, err := repo.Create(ctx, model.CreateEntryInput{MovieID: movieID, GroupNumber: 5})
			if err != nil {
				errs <- err
				return
			}
			positions <- entry.Position
		}(movieID)
	}
	wg.Wait()
	close(errs)
	close(positions)
	for err := range errs {
		t.Fatalf("concurrent create failed: %v", err)
	}
	seen := map[int]bool{}
	for position := range positions {
		if position < 1 || position > creators || seen[position] {
			t.Fatalf("unexpected or duplicate position %d", position)
		}
		seen[position] = true
	}
	if len(seen) != creators {
		t.Fatalf("got %d positions, want %d", len(seen), creators)
	}
}

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
		entries, err := repo.ListByGroup(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]uuid.UUID, len(entries))
		for i, entry := range entries {
			ids[i] = entry.ID
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

	// An entry from another group rejects the whole reorder and changes nothing.
	if err := repo.ReorderEntries(ctx, 1, []uuid.UUID{second.ID, other.ID, first.ID}); err == nil {
		t.Fatal("expected reorder with foreign entry to fail")
	}
	assertOrder(first.ID, third.ID, second.ID)
	if group, position := entryGroupAndPosition(t, ctx, pool, other.ID); group != 2 || position != 1 {
		t.Fatalf("foreign entry changed to group=%d position=%d", group, position)
	}

	if err := repo.ReorderEntries(ctx, 1, nil); err != nil {
		t.Fatalf("empty reorder: %v", err)
	}

	// New entries still land after reordered ones.
	fourth := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Fourth"), 1)
	if fourth.Position != 4 {
		t.Fatalf("new entry position %d, want 4", fourth.Position)
	}
}
