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
| `jobs` | Jobs in SQLite with queues, named steps, leases, resource keys, cancel, retry, logs and resume after a restart. |
| `backup` | A daily `VACUUM INTO` copy at 03:30, keeping 14. |
| `hub` | In-process publish and subscribe, feeding the event streams and the bot. |
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

A module implements `Name()` and `Migrations()` and declares the rest through optional interfaces, found by type assertion, as in `proxier/internal/platform/module/module.go`. shelf adds `SourceDeclarer` and `LibraryDeclarer`. Adding a source means one new module and one line in `main`.

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
    Health(ctx context.Context) Health
}

type LinkParser interface {
    ParseLink(raw string) (Ref, bool)
}

type Searcher interface {
    Search(ctx context.Context, q Query) (Page, error) // q.Kind: album, track, artist or playlist
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

- `Item` carries what the UI and `shelf.txt` show: title, version, artist, year, release date, label, UPC or ISRC, genre, explicit flag, track count, duration, cover URL, the item's page on the source's site, and quality. An album or playlist item has its tracks. An artist item has its discography in sections: albums, EPs and singles, compilations, appears on.
- `Quality` has the codec, bit depth, sample rate, hi-res and Atmos flags, and the badge text, such as `Hi-Res 24/96`.
- `Format` has an ID such as `qobuz:27` or `applemusic:atmos`, a label, and a default flag.
- `DownloadRequest` has the download ID, the ref, the format, the force flag and the source's folder under the music root.
- `Reporter` receives the planned tracks, track starts, byte counts, finished tracks with their file or error, and log lines.
- `DownloadResult` lists the release folders and files, relative to the music root, and the tool that wrote them, such as `amdl 0.1.4`.

## Downloads

- `POST /api/downloads`, the bot and `manage` commands all go through `downloads.Service.Queue`.
- Before queuing, the service looks up `downloaded_items`. A hit creates a `skipped` download unless the request has `force`.
- A download gets one job of type `downloads.download` on its source's queue. Queues and concurrency: `qobuz` 1, `applemusic` 1, `library` 1, `system` 1.
- The job's steps:
  1. `resolve` fetches the item and stores its title, artist, cover and tracks. An artist or label creates one child download per album and ends there.
  2. `fetch` calls the source's `Download`. Progress goes to `download_tracks` at most once a second and to the hub on every change.
  3. `finish` writes `shelf.txt` into each release folder, records `downloaded_items`, sets the result to `done`, `partial` or `failed`, updates the parent's counts, and enqueues a Navidrome scan.
  4. `notify` calls the notifier port, which gives a Telegram status message its final text. The `notified` flag stops a repeat after a restart.
- The resource key `item:<source>:<kind>:<id>` keeps two downloads of one item from running at once.
- Cancel stops the job's context. Retry starts a new job for the same download and keeps the old job's log.
- At shutdown the jobs platform marks running jobs `interrupted`. At start they resume from their first unfinished step; sources skip files that are already complete.
- A parent's status follows its children: running while any child is queued or running, then `done` when all are done, `failed` when all failed, and `partial` otherwise.

## Events

The hub carries three kinds of event. `GET /api/events` streams them as server-sent events, and the bot subscribes to `download`.

| Event | Fields |
| --- | --- |
| `download` | `id`, `parent_id`, `status`, `tracks_done`, `tracks_total`, `tracks_failed`, `current`, `bytes`, `error` |
| `scan` | `scanning`, `count`, `last_scan` |
| `source` | `id`, `ok`, `message`; for Apple Music also the wrapper state |

Streams send `X-Accel-Buffering: no` and a keepalive comment every 15 seconds. A client that reconnects reloads the lists, so the stream needs no replay. The per-download log stream, `GET /api/downloads/{id}/log`, does resume from `Last-Event-ID`, as proxier's job log does.

## HTTP API

JSON in and out. Errors are `{"error": "<message>"}` with a 4xx or 5xx status.

| Route | Purpose |
| --- | --- |
| `GET /api/sources` | Modules with capabilities, formats and health. |
| `GET /api/search?source=&type=&q=&limit=&offset=` | Search one source for albums, tracks, artists or playlists. |
| `POST /api/resolve` | `{"url"}`: the item behind a pasted link. |
| `GET /api/items/{source}/{kind}/{id}` | An album with tracks, an artist with sections, a playlist with tracks. Each entry has its download status. |
| `POST /api/downloads` | `{"ref"}` or `{"url"}`, with `format` and `force`. Answers the new download. |
| `GET /api/downloads?status=&source=&before=&limit=` | Downloads newest first, with cursor paging. |
| `GET /api/downloads/{id}` | One download with its tracks, children and `shelf.txt` path. |
| `POST /api/downloads/{id}/cancel` | Cancel a queued or running download. |
| `POST /api/downloads/{id}/retry` | Retry a failed, partial or canceled download. |
| `GET /api/downloads/{id}/log` | Server-sent log lines. |
| `GET /api/library/scan`, `POST /api/library/scan` | The Navidrome scan state, or start a scan. |
| `GET /api/applemusic/wrapper` | The wrapper state for the Status page. |
| `POST /api/applemusic/wrapper/login`, `/code`, `/restart` | Sign-in and restart, passed to the supervisor. |
| `GET /api/events` | Server-sent `download`, `scan` and `source` events. |
| `GET /api/version` | `{"version"}` for the UI. |
| `GET /healthz` | Liveness for Docker and `shelf healthcheck`. |

Every other GET or HEAD path gets the file at that path from the embedded Vue build, or `index.html` when there is no such file, so the Vue router handles it. Files under `/assets/` have hashed names and get `Cache-Control: public, max-age=31536000, immutable`. A missing one is a 404, not `index.html`. The rest of the build gets `no-cache`. Without a build, shelf serves a short "UI not built" page.

An unknown `/api` path answers a JSON 404 for any method. The client sees only an `echo.HTTPError`'s own message. For any other error it gets the status text, and the request log gets the error.

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

The supervisor in shelf-wrapper reads `SHELF_WRAPPER_TOKEN`, plus optional `SHELF_WRAPPER_LITE_ARGS` for extra wrapper-lite flags.
