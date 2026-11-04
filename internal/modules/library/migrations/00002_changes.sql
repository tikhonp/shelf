-- +goose Up

-- Every tag write: a Save, or the undo or redo of one. A row holds the
-- write's request until its job runs, and each file's tags before and
-- after it. History outlives the index, so nothing here points into it.
CREATE TABLE library_changes (
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
    request     TEXT NOT NULL DEFAULT '{}',        -- JSON: the edits as asked
    conflicts   TEXT NOT NULL DEFAULT '[]',        -- JSON: what an undo or redo left alone
    tags        INTEGER NOT NULL DEFAULT 0,        -- keys changed, over all files
    job_id      INTEGER,                           -- a platform table, so no foreign key
    created_at  TEXT NOT NULL,
    finished_at TEXT
) STRICT;
CREATE INDEX library_changes_state ON library_changes (state);
CREATE INDEX library_changes_target ON library_changes (target);

CREATE TABLE library_change_files (
    change_id INTEGER NOT NULL REFERENCES library_changes (id) ON DELETE CASCADE,
    path      TEXT NOT NULL,                   -- inside the music folder, with slashes
    before    TEXT NOT NULL,                   -- JSON: every key and its values
    after     TEXT NOT NULL,                   -- JSON: as planned, then as read back
    revision  TEXT NOT NULL DEFAULT '',        -- after the write
    written   INTEGER NOT NULL DEFAULT 0 CHECK (written IN (0, 1)),
    PRIMARY KEY (change_id, path)
) STRICT;

-- +goose Down
DROP TABLE library_change_files;
DROP TABLE library_changes;
