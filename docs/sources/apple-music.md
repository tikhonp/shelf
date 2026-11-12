# Apple Music

Three pieces make up Apple Music in shelf:

- The catalog client in the `applemusic` module searches and resolves items over Apple's web catalog API.
- The amdl runner in the same module runs the upstream amdl 0.1.4 binary once per download.
- shelf-wrapper is a separate privileged container: wrapper-lite plus a supervisor that runs it and signs it in to an Apple ID. amdl needs wrapper-lite for every track.

amdl's repository has no licence, so shelf copies none of its code. The catalog client makes the same requests amdl makes, written from scratch.

The module is on when `SHELF_AMDL_CONFIG` names a readable amdl config. An empty value, a missing file or a broken one registers the source switched off, and a broken file is logged at start with the reason. The wrapper routes work whenever `SHELF_AMDL_CONFIG` is set, so the Status page can still sign in.

## Catalog client

- The bearer token comes from the web player, as in amdl: fetch `https://music.apple.com`, which redirects to a storefront's page, find the script `/assets/index~<hash>.js`, and take the first `eyJ…` JWT in it. The client keeps the token until 10 minutes before its `exp` claim, or for an hour when it has none, and scrapes again after a 401. A second 401 in a row is an error.
- Requests go to `https://amp-api.music.apple.com/v1/catalog/{storefront}/…` with `Authorization: Bearer <token>`, `Origin: https://music.apple.com` and a browser user agent. Each call has a context and a 15-second timeout. After a 429 the client waits for `Retry-After`, capped at a minute, or else 2, 4 and 8 seconds, and fails after the third wait.
- The storefront is the account's: the first entry of `regions` in wrapper-lite's `GET /status`, in lower case, kept for 10 minutes. When the wrapper doesn't answer or lists no region, the last known storefront stays; before there is one, the client uses `general.storefront` from the amdl config. It asks a wrapper that didn't answer again after a minute. The language is `general.language`, sent as `l`.
- The token travels only in the header, so no error carries it.

| Call | Request |
| --- | --- |
| Search | `search?term=&types=<one type>&limit=&offset=`, at most 25 per page. Apple gives no total, so the page's total counts one past its items while Apple has a `next` page. |
| Album | `albums/{id}?include=tracks,artists`, then the `next` links of its tracks |
| Artist | `artists/{id}?views=full-albums,singles,compilation-albums,appears-on-albums`, then each view's `next` links. When Apple answers 400 or 403, or without views: `artists/{id}`, then `artists/{id}/albums?limit=100&offset=` |
| Playlist | `playlists/{id}?include=tracks`, then the `next` links of its tracks |
| Song | `songs/{id}?include=albums,artists` |

The views become an artist's sections: Albums, EPs and singles, Compilations, and Appears on. Without views, the client splits the albums by their `isSingle` and `isCompilation` flags, and there is no Appears on.

Albums and playlists list their songs and leave out music videos. amdl still downloads such a video, as an `.mp4` in the release folder. amdl numbers the items of an album or playlist with the videos counted, and the runner maps its numbers to the songs' positions.

Most of the tests' answers in `internal/modules/applemusic/testdata` are hand-written in the API's shape. `testdata/live` holds answers Apple gave on 2026-11-04, without editorial notes: Radiohead's artist views with their `next` pages, and the album In Rainbows. A view answers 10 albums per page.

Quality comes from the item's `audioTraits`:

| Traits | Badge |
| --- | --- |
| contains `hi-res-lossless` | `Hi-Res Lossless` |
| contains `lossless` | `Lossless` |
| contains `atmos` | an extra `Dolby Atmos` badge, from the `atmos` flag |
| only lossy traits | `AAC` |

Apple doesn't give an item's bit depth or sample rate, so the badge names the tier. A written FLAC file's quality comes from its STREAMINFO block.

Artwork URLs are templates with `{w}x{h}`. The client fills them at 600x600; the UI may change that part of the URL for another size.

## Links

`ParseLink` accepts links on `music.apple.com`, `geo.music.apple.com`, `beta.music.apple.com`, `classical.music.apple.com` and `itunes.apple.com`, with or without the storefront and the slug, and with an `id` prefix on numeric IDs:

- `/<storefront>/album/<slug>/<id>` as an album, or as a track when the URL has `?i=<song id>`
- `/<storefront>/song/<slug>/<id>` as a track
- `/<storefront>/playlist/<slug>/pl.<id>` as a playlist
- `/<storefront>/artist/<slug>/<id>` as an artist

Music video and station links are refused with "Music videos and stations are not supported yet.": the module implements `source.LinkRefuser` for them, and `ParseLink` leaves them. The bot, `/api/resolve` and `POST /api/downloads` all answer with that text.

