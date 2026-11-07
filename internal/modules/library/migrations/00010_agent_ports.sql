-- +goose Up
-- The proposal acceptance a change came from: "proposal:<p>/acceptance:<a>".
-- Ordinary changes have none, and one acceptance makes at most one change.
ALTER TABLE library_changes ADD COLUMN origin TEXT;
CREATE UNIQUE INDEX library_changes_origin ON library_changes (origin) WHERE origin IS NOT NULL;
-- Covers another module still needs, by holder such as "agents:proposal:12".
-- Retention keeps them until the holder lets go.
CREATE TABLE library_cover_pins (
    holder     TEXT NOT NULL,
    cover      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (holder, cover)
) STRICT;
-- +goose Down
DROP TABLE library_cover_pins;
DROP INDEX library_changes_origin;
ALTER TABLE library_changes DROP COLUMN origin;
