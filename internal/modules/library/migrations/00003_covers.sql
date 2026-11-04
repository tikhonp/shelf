-- +goose Up

-- A change's rows also hold covers: each audio file's front cover before
-- and after, and the folder's cover files, by the SHA-256 of their bytes
-- in the cover store under /data. An empty hash is no cover.
ALTER TABLE library_change_files ADD COLUMN kind TEXT NOT NULL DEFAULT 'audio' CHECK (kind IN ('audio', 'cover'));
ALTER TABLE library_change_files ADD COLUMN cover_before TEXT NOT NULL DEFAULT '';
ALTER TABLE library_change_files ADD COLUMN cover_after TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE library_change_files DROP COLUMN cover_after;
ALTER TABLE library_change_files DROP COLUMN cover_before;
ALTER TABLE library_change_files DROP COLUMN kind;
