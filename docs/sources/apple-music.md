# Apple Music

Three pieces make up Apple Music in shelf:

- The catalog client in the `applemusic` module searches and resolves items over Apple's web catalog API.
- The amdl runner in the same module runs the upstream amdl 0.1.4 binary once per download.
- shelf-wrapper is a separate privileged container: wrapper-lite plus a supervisor that runs it and signs it in to an Apple ID. amdl needs wrapper-lite for every track.

amdl's repository has no licence, so shelf copies none of its code. The catalog client makes the same requests amdl makes, written from scratch.

## Catalog client

- The bearer token comes from the web player, as in amdl: fetch `https://music.apple.com`, find the script `/assets/index~<hash>.js`, and take the `eyJ…` JWT from it. The client caches the token until its `exp` claim and scrapes again after a 401.
- Requests go to `https://amp-api.music.apple.com/v1/catalog/{storefront}/…` with `Authorization: Bearer <token>`, `Origin: https://music.apple.com` and a browser user agent. Each call has a context and a 15-second timeout. After a 429 the client backs off and retries.
- The storefront is the account's: the first entry of `regions` in wrapper-lite's `GET /status`. If the wrapper doesn't answer, `general.storefront` from the amdl config is used. The language is `general.language`, sent as `l`.

| Call | Request |
| --- | --- |
| Search | `search?term=&types=albums,songs,artists,playlists&limit=&offset=` |
| Album | `albums/{id}?include=tracks,artists,record-labels&extend=extendedAssetUrls`, then `albums/{id}/tracks?offset=` for long albums |
| Artist | `artists/{id}?views=full-albums,singles,compilation-albums,appears-on-albums`, paging each view through its `next`; if views are refused, `artists/{id}/albums?limit=100&offset=` |
| Playlist | `playlists/{id}?include=tracks`, then `playlists/{id}/tracks?offset=` |
| Song | `songs/{id}?include=albums,artists` |

Step 1e checks the artist views against the live API and records the answers as test fixtures.

Quality comes from the item's `audioTraits`:

| Traits | Badge |
| --- | --- |
| contains `hi-res-lossless` | `Hi-Res Lossless` |
| contains `lossless` | `Lossless` |
| contains `atmos` | an extra `Dolby Atmos` badge |
| only lossy traits | `AAC` |

Artwork URLs are templates with `{w}x{h}`; the UI asks for the size it shows.

## Links

`ParseLink` accepts `music.apple.com` and `itunes.apple.com` links:

- `/<storefront>/album/<slug>/<id>` as an album, or as a track when the URL has `?i=<song id>`
- `/<storefront>/song/<slug>/<id>` as a track
- `/<storefront>/playlist/<slug>/pl.<id>` as a playlist
- `/<storefront>/artist/<slug>/<id>` as an artist

Music video and station links are refused with "Music videos and stations are not supported yet."

## The amdl runner

### Config

At startup the runner reads the base config from `SHELF_AMDL_CONFIG`. In production that is today's `secrets/blackberry/apple-music-config.yaml`, mounted read-only. The runner overrides four things and writes the result to `/data/amdl/config.yaml` with mode 0600:

| Key | Value | Why |
| --- | --- | --- |
| `general.lite-server` | `SHELF_WRAPPER_URL` | The wrapper's address on the compose network. |
| `general.exit-on-error` | `true` | Otherwise a failed track makes amdl wait on stdin and rerun the whole queue. |
| `general.proxy` | empty | A proxy here would also carry the wrapper traffic. |
| `paths.alac`, `paths.atmos`, `paths.aac`, `paths.mv` | `/music/<SHELF_APPLE_DIR>` | Files land where they land today. |

Everything else keeps its current value. Today's production config, with secrets left out:

- `general`: `language: en-GB`, `storefront: ru`, `max-memory-limit: 1024`, `exit-on-error: true`, empty `proxy` and `authorization-token`, and a `media-user-token` that only stations need.
- `media`: `get-m3u8-mode: hires`, `aac-type: aac-lc`, `alac-fix: true`, `alac-max: 192000`, `atmos-max: 2768`, music videos `atmos` audio up to 2160p.
- `paths`: `album-folder: "{ArtistName} - {AlbumName} ({ReleaseYear})[{Quality}]"`, `playlist-folder: "{PlaylistName}"`, `artist-folder: ""`, `song-file: "{SongNumer}. {SongName}"`, `limit-max: 200`, markers `[E]`, `[C]` and `[M]`.
- `metadata`: lyrics saved as `.lrc` files and embedded; artwork embedded at `5000x5000` in the original format; animated artwork saved; playlist tracks get their own album covers; sort tags and iTunes IDs written.
- `convert`: after download to `flac` with ffmpeg, the original `.m4a` removed, lossy-to-lossless conversion skipped.

### A run

