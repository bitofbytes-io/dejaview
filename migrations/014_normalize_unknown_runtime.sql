-- +goose Up
-- TMDB uses zero for an unknown runtime. Preserve that meaning for old rows.
UPDATE movies SET runtime_minutes = NULL WHERE runtime_minutes = 0;

-- +goose Down
-- Unknown runtimes cannot be distinguished from previously NULL values.
-- Retain the normalized data when rolling back the application.
SELECT 1;
