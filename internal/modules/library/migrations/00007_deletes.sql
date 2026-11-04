-- +goose NO TRANSACTION
-- +goose Up

-- A delete of a release folder is a change too, so the history shows it,
-- though nothing undoes it. SQLite can't change a CHECK in place, so the
-- table is built again. Foreign keys stay off while it is, or dropping the
-- old table would delete every change's files, runs and undos with it. One
-- write connection runs every statement, so the pragma covers them all. The
-- sequence carries over, so no ID of a cleared change comes back.
PRAGMA foreign_keys = OFF;
BEGIN;
CREATE TABLE library_changes_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    kind        TEXT NOT NULL CHECK (kind IN ('save', 'undo', 'redo', 'delete')),
    target      INTEGER REFERENCES library_changes (id) ON DELETE CASCADE, -- the save an undo or redo works on
    album_id    INTEGER NOT NULL,                  -- the index's album row when the change was asked for
    folder      TEXT NOT NULL,                     -- the album's release folder
    title       TEXT NOT NULL,                     -- the album and its artist when the change was asked for
    artist      TEXT NOT NULL,
    state       TEXT NOT NULL CHECK (state IN ('queued', 'writing', 'done', 'partial', 'failed')),
    reason      TEXT NOT NULL DEFAULT '',          -- why it failed: 'conflict', 'locked', 'busy', 'refused', 'verify', 'interrupted', 'error'
    error       TEXT NOT NULL DEFAULT '' CHECK (length(error) <= 4100),
    undone      INTEGER NOT NULL DEFAULT 0 CHECK (undone IN (0, 1)), -- a save whose undo is the latest that ran
    request     TEXT NOT NULL DEFAULT '{}',        -- JSON: the edits as asked, or what a delete removed
    conflicts   TEXT NOT NULL DEFAULT '[]',        -- JSON: what an undo or redo left alone
    tags        INTEGER NOT NULL DEFAULT 0,        -- keys changed, over all files
    job_id      INTEGER,                           -- a platform table, so no foreign key
    created_at  TEXT NOT NULL,
    finished_at TEXT,
    parent      INTEGER REFERENCES library_changes (id) ON DELETE CASCADE
) STRICT;
INSERT INTO library_changes_new (id, kind, target, album_id, folder, title, artist, state, reason, error, undone, request, conflicts, tags, job_id, created_at, finished_at, parent)
    SELECT id, kind, target, album_id, folder, title, artist, state, reason, error, undone, request, conflicts, tags, job_id, created_at, finished_at, parent FROM library_changes;
DELETE FROM sqlite_sequence WHERE name = 'library_changes_new';
INSERT INTO sqlite_sequence (name, seq) SELECT 'library_changes_new', seq FROM sqlite_sequence WHERE name = 'library_changes';
DROP TABLE library_changes;
ALTER TABLE library_changes_new RENAME TO library_changes;
CREATE INDEX library_changes_state ON library_changes (state);
CREATE INDEX library_changes_target ON library_changes (target);
CREATE INDEX library_changes_parent ON library_changes (parent);
COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
PRAGMA foreign_keys = OFF;
BEGIN;
DELETE FROM library_changes WHERE kind = 'delete';
CREATE TABLE library_changes_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    kind        TEXT NOT NULL CHECK (kind IN ('save', 'undo', 'redo')),
    target      INTEGER REFERENCES library_changes (id) ON DELETE CASCADE, -- the save an undo or redo works on
    album_id    INTEGER NOT NULL,                  -- the index's album row when the change was asked for
    folder      TEXT NOT NULL,                     -- the album's release folder
    title       TEXT NOT NULL,                     -- the album and its artist when the change was asked for
    artist      TEXT NOT NULL,
    state       TEXT NOT NULL CHECK (state IN ('queued', 'writing', 'done', 'partial', 'failed')),
    reason      TEXT NOT NULL DEFAULT '',          -- why it failed: 'conflict', 'locked', 'busy', 'refused', 'verify', 'interrupted', 'error'
    error       TEXT NOT NULL DEFAULT '' CHECK (length(error) <= 4100),
    undone      INTEGER NOT NULL DEFAULT 0 CHECK (undone IN (0, 1)), -- a save whose undo is the latest that ran
    request     TEXT NOT NULL DEFAULT '{}',        -- JSON: the edits as asked, or what a delete removed
    conflicts   TEXT NOT NULL DEFAULT '[]',        -- JSON: what an undo or redo left alone
    tags        INTEGER NOT NULL DEFAULT 0,        -- keys changed, over all files
    job_id      INTEGER,                           -- a platform table, so no foreign key
    created_at  TEXT NOT NULL,
    finished_at TEXT,
    parent      INTEGER REFERENCES library_changes (id) ON DELETE CASCADE
) STRICT;
INSERT INTO library_changes_new (id, kind, target, album_id, folder, title, artist, state, reason, error, undone, request, conflicts, tags, job_id, created_at, finished_at, parent)
    SELECT id, kind, target, album_id, folder, title, artist, state, reason, error, undone, request, conflicts, tags, job_id, created_at, finished_at, parent FROM library_changes;
DELETE FROM sqlite_sequence WHERE name = 'library_changes_new';
INSERT INTO sqlite_sequence (name, seq) SELECT 'library_changes_new', seq FROM sqlite_sequence WHERE name = 'library_changes';
DROP TABLE library_changes;
ALTER TABLE library_changes_new RENAME TO library_changes;
CREATE INDEX library_changes_state ON library_changes (state);
CREATE INDEX library_changes_target ON library_changes (target);
CREATE INDEX library_changes_parent ON library_changes (parent);
COMMIT;
PRAGMA foreign_keys = ON;
