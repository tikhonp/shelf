# Qobuz

The `qobuz` module is an adapter over [qobuz-dl-go](https://github.com/tikhonp/qobuz-dl-go), the Go port of qobuz-dl 2.5.9. The port does the API calls, file names, tags and transfers; the module maps them onto shelf's source interfaces. How the port behaves is described in qobuz-dl-go's `docs/port-spec.md`.

## Settings

| Variable | Maps to |
| --- | --- |
| `SHELF_QOBUZ_TOKEN` | the user auth token. Empty disables the module. |
| `SHELF_QOBUZ_QUALITY` | requested format ID, default 27 |
| `SHELF_QOBUZ_FALLBACK` | quality fallback, default on |
| `SHELF_QOBUZ_WORKERS`, `SHELF_QOBUZ_DELAY` | `max_workers` and `delay` |
| `SHELF_QOBUZ_DIR` | folder under the music root, default `qobuz` |
| `SHELF_QOBUZ_USE_PROXY` | send the module's HTTP client through `SHELF_PROXY_URL` |

The rest are qobuz-dl's defaults, as the old bot ran them: `cover.jpg` at original size and not embedded, the tracklist file, a playlist m3u8, no lyrics, smart discography off, the folder template `{artist} - {album} ({year}) [{bit_depth}B-{sampling_rate}kHz]` and the track template `{track_number} - {track_title}`. Files land in `/music/qobuz`, which is `${SWINGMUSIC_DATA}/music/qobuz` on blackberry.

The module's `*http.Client` has no `Timeout`, because it would cut long transfers. qobuz-dl-go sets its own per call through `Options.Timeouts`: 30 seconds per API call, 10 seconds to connect, and 60 seconds without a byte during a transfer. Every request, covers and booklets included, goes through that one client.

A qobuz-dl-go instance has one quality and one downloads database, and `HandleID` can't change either per call. So the module keeps one instance per format, and one more per format for forced downloads, which has no database. The first download in a format logs in again with the app ID and secrets from the start.

## Startup and health

- At startup the module fetches the web player bundle with `GetTokens`, which keeps the app ID and the non-empty secrets. `InitializeClient` then checks the token with `user/get` and picks the first secret that passes a signed `track/getFileUrl` call.
- qobuz-dl treats the app ID `798273057` as a legacy ID and fetches the bundle a second time, keeping all its secrets, the empty one included. The bundle carries that ID, so startup makes two bundle fetches and tries the empty secret first, which fails. The port does the same.
- When no secret passes, the port logs a warning and goes on with the first, as qobuz-dl does without the warning. Later calls then fail with HTTP 400.
- A `user/get` failure other than a 401 doesn't stop startup: qobuz-dl logs "Profile validation bypassed" and goes on with the label `Studio` and no user ID.
- All of this runs in `Run`, in the background, so shelf starts when Qobuz is unreachable. Until the first attempt ends, health says `starting`. After a failed attempt it says `no answer, trying again every 5 minutes`, and the module tries again that often.
- Search, lookups and downloads wait up to a minute for the first attempt to end, because a download resumed after a restart reaches the source before the login is over. After a failed attempt they fail at once.
- Health reports the subscription label from `user/get`, such as `Studio`, and whether the proxy is in use: `Studio, through the proxy`.
- A 401 marks the source unhealthy with "token expired" and the app keeps running. Search and new downloads for Qobuz then fail with that message, without asking Qobuz, until the token is replaced and shelf restarts. The API answers `Qobuz: token expired`, and a download's error reads `Lookup failed: token expired`.
- In a download, the 401 arrives in `Release.Err`, in a track's `Err`, or inside `ErrUnhandled` for a playlist's pages. The module looks in all three.

## Links

`ParseLink` checks the host first, then passes the whole URL to qobuz-dl-go's `GetURLInfo`. Accepted forms:

- `https://play.qobuz.com/<kind>/<id>` and `https://open.qobuz.com/<kind>/<id>`
- `https://www.qobuz.com/<kind>/<slug>/<id>` and `https://www.qobuz.com/<locale>/<kind>/<slug>/<id>`, where locale is like `us-en`, and the same without `www`
- kinds `album`, `track`, `playlist`, `artist`, `label`

Upstream qobuz-dl doesn't accept three forms the site uses, so the module reads them itself. Their IDs are numbers, and the locale is optional:

- `https://www.qobuz.com/<locale>/interpreter/<slug>/<id>` as an artist link
- `https://www.qobuz.com/<locale>/playlists/<slug>/<id>` and `/playlists/<id>` as a playlist link
- `https://www.qobuz.com/<locale>/label/<slug>/download-streaming-albums/<id>` as a label link

`GetURLInfo` follows upstream's regex exactly, gaps included, and the module closes them:

- The regex searches anywhere in the text, so it finds `/album/x/123` on any site. The module claims only the hosts above.
- The regex stops at a dash, so `/label/<slug>/download-streaming-albums` reads as the ID `download`. It also finds a path in a query string. The module refuses a link unless the ID is its whole last segment.

## Search

`catalog/search` with `type` of albums, tracks, artists or playlists, `query`, `limit` and `offset`, through qobuz-dl-go's `SearchAlbums`, `SearchTracks`, `SearchArtists` and `SearchPlaylists`. They return a typed page with the items and the total, and return errors where qobuz-dl answers `{}`. Queries need at least 3 characters, as in qobuz-dl; the typed search doesn't check that, so the module answers a shorter query with an empty page.

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

- Album and track downloads call qobuz-dl-go's `DownloadFromID` with `IgnoreDB`. Playlists call `HandleID` with the kind `playlist`, which is `handle_url` without the URL parsing and writes the m3u8; `DownloadFromID` alone writes none.
- The port reports progress through its `Progress` interface, which the module turns into `Reporter` calls. A playlist sends one `ReleaseStart` with every track planned.
- A track ends in the work folder, so `Reporter.Done` goes out twice for a track with a file: at `TrackEnd`, which moves the counts, and after the call returns, with the final path and size.
- Each call returns a `*Result`: the final folder, the album metadata that was used, and per track the final path, the delivered format, bit depth and sampling rate, and whether it was done, skipped because the file existed, or failed. A skipped track's format is read from its file. The error is non-nil only when nothing could start, the context ended, or upstream would have crashed (`ErrUnhandled`).
- Cancelling the context starts no new tracks, stops transfers, removes temp files and renames `[IN PROGRESS]` to `[INCOMPLETE]`.
- shelf's `resolve` step expands an artist or label into one album download per album, using `artist/get` and `label/get` with `extra=albums`. Smart discography stays off.
- Folder states come from qobuz-dl: an album downloads into `[IN PROGRESS] <folder>` and is renamed to `<folder>` when every track succeeded, or to `[INCOMPLETE] <folder>` when some failed. Playlists download into a flat folder named after the playlist and get `<name>.m3u8`.
- Multi-disc albums put tracks in `CD 01`, `CD 02` subfolders.
- Extras: `cover.jpg`, `<album> - Tracklist.txt`, and booklet PDFs named `<album> (<id>).pdf`.
- Tags follow qobuz-dl 2.5.9's `metadata.py`. In MP3 files the ID3 frame order and padding differ from mutagen's; players show neither. The frame order also changes between runs, because `bogem/id3v2` keeps frames in a map, so two downloads of one MP3 differ byte for byte. The audio and the tags are the same.
- `shelf.txt` goes into the final folder after qobuz-dl-go returns.

## Already downloaded

`Queue` looks in `downloaded_items` before a download gets a job, and a hit gives a `skipped` download unless the request has force. qobuz-dl-go asks a `DownloadsDB` the same question, and the module implements it over the same table, so the check is the same for the web, the bot and imported history.

Album and track downloads pass `IgnoreDB`, because `Queue` has asked already and a job that reaches `Download` must run. Asked again, the database would find the row of a copy queued next to it, or the job's own row after a restart, and the release would skip with no tracks to report. The check for files on disk still skips what is complete. So only a playlist's tracks reach `Has`.

A forced download uses an instance with no database, since `HandleID` has no `IgnoreDB`.

```go
Has(ctx context.Context, mediaType, id string, quality int) (bool, error)
Add(ctx context.Context, rec qobuzdl.DownloadRecord) error
```

Python matches the ID and quality only. The port also passes the media type, `album` or `track`, because `downloaded_items` is keyed on it. `Has` ignores the quality, because the table keeps what was delivered and not what was asked for, and `Queue` ignores the format too. A track on record at CD quality counts as downloaded when a playlist asks for Max.

`Add` writes a row without a download ID and keeps a row already there. For an album or track, `finish` then fills in the download. For a playlist's track, it is the only row, so that track alone is skipped later, with the playlist's folder as its path. A failing `Has` stops the download with its error; a failing `Add` is logged and the release stays done.

A playlist track that `Has` skips writes no file, as in Python. That also covers a playlist resumed after a restart: the tracks of the first run come back `skipped`, without a file name or format in `shelf.txt`. Their files stay in the folder, and the m3u8 lists them.

## Import of the old bot's history

`shelf manage import-qobuz-db <path>` reads the old bot's `qobuz_dl.db`, which was `/data/qobuz_dl.db` in the old container. Production has imported it, and the host no longer keeps the file. The command opens the file read only, so a read-only mount works. Each album and track row of its `downloads` table becomes a `downloaded_items` row with `source = 'qobuz'`, `kind` from `media_type`, `item_id` from `id`, `quality` from `file_format`, `bit_depth` and `sampling_rate`, `path` from `saved_path`, and `imported = 1`. `at` is the time of the import, because the old table has no time.

- The old container mounted `music/qobuz` at `/downloads`, so `/downloads/<folder>` becomes `<SHELF_QOBUZ_DIR>/<folder>`. A path under the music root becomes relative to it. Any other path stays as it is and is logged.
- Rows of other kinds are skipped and counted.
- qobuz-dl keeps a row per ID and quality, and `downloaded_items` one per item. The row of the highest quality wins.
- Existing rows are kept, so a second run changes nothing.
- `artist` is kept, and `album` becomes the title of an album row. A track row's `album` isn't the track's title, so its title stays empty. qobuz-dl added these columns late. A file it hasn't opened since has neither, and imports without them.
- The old table has no cover. The import stays offline, and the first skip of an imported item asks Qobuz for what it lacks.

The command prints how many rows it read, added, found on record, skipped for their kind, and kept with a path outside the music folder.
