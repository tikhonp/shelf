# Deployment

Music Shelf runs as a Docker container next to Navidrome, or as a standalone binary. [compose.example.yaml](../compose.example.yaml) provides a starting Docker Compose file.

## Images and packages

| Package | Built from | Platforms | Runs as |
| --- | --- | --- | --- |
| `ghcr.io/tikhonp/shelf` | `Dockerfile`, target `prod`: `alpine:3.24` with `ca-certificates`, `chromaprint` (`fpcalc`), `ffmpeg`, `tzdata`, `amdl` from `ghcr.io/zhaarey/apple-music-downloader:0.1.4`, and the `shelf` binary | `linux/amd64`, `linux/arm64` | `10001:10001` |
| `ghcr.io/tikhonp/shelf-wrapper` | `Dockerfile`, target `wrapper`: pinned `ghcr.io/worldobservationlog/wrapper-lite` plus `tini` and `shelf-wrapper` | `linux/amd64` only | `root`, privileged |

Pushes to `main` publish `:main` and `:<sha>`. Pushing a `vX.Y.Z` tag such as `v1.3.0` publishes `:<tag>` (and advances `:v<major>.<minor>`, `:v<major>`, and `:latest` when it is the highest stable release in that scope) and creates a GitHub Release with prebuilt static `shelf` binaries for Linux and macOS plus `checksums.txt`. The GitHub workflow also supports `workflow_dispatch` to rebuild a branch or tag manually.

When deploying `shelf-wrapper` alongside `shelf`, keep both images on the same version tag because they share the control API types.

> **Maintainer note:** On the first push to a new GitHub account, GHCR packages default to private; switch both `shelf` and `shelf-wrapper` to Public in GitHub Package settings, and push (or dispatch) a `vX.Y.Z` tag so `:latest` exists. `.forgejo/workflows/ci.yaml` is the maintainer's separate private deploy pipeline kept temporarily alongside `.github/workflows/ci.yaml`.

### Running the standalone binary

Download the archive for your OS and architecture from GitHub Releases and verify `checksums.txt`. On macOS, remove the quarantine attribute on the unsigned binary before running it (`xattr -d com.apple.quarantine ./shelf`). Set `SHELF_DATA_DIR` and `SHELF_MUSIC_DIR` to writable directories, then run `shelf serve`.

Install `ffmpeg` on `PATH` for ReplayGain, browser transcoding, and cover checks. Install `fpcalc` (from Chromaprint) if you configure `SHELF_ACOUSTID_KEY`. For Apple Music downloads, `amdl` must also be on `PATH`.

## Docker Compose

Copy [compose.example.yaml](../compose.example.yaml) to `compose.yaml` and adjust:

- Mount persistent storage at `/data` for `shelf.db`, daily backups, and cover history.
- Mount your Navidrome music library at `/music` (read-write for Shelf; Navidrome can keep a read-only mount).
- Put `shelf` and `navidrome` on a shared Docker network, or point `SHELF_NAVIDROME_URL` at Navidrome's address. Set `SHELF_NAVIDROME_USER` and `SHELF_NAVIDROME_PASSWORD` to a Navidrome admin account so Shelf can read Navidrome's tag settings and trigger scans.
- Set `SHELF_TIMEZONE` to an IANA time zone (default `UTC`) for displayed dates, `shelf.txt`, and the 03:30 daily backup schedule.
- Hardening options (`read_only: true`, `cap_drop: [ALL]`, `no-new-privileges:true`, `/tmp` tmpfs) work out of the box. Use `shelf healthcheck` as the container healthcheck and a 30-second stop grace period.

Create the host data folder before starting Docker so it is not created as `root`:

```sh
sudo install -d -o 10001 -g 10001 -m 755 ./data
```

If your host already uses a different UID and GID for the music library, set `user: "<uid>:<gid>"` on the `shelf` service in `compose.yaml` instead of changing file ownership.

## Reverse proxy

Put Shelf behind a TLS-terminating reverse proxy such as nginx, Caddy, or Traefik:

- Forward to `shelf:8080` (or `127.0.0.1:8080`).
- Disable response buffering and raise the read timeout for Server-Sent Events (`/api/events` and `/api/downloads/{id}/log`). In nginx, set `proxy_buffering off;` and `proxy_read_timeout 1h;`.
- Do not overwrite `Content-Security-Policy`; Shelf sends its own.
- Raise the proxy's request body limit to at least 32 MiB if you upload large cover art.
- Forward client addresses (`X-Forwarded-For` or `X-Real-IP`). By default Shelf reads `X-Forwarded-For` from loopback, link-local, or private peers (`SHELF_IP_HEADER=x-forwarded-for`). Set `SHELF_IP_HEADER=x-real-ip` to use `X-Real-IP` instead, or `none` to always log the TCP peer.
- Set `SHELF_PUBLIC_URL` to the public HTTPS origin, such as `https://shelf.example.com`.

## Music folder permissions

- By default `shelf` runs as `10001:10001` with umask `022`, creating folders with `0755` and files with `0644`. `amdl` and `ffmpeg` inherit the umask, and `qobuz-dl-go` passes modes explicitly.
- Shelf refuses to write or delete any file owned by another UID, or with a group Shelf does not belong to, reporting it as locked without touching disk.
- Shelf never changes the owner or mode of a file it did not create. Besides its own temp files (`.shelf-tmp-*`), it deletes a release folder's `cover.*` when you replace or delete a cover (keeping a copy under `/data/library/covers` for undo), and deletes a release folder only when you confirm Delete in the Library tab and Shelf's UID owns every file inside it.
- Navidrome can keep a read-only mount because new files are world-readable.
- At startup Shelf runs a write probe in each top folder of `/music`, and every index walk counts locked files. `GET /api/library/index` reports folder writability and the `locked` count.

