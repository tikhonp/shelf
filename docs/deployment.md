# Deployment

shelf runs on blackberry in the `music` compose group of `~/projects/sh-blackberry`, reached through the sh-main gateway at `shelf.music.tikhonnnnn.com`. Changes to those repos and to sh-secrets are drafted for the user to commit.

## Images

| Image | Built from | Runs as |
| --- | --- | --- |
| `code.tikhonnnnn.com/tikhon/shelf` | `Dockerfile`, target `prod`: `alpine:3.22` with `ca-certificates`, `ffmpeg`, `tzdata`, amdl copied from `ghcr.io/zhaarey/apple-music-downloader:0.1.4`, and the shelf binary | `10001:10001` |
| `code.tikhonnnnn.com/tikhon/shelf-wrapper` | `Dockerfile`, target `wrapper`: the pinned `ghcr.io/tikhonp/wrapper-lite` image plus tini and `shelf-wrapper`, amd64 only | root, privileged |

Forgejo CI builds both on every push to `main` and tags them with the SHA, `main` and `latest`.

## sh-blackberry

shelf has its own file, `shelf.yaml`, in `compose.yaml`'s `include:` list. It runs beside the old pipeline in `music.yaml`: `music-download-bot`, `apple-music-api` and `apple-music-wrapper` stay as they are until the user removes them, and shelf touches none of them. Both write into the same `music/qobuz` and `music/apple-music` folders.

Like the other music services, shelf keeps its data in `${SWINGMUSIC_DATA}`: `shelf/` for the app and `shelf-wrapper/` for the wrapper. `.env` doesn't change.

`shelf.yaml` holds three services, in the Dozzle group `shelf`:

| Service | What | Notes |
| --- | --- | --- |
| `shelf` | the app, modeled on `proxier.yaml` | `10001:10001`, read-only root, no capabilities, `/tmp` on tmpfs. Mounts `${SWINGMUSIC_DATA}/shelf` at `/data`, `${SWINGMUSIC_DATA}/music` at `/music`, `apple-music-config.yaml` at `/config/amdl.yaml`, and the old bot's folder read-only at `/import` for the import. Sets `SHELF_PUBLIC_URL` and points `SHELF_WRAPPER_URL` and `SHELF_WRAPPER_CONTROL_URL` at `shelf-wrapper`. Healthcheck `shelf healthcheck`, 30 seconds of stop grace. |
| `shelf-wrapper` | wrapper-lite under the supervisor | Privileged. Its own data folder, `${SWINGMUSIC_DATA}/shelf-wrapper`, and its own `--device-info` in `SHELF_WRAPPER_LITE_ARGS`, because wrapper-lite derives its device ID from the Apple ID and `apple-music-wrapper` uses the same one. It starts signed out, and the Status page signs it in. |
| `ssht-shelf` | the tunnel | Port 10013 to `shelf:8080`, with the pinned host key and the healthcheck every `ssht-*` has. |

Neither `shelf` nor `shelf-wrapper` has the `dev.dozzle.update` label. They share the control API's types, so both move together, by hand, with `docker compose pull shelf shelf-wrapper && docker compose up -d shelf shelf-wrapper`.

`backup.yaml`'s swingmusic task copies the whole of `${SWINGMUSIC_DATA}` to the HDD at 02:45, so it carries shelf's daily copies in `shelf/backups/` and the wrapper's data. It leaves out `shelf/shelf.db*`, the live database, which rsync could catch mid-write, and `shelf/amdl/`, which shelf writes on every start. The task runs before shelf's 03:30 copy, so each copy reaches the HDD the next night.

sh-secrets gains `blackberry/shelf.env`:

```
SHELF_QOBUZ_TOKEN=          the old bot's QOBUZ_PASSWORD, which holds the user auth token
SHELF_WRAPPER_TOKEN=        openssl rand -base64 32
SHELF_NAVIDROME_USER=       the u and p of the old bot's START_SCAN_ENDPOINT
SHELF_NAVIDROME_PASSWORD=
SHELF_TELEGRAM_TOKEN=       a new bot of shelf's own, not music-download-bot's
SHELF_TELEGRAM_USERS=       the old bot's WHITELIST_USERS
```

