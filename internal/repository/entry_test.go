package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
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

// insertTestEntry stores an entry at the end of a group with plain SQL, for
// tests that need a group number Create rejects because it skips ahead.
func insertTestEntry(t *testing.T, ctx context.Context, pool *pgxpool.Pool, movieID uuid.UUID, group int) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO entries (movie_id, group_number, position)
		VALUES ($1, $2, COALESCE((SELECT MAX(position) FROM entries WHERE group_number = $2), 0) + 1)`, movieID, group); err != nil {
		t.Fatal(err)
	}
}

func entryGroupAndPosition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (int, int) {
	t.Helper()
	var group, position int
	if err := pool.QueryRow(ctx, `SELECT group_number, position FROM entries WHERE id = $1`, id).Scan(&group, &position); err != nil {
		t.Fatal(err)
	}
	return group, position
}

func TestEntryDeletePostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewEntryRepository(pool)

	entry := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Alien"), 1)
	if err := repo.Delete(ctx, entry.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetByID(ctx, entry.ID); err != nil || got != nil {
		t.Fatalf("deleted entry still readable: %+v, err=%v", got, err)
	}
	if err := repo.Delete(ctx, entry.ID); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("expected ErrEntryNotFound deleting a missing entry, got %v", err)
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

	// Adding a movie that is already in the group changes nothing.
	if _, err := repo.Create(ctx, model.CreateEntryInput{MovieID: movie, GroupNumber: 1}); !errors.Is(err, ErrEntryExistsInGroup) {
		t.Fatalf("expected ErrEntryExistsInGroup, got %v", err)
	}

	// Only existing groups (1, 2) and the next new one (3) accept entries.
	for _, group := range []int{0, -1, 4, 1000} {
		if err := repo.CheckGroup(ctx, group); !errors.Is(err, ErrInvalidGroup) {
			t.Fatalf("CheckGroup(%d): expected ErrInvalidGroup, got %v", group, err)
		}
		if _, err := repo.Create(ctx, model.CreateEntryInput{MovieID: movie, GroupNumber: group}); !errors.Is(err, ErrInvalidGroup) {
			t.Fatalf("group %d: expected ErrInvalidGroup, got %v", group, err)
		}
	}
	for _, group := range []int{1, 2, 3} {
		if err := repo.CheckGroup(ctx, group); err != nil {
			t.Fatalf("CheckGroup(%d): %v", group, err)
		}
	}
	if entry := createTestEntry(t, ctx, repo, movie, 3); entry.GroupNumber != 3 || entry.Position != 1 {
		t.Fatalf("new group entry at group=%d position=%d, want 3/1", entry.GroupNumber, entry.Position)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM entries`).Scan(&count); err != nil || count != 5 {
		t.Fatalf("got %d entries (err %v), want 5", count, err)
	}
}

func TestEntryCreateInEmptyDatabasePostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewEntryRepository(pool)

	movie := insertTestMovie(t, ctx, pool, "First")
	if _, err := repo.Create(ctx, model.CreateEntryInput{MovieID: movie, GroupNumber: 2}); !errors.Is(err, ErrInvalidGroup) {
		t.Fatalf("group 2 in an empty database: expected ErrInvalidGroup, got %v", err)
	}
	if entry := createTestEntry(t, ctx, repo, movie, 1); entry.GroupNumber != 1 || entry.Position != 1 {
		t.Fatalf("first entry at group=%d position=%d, want 1/1", entry.GroupNumber, entry.Position)
	}
}

// TestMovieCreateReturnsExistingTMDBMoviePostgres covers two requests adding
// the same TMDB movie at once: both get the one stored movie.
func TestMovieCreateReturnsExistingTMDBMoviePostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	movies := NewMovieRepository(pool)

	tmdbID := 348
	first, err := movies.Create(ctx, model.CreateMovieInput{Title: "Alien", TMDBId: &tmdbID})
	if err != nil {
		t.Fatal(err)
	}
	second, err := movies.Create(ctx, model.CreateMovieInput{Title: "Alien (again)", TMDBId: &tmdbID})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Title != "Alien" {
		t.Fatalf("second create returned %+v, want the existing movie %s", second, first.ID)
	}

	// Concurrent creates all land on one row.
	otherID := 949
	const creators = 6
	ids := make(chan uuid.UUID, creators)
	errs := make(chan error, creators)
	var wg sync.WaitGroup
	for i := 0; i < creators; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			movie, err := movies.Create(ctx, model.CreateMovieInput{Title: "Heat", TMDBId: &otherID})
			if err != nil {
				errs <- err
				return
			}
			ids <- movie.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var want uuid.UUID
	for id := range ids {
		if want == uuid.Nil {
			want = id
		} else if id != want {
			t.Fatalf("concurrent creates returned %s and %s", want, id)
		}
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
	// Group 5 is the next new group.
	insertTestEntry(t, ctx, pool, insertTestMovie(t, ctx, pool, "Group 4"), 4)

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