- Command: `amdl --config /data/amdl/config.yaml --json <url>`, plus `--atmos` for the Dolby Atmos format or `--aac` for AAC.
- Working folder `/data/amdl`, with `XDG_CACHE_HOME=/data/amdl/.cache` and `TMPDIR=/data/amdl/tmp`.
- Its own process group, a timeout of `SHELF_AMDL_TIMEOUT`, and the container's umask 022.
- stdout and stderr go line by line into the job log. Both `\n` and `\r` end a line, because amdl draws progress bars with `\r`.
- Track progress comes from amdl's per-track lines. Step 1e records a real run's output as a fixture and writes the parsing rules against it.
- At the end amdl prints a JSON array of added tracks: path, artist, artist ID, album, song. The runner takes the files and release folders from it.
- A non-zero exit fails the download with the last error lines. A panic in amdl ends up the same way.
- Cancel sends SIGTERM to the process group, then SIGKILL after 5 seconds. That stops ffmpeg too.

### Formats

| Format | Label | amdl flag |
| --- | --- | --- |
| `applemusic:alac` | ALAC | none; the config converts the files to FLAC |
| `applemusic:atmos` | Dolby Atmos | `--atmos` |
| `applemusic:aac` | AAC | `--aac` |

### Artists

An artist link is expanded by shelf's `resolve` step into one album download per album from the catalog client's views. amdl's own artist mode asks on stdin and also takes music videos, so shelf doesn't use it.

## shelf-wrapper

The image starts from the `ghcr.io/tikhonp/wrapper-lite` image, pinned by digest, and uses the static `shelf-wrapper` binary from this repo as its entrypoint. That replaces the image's `entrypoint.sh`, which checks an outdated account database path.

### Supervisor

- It runs wrapper-lite in service mode: `/app/wrapper-lite-rootless --base-dir /data --host 0.0.0.0 --port 12340` from `/app`. `--base-dir` is a path inside wrapper-lite's chroot, so `/data` there is `/app/rootfs/data` in the container, which is the mounted volume.
- When service mode exits, the supervisor starts it again with backoff from 1 second to 1 minute.
- To stop it, the supervisor sends SIGINT, the only signal the launcher handles, and SIGKILL after 10 seconds. Docker's SIGTERM to the supervisor starts that sequence, so compose needs no `stop_signal`.
- It keeps the last 1,000 lines of wrapper-lite output, with passwords removed from any logged command line.

### Control API

Port 12341. Every request needs `Authorization: Bearer <SHELF_WRAPPER_TOKEN>`; the token is compared in constant time.

| Route | Does |
| --- | --- |
| `GET /v1/state` | `{"state", "storefront", "since", "error", "code_deadline"}` |
| `POST /v1/login` | `{"username", "password"}`. Starts a sign-in. 409 while one is running. |
| `POST /v1/code` | `{"code"}`, six digits. Writes `/app/rootfs/data/2fa.txt`. 409 outside `awaiting_code`. |
| `POST /v1/restart` | Restarts service mode. |
| `GET /v1/log?after=` | Log lines after a line number. |

States:

| State | Meaning |
| --- | --- |
| `running` | Service mode answers and `/status` lists a region. |
| `signed_out` | Service mode answers, but `/status` lists no region. |
| `signing_in` | A sign-in run is going and hasn't asked for a code. |
| `awaiting_code` | wrapper-lite asked for the 2FA code. `code_deadline` is 60 seconds after the request. |
| `error` | The last sign-in failed or service mode keeps exiting. `error` says why. |

### Sign-in

1. `POST /v1/login` stops service mode and runs `/app/wrapper-lite-rootless --login <user>:<pass> --code-from-file --base-dir /data`. State `signing_in`.
2. When wrapper-lite logs `Enter your 2FA code`, the state becomes `awaiting_code` with a deadline 60 seconds later. wrapper-lite polls for `2fa.txt` every 3 seconds and gives up after 60.
3. `POST /v1/code` writes the code. wrapper-lite logs `Code file detected! Logging in...` and deletes the file.
4. `login complete, exiting` and exit code 0 mean success. The supervisor starts service mode, and the state becomes `running` once `/status` lists a region.
5. Any other exit records the last line that starts with `auth error`, `server message` or `2FA code timeout`, or else the exit code, sets `error`, and starts service mode again.

The shelf UI drives this through `/api/applemusic/wrapper/*`. Neither shelf nor the supervisor stores the Apple ID password.

### Limits to know

- wrapper-lite is amd64-only and needs a privileged container.
- The code window is 60 seconds. That limit is in wrapper-lite, not shelf.
- wrapper-lite serves `/key` and `/m3u8` one request at a time.
- Its Android libraries use the DNS servers 223.5.5.5 and 223.6.6.6, so the container needs outbound DNS to them.
- The data volume stays `${SWINGMUSIC_DATA}/apple-music-lite-data` at `/app/rootfs/data`, so the current session survives the switch to shelf-wrapper.
