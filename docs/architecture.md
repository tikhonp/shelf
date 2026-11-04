# Architecture

## Containers

On blackberry, in the `music` group of `sh-blackberry/music.yaml`:

- `shelf` is the Go binary. It serves the web UI and the JSON API on port 8080, keeps its SQLite file and backups in `/data`, writes music into `/music`, runs the bot, and starts amdl and ffmpeg as child processes.
- `apple-music-wrapper` runs the shelf-wrapper image: wrapper-lite on port 12340 and its supervisor's control API on port 12341. It is privileged and amd64-only.
- `navidrome` reads the music folder through a read-only mount. shelf asks it to scan over the Subsonic API.
- `ssht-shelf` tunnels port 8080 to the sh-main gateway, where nginx serves `shelf.music.tikhonnnnn.com` behind `auth_basic`.

Outside services: the Qobuz API and CDN, the Apple Music catalog API at `amp-api.music.apple.com`, and the Telegram Bot API. Each source can send its traffic through `SHELF_PROXY_URL`.

## Inside shelf

`cmd/shelf/main.go` builds the platform and the list of modules. The platform packages come from proxier and are trimmed to what shelf uses:

| Package | Job |
| --- | --- |
| `config` | Reads `SHELF_*` variables, reports every problem at once, never logs values. |
| `obs` | slog JSON logger, app version. |
| `db`, `dbtest` | One write connection and a read pool on modernc SQLite; temp-file databases for tests. |
| `migrations` | goose with a version table per module. |
| `module` | The `Module` interface and the optional interfaces a module declares. |
| `jobs` | Jobs in SQLite with queues, named steps, leases, resource keys, cancel, retry, logs and resume after a restart. A request may name its queue, so one job type can run on several. Cancel ends the running step's context. A job at 05:00 deletes finished jobs, failed ones after 90 days and the rest after 30. |
| `backup` | A daily `VACUUM INTO` copy at 03:30, keeping 14. |
| `hub` | In-process publish and subscribe, feeding the event streams and the bot. A subscriber that falls 64 events behind misses the next ones; publishers never wait. |
| `httpx`, `web` | Echo v5, middleware, JSON helpers, server-sent events. |
| `ui` | The embedded Vue build. |

Modules:

| Module | Job |
| --- | --- |
| `downloads` | Downloads, their tracks and the downloaded-items registry; the download job type; `shelf.txt`; `/api/downloads` and `/api/events`. |
| `catalog` | `/api/sources`, `/api/search`, `/api/resolve`, `/api/items` over every registered source. |
| `qobuz` | The Qobuz source, an adapter over qobuz-dl-go. |
| `applemusic` | The Apple Music source: catalog client, amdl runner, wrapper control client. |
| `navidrome` | The library port: scans and their state. |
| `telegram` | The bot. It implements the downloads module's notifier port. |
| `fakesource` | A source with canned data for tests, and for UI work when `SHELF_DEV_FAKE_SOURCE` is on. |

A module implements `Name()` and `Migrations()` and declares the rest through optional interfaces, found by type assertion, as in `proxier/internal/platform/module/module.go`: `Initializer`, `Migrated`, `RouteDeclarer`, `JobDeclarer`, `SourceDeclarer` and `Runner`. Only serve calls a `Runner`'s `Run`, which holds work that lasts as long as the server, such as the bot's long polling. `manage` opens the platform too, so `Init` never logs in or reaches the network. The platform logs a `Run` that fails, and the server goes on without it. `module.Deps` gives a module the config, the logger, the database, the jobs system, the hub, the source registry, and a `Closing` channel that long-lived responses end on. The platform registers job types, schedules and sources after every `Init`. Adding a source means one new module and one line in `main`.

Modules reach each other through ports that `main` wires. The catalog module's `Statuses` port is the downloads module. The downloads module's `Notifier` port is the bot, and its `Scanner` port is the navidrome module. The bot reaches the downloads module through its own `Downloads` port, which the downloads service implements. The downloads service exists from `downloads.New`, so `main` wires these before `Init`.

## Source interfaces

`internal/source` holds the shared types and the interfaces. A source implements `Source` and any of the others.

