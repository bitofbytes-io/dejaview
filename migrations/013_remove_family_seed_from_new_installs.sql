-- +goose Up
-- Migration 003 seeds one family's four people into every new database.
-- Released migrations cannot change, so remove that seed here, but only while
-- creating a database: goose applied 003 within the last minute, in this same
-- run, so nobody can have rated or picked a movie yet. As a second check, the
-- database may hold no ratings or picks at all. Databases migrated earlier,
-- including the original install, keep their people, as does SQL applied
-- without goose_db_version. The README shows how to add people.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('goose_db_version') IS NULL THEN
        RETURN;
    END IF;
    IF NOT COALESCE((
        SELECT MIN(tstamp) > LOCALTIMESTAMP - INTERVAL '1 minute'
        FROM goose_db_version
        WHERE version_id = 3 AND is_applied
    ), false) THEN
        RETURN;
    END IF;

    IF EXISTS (SELECT 1 FROM ratings)
        OR EXISTS (SELECT 1 FROM entries WHERE picked_by_person_id IS NOT NULL) THEN
        RETURN;
    END IF;

    DELETE FROM persons
    WHERE (initial, name) IN (('D', 'Daniel'), ('J', 'Jennifer'), ('C', 'Caleb'), ('A', 'Aiden'));
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Removing the seed is not undone.