amdl gets `https://music.apple.com/<account storefront>/album/<id>`, `/song/<id>` or `/playlist/<id>`, so it reads the catalog the account can play.

## The amdl runner

### Config

Before each run the runner checks the base config from `SHELF_AMDL_CONFIG`. In production that is `secrets/blackberry/apple-music-config.yaml`, mounted read-only. When the file changed since the last write, or the rendered config is missing, the runner sets these keys and writes the result to `/data/amdl/config.yaml` with mode 0600, through a temporary file:

| Key | Value | Why |
| --- | --- | --- |
| `general.lite-server` | `SHELF_WRAPPER_URL` | The wrapper's address on the compose network. |
| `general.exit-on-error` | `true` | Otherwise amdl reads stdin after a pass in which a song failed, and runs the whole queue again. With stdin empty it would repeat that until the timeout. |
| `general.proxy` | empty | A proxy here would also carry the wrapper traffic. |
| `paths.alac`, `paths.atmos`, `paths.aac`, `paths.mv` | `/music/<SHELF_APPLE_DIR>` | The folder the old setup wrote to. |

Every other key keeps its value, its place and its comments, and the renderer adds a missing `general` or `paths` section. Init only reads and checks the file, since `manage` runs Init too. The production config, without its secrets:

- `general`: `language: en-GB`, `storefront: ru`, `max-memory-limit: 1024`, `exit-on-error: true`, empty `proxy` and `authorization-token`, and a `media-user-token` that only stations need.
- `media`: `get-m3u8-mode: hires`, `aac-type: aac-lc`, `alac-fix: true`, `alac-max: 192000`, `atmos-max: 2768`, music videos `atmos` audio up to 2160p.
- `paths`: `album-folder: "{ArtistName} - {AlbumName} ({ReleaseYear})[{Quality}]"`, `playlist-folder: "{PlaylistName}"`, `artist-folder: ""`, `song-file: "{SongNumer}. {SongName}"`, `limit-max: 200`, markers `[E]`, `[C]` and `[M]`.
- `metadata`: lyrics saved as `.lrc` files and embedded; artwork embedded at `5000x5000` in the original format; animated artwork saved; playlist tracks get their own album covers; sort tags and iTunes IDs written.
- `convert`: after download to `flac` with ffmpeg, the original `.m4a` removed, lossy-to-lossless conversion skipped.

### A run

- Command: `amdl --config /data/amdl/config.yaml --json <url>`, plus `--atmos` for the Dolby Atmos format or `--aac` for AAC.
- Working folder `/data/amdl`, with `XDG_CACHE_HOME=/data/amdl/.cache` and `TMPDIR=/data/amdl/tmp`, and stdin empty.
- Its own process group, a timeout of `SHELF_AMDL_TIMEOUT`, and the container's umask 022.
- stdout and stderr go line by line into the job log, except the closing list. Both `\n` and `\r` end a line, because amdl redraws its progress bars with `\r`. amdl writes no color codes into a pipe.
- amdl draws two bars, without the bar itself: `Decrypting...` while it fetches and decrypts a song, and `Downloading...` for AAC. A redraw looks like `Decrypting...  42%  (12/27 MB, 4.6 MB/s)`, and the last one like `Decrypting... 100%  (27/27 MB, 24 MB/s) [1s] Decrypted`. Redraws reach the log once every 5 seconds, and at 100%. Each redraw's count becomes the song's progress, at most once a second and always at 100%. The bar rounds the count and uses decimal units. It measures the stream amdl fetches, and the song's size becomes the file's size when the run ends.
- amdl prints `Track 3 of 12: songs` when it starts an item. The runner marks that song started. After it, `Track already exists locally.` or `Converted track already exists locally.` marks the song skipped. The runner keeps a line that says something failed, such as `Unavailable`, `⚠ Failed to extract info from manifest: …` or `Failed to run v4: …`, as the song's error. amdl's notes that it falls back to AAC, `Repaired 3 affected packets`, and its closing tally don't count as failures.
- A failure that cuts a bar short follows it on the same line: `Decrypting...   7%  (1.5/22 MB, 35 MB/s) Failed to run v4: lite-server /key returned code=500 msg=internal error`. The runner splits the bar off, and such a line always reaches the log.
- At the end amdl prints a JSON array of the songs it wrote or found complete: path, artist, artist ID, album, song. The runner reads the last line that parses as one, since a name may start with `[` too. Each entry matches the next song of the release with that name, in release order. The release folders are the folders of those files, relative to the music root.
- A release with no songs, such as a playlist of music videos, fails before amdl runs: amdl would save the videos and list nothing.
- A Dolby Atmos download fails before amdl runs with "<title> has no Dolby Atmos version" when no song of the release has the `atmos` trait. amdl would fail every song with `⚠ Failed to extract info from manifest: no codec found`, exit 0, print `null`, and leave a release folder with lyrics and a cover. A song without traits lets amdl try.
- A song amdl didn't list fails with its error from the output, or "amdl did not write this song". A run that lists no song fails the download with "amdl wrote no songs" and the last failure. A run that stops before its list, such as without a token, fails with its last lines.
- A run that ends with `Error detected, exiting...` went through every song, and some failed; amdl exits 1 before it prints its list. The runner runs amdl once more. amdl skips the files it finished and lists them, and a song may pass on the second try. A song the first run wrote counts as done, not skipped.
- A second failed run, or any other non-zero exit, fails the download with the last three error lines and no per-song results. A retry runs amdl again, which skips the files it finds complete and lists them.
- Cancel and the timeout send SIGTERM to the process group, then SIGKILL after 5 seconds. That stops ffmpeg too. Each run has its own timeout. The tool line in `shelf.txt` is `amdl 0.1.4, wrapper-lite storefront <storefront>`, and Apple Music calls a track a song.

