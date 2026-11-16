-- +goose Up

-- Each album's release types, lower case, as ",album,soundtrack,", so the
-- list can filter by one. Rows stay "," until the walk after the start
-- fills them.
ALTER TABLE library_albums ADD COLUMN types TEXT NOT NULL DEFAULT ',';

-- +goose Down
ALTER TABLE library_albums DROP COLUMN types;
