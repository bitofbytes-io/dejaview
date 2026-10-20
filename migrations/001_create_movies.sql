-- +goose Up
-- +goose StatementBegin
CREATE TABLE movies (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    title           TEXT NOT NULL,
    release_year    INTEGER,
    poster_url      TEXT,
    synopsis        TEXT,
    runtime_minutes INTEGER,
    tmdb_id         INTEGER UNIQUE,
    imdb_id         TEXT,
    metadata_json   JSONB
);

-- Index for TMDB lookups
CREATE INDEX idx_movies_tmdb_id ON movies(tmdb_id) WHERE tmdb_id IS NOT NULL;

-- Index for title searches
CREATE INDEX idx_movies_title ON movies(title);

-- Trigger to auto-update updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_movies_updated_at
    BEFORE UPDATE ON movies
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS update_movies_updated_at ON movies;
DO $$
DECLARE
    func_oid oid;
BEGIN
    func_oid := to_regprocedure('update_updated_at_column()');
    IF func_oid IS NOT NULL THEN
        IF NOT EXISTS (
            SELECT 1
            FROM pg_trigger
            WHERE tgfoid = func_oid
              AND NOT tgisinternal
        ) THEN
            DROP FUNCTION update_updated_at_column();
        END IF;
    END IF;
END $$;
DROP TABLE IF EXISTS movies;
-- +goose StatementEnd

