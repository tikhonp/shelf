-- +goose Up

-- Where each album came from, which the walk works out from the downloads'
-- records, shelf.txt and the source folders. Rows stay empty until the
-- walk after the start fills them.
ALTER TABLE library_albums ADD COLUMN source TEXT NOT NULL DEFAULT '';        -- a source's ID; empty when nothing tells
ALTER TABLE library_albums ADD COLUMN source_info TEXT NOT NULL DEFAULT '{}'; -- JSON: what the list and the editor show of it
CREATE INDEX library_albums_source ON library_albums (source);

-- +goose Down
DROP INDEX library_albums_source;
ALTER TABLE library_albums DROP COLUMN source_info;
ALTER TABLE library_albums DROP COLUMN source;
