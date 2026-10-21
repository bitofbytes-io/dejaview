-- +goose Up
-- Keep the historical first group's capacity separate from its display number.
-- A generation change invalidates numeric selections when a group disappears.
CREATE TABLE entry_group_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    version bigint NOT NULL DEFAULT 0 CHECK (version >= 0),
    first_group_capacity integer NOT NULL DEFAULT 8 CHECK (first_group_capacity IN (8, 9))
);
-- Older releases allowed gaps. If later groups exist but group 1 is absent,
-- the historical eight-pick group has already disappeared. A fresh database
-- still starts with the historical capacity for its first group.
INSERT INTO entry_group_state (singleton, first_group_capacity)
SELECT true, CASE
    WHEN NOT EXISTS (SELECT 1 FROM entries WHERE group_number = 1)
         AND EXISTS (SELECT 1 FROM entries WHERE group_number > 1)
    THEN 9 ELSE 8
END;

-- +goose Down
DROP TABLE entry_group_state;