The easiest option is to set `user: "<uid>:<gid>"` on the `shelf` service in `compose.yaml` to match both the UID and GID of your existing library and data folders.

Alternatively, if you keep the default `10001:10001` container user, check ownership and read permissions on your library:

```sh
M=/path/to/music
find "$M" \( ! -uid 10001 -o ! -gid 10001 -o -type d ! -perm -0555 -o -type f ! -perm -0444 \) -print | head
```

> **Warning:** Running `chown -R 10001:10001` transfers ownership of every file in your library to UID/GID `10001`. Your host login user and desktop tools such as MusicBrainz Picard will lose write access unless the files are group- or world-writable. If you edit files locally on the host, prefer setting `user: "<uid>:<gid>"` in `compose.yaml` instead.

To transfer the library to `10001:10001` while keeping world-read access for Navidrome:

```sh
sudo chown -R 10001:10001 "$M"
sudo chmod -R u+rwX,go+rX "$M"
```

After restarting Shelf, `GET /api/library/index` should report `locked: 0` and every folder writable.

## Backups

Shelf's `platform.backup` job runs at 03:30 in `SHELF_TIMEZONE`. It uses `VACUUM INTO` on a read-only connection, syncs the temp file, and renames it to `/data/backups/shelf-YYYY-MM-DD.db`, keeping the 14 newest copies.

Always restore from a file in `/data/backups/`, not from an rsync copy of the live WAL database. Because `shelf.db` stores account credentials and sessions when authentication is enabled, treat backups as secrets.

## Optional account authentication

Account authentication is off by default (`SHELF_AUTH_PROVIDER=none`) so local setups and deployments behind reverse-proxy authentication work out of the box.

To enable Shelf accounts and passkeys:

1. Start Shelf once (or run `shelf manage migrate up`) so migrations are applied.
2. Set `SHELF_AUTH_PROVIDER=simple` and an HTTPS `SHELF_PUBLIC_URL`, then restart Shelf. Keep `SHELF_AUTH_SETUP_UI=false` on public URLs so nobody else can claim the first account before you do (`SHELF_AUTH_SETUP_UI=true` is the binary default and lets whoever reaches a fresh install first create an account).
3. Create your first account from the CLI (or open Shelf in your browser on a private/local URL if `SHELF_AUTH_SETUP_UI=true`):
   ```sh
   docker exec -it shelf shelf manage users add <username>
   ```
4. Sign in through the public HTTPS URL, optionally register a passkey on Status, and verify that logged-out requests to `/api/library/albums`, audio, covers, logs, and `/api/events` return `401`.
5. If you previously used proxy basic auth, remove it once sign-in works.

The CLI can reset passwords or passkeys while the server is stopped; both revoke existing sessions. Switching `SHELF_AUTH_PROVIDER` back to `none` opens access and purges saved sessions, so restore reverse-proxy auth before rolling back. See [auth.md](auth.md) for all commands and cookie rules.

## Optional downloads

Downloads, catalog search, Qobuz, Apple Music, and the Telegram bot are off by default. Without `SHELF_DOWNLOADS_ENABLED=true`, Shelf runs as a Navidrome library tagger and ignores `SHELF_PROXY_URL`, Qobuz, Apple Music, Telegram, and fake-source settings (while valid `SHELF_QOBUZ_DIR` and `SHELF_APPLE_DIR` values still control library folder attribution).

To enable downloads:

1. Set `SHELF_DOWNLOADS_ENABLED=true`.
2. **Qobuz:** set `SHELF_QOBUZ_TOKEN` to your Qobuz user auth token. See [sources/qobuz.md](sources/qobuz.md).
3. **Apple Music:** mount your base `amdl` YAML config read-only at `/config/amdl.yaml` (or set `SHELF_AMDL_CONFIG`), generate a shared secret (`openssl rand -base64 32`) for `SHELF_WRAPPER_TOKEN`, uncomment `shelf-wrapper` in `compose.yaml`, and sign the wrapper in from the Status page. See [sources/apple-music.md](sources/apple-music.md).
4. **Telegram bot:** set `SHELF_TELEGRAM_TOKEN` and comma-separated numeric IDs in `SHELF_TELEGRAM_USERS`. See [telegram.md](telegram.md).
5. **Legacy Qobuz history import (optional):** mount a directory containing `qobuz_dl.db` at `/import:ro` and run `docker exec shelf shelf manage import-qobuz-db /import/qobuz_dl.db`. Running it twice is safe. Remove the mount afterwards.

## Optional agent API

External agent support is off unless `SHELF_AGENT_ENABLED=true`. See [agents.md](agents.md) for scoped tokens, permissions, shared memory, and the Inbox review flow.

Enable it only with `SHELF_AUTH_PROVIDER=simple` or behind reverse-proxy authentication. With Shelf accounts enabled, your reverse proxy needs no special rule for agents: Shelf validates scoped bearer tokens under `/api/agent/v1/` and sessions everywhere else. If you use proxy basic auth instead, exempt only `/api/agent/` and validate the raw request URI in addition to the normalized path. Never give an agent a gateway password.
