-- +goose Up
-- What an older tool's history knows of an item, for a skipped download with
-- no earlier download to read it from.
ALTER TABLE downloaded_items ADD COLUMN title TEXT NOT NULL DEFAULT '';
ALTER TABLE downloaded_items ADD COLUMN artist TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE downloaded_items DROP COLUMN artist;
ALTER TABLE downloaded_items DROP COLUMN title;