```go
type Kind string // "track", "album", "playlist", "artist", "label"

type Ref struct {
    Source string // "qobuz", "applemusic"
    Kind   Kind
    ID     string
}

type Source interface {
    ID() string
    Name() string
    Health(ctx context.Context) Health // from what the source knows, without waiting on the network
}

type Switch interface {
    Enabled() bool // false: registered but switched off; shelf still recognises its links
}

type LinkParser interface {
    ParseLink(raw string) (Ref, bool)
}

type LinkRefuser interface {
    RefuseLink(raw string) bool // its own link to something shelf can't download, such as a music video
}

type Searcher interface {
    Search(ctx context.Context, q Query) (Page, error) // q.Kind: album, track, artist or playlist
    SearchKinds() []Kind
}

type Resolver interface {
    Item(ctx context.Context, ref Ref) (Item, error)
}

type Downloader interface {
    Formats() []Format
    Download(ctx context.Context, req DownloadRequest, rep Reporter) (DownloadResult, error)
}

type Library interface {
    StartScan(ctx context.Context) error
    ScanStatus(ctx context.Context) (ScanStatus, error)
}
```

- `Item` carries what the UI and `shelf.txt` show: title, version, artist, the album of a track, year, release date, label, UPC or ISRC, genre, explicit flag, track count, duration in seconds, cover URL, the item's page on the source's site, and quality. An album or playlist item has its tracks. An artist or label item has its discography in sections: albums, EPs and singles, compilations, appears on. An unknown item is `source.ErrNotFound`.
- Items and tracks have a `status` that only shelf fills: `in_library`, `queued`, `running`, `partial` or `failed`, with the download ID and the track counts.
- `Quality` has the codec, bit depth, sample rate, hi-res and Atmos flags, and the badge text, such as `Hi-Res 24/96`. `Words()` gives `FLAC 24-bit 96 kHz` and `Short()` gives `FLAC 24/96`.
- `Format` has an ID such as `qobuz:27` or `applemusic:atmos`, a label such as `Max`, a description in words for `shelf.txt`, and a default flag.
- `DownloadRequest` has the download ID, the ref, the format, the force flag and the music root. The source writes under its own folder there.
- `Reporter` has `Plan`, `Start`, `Progress` with the bytes written, `Done` with the file, its quality and size, a skipped flag or an error, and `Log`, the download's log. A position, 1 to n in release order, names each track. Several goroutines may call its methods.
- `Download` returns an error only when the download could not run or its context ended. A failed track goes to `Reporter.Done`.
- `DownloadResult` lists the release folders and files, relative to the music root, the tool that wrote them, such as `amdl 0.1.4`, and what the catalog calls a track, `track` or `song`.
- A source that can't answer now returns `source.Unavailable` with a reason people may read, such as `token expired`. The API passes that reason on and answers 503. Other source errors stay in the logs, since they may hold a URL with a token.
- `source.Registry` holds every registered source. `ParseLink` asks all of them, switched on or not, so shelf can refuse a link from a switched-off source by name. `RefusedLink` finds the source that refuses a link, so the bot can say music videos are not supported rather than that the link is unknown.

## Downloads

- `POST /api/downloads`, the bot and `manage` commands all go through `downloads.Service.Queue`. A format without the source's prefix, such as `27`, is the source's; no format is the source's default.
- Before queuing, the service looks up `downloaded_items`. A hit creates a `skipped` download unless the request has `force`. It has no job, and takes its title and cover from the item's last download, then its title and artist from the `downloaded_items` row. For anything still missing, `Queue` asks the source after the transaction, for up to 5 seconds. When that fails, the download stays `skipped` without it, and the next skip asks again.
- `force` passes this check and nothing more. The sources still skip complete files, so a forced download fetches what is missing, and on a complete folder it ends `done` with every track `skipped`. `bytes` counts the files it skipped.
- A download gets one job of type `downloads.download` on its source's queue: the queue named after the source, or `system` for a source without one, such as the fake source. Queues and concurrency: `qobuz` 1, `applemusic` 1, `library` 1, `system` 1.
- The job's steps:
  1. `resolve` fetches the item and stores its title, artist, cover and tracks, and keeps the label, release date, UPC, ISRC and link for `shelf.txt` in the job's payload. An artist or label creates one child download per album of every section, once, and ends there.
  2. `fetch` calls the source's `Download`. Progress goes to `download_tracks` at most once a second and to the hub on every change.
  3. `finish` writes `shelf.txt` into each release folder, records `downloaded_items`, sets the result to `done`, `partial` or `failed`, updates the parent's counts, and asks the `Scanner` port for a scan.
  4. `notify` calls the notifier port, which gives a Telegram status message its final text. The `notified` flag stops a repeat after a restart. shelf logs a failing call and doesn't retry it; the bot's send queue retries on its own.