and `blackberry/shelf-wrapper.env` with the same `SHELF_WRAPPER_TOKEN`. Commit in sh-secrets first, then bump the submodule pointer.

`CLAUDE.md` in sh-blackberry describes the `shelf` group and counts port 10013 as taken.

Before the first start, `${SWINGMUSIC_DATA}/shelf` must exist and belong to UID 10001. Otherwise Docker creates it as root and shelf can't write. shelf creates `backups/` inside it.

```sh
sudo install -d -o 10001 -g 10001 -m 755 ~/.local/share/swingmusic/shelf
```

## sh-main

- `nginx/conf.d/shelf.music.conf`, copied from `audiomuse.music.conf`: upstream `ssh-server:10013`, `server_name shelf.music.tikhonnnnn.com`, its certificate, `auth_basic` with the shared htpasswd, `proxy_buffering off` and `proxy_read_timeout 1h` for the event streams, `robots-disallow.conf`, and no CSP header, because shelf sends its own.
- Port 10013 in the `ssh-server` `expose` list in `compose.yaml`, with a `# shelf.music.tikhonnnnn.com` comment, and on the `PermitListen` line of `ssh-server/sshd_config.d/tunnel-restrict.conf`. Then `docker restart ssh-server`.
- A DNS record for `shelf.music.tikhonnnnn.com`, since `*.music` has no wildcard. Then the domain in `DOMAINS` in the `Makefile`, `make shelf.music.tikhonnnnn.com` and `docker exec nginx nginx -s reload`.
- A Music Shelf line in `nginx-contents/contents.html` and `contents-ru.html`.

## Music folder permissions

- shelf runs as `10001:10001`, the user that owns the music folder today. The old bot, `apple-music-api` and Picard all write as 10001.
- At startup shelf sets umask 022, so new folders are 0755 and new files 0644. amdl and ffmpeg inherit the umask. qobuz-dl-go passes 0755 and 0644 explicitly.
- shelf never changes the owner or mode of a file it did not create.
- Navidrome keeps its read-only mount. The files are world-readable, so Navidrome reads them whatever user it runs as.
- At startup shelf creates and removes a probe file in each source folder. The Status page reports a folder shelf can't write to or one owned by another UID.

Before the first start, run this on blackberry and fix what it lists once, by hand:

```sh
find "${SWINGMUSIC_DATA}/music" \( ! -uid 10001 -o -type d ! -perm -0555 -o -type f ! -perm -0444 \) -print | head -50
```

## Backups

- shelf's `backup` job runs at 03:30 in the display time zone. It writes `VACUUM INTO` to a temp file on its own read-only connection, syncs it, and renames it to `/data/backups/shelf-YYYY-MM-DD.db`. It keeps 14.
- `backup.yaml`'s swingmusic task copies `backups/` to the HDD and leaves out the live database, so rsync never reads it mid-write. The HDD then goes to the remote node with the rest.

## Rollout

The old pipeline keeps running through all of this, so nothing needs a rollback. Stopping shelf leaves the old pipeline as it was.

1. CI publishes `shelf` and `shelf-wrapper`.
2. Run the permission check above and fix what it lists.
3. Create shelf's data folders as above, add the DNS record, deploy sh-main, and start `shelf.yaml` with `SHELF_TELEGRAM_TOKEN` empty.
4. Sign `shelf-wrapper` in from the Status page.
5. Run `docker exec shelf shelf manage import-qobuz-db /import/qobuz_dl.db`. The old bot keeps writing to its database, so the import is a snapshot. An album the old bot gets later isn't in shelf's history, so shelf doesn't skip it outright, but every track already on disk ends `skipped`.
6. Run the web part of [manual-checklist.md](manual-checklist.md).
7. Create shelf's bot with BotFather, put its token in `SHELF_TELEGRAM_TOKEN`, recreate shelf, and run the Telegram part of the checklist.
8. Remove the `/import` mount from `shelf.yaml`.

Removing `music-download-bot`, `apple-music-api` and `apple-music-wrapper` from `music.yaml` is a later change, made by the user. Keep `${SWINGMUSIC_DATA}/music-downloader-bot` until it is no longer needed.
