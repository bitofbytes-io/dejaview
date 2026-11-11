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
