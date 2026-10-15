-- +goose Up
-- +goose StatementBegin
ALTER TABLE persons ADD COLUMN color TEXT NOT NULL DEFAULT 'cyan';

-- Keep the colours the seeded family already had; give anyone else a palette
-- colour by initial order.
UPDATE persons p
SET color = CASE p.initial
    WHEN 'D' THEN 'cyan'
    WHEN 'J' THEN 'rose'
    WHEN 'C' THEN 'amber'
    WHEN 'A' THEN 'violet'
    ELSE (ARRAY['cyan', 'rose', 'amber', 'mint', 'violet'])[1 + (n.rn - 1) % 5]
END
FROM (SELECT id, ROW_NUMBER() OVER (ORDER BY initial) AS rn FROM persons) n
WHERE n.id = p.id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE persons DROP COLUMN IF EXISTS color;
-- +goose StatementEnd
