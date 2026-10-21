package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrCommitOutcomeUnknown means COMMIT may have reached PostgreSQL, but its
// outcome could not be confirmed. Callers must not treat it as a known rollback.
// The returned error also wraps the original cause for cancellation handling.
var ErrCommitOutcomeUnknown = errors.New("commit outcome unknown")

// transactionCommitter is the commit boundary shared by entry and rating writes.
type transactionCommitter interface {
	Commit(context.Context) error
}

// commitWrite preserves request cancellation and distinguishes confirmed
// rejection from a lost COMMIT response. A subsequent Rollback cannot resolve
// uncertainty: pgx closes the transaction when Commit returns an error.
func commitWrite(ctx context.Context, tx transactionCommitter) error {
	err := tx.Commit(ctx)
	if err == nil || pgconn.SafeToRetry(err) || errors.Is(err, pgx.ErrTxCommitRollback) {
		return err
	}
	// Integrity-constraint and transaction-rollback errors confirm rejection.
	// PostgreSQL's statement_completion_unknown (40003) explicitly does not.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (strings.HasPrefix(pgErr.Code, "23") ||
		(strings.HasPrefix(pgErr.Code, "40") && pgErr.Code != "40003")) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrCommitOutcomeUnknown, err)
}
