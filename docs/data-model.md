# Data model

One SQLite file, `/data/shelf.db`, in WAL mode with foreign keys on. Each module has its own goose version table, `<module>_goose_db_version`. Tables are STRICT. Times are UTC text in the form `2006-01-02T15:04:05.000Z`.

## Platform

`internal/platform/migrations/00001_jobs.sql`, from proxier's `00003_jobs.sql` without the vault column, the subject columns and the event cursors:

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
    origin_ref    TEXT NOT NULL DEFAULT '{}', -- JSON: chat_id, request_message_id, status_message_id, user_id, username
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
    PRIMARY KEY (source, kind, item_id)
) STRICT;
```

Rules:

- A download's `status` moves from `queued` to `running` to one of `done`, `partial`, `failed` or `canceled`. `skipped` is set at creation and never changes. Retry moves `failed`, `partial` and `canceled` back to `queued`, clears `error`, the counts, the times and `notified`, and points `job_id` at a new job.
- `partial` means some tracks failed. The release folder stays where the source left it; for Qobuz that is an `[INCOMPLETE]` folder.
- `error` says why a download failed. A running download with an error has failed in `resolve` or `fetch` and will end at `finish`. An artist or label without albums ends `done` with "nothing to download" there.
- `finish` marks a track that never ended, because the source stopped or skipped it, `failed` with "not downloaded".
- An artist or label download has children and no tracks. Its counts are the sums of its children's counts. Children share their parent's origin, format and force flag, with an empty `origin_ref`.
- `downloaded_items` gets a row when an album, playlist or track download ends `done`. A `partial` download writes no row, so asking for it again fetches the missing tracks; the sources skip files that are already complete. `skipped` downloads read the table and never write it. `shelf manage import-qobuz-db` writes rows with `imported = 1` and no `download_id`.
- `origin_ref` holds what the bot needs to edit its status message. It never holds tokens.
- Paths are relative to `SHELF_MUSIC_DIR`, so the database stays valid if the mount point changes.
