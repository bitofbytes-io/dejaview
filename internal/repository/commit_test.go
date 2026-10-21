package repository

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// commitFunc simulates the server accepting COMMIT before its response is lost.
type commitFunc func(context.Context) error

func (f commitFunc) Commit(ctx context.Context) error { return f(ctx) }

// TestCommitWriteLostResponse keeps committed writes out of the failure bucket,
// including cancellation after PostgreSQL accepted the transaction.
func TestCommitWriteLostResponse(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, io.ErrUnexpectedEOF} {
		t.Run(cause.Error(), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			committed := false
			err := commitWrite(ctx, commitFunc(func(got context.Context) error {
				if got != ctx {
					t.Fatal("commit context was replaced")
				}
				committed = true
				cancel()
				return cause
			}))
			if !committed || !errors.Is(err, ErrCommitOutcomeUnknown) || !errors.Is(err, cause) {
				t.Fatalf("committed=%v err=%v", committed, err)
			}
		})
	}
}

// TestCommitWriteClassification distinguishes authoritative rollback responses
// from arbitrary transport errors and PostgreSQL's explicit uncertainty codes.
func TestCommitWriteClassification(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cause   error
		unknown bool
	}{
		{name: "confirmed success"},
		{name: "confirmed rollback", cause: pgx.ErrTxCommitRollback},
		{name: "deferred constraint", cause: &pgconn.PgError{Code: "23505"}},
		{name: "serialization rollback", cause: &pgconn.PgError{Code: "40001"}},
		{name: "statement completion unknown", cause: &pgconn.PgError{Code: "40003"}, unknown: true},
		{name: "transaction resolution unknown", cause: &pgconn.PgError{Code: "08007"}, unknown: true},
		{name: "connection shutdown", cause: &pgconn.PgError{Code: "57P01"}, unknown: true},
		{name: "closed transaction", cause: pgx.ErrTxClosed, unknown: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := commitWrite(context.Background(), commitFunc(func(context.Context) error { return tc.cause }))
			if errors.Is(err, ErrCommitOutcomeUnknown) != tc.unknown || !errors.Is(err, tc.cause) {
				t.Fatalf("err=%v unknown=%v", err, tc.unknown)
			}
		})
	}
}

// TestCommitWritePostgres exercises actual pgx responses for success, a deferred
// commit-time constraint failure, and cancellation before COMMIT can be sent.
func TestCommitWritePostgres(t *testing.T) {
	pool := ratingTestPool(t)
	ctx := context.Background()
	// Hold one connection so the temporary table remains available.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `CREATE TEMP TABLE commit_fixture (id int UNIQUE DEFERRABLE INITIALLY DEFERRED)`); err != nil {
		t.Fatal(err)
	}
	if err = commitWrite(ctx, tx); err != nil {
		t.Fatal(err)
	}
	tx, err = conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO commit_fixture VALUES (1),(1)`); err != nil {
		t.Fatal(err)
	}
	err = commitWrite(ctx, tx)
	var pgErr *pgconn.PgError
	if errors.Is(err, ErrCommitOutcomeUnknown) || !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("deferred rejection: %v", err)
	}
	// Simulate a lost acknowledgement after a real PostgreSQL commit. The row
	// persists even though the caller receives an error and records unknown.
	tx, err = conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO commit_fixture VALUES (2)`); err != nil {
		t.Fatal(err)
	}
	err = commitWrite(ctx, commitFunc(func(ctx context.Context) error {
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return io.ErrUnexpectedEOF
	}))
	if !errors.Is(err, ErrCommitOutcomeUnknown) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("lost acknowledgement: %v", err)
	}
	var count int
	if err = conn.QueryRow(ctx, `SELECT COUNT(*) FROM commit_fixture WHERE id = 2`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("persisted rows=%d err=%v", count, err)
	}
	tx, err = conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	err = commitWrite(canceled, tx)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrCommitOutcomeUnknown) || !pgconn.SafeToRetry(err) {
		t.Fatalf("pre-send cancellation: %v", err)
	}
}