The output rules come from amdl's source and from real runs, kept in `internal/modules/applemusic/amdl/testdata/live`: In Rainbows downloaded for the first time, again over its own files, and with `--atmos`.

### Formats

| Format | Label | amdl flag |
| --- | --- | --- |
| `applemusic:alac` | ALAC | none; the config converts the files to FLAC |
| `applemusic:atmos` | Dolby Atmos | `--atmos` |
| `applemusic:aac` | AAC | `--aac` |

### Artists

shelf's `resolve` step expands an artist link into one album download per album from the catalog client's views. amdl's own artist mode asks on stdin and also takes music videos, so shelf doesn't use it.

## shelf-wrapper

The image starts from the `ghcr.io/worldobservationlog/wrapper-lite` image, pinned by digest, and adds Debian's tini and the static `shelf-wrapper` binary from this repo. tini is PID 1: it passes Docker's SIGTERM to the supervisor and reaps the wrapper-lite processes a SIGKILL leaves behind. It replaces the image's `entrypoint.sh`, which checks an outdated account database path. Build it with `make build-wrapper-image`; it is amd64 only, and runs under emulation on an arm64 Mac.

### Supervisor

- It runs wrapper-lite in service mode: `/app/wrapper-lite-rootless --base-dir /data --host 0.0.0.0 --port 12340` from `/app`, plus the flags in `SHELF_WRAPPER_LITE_ARGS`, split on spaces, with single or double quotes around a value that has spaces. `--base-dir` is a path inside wrapper-lite's chroot, so `/data` there is `/app/rootfs/data` in the container, which is the mounted volume.
- Each run of wrapper-lite has its own process group. To stop it, the supervisor sends SIGINT to the launcher, the only signal it handles, and SIGKILL to the group after 10 seconds. Docker's SIGTERM starts that sequence, so compose needs no `stop_signal`.
- When service mode exits by itself, the state becomes `error` with the exit code and the last line, and the supervisor starts it again after a wait that doubles from 1 second to 1 minute. A run that lasted a minute starts the next wait from 1 second.
- It reads `GET /status` every second until a new run answers, then every 15 seconds. A poll that fails changes nothing, since a busy wrapper-lite can be slow to answer.
- In service mode, wrapper-lite logs a `credentialHandler:` line when Apple no longer accepts its session and asks for the Apple ID password again. Service mode has no password to give. Every `/m3u8` then fails with 404, and every `/key` with 500 and `Invalid CKC error.`, yet `/status` still lists the region. On that line the supervisor sets `error` with "Apple no longer accepts the wrapper's sign-in. Sign in again." It can notice only during a download, because wrapper-lite asks for the password only when amdl requests a stream.
- It keeps the last 1,000 lines of wrapper-lite's output and its own notes, and writes each to stdout as a JSON log line. The supervisor drops wrapper-lite's `request: GET /status` lines, which would fill the log. While a sign-in runs, the supervisor replaces its password with `[redacted]` in every line, and the supervisor's own note of the command shows `user:[redacted]`. wrapper-lite prints the first characters of its tokens when it loads its token cache, `token cache loaded (dev=<16 characters>... music=<16 characters>...)`, and again in its debug lines; the supervisor replaces each `dev=` and `music=` value with `[redacted]`.

### Control API

Port 12341. Every request needs `Authorization: Bearer <SHELF_WRAPPER_TOKEN>`; the token is compared in constant time. With the variable empty, every request answers 401. Errors are `{"error"}`.

