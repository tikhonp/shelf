-- +goose Up

-- Answers from MusicBrainz, the Cover Art Archive and LRCLIB, by request,
-- so an album opened again costs no request. Only found (200) and not
-- found (404) answers are kept.
CREATE TABLE library_lookups (
    url        TEXT PRIMARY KEY,
    status     INTEGER NOT NULL CHECK (status IN (200, 404)),
    body       TEXT NOT NULL DEFAULT '',
    fetched_at TEXT NOT NULL
) STRICT;
CREATE INDEX library_lookups_fetched ON library_lookups (fetched_at);

-- The remote images a suggestion offered. The editor names them by ID, so
-- shelf fetches only images it found itself.
CREATE TABLE library_remote (
    id         TEXT PRIMARY KEY,               -- the first 32 hex digits of the image URL's SHA-256
    url        TEXT NOT NULL,                  -- the full image
    preview    TEXT NOT NULL,                  -- a smaller copy, or the same URL
    created_at TEXT NOT NULL
) STRICT;
CREATE INDEX library_remote_created ON library_remote (created_at);

-- +goose Down
DROP TABLE library_remote;
DROP TABLE library_lookups;
