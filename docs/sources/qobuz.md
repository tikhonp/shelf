# Qobuz

The `qobuz` module is an adapter over [qobuz-dl-go](https://github.com/tikhonp/qobuz-dl-go), the Go port of qobuz-dl 2.5.9. The port does the API calls, file names, tags and transfers; the module maps them onto shelf's source interfaces. How the port behaves is described in qobuz-dl-go's `docs/port-spec.md`.

## What runs in production today

The old bot calls qobuz-dl 2.5.9 with these effective settings. shelf keeps them.

| Setting | Value | From |
| --- | --- | --- |
| Quality | 27, FLAC up to 24-bit 192 kHz | hard-coded in the bot |
| Fallback to lower quality | on | `QOBUZ_QUALITY_FALLBACK=true` |
| Track workers | 3, no delay | `QOBUZ_BATCH_DOWNLOAD_ENABLED` unset |
| Cover | `cover.jpg` at original size, not embedded | `QOBUZ_EMBED_COVER` has no effect; `settings.embed_art` stays off |
| Tracklist file | on | qobuz-dl default |
| Playlist m3u8 | on | qobuz-dl default |
| Smart discography | off | qobuz-dl default |
| Lyrics | off | qobuz-dl default |
| Folder template | `{artist} - {album} ({year}) [{bit_depth}B-{sampling_rate}kHz]` | `QobuzDL` default |
| Track template | `{track_number} - {track_title}` | `QobuzDL` default |
| Proxy | none | `PROXY_URL` unset |
| Auth | the web player's `user_auth_token` | `QOBUZ_PASSWORD` holds it; qobuz-dl treats a password longer than 60 characters as the token |

Files land in `${SWINGMUSIC_DATA}/music/qobuz`, which shelf sees as `/music/qobuz`.

## Settings

| Variable | Maps to |
| --- | --- |
| `SHELF_QOBUZ_TOKEN` | the user auth token. Empty disables the module. |
| `SHELF_QOBUZ_QUALITY` | requested format ID, default 27 |
| `SHELF_QOBUZ_FALLBACK` | quality fallback, default on |
| `SHELF_QOBUZ_WORKERS`, `SHELF_QOBUZ_DELAY` | `max_workers` and `delay` |
| `SHELF_QOBUZ_DIR` | folder under the music root, default `qobuz` |
| `SHELF_QOBUZ_USE_PROXY` | send the module's HTTP client through `SHELF_PROXY_URL` |

The module's `*http.Client` has no `Timeout`, because it would cut long transfers. qobuz-dl-go sets its own per call through `Options.Timeouts`: 30 seconds per API call, 10 seconds to connect, and 60 seconds without a byte during a transfer. Every request, covers and booklets included, goes through that one client.

## Startup and health

- At startup the module fetches the web player bundle with `GetTokens`, which keeps the app ID and the non-empty secrets. `InitializeClient` then checks the token with `user/get` and picks the first secret that passes a signed `track/getFileUrl` call.
- qobuz-dl treats the app ID `798273057` as a legacy ID and fetches the bundle a second time, keeping all its secrets, the empty one included. On 2026-11-03 the bundle carried that ID, so startup makes two bundle fetches and tries an empty secret, which fails before the next one is tried. The port does the same.
- When no secret passes, the port logs a warning and goes on with the first, as qobuz-dl does without the warning. Later calls then fail with HTTP 400.
- A `user/get` failure other than a 401 doesn't stop startup: qobuz-dl logs "Profile validation bypassed" and goes on with the label `Studio` and no user ID.
- Health reports the subscription label from `user/get`, such as `Studio`, and whether the proxy is in use.
- A 401 marks the source unhealthy with "token expired" and the app keeps running. Search and new downloads for Qobuz then fail with that message until the token is replaced and shelf restarts.

## Links

`ParseLink` passes the whole URL to qobuz-dl-go's `GetURLInfo`. Accepted forms:

- `https://play.qobuz.com/<kind>/<id>` and `https://open.qobuz.com/<kind>/<id>`
- `https://www.qobuz.com/<kind>/<slug>/<id>` and `https://www.qobuz.com/<locale>/<kind>/<slug>/<id>`, where locale is like `us-en`
- kinds `album`, `track`, `playlist`, `artist`, `label`
- `https://www.qobuz.com/<locale>/interpreter/<slug>/<id>` as an artist link; upstream qobuz-dl doesn't accept this one, so the module adds it

The old bot's own regex dropped links with a locale and cut links with a slug. That bug is gone because the module parses the whole URL.

`GetURLInfo` follows upstream's regex exactly, gaps included:

- A link with an extra path segment, such as `/label/<slug>/download-streaming-albums/<id>`, reads as the ID `download`.
- `/playlists/` in the plural doesn't match.
- The regex searches anywhere in the text, so it also finds a path such as `/album/x/123` inside a longer string.

## Search

`catalog/search` with `type` of albums, tracks, artists or playlists, `query`, `limit` and `offset`, through qobuz-dl-go's `SearchAlbums`, `SearchTracks`, `SearchArtists` and `SearchPlaylists`. They return a typed page with the items and the total, and return errors where qobuz-dl answers `{}`. Queries need at least 3 characters, as in qobuz-dl; the typed search doesn't check that, so the module does.

Quality on search results and items:

| Qobuz fields | Badge |
| --- | --- |
| `hires_streamable` true, `maximum_bit_depth` 24, `maximum_sampling_rate` 192 | `Hi-Res 24/192` |
| `hires_streamable` true, 24-bit, 96 kHz | `Hi-Res 24/96`, and so on for other rates |
| otherwise | `CD 16/44.1` |

## Formats

| Format | Label | qobuz-dl format ID |
| --- | --- | --- |
| `qobuz:27` | Max | 27, FLAC 24-bit above 96 kHz |
| `qobuz:7` | 24/96 | 7, FLAC 24-bit up to 96 kHz |
| `qobuz:6` | CD | 6, FLAC 16-bit 44.1 kHz |
| `qobuz:5` | MP3 320 | 5, MP3 320 kbps |

With fallback on, Qobuz may deliver less than asked. `shelf.txt` and the download record the delivered format.

## Downloads

- Album and track downloads call qobuz-dl-go's `DownloadFromID`. Playlists call `HandleID` with the kind `playlist`, which is `handle_url` without the URL parsing and writes the m3u8; `DownloadFromID` alone writes none.
- The port reports progress through its `Progress` interface, which the module turns into `Reporter` calls. A playlist sends one `ReleaseStart` with every track planned.
- Each call returns a `*Result`: the final folder, the album metadata that was used, and per track the final path, the delivered format, bit depth and sampling rate, and whether it was done, skipped because the file existed, or failed. A skipped track's format is read from its file. The error is non-nil only when nothing could start, the context ended, or upstream would have crashed (`ErrUnhandled`).
- Cancelling the context starts no new tracks, stops transfers, removes temp files and renames `[IN PROGRESS]` to `[INCOMPLETE]`.
- Artist and label downloads are expanded by shelf's `resolve` step into one album download per album, using `artist/get` and `label/get` with `extra=albums`. Smart discography stays off, as today.
- Folder states come from qobuz-dl: an album downloads into `[IN PROGRESS] <folder>` and is renamed to `<folder>` when every track succeeded, or to `[INCOMPLETE] <folder>` when some failed. Playlists download into a flat folder named after the playlist and get `<name>.m3u8`.
- Multi-disc albums put tracks in `CD 01`, `CD 02` subfolders.
- Extras: `cover.jpg`, `<album> - Tracklist.txt`, and booklet PDFs named `<album> (<id>).pdf`.
- Tags follow qobuz-dl 2.5.9's `metadata.py`. In MP3 files the ID3 frame order and padding differ from mutagen's; players show neither.
- `shelf.txt` goes into the final folder after qobuz-dl-go returns.

## Already downloaded

qobuz-dl-go asks a `DownloadsDB` whether an album or track was downloaded before. The module implements it over shelf's `downloaded_items`, so the check is the same for the web, the bot and imported history. A forced download passes no database.

```go
Has(ctx context.Context, mediaType, id string, quality int) (bool, error)
Add(ctx context.Context, rec qobuzdl.DownloadRecord) error
```

Python matches the ID and quality only. The port also passes the media type, `album` or `track`, because `downloaded_items` is keyed on it. A failing `Has` stops the download with its error; a failing `Add` is logged and the release stays done.

## Import of the old bot's history

`shelf manage import-qobuz-db <path>` reads the old bot's `qobuz_dl.db`, which is `/data/qobuz_dl.db` in the old container and `${SWINGMUSIC_DATA}/music-downloader-bot/qobuz_dl.db` on the host. Each row of its `downloads` table becomes a `downloaded_items` row with `source = 'qobuz'`, `kind` from `media_type`, `item_id` from `id`, `path` from `saved_path` made relative to the music root, and `imported = 1`. Existing rows are kept, so a second run changes nothing.