- A source's error in `resolve` or `fetch` fails the download, not the job. The step records it, `fetch` skips the source, and `finish` ends the download `failed`, or `partial` when the source wrote some tracks. Only shelf's own errors, such as a failed database write, fail the job.
- The resource key `item:<source>:<kind>:<id>` keeps two downloads of one item from running at once.
- Cancel ends the running step's context. A queued download ends `canceled` at once, a running one when the source returns. A download that ends canceled, or whose job fails, never reaches its `notify` step, so a `downloads.notify` job on `system` sends the final message instead. Canceling an artist or label cancels its albums.
- Retry accepts `failed`, `partial` and `canceled` downloads. It starts a new job from `resolve` and keeps the old job and its log. Retry of an artist or label retries its albums that did not end done. To retry an album of an artist, retry the artist.
- At shutdown the jobs platform gives running steps 3 seconds, then marks them `interrupted`. At start they resume from their first unfinished step; sources skip files that are already complete.
- A parent's status follows its children: running while any child is queued or running, then `done` when all are done or skipped, `failed` when all failed, `canceled` when all were canceled, and `partial` otherwise. A parent without children ends `done` with the note "nothing to download" in `error`. Its counts are the sums of its children's.

## Library

The navidrome module's `navidrome.Client` is the `Library`. It calls Navidrome's Subsonic API with token authentication, so the password never travels, and the URL, which carries the token, never reaches an error or the log.

- Each download that ends done or partial asks the `Scanner` port for a scan. That queues a `navidrome.scan` job on the `library` queue for `SHELF_NAVIDROME_SCAN_DELAY` from now, with the coalescing key `scan`. A request that comes while the scan waits merges into it and moves it later, so a burst of downloads gives one scan.
- `POST /api/library/scan` queues a scan that runs now, or moves the waiting one to now.
- The job reads `getScanStatus`. If Navidrome is scanning, the job defers for 30 seconds, so a scan that starts after the downloads that asked reads their files. Otherwise it calls `startScan`, a quick scan.
- When Navidrome doesn't answer, the job defers for a minute without counting an attempt, so it stays the one queued scan that requests merge into. After ten failures in a row the job fails. A Subsonic error, such as a wrong password, fails it at once.
- The hub gets a `scan` event when a scan starts, and when a poll sees it end. Polling runs every 10 seconds only while a scan is known to run: after the job started one, after the job found one running, or after `GET /api/library/scan` saw one. It stops after about five minutes without an answer.
- With `SHELF_NAVIDROME_URL` empty the module is off: downloads queue no scan, and `POST /api/library/scan` answers 422.

## Events

The hub carries three kinds of event. `GET /api/events` streams them as server-sent events, and the bot subscribes to `download`.

| Event | Fields |
| --- | --- |
| `download` | `id`, `parent_id`, `status`, `tracks_done`, `tracks_total`, `tracks_failed`, `current`, the title of the track being fetched, `bytes`, `error` |
| `scan` | `scanning`, `count`, `last_scan` |
| `source` | `id`, `ok`, `message`; for Apple Music also `wrapper`, the wrapper state, null when it doesn't answer |

Streams send `Cache-Control: no-store`, `X-Accel-Buffering: no` and a keepalive comment every 15 seconds, and end when serve stops. A client that reconnects reloads the lists, so the stream needs no replay. The per-download log stream, `GET /api/downloads/{id}/log`, sends the latest job's lines as `line` events with `time`, `level`, `step` and `text`, and the line's row ID as the event ID. It resumes after `Last-Event-ID`, or after `?after=` on a fresh connection, as proxier's job log does. When the job has ended and every line is sent, it sends a `done` event with the job's `state`, and the client closes the stream, or `EventSource` would reconnect.

## HTTP API

JSON in and out. Errors are `{"error": "<message>"}` with a 4xx or 5xx status.

