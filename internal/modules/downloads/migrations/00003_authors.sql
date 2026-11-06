-- +goose Up
ALTER TABLE downloads ADD COLUMN author_id TEXT;
ALTER TABLE downloads ADD COLUMN author TEXT NOT NULL DEFAULT '';
-- +goose Down
ALTER TABLE downloads DROP COLUMN author;
ALTER TABLE downloads DROP COLUMN author_id;
