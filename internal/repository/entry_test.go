package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

	// Moving past the next new group would leave a gap, so it is rejected.
	for _, invalid := range []int{0, 4} {
		if err := repo.Update(ctx, a.ID, model.UpdateEntryInput{GroupNumber: &invalid}); !errors.Is(err, ErrInvalidGroup) {
			t.Fatalf("move to group %d: expected ErrInvalidGroup, got %v", invalid, err)
		}
	}
	if group, position := entryGroupAndPosition(t, ctx, pool, a.ID); group != 1 || position != 1 {
		t.Fatalf("rejected move changed entry to group=%d position=%d", group, position)
	}

	// An unknown picker is rejected and rolls back the move with it.
	next := 3
	unknown := uuid.New()
	if err := repo.Update(ctx, a.ID, model.UpdateEntryInput{GroupNumber: &next, PickedByPersonID: &unknown}); !errors.Is(err, ErrPersonNotFound) {
		t.Fatalf("expected ErrPersonNotFound, got %v", err)
	}
	if group, position := entryGroupAndPosition(t, ctx, pool, a.ID); group != 1 || position != 1 {
		t.Fatalf("unknown picker left entry at group=%d position=%d", group, position)
	}

	// Moving to the next new group starts at position 1 and still applies picked_by.
	var picker uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM persons ORDER BY initial LIMIT 1`).Scan(&picker); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, a.ID, model.UpdateEntryInput{GroupNumber: &next, PickedByPersonID: &picker}); err != nil {
		t.Fatal(err)
	}
	moved, err := repo.GetByID(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if moved.GroupNumber != 3 || moved.Position != 1 || moved.PickedByPersonID == nil || *moved.PickedByPersonID != picker {
		t.Fatalf("unexpected moved entry: group=%d position=%d picked_by=%v", moved.GroupNumber, moved.Position, moved.PickedByPersonID)
	}

	// The moved entry does not count toward the next new group, so the only
	// entry of the last group cannot skip ahead and leave its group empty.
	skip := 4
	if err := repo.Update(ctx, a.ID, model.UpdateEntryInput{GroupNumber: &skip}); !errors.Is(err, ErrInvalidGroup) {
		t.Fatalf("expected ErrInvalidGroup moving the last group's only entry ahead, got %v", err)
	}

	if err := repo.Update(ctx, uuid.New(), model.UpdateEntryInput{GroupNumber: &target}); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("expected ErrEntryNotFound, got %v", err)
	}
	if err := repo.Update(ctx, uuid.New(), model.UpdateEntryInput{PickedByPersonID: &picker}); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("expected ErrEntryNotFound without a group change, got %v", err)
	}
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

func TestEntryUpdateConcurrentMovesPostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewEntryRepository(pool)

	insertTestEntry(t, ctx, pool, insertTestMovie(t, ctx, pool, "Target resident"), 9)
	const movers = 8
	ids := make([]uuid.UUID, movers)
	for i := range ids {
		// Each mover starts at position 1 of its own group, colliding with the target's position 1.
		insertTestEntry(t, ctx, pool, insertTestMovie(t, ctx, pool, "Source resident"), i+1)
		ids[i] = createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Mover"), i+1).ID
	}

	target := 9
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
	if err := pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(DISTINCT position), MAX(position) FROM entries WHERE group_number = 9`).Scan(&count, &distinct, &maxPosition); err != nil {
		t.Fatal(err)
	}
	if count != movers+1 || distinct != count || maxPosition != count {
		t.Fatalf("group 9 has count=%d distinct positions=%d max=%d", count, distinct, maxPosition)
	}
}