| Route | Purpose |
| --- | --- |
| `GET /api/sources` | Every source, switched on or not, with `enabled`, `capabilities` (`links`, `search` kinds, `resolve`, `download`), `formats` and `health`. |
| `GET /api/search?source=&type=&q=&limit=&offset=` | Search one source. `type` is `album`, `track`, `artist` or `playlist`, `limit` 1 to 100 with 20 by default. Answers `{"items", "total", "limit", "offset"}`, each item with its download status. |
| `POST /api/resolve` | `{"url"}`: the item behind a pasted link, with download statuses. |
| `GET /api/items/{source}/{kind}/{id}` | An album with tracks, an artist with sections, a playlist with tracks. Each entry has its download status. |
| `POST /api/downloads` | `{"ref": {"source", "kind", "id"}}` or `{"url"}`, with `format` and `force`. Answers 201 with the new download. |
| `GET /api/downloads?status=&source=&before=&limit=` | Top-level downloads newest first; an artist's albums are in its detail. `status` takes a comma-separated list, `limit` 1 to 200 with 50 by default. Answers `{"downloads", "next"}`, where `next` is the `before` of the next page, or null. |
| `GET /api/downloads/{id}` | One download with its tracks, children and `shelf.txt` path. |
| `POST /api/downloads/{id}/cancel` | Cancel a queued or running download. Answers the download; 409 when it has ended. |
| `POST /api/downloads/{id}/retry` | Retry a failed, partial or canceled download. Answers the download; 409 for any other. |
| `GET /api/downloads/{id}/log` | Server-sent log lines. |
| `GET /api/library/scan` | The scan state read from Navidrome now, and whether it answers: `{"enabled", "ok", "message", "address", "latency_ms", "scanning", "count", "last_scan"}`. A Navidrome that doesn't answer is `ok: false` with a message, not a failed request. |
| `POST /api/library/scan` | Start a scan now. Answers 202 with `{"job_id"}`; 422 when Navidrome is not set up. |
| `GET /api/telegram` | The bot for the Status page: `{"enabled", "connected", "username", "users"}`. |
| `GET /api/applemusic/wrapper` | The wrapper state for the Status page: `{"state", "storefront", "since", "error", "code_deadline"}`. 503 when the wrapper doesn't answer. |
| `POST /api/applemusic/wrapper/login`, `/code`, `/restart` | Sign-in and restart, passed to the supervisor. Its 409 and 400 answers come through with their message; 502 when it refuses shelf's token. 422 when Apple Music is not set up. |
| `GET /api/applemusic/wrapper/log?after=` | The wrapper's kept log lines after a line number, for the Status page's log tail: `{"lines": [{"n", "time", "text"}]}`. |
| `GET /api/events` | Server-sent `download`, `scan` and `source` events. |
| `GET /api/version` | `{"version"}` for the UI. |
| `GET /healthz` | Liveness for Docker and `shelf healthcheck`. |

Every other GET or HEAD path gets the file at that path from the embedded Vue build, or `index.html` when there is no such file, so the Vue router handles it. Files under `/assets/` have hashed names and get `Cache-Control: public, max-age=31536000, immutable`. A missing one is a 404, not `index.html`. The rest of the build gets `no-cache`. Without a build, shelf serves a short "UI not built" page.

An unknown `/api` path answers a JSON 404 for any method. The client sees only an `echo.HTTPError`'s own message. For any other error it gets the status text, and the request log gets the error.

The API answers a download as `{"id", "parent_id", "source", "kind", "item_id", "url", "format", "title", "artist", "cover_url", "status", "error", "tracks_total", "tracks_done", "tracks_failed", "bytes", "folder", "origin", "created_at", "started_at", "finished_at"}`, with null for an unset parent or time. The detail adds `tracks`, `children` and `provenance_path`. A link no source knows, a source that is unknown, switched off or can't do the request, and an unknown format answer 422. A source's own error in search or lookup answers 502 with a short message, and the log gets the error, since it may hold a URL with a token.

## Security

- The gateway asks for basic auth before any request reaches shelf.
- shelf checks unsafe methods with `http.NewCrossOriginProtection` and also trusts `SHELF_PUBLIC_URL`'s origin, because the gateway may rewrite `Host`. It limits request bodies to 1 MiB and sends a CSP that allows only `'self'`, plus images from `static.qobuz.com` and `*.mzstatic.com`, with `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`.
- Tokens and passwords live only in environment variables, never in the database, logs or `shelf.txt`. The Apple ID password passes through shelf to the supervisor and is not stored by either.
- The supervisor's control API requires `SHELF_WRAPPER_TOKEN` and is reachable only on the compose network.

