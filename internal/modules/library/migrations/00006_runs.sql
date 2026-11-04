-- +goose Up

-- A bulk run is one change at the head, which the history lists, with a child
-- change per album. An undo of a run is the same shape, so the rows here
-- serve both.
ALTER TABLE library_changes ADD COLUMN parent INTEGER REFERENCES library_changes (id) ON DELETE CASCADE;
CREATE INDEX library_changes_parent ON library_changes (parent);

CREATE TABLE library_runs (
    change_id    INTEGER PRIMARY KEY REFERENCES library_changes (id) ON DELETE CASCADE,
    fixes        TEXT NOT NULL,                     -- JSON: the kinds chosen, and 'replaygain'
    cancelled    INTEGER NOT NULL DEFAULT 0 CHECK (cancelled IN (0, 1)),
    analysis_job INTEGER                            -- a platform table, so no foreign key
) STRICT;

-- The selection, which outgrows a job's payload, and how each album went.
CREATE TABLE library_run_albums (
    run         INTEGER NOT NULL REFERENCES library_changes (id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    album_id    INTEGER NOT NULL,                  -- the index's row when the run started
    target      INTEGER REFERENCES library_changes (id) ON DELETE SET NULL, -- the album's save an undo or redo works on
    change_id   INTEGER REFERENCES library_changes (id) ON DELETE SET NULL, -- this album's write
    title       TEXT NOT NULL,
    artist      TEXT NOT NULL,
    state       TEXT NOT NULL CHECK (state IN ('waiting', 'analyzing', 'ready', 'writing', 'done', 'skipped', 'failed', 'cancelled')),
    gains       TEXT NOT NULL DEFAULT '',          -- JSON: what the analysis measured
    fixes       INTEGER NOT NULL DEFAULT 0,
    files       INTEGER NOT NULL DEFAULT 0,
    message     TEXT NOT NULL DEFAULT '' CHECK (length(message) <= 1000),
    finished_at TEXT,
    PRIMARY KEY (run, position)
) STRICT;
CREATE INDEX library_run_albums_state ON library_run_albums (run, state);

-- What ffmpeg measured, by the hash of the audio. A retag keeps the hash, so a
-- run cut off or asked for again doesn't decode the same audio twice.
CREATE TABLE library_gains (
    audio       TEXT PRIMARY KEY,
    peak        REAL NOT NULL,
    blocks      TEXT NOT NULL,                     -- JSON: the loudness histogram
    measured_at TEXT NOT NULL
) STRICT;

-- +goose Down
DROP TABLE library_gains;
DROP TABLE library_run_albums;
DROP TABLE library_runs;
DROP INDEX library_changes_parent;
ALTER TABLE library_changes DROP COLUMN parent;
