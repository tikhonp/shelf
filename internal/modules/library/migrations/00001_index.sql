-- +goose Up

-- The library index: what shelf last read of each audio file, and the albums
-- they form. Nothing here is a copy of record; a walk can rebuild all of it.
CREATE TABLE library_albums (
    id        INTEGER PRIMARY KEY AUTOINCREMENT, -- never reused, so an old link can't open another album
    folder    TEXT NOT NULL,                     -- the release folder inside the music folder
    nd_id     TEXT NOT NULL,                     -- the album's ID in Navidrome
    title     TEXT NOT NULL,
    artist    TEXT NOT NULL,
    year      INTEGER NOT NULL,
    tracks    INTEGER NOT NULL,
    quality   TEXT NOT NULL,                     -- the most common track quality, such as 'FLAC 24/96'
    length_ms INTEGER NOT NULL,
    size      INTEGER NOT NULL,
    added_at  TEXT NOT NULL,
    has_mbid  INTEGER NOT NULL CHECK (has_mbid IN (0, 1)),
    lamp      INTEGER NOT NULL CHECK (lamp IN (0, 1)), -- an issue that isn't style only
    issues    TEXT NOT NULL DEFAULT '[]',        -- JSON: [{kind, fields}]
    kinds     TEXT NOT NULL DEFAULT ',',         -- ',no_cover,split_album,' for the filter
    search    TEXT NOT NULL,                     -- folded title, artist and folder
    UNIQUE (folder, nd_id)
) STRICT;
CREATE INDEX library_albums_title ON library_albums (title COLLATE NOCASE);

CREATE TABLE library_files (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    path       TEXT NOT NULL UNIQUE,             -- inside the music folder, with slashes
    album_id   INTEGER REFERENCES library_albums (id) ON DELETE SET NULL,
    size       INTEGER NOT NULL,
    mtime_ns   INTEGER NOT NULL,
    revision   TEXT NOT NULL DEFAULT '',
    rules      TEXT NOT NULL DEFAULT '',         -- the Navidrome rules the facts were read under
    version    INTEGER NOT NULL DEFAULT 0,       -- the code's facts version
    error      TEXT NOT NULL DEFAULT '' CHECK (length(error) <= 1000),
    first_seen TEXT NOT NULL,
    facts      TEXT NOT NULL DEFAULT '{}'        -- JSON: what the checks and the list need
) STRICT;
CREATE INDEX library_files_album ON library_files (album_id);

-- Navidrome's tag settings as last read, for a start when it doesn't answer.
CREATE TABLE library_state (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) STRICT;

-- +goose Down
DROP TABLE library_state;
DROP TABLE library_files;
DROP TABLE library_albums;