| Route | Does |
| --- | --- |
| `GET /v1/state` | `{"state", "storefront", "since", "error", "code_deadline"}` |
| `POST /v1/login` | `{"username", "password"}`. Starts a sign-in and answers 202 with the state, already `signing_in`. 400 without both, 409 while one is running. |
| `POST /v1/code` | `{"code"}`, six digits, else 400. Writes the code file. 409 outside `awaiting_code`. |
| `POST /v1/restart` | Restarts service mode, or stops a running sign-in and starts service mode. Answers 202 with the state, already `starting`. |
| `GET /v1/log?after=` | `{"lines": [{"n", "time", "text"}]}`: the kept lines numbered above `after`. |

States:

| State | Meaning |
| --- | --- |
| `starting` | The supervisor started service mode, which hasn't answered yet. |
| `running` | Service mode answers and `/status` lists a region. |
| `signed_out` | Service mode answers, but `/status` lists no region. |
| `signing_in` | A sign-in run is going and hasn't asked for a code, or has its code. |
| `awaiting_code` | wrapper-lite asked for the 2FA code. `code_deadline` is 60 seconds after the request. |
| `error` | The last sign-in failed, service mode exited by itself, or Apple no longer accepts the session. `error` says why. |

`since` moves only when the state changes. An error from a crash clears when the next run answers. An error from a sign-in stays while `/status` lists no region, so the reason stays on show; a region, a new sign-in or a restart clears it. An expired session's error stays even though `/status` lists the region, and through crashes of service mode; only a new sign-in or a restart clears it.

### Sign-in

1. `POST /v1/login` stops service mode, removes a code file left from an earlier sign-in, and runs `/app/wrapper-lite-rootless --login <user>:<pass> --code-from-file --base-dir /data`. State `signing_in`.
2. When wrapper-lite logs `Enter your 2FA code`, the state becomes `awaiting_code` with a deadline 60 seconds later. wrapper-lite polls for `2fa.txt` every 3 seconds and gives up after 60.
3. `POST /v1/code` writes the code to a temporary file in `/app/rootfs/data`, sets mode 0600, and renames it to `2fa.txt`, so wrapper-lite never reads half of it. The state goes back to `signing_in`. wrapper-lite logs `Code file detected! Logging in...` and deletes the file.
4. `login complete, exiting` and exit code 0 mean success. The launcher exits 0 however the sign-in ends, so the line decides. The supervisor starts service mode, and the state becomes `running` once `/status` lists a region.
5. Otherwise the supervisor records the last line whose message starts with `auth error`, `server message`, `2FA code timeout` or `auth failed`, or else "the sign-in did not finish" with the exit code, sets `error`, and starts service mode again. An `auth error` never replaces a `server message`, because Apple's message is readable and the `auth error` after it can be only a code. wrapper-lite's lines start with the time and a level padded to five letters, `[INFO ]`, which the supervisor strips before it compares.

The shelf UI drives this through `/api/applemusic/wrapper/*`. Neither shelf nor the supervisor stores the Apple ID password, and neither logs it.

### In shelf

- `internal/modules/applemusic/wrapper` calls the control API with the token and a 10-second timeout, and wrapper-lite's `/status` for the storefront.
- The module's `Run` reads `GET /v1/state` every 15 seconds, every 2 while the wrapper starts or signs in, and at once after a route changed it. `Health` answers from the last read: OK only while `running`, with "Signed in, storefront RU"; otherwise "The wrapper is signed out", "The wrapper is starting", "Signing in", "Waiting for the code", the sign-in's error, "The wrapper does not answer", or "The wrapper refused Shelf's token. Check SHELF_WRAPPER_TOKEN." `Detail` holds the state.
- A change of health publishes a `source` event: `{"id": "applemusic", "ok", "message", "wrapper"}`, with `wrapper` null when the wrapper doesn't answer.
- The routes pass the supervisor's 4xx answers through with their message, answer 503 when it doesn't answer, and 502 when it refuses shelf's token.

### Limits to know

- wrapper-lite is amd64-only and needs a privileged container.
- The code window is 60 seconds. That limit is in wrapper-lite, not shelf.
- wrapper-lite serves `/key` and `/m3u8` one request at a time.
- Its Android libraries use the DNS servers 223.5.5.5 and 223.6.6.6, so the container needs outbound DNS to them.
- wrapper-lite derives its device ID from the Apple ID, so a second wrapper signed in to the same account looks like the same device, and its sign-in may disturb the other's session. Every wrapper on one account needs its own data folder and its own ID: `SHELF_WRAPPER_LITE_ARGS='--device-info "Music/5.0.2/Android/10/Pixel 8/7663314/en-US/en-US/<16 hex digits>"'`, wrapper-lite's default device info with another last part. In production shelf-wrapper has both.
- shelf-wrapper's data volume is `${SWINGMUSIC_DATA}/shelf-wrapper`, mounted at `/app/rootfs/data`.
