-- +goose Up
-- Migration 003 seeds one family's four people into every new database.
-- Released migrations cannot change, so remove that seed here wherever it
-- was never used: the database holds no ratings and no picks at all, so no
-- data refers to anyone yet. The original install, which has ratings, keeps
-- its people, as does SQL applied without goose_db_version (as the
-- repository tests do). The README shows how to add people, and the down
-- migration puts the seed back.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('goose_db_version') IS NULL THEN
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
-- +goose StatementBegin
INSERT INTO persons (initial, name) VALUES
    ('D', 'Daniel'),
    ('J', 'Jennifer'),
    ('C', 'Caleb'),
    ('A', 'Aiden')
ON CONFLICT (initial) DO NOTHING;
-- +goose StatementEnd
