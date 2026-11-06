-- +goose Up
ALTER TABLE library_changes ADD COLUMN author_id TEXT;
ALTER TABLE library_changes ADD COLUMN author TEXT NOT NULL DEFAULT '';
-- +goose Down
ALTER TABLE library_changes DROP COLUMN author;
ALTER TABLE library_changes DROP COLUMN author_id;