// waitForBlockedMove waits until this test schema's pending writer is blocked.
// Each test uses a unique application_name so concurrent test processes cannot
// make another schema's lock wait satisfy this check.
func waitForBlockedMove(t *testing.T, ctx context.Context, pool *pgxpool.Pool, moved <-chan error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_locks l
				JOIN pg_stat_activity a ON a.pid = l.pid
				WHERE NOT l.granted AND a.datname = current_database()
				  AND a.application_name = current_setting('application_name')
			)`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case err := <-moved:
			t.Fatalf("move finished before the competing move committed: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("move never blocked behind the competing move")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// competingMove moves id to group in an open transaction that holds the group
// locks and the row lock until the caller commits it.
func competingMove(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, group int) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if err := moveEntryToGroup(ctx, tx, id, group); err != nil {
		t.Fatalf("competing move: %v", err)
	}
	return tx
}

func assertContiguousGroups(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want map[int]int) {
	t.Helper()
	for group, wantCount := range want {
		var count, distinct, minPosition, maxPosition int
		if err := pool.QueryRow(ctx, `
			SELECT COUNT(*), COUNT(DISTINCT position), MIN(position), MAX(position)
			FROM entries WHERE group_number = $1`, group).Scan(&count, &distinct, &minPosition, &maxPosition); err != nil {
			t.Fatal(err)
		}
		if count != wantCount || distinct != count || minPosition != 1 || maxPosition != count {
			t.Fatalf("group %d has count=%d distinct=%d positions %d..%d, want %d contiguous from 1",
				group, count, distinct, minPosition, maxPosition, wantCount)
		}
	}
}

// TestEntryUpdateSerializesCompetingMovePostgres verifies a second move waits
// for the global lock before reading the entry's group, then uses the new group.
func TestEntryUpdateSerializesCompetingMovePostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewEntryRepository(pool)

	// Each test uses its own schema; keep the fixture groups contiguous.
	const source, competing, target = 1, 2, 3
	for _, group := range []int{source, competing, target} {
		insertTestEntry(t, ctx, pool, insertTestMovie(t, ctx, pool, "Resident"), group)
		insertTestEntry(t, ctx, pool, insertTestMovie(t, ctx, pool, "Resident"), group)
	}
	// The contested entry is last in its group, so moving it out leaves no gap.
	contested := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Contested"), source)

	// The competing move holds the source and competing group locks, uncommitted.
	competingTx := competingMove(t, ctx, pool, contested.ID, competing)

	targetGroup := target
	moved := make(chan error, 1)
	go func() {
		moved <- repo.Update(ctx, contested.ID, model.UpdateEntryInput{GroupNumber: &targetGroup})
	}()

	// The second move must wait before reading any group numbers.
	waitForBlockedMove(t, ctx, pool, moved)

	if err := competingTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-moved; err != nil {
		t.Fatalf("move after competing move: %v", err)
	}

	var groups int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM entries WHERE id = $1`, contested.ID).Scan(&groups); err != nil || groups != 1 {
		t.Fatalf("contested entry appears %d times, err=%v", groups, err)
	}
	if group, position := entryGroupAndPosition(t, ctx, pool, contested.ID); group != target || position != 3 {
		t.Fatalf("contested entry at group=%d position=%d, want group=%d position=3", group, position, target)
	}
	assertContiguousGroups(t, ctx, pool, map[int]int{source: 2, competing: 2, target: 3})
}

// TestEntryUpdateSameGroupRechecksUnderLockPostgres covers a request that names
// the entry's current group while a competing move to another group is still
// uncommitted: the request must not report success with the entry left in the
// other group, so it rechecks under the lock and moves the entry back.
func TestEntryUpdateSameGroupRechecksUnderLockPostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := NewEntryRepository(pool)

	// Each test uses its own schema; keep the fixture groups contiguous.
	const home, competing = 1, 2
	for _, group := range []int{home, competing} {
		insertTestEntry(t, ctx, pool, insertTestMovie(t, ctx, pool, "Resident"), group)
		insertTestEntry(t, ctx, pool, insertTestMovie(t, ctx, pool, "Resident"), group)
	}
	contested := createTestEntry(t, ctx, repo, insertTestMovie(t, ctx, pool, "Contested"), home)
	var picker uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM persons ORDER BY initial LIMIT 1`).Scan(&picker); err != nil {
		t.Fatal(err)
	}

	competingTx := competingMove(t, ctx, pool, contested.ID, competing)

	homeGroup := home
	moved := make(chan error, 1)
	go func() {
		moved <- repo.Update(ctx, contested.ID, model.UpdateEntryInput{GroupNumber: &homeGroup, PickedByPersonID: &picker})
	}()

	// The request blocks behind the competing move before reading its group.
	waitForBlockedMove(t, ctx, pool, moved)

	if err := competingTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-moved; err != nil {
		t.Fatalf("same-group update after competing move: %v", err)
	}

	entry, err := repo.GetByID(ctx, contested.ID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.GroupNumber != home || entry.Position != 3 || entry.PickedByPersonID == nil || *entry.PickedByPersonID != picker {
		t.Fatalf("contested entry at group=%d position=%d picked_by=%v, want group=%d position=3 picked_by=%v",
			entry.GroupNumber, entry.Position, entry.PickedByPersonID, home, picker)
	}
	assertContiguousGroups(t, ctx, pool, map[int]int{home: 3, competing: 2})
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
