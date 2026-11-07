# Data model

One SQLite file, `/data/shelf.db`, in WAL mode with foreign keys on. Each module has its own goose version table, `<module>_goose_db_version`. Tables are STRICT. Times are UTC text in the form `2006-01-02T15:04:05.000Z`.

## Platform

`internal/platform/migrations/00001_jobs.sql`, proxier's jobs tables trimmed:

- `jobs`: one row per job, with its queue, type, resource and coalescing keys, a state of `queued`, `running`, `succeeded`, `failed`, `cancelled` or `interrupted`, payload JSON, attempt counters, lease, timestamps, error and log counters.
- `job_steps`: each job's named steps and their state.
- `job_log_lines`: log lines per job. The row ID is the event ID of the log stream. A job keeps its first and last 5,000 lines.
- `schedules`: run-time state of scheduled jobs: `platform.backup` at 03:30, `platform.retention` at 05:00 and `library.retention` at 05:15.

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
- A delete in the library removes the rows of its folder and the folders under it, so asking for one of them downloads it again.
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
    source      TEXT NOT NULL DEFAULT '',        -- a source's ID; empty when nothing tells (00008)
    source_info TEXT NOT NULL DEFAULT '{}',      -- JSON: the album's source as the API answers it (00008)
    UNIQUE (folder, nd_id)
) STRICT;
CREATE INDEX library_albums_title ON library_albums (title COLLATE NOCASE);
CREATE INDEX library_albums_source ON library_albums (source);

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

`00002_changes.sql`, with columns from `00003_covers.sql` and `00005_cover_descriptions.sql`, holds every tag write and every delete. Unlike the index, the files can't rebuild it. `00006_runs.sql` below adds `parent` to `library_changes`, and `00007_deletes.sql` builds the table again to add the kind `delete`, with foreign keys off so the old table's drop deletes nothing, and the ID sequence kept.

```sql
CREATE TABLE library_changes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    kind        TEXT NOT NULL CHECK (kind IN ('save', 'undo', 'redo', 'delete')),
    target      INTEGER REFERENCES library_changes (id) ON DELETE CASCADE, -- the save an undo or redo works on
    album_id    INTEGER NOT NULL,          -- the index's album row when asked for; no foreign key
    folder      TEXT NOT NULL,
    title       TEXT NOT NULL,             -- the album and its artist when asked for
    artist      TEXT NOT NULL,
    state       TEXT NOT NULL CHECK (state IN ('queued', 'writing', 'done', 'partial', 'failed')),
    reason      TEXT NOT NULL DEFAULT '',  -- 'conflict', 'locked', 'busy', 'refused', 'verify', 'interrupted', 'error'
    error       TEXT NOT NULL DEFAULT '' CHECK (length(error) <= 4100),
    undone      INTEGER NOT NULL DEFAULT 0 CHECK (undone IN (0, 1)),
    request     TEXT NOT NULL DEFAULT '{}', -- JSON: the edits as asked, or what a delete removed
    conflicts   TEXT NOT NULL DEFAULT '[]', -- JSON: what an undo or redo left alone
    tags        INTEGER NOT NULL DEFAULT 0, -- keys changed, over all files
    job_id      INTEGER,
    created_at  TEXT NOT NULL,
    finished_at TEXT
) STRICT;

CREATE TABLE library_change_files (
    change_id INTEGER NOT NULL REFERENCES library_changes (id) ON DELETE CASCADE,
    path      TEXT NOT NULL,                -- inside the music folder
    before    TEXT NOT NULL,                -- JSON: every key and its values
    after     TEXT NOT NULL,                -- JSON: as planned, then as read back
    revision  TEXT NOT NULL DEFAULT '',     -- after the write
    written   INTEGER NOT NULL DEFAULT 0 CHECK (written IN (0, 1)),
    kind      TEXT NOT NULL DEFAULT 'audio' CHECK (kind IN ('audio', 'cover')),
    cover_before TEXT NOT NULL DEFAULT '',  -- SHA-256 of the front cover; '' is no cover
    cover_after  TEXT NOT NULL DEFAULT '',
    cover_before_description TEXT NOT NULL DEFAULT '',
    cover_after_description  TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (change_id, path)
) STRICT;
```

- A delete is a change with no files. Its `request` holds `{"files", "bytes", "albums", "forgot", "navidrome", "navidrome_tracks"}`, which the job fills in as it goes. Nothing undoes it.
- A change moves from `queued` to `writing` when its journal begins, then to `done`, `partial` or `failed` by how many of its files were replaced. A change the checks stopped goes from `queued` to `failed` and has no files.
- `request` holds the edits with each file's path and revision, because a job's payload stops at 64 KiB. The job's payload names only the change.
- A file row holds the full tag map before and after, so an undo can restore any key. `after` is the plan until the write ends, then what the file read back.
- An `audio` row holds its front cover's hash and description before and after. The hashes are equal when the change left the cover alone. An undo that puts a removed cover back gives it its description. A `cover` row is a folder's cover file, with `{}` for its tags. The images live in `/data/library/covers` under their hashes.
- History has no foreign key to the index, which a walk rebuilds. Paths are relative to the music folder, as in the index.
- An undo or redo has its save as `target` and lists the keys it left alone in `conflicts`. When it replaces any file, it sets its save's `undone`, to 1 for an undo and 0 for a redo.
- The `library.retention` job at 05:15 keeps the newest 1,000 saves and deletes, and `ON DELETE CASCADE` takes their undos, redos and files with them. Clear history deletes every ended change, except a save whose undo or redo is still to run. The same job removes stored covers no change refers to, except uploads younger than a day.