## Configuration

Parsed by hand like `proxier/internal/platform/config/config.go`. Every problem is reported at once, and values are never logged.

An unset variable takes its default. A variable set to an empty value is empty. An empty `SHELF_AMDL_CONFIG` or `SHELF_NAVIDROME_URL` turns Apple Music or scans off despite the default, and an empty value is an error for a variable that needs one, such as `SHELF_TIMEZONE`. `Load` trims every value.

Tokens and passwords are `config.Secret`, which prints, logs and marshals as `[redacted]`. `Reveal()` gives the value to the one place that sends it. `Config` prints and logs as a placeholder whatever the verb, because `SHELF_PROXY_URL` may hold a password. `Warnings()` names valid settings that leave a part unable to work: a Telegram token without users, Navidrome without a user or password, and Apple Music without `SHELF_WRAPPER_TOKEN`.

| Variable | Default | Meaning |
| --- | --- | --- |
| `SHELF_DATA_DIR` | `/data` | Database, backups and the amdl work folder. |
| `SHELF_MUSIC_DIR` | `/music` | The music library root. |
| `SHELF_HTTP_ADDR` | `:8080` | Listen address. |
| `SHELF_PUBLIC_URL` | | Base URL for links in Telegram messages and in `shelf.txt`. |
| `SHELF_TIMEZONE` | `Europe/Moscow` | Display time zone for `shelf.txt`, the UI and the backup schedule. |
| `SHELF_PROXY_URL` | | Proxy for modules that opt in. |
| `SHELF_QOBUZ_TOKEN` | | Qobuz `user_auth_token`, today's `QOBUZ_PASSWORD`. Empty disables Qobuz. |
| `SHELF_QOBUZ_DIR` | `qobuz` | Folder under the music root. |
| `SHELF_QOBUZ_QUALITY` | `27` | Requested format ID. |
| `SHELF_QOBUZ_FALLBACK` | `true` | Allow lower quality, as in production now. |
| `SHELF_QOBUZ_WORKERS` | `3` | Tracks downloaded at once. |
| `SHELF_QOBUZ_DELAY` | `0s` | Pause after each track. Above zero, one worker. The old `QOBUZ_BATCH_DOWNLOAD_ENABLED=false` maps to `1s`. |
| `SHELF_QOBUZ_USE_PROXY` | `false` | Send Qobuz traffic through the proxy. |
| `SHELF_AMDL_CONFIG` | `/config/amdl.yaml` | Base amdl config. Empty disables Apple Music. |
| `SHELF_AMDL_TIMEOUT` | `2h` | Limit per amdl run. |
| `SHELF_APPLE_DIR` | `apple-music` | Folder under the music root. |
| `SHELF_WRAPPER_URL` | `http://apple-music-wrapper:12340` | wrapper-lite API, for amdl and the storefront. |
| `SHELF_WRAPPER_CONTROL_URL` | `http://apple-music-wrapper:12341` | Supervisor control API. |
| `SHELF_WRAPPER_TOKEN` | | Shared with shelf-wrapper. |
| `SHELF_NAVIDROME_URL` | `http://navidrome:4533` | Empty disables scans. |
| `SHELF_NAVIDROME_USER`, `SHELF_NAVIDROME_PASSWORD` | | An admin account. |
| `SHELF_NAVIDROME_SCAN_DELAY` | `30s` | Wait after the last finished download. |
| `SHELF_TELEGRAM_TOKEN` | | Empty disables the bot. |
| `SHELF_TELEGRAM_USERS` | | Comma-separated user IDs the bot answers. |
| `SHELF_TELEGRAM_USE_PROXY` | `false` | Send Telegram traffic through the proxy. |
| `SHELF_DEV_FAKE_SOURCE` | `false` | Register the fake source, for UI work without accounts. |
| `SHELF_LOG_LEVEL` | `info` | slog level. |

The supervisor in shelf-wrapper reads `SHELF_WRAPPER_TOKEN`, plus optional `SHELF_WRAPPER_LITE_ARGS`: more wrapper-lite flags for service mode and sign-in, split on spaces, with quotes around a value that has spaces.
