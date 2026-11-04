# Data model

One SQLite file, `/data/shelf.db`, in WAL mode with foreign keys on. Each module has its own goose version table, `<module>_goose_db_version`. Tables are STRICT. Times are UTC text in the form `2006-01-02T15:04:05.000Z`.

## Platform

`internal/platform/migrations/00001_jobs.sql`, proxier's jobs tables trimmed:

- `jobs`: one row per job, with its queue, type, resource and coalescing keys, a state of `queued`, `running`, `succeeded`, `failed`, `cancelled` or `interrupted`, payload JSON, attempt counters, lease, timestamps, error and log counters.
- `job_steps`: each job's named steps and their state.
- `job_log_lines`: log lines per job. The row ID is the event ID of the log stream. A job keeps its first and last 5,000 lines.
- `schedules`: run-time state of scheduled jobs: `platform.backup` at 03:30 and `platform.retention` at 05:00.

The retention job deletes failed jobs after 90 days and other finished jobs after 30, with their steps and log lines. A download keeps its row; its log goes with its job.

## Downloads module

```sql
CREATE TABLE downloads (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id     INTEGER REFERENCES downloads (id) ON DELETE CASCADE,
    job_id        INTEGER,                    -- the latest job; a platform table, so no foreign key
    source        TEXT NOT NULL,              -- 'qobuz', 'applemusic', 'fake'
    kind          TEXT NOT NULL CHECK (kind IN ('track', 'album', 'playlist', 'artist', 'label')),
    item_id       TEXT NOT NULL,
    url           TEXT NOT NULL DEFAULT '',   -- the link it came from, if any
    format        TEXT NOT NULL,              -- 'qobuz:27', 'applemusic:alac'
    force         INTEGER NOT NULL DEFAULT 0 CHECK (force IN (0, 1)),
    title         TEXT NOT NULL DEFAULT '',
    artist        TEXT NOT NULL DEFAULT '',
    cover_url     TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL CHECK (status IN ('queued', 'running', 'done', 'partial', 'failed', 'canceled', 'skipped')),
    error         TEXT NOT NULL DEFAULT '' CHECK (length(error) <= 4100),
    tracks_total  INTEGER NOT NULL DEFAULT 0,
    tracks_done   INTEGER NOT NULL DEFAULT 0,
    tracks_failed INTEGER NOT NULL DEFAULT 0,
    bytes         INTEGER NOT NULL DEFAULT 0,
    folder        TEXT NOT NULL DEFAULT '',   -- the release folder, relative to the music root
    origin        TEXT NOT NULL CHECK (origin IN ('web', 'telegram', 'cli')),
    origin_ref    TEXT NOT NULL DEFAULT '{}', -- JSON: chat_id, request_message_id, status_message_id, status_is_text, user_id, username
    notified      INTEGER NOT NULL DEFAULT 0 CHECK (notified IN (0, 1)),
    created_at    TEXT NOT NULL,
    started_at    TEXT,
    finished_at   TEXT
) STRICT;
CREATE INDEX downloads_list ON downloads (status, id);
CREATE INDEX downloads_parent ON downloads (parent_id);
CREATE INDEX downloads_item ON downloads (source, kind, item_id);
CREATE INDEX downloads_job ON downloads (job_id);

CREATE TABLE download_tracks (
    download_id INTEGER NOT NULL REFERENCES downloads (id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,             -- 1..n in release order
    disc        INTEGER NOT NULL DEFAULT 1,
    item_id     TEXT NOT NULL,
    title       TEXT NOT NULL,
    artist      TEXT NOT NULL DEFAULT '',
    isrc        TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL CHECK (status IN ('pending', 'running', 'done', 'skipped', 'failed')),
    path        TEXT NOT NULL DEFAULT '',     -- relative to the music root
    codec       TEXT NOT NULL DEFAULT '',     -- 'flac', 'alac', 'aac', 'ec-3', 'mp3'
    bit_depth   INTEGER,
    sample_rate INTEGER,                      -- Hz
    size        INTEGER,
    error       TEXT NOT NULL DEFAULT '' CHECK (length(error) <= 4100),
    PRIMARY KEY (download_id, position)
) STRICT;

CREATE TABLE downloaded_items (
    source      TEXT NOT NULL,
    kind        TEXT NOT NULL,
    item_id     TEXT NOT NULL,
    download_id INTEGER REFERENCES downloads (id) ON DELETE SET NULL,
    quality     TEXT NOT NULL DEFAULT '',     -- what was delivered, such as 'FLAC 24/96'
    path        TEXT NOT NULL DEFAULT '',     -- the release folder, relative to the music root
    imported    INTEGER NOT NULL DEFAULT 0 CHECK (imported IN (0, 1)),
    at          TEXT NOT NULL,
    title       TEXT NOT NULL DEFAULT '',     -- from an older tool's history
    artist      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (source, kind, item_id)
) STRICT;
```