`00004_lookups.sql` holds what suggestions looked up. The services can answer again, so nothing here is a record.

```sql
CREATE TABLE library_lookups (
    url        TEXT PRIMARY KEY,               -- the request
    status     INTEGER NOT NULL CHECK (status IN (200, 404)),
    body       TEXT NOT NULL DEFAULT '',
    fetched_at TEXT NOT NULL
) STRICT;

CREATE TABLE library_remote (
    id         TEXT PRIMARY KEY,               -- the first 32 hex digits of the image URL's SHA-256
    url        TEXT NOT NULL,                  -- the full image
    preview    TEXT NOT NULL,                  -- a smaller copy, or the same URL
    created_at TEXT NOT NULL
) STRICT;
```

- `library_lookups` keeps answers from MusicBrainz, the Cover Art Archive and LRCLIB, and the releases AcoustID found for an album, under `acoustid:` with its folder and Navidrome ID. A 200 serves for 30 days and a 404 for one day. No request holds a secret.
- `library_remote` keeps each cover a suggestion offered, so the editor names an image by ID and shelf fetches only images it found itself. An offer lasts 30 days.
- The `library.retention` job removes lookups and offers older than 30 days.

`00006_runs.sql` holds bulk runs and ReplayGain's measures.

```sql
ALTER TABLE library_changes ADD COLUMN parent INTEGER REFERENCES library_changes (id) ON DELETE CASCADE;

CREATE TABLE library_runs (
    change_id    INTEGER PRIMARY KEY REFERENCES library_changes (id) ON DELETE CASCADE,
    fixes        TEXT NOT NULL,                     -- JSON: the kinds chosen, and 'replaygain'
    cancelled    INTEGER NOT NULL DEFAULT 0 CHECK (cancelled IN (0, 1)),
    analysis_job INTEGER                            -- a platform table, so no foreign key
) STRICT;

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

CREATE TABLE library_gains (
    audio       TEXT PRIMARY KEY,                  -- the hash a write checks, which a retag doesn't move
    peak        REAL NOT NULL,
    blocks      TEXT NOT NULL,                     -- JSON: the loudness histogram
    measured_at TEXT NOT NULL
) STRICT;
```

- A run is a `save`, `undo` or `redo` row in `library_changes` with a `library_runs` row and no files. Each album's write is a change with the run as its `parent`. The history lists only rows without a parent, and deleting a run takes its albums' changes with it.
- An album starts `waiting` when the run measures ReplayGain, else `ready`. It moves through `analyzing` and `writing` to `done`, `skipped`, `failed` or `cancelled`. `done` with no `files` had nothing to change.
- `library_gains` has no retention. It holds about 3 KB a track, and a row of audio that left the library costs only that.


## Accounts and authors

The platform owns these STRICT tables, added by migration 00002_accounts:

| Table | Stored data |
| --- | --- |
| `accounts` | AUTOINCREMENT ID, case-sensitive unique username, salted password hash, opaque WebAuthn user handle, credential generation, creation time. |
| `account_sessions` | Hashed token primary key, account ID, credential generation, last proof time, fixed expiry. Account deletion cascades. |
| `account_passkeys` | Credential ID primary key, account ID, user-chosen credential name, verified public credential JSON, creation and last-use times. Account deletion cascades. |

Passkey ceremonies stay in memory. Sessions survive restarts. A reset increments the account's credential generation and deletes its sessions atomically. See [auth.md](auth.md).

Downloads migration 00003_authors and library migration 00009_authors add nullable `author_id` and a non-null `author` string. Existing rows have a null ID and an empty name. The API displays Unknown, or the existing Telegram identity for a download that has one. New rows capture both fields at request time. Child downloads and run children copy them from the parent row. No foreign key connects authors to accounts, so deletion preserves history. No new activity log exists.


## Agents and library integration

Library migration `00010_agent_ports` adds nullable unique `library_changes.origin`, linking a Save to `proposal:<id>/acceptance:<id>`, and STRICT `library_cover_pins` keyed by holder and cover hash. Retention honors the pins. `library_state.history_trimmed_through` records the highest removed top-level history ID in the same transaction as trimming.

The agents module always migrates its STRICT tables, including when the feature is off:

| Table | Stored data |
| --- | --- |
| `agents_connections` | Connection identity, hashed token, operations, creating account and author, timestamps, revocation. No foreign key crosses module boundaries. |
| `agents_runs` | Connection-bound external sessions. |
| `agents_proposals` | Original album context, review group, follow-up link, current revision, pending count, withdrawal and revision-request flags. |
| `agents_revisions` | Immutable agent/user values, evidence, recorded file paths and revisions, operations, preview and per-operation digests. |
| `agents_ops` | Stable operation IDs, fingerprints, decisions, reviewer and acceptance links. |
| `agents_targets` | Recorded library-relative paths for album links. |
| `agents_acceptances` | Exact selected revision and preview, stable request key, confirmations, reviewer, library change ID, durable outcome snapshot. |
| `agents_events` | Ordered feedback, comments, decisions, revisions, memory corrections and write/undo outcomes. |
| `agents_memory` | Versioned shared rules, inferred preferences, context, evidence and deletion tombstones. |
| `agents_learning` | One library-wide versioned checkpoint and event/history cursors. |
| `agents_usage` | Daily per-connection counters. |
| `agents_state` | The highest retained history ID returned through the port, bounding learning commits. |

Accepting selected operations writes their decisions and the library Save/job in one SQLite transaction. File reads, preparation and image decoding happen before that transaction. A repeated request ID returns its acceptance; a changed payload with that ID conflicts. Stored authors do not depend on live accounts. Account deletion revokes its connections on their next authenticated call.
