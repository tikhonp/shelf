-- +goose Up

-- An audio file's front cover description before and after a change, so
-- an undo that puts a removed cover back gives it its description too.
ALTER TABLE library_change_files ADD COLUMN cover_before_description TEXT NOT NULL DEFAULT '';
ALTER TABLE library_change_files ADD COLUMN cover_after_description TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE library_change_files DROP COLUMN cover_after_description;
ALTER TABLE library_change_files DROP COLUMN cover_before_description;