Rules:

- A download's `status` moves from `queued` to `running` to one of `done`, `partial`, `failed` or `canceled`. `skipped` is set at creation and never changes. Retry moves `failed`, `partial` and `canceled` back to `queued`, clears `error`, the counts, the times and `notified`, and points `job_id` at a new job.
- `partial` means some tracks failed. The release folder stays where the source left it; for Qobuz that is an `[INCOMPLETE]` folder.
- `error` says why a download failed. A running download with an error has failed in `resolve` or `fetch` and will end at `finish`. An artist or label without albums ends `done` with "nothing to download" there.
- `finish` marks a track that never ended, because the source stopped or skipped it, `failed` with "not downloaded".
- A track a cancel or a shutdown cut off goes back to `pending`, even when the source ends it as failed or never reports it, as a killed amdl does. So `tracks_failed` of a `canceled` download counts only tracks that failed before the cancel.
- An artist or label download has children and no tracks. Its counts are the sums of its children's counts. Children share their parent's origin, format and force flag, with an empty `origin_ref`.
- `downloaded_items` gets a row when an album, playlist or track download ends `done`, and one for each track of a Qobuz playlist, without a `download_id`. A `partial` download writes no row, so asking for it again fetches the missing tracks; the sources skip files that are already complete. `skipped` downloads read the table and never write it.
- `shelf manage import-qobuz-db` writes rows with `imported = 1`, no `download_id`, and the title and artist the old bot's database knew. See [sources/qobuz.md](sources/qobuz.md).
- `origin_ref` holds what the bot needs to edit its status message. `status_is_text` marks a status message sent as text, without a cover, whose edits change the text and not a caption. The bot sends the message before it queues, so a download from Telegram has its message from the start. It never holds tokens.
- Paths are relative to `SHELF_MUSIC_DIR`, so the database stays valid if the mount point changes.

## Library module

`internal/modules/library/migrations/00001_index.sql`. The index is a cache, and a walk can rebuild all of it from the files.

```sql
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

CREATE TABLE library_state (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) STRICT;
```

Rules:

- `facts` holds what the list and the checks need: the Navidrome album ID, the album and artists as Navidrome shows them, track and disc, the year shown, the compilation flag, the MusicBrainz album ID, whether a picture is embedded, the quality and length, the values of the fields an album should share, and the file's own issues. [tags.md](tags.md) has the checks.
- A file is read again when its size or mtime changes, or when `rules` or `version` differ from the running ones. A file that can't be read keeps its `error` and no `album_id`.
- `first_seen` is the file's mtime when the index first saw it. A later read keeps it, so "date added" doesn't move with a retag.
- An album is one `(folder, nd_id)` pair. Each walk writes every album it found and deletes the rest, and an unchanged pair keeps its row ID. A walk that finds the same albums as the last one writes nothing.
- `kinds` lists the issue kinds between commas, so the filter matches `,kind,`. `search` is the title, artist and folder, lowercased with typographic quotes and dashes made plain.
- `library_state` holds one row, `navidrome_config`, with the tag settings last read from Navidrome as JSON.

