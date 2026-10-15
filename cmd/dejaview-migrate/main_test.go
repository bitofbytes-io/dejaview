package main

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Applies every migration to an empty schema, twice, the way the deploy hook
// runs dejaview-migrate.
func TestRunAppliesMigrations(t *testing.T) {
	databaseURL := os.Getenv("DEJAVIEW_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DEJAVIEW_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	schema := "migrate_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()

	for range 2 {
		if err := run(ctx, parsed.String()); err != nil {
			t.Fatal(err)
		}
	}

	var version int64
	if err := admin.QueryRow(ctx, "SELECT max(version_id) FROM "+identifier+".goose_db_version WHERE is_applied").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < 13 {
		t.Fatalf("goose version = %d, want at least 13", version)
	}
	var hasColor bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = 'persons' AND column_name = 'color')`, schema).Scan(&hasColor); err != nil {
		t.Fatal(err)
	}
	if !hasColor {
		t.Fatal("persons.color is missing after migrations")
	}
}

func TestRunRequiresDatabaseURL(t *testing.T) {
	if err := run(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("run error = %v, want DATABASE_URL error", err)
	}
}
