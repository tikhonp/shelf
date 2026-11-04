# Deployment

shelf runs on blackberry from `shelf.yaml` in `~/projects/sh-blackberry`. The sh-main gateway serves it at `shelf.music.tikhonnnnn.com`, and the bot is `@music_shelf_bot`. Changes to sh-blackberry, sh-main and sh-secrets are drafted for the user to commit.

## Images

| Image | Built from | Runs as |
| --- | --- | --- |
| `code.tikhonnnnn.com/tikhon/shelf` | `Dockerfile`, target `prod`: `alpine:3.22` with `ca-certificates`, `ffmpeg`, `tzdata`, amdl copied from `ghcr.io/zhaarey/apple-music-downloader:0.1.4`, and the shelf binary | `10001:10001` |
| `code.tikhonnnnn.com/tikhon/shelf-wrapper` | `Dockerfile`, target `wrapper`: the pinned `ghcr.io/tikhonp/wrapper-lite` image plus tini and `shelf-wrapper`, amd64 only | root, privileged |

Forgejo CI builds both on every push to `main` and tags them with the SHA, `main` and `latest`.

## sh-blackberry

`shelf.yaml` is in `compose.yaml`'s `include:` list and holds three services, in the Dozzle group `shelf`:

| Service | What | Notes |
| --- | --- | --- |
| `shelf` | the app | `10001:10001`, read-only root, no capabilities, `/tmp` on tmpfs. Mounts `${SWINGMUSIC_DATA}/shelf` at `/data`, `${SWINGMUSIC_DATA}/music` at `/music`, and `apple-music-config.yaml` at `/config/amdl.yaml`. Sets `SHELF_PUBLIC_URL` and points `SHELF_WRAPPER_URL` and `SHELF_WRAPPER_CONTROL_URL` at `shelf-wrapper`. Healthcheck `shelf healthcheck`, 30 seconds of stop grace. |
| `shelf-wrapper` | wrapper-lite under the supervisor | Privileged. Its own data folder, `${SWINGMUSIC_DATA}/shelf-wrapper`, and its own `--device-info` in `SHELF_WRAPPER_LITE_ARGS`. The Status page signs it in. |
| `ssht-shelf` | the tunnel | Port 10013 to `shelf:8080`, with the pinned host key and the healthcheck every `ssht-*` has. |

Neither `shelf` nor `shelf-wrapper` has the `dev.dozzle.update` label. They share the control API's types, so update both together:

```sh
docker compose pull shelf shelf-wrapper && docker compose up -d shelf shelf-wrapper
```

sh-secrets holds `blackberry/shelf.env`:

```
SHELF_QOBUZ_TOKEN=          the Qobuz user auth token
SHELF_WRAPPER_TOKEN=        openssl rand -base64 32
SHELF_NAVIDROME_USER=       a Navidrome admin
SHELF_NAVIDROME_PASSWORD=
SHELF_TELEGRAM_TOKEN=       the token of `@music_shelf_bot`
SHELF_TELEGRAM_USERS=       the user IDs the bot answers
```

and `blackberry/shelf-wrapper.env` with the same `SHELF_WRAPPER_TOKEN`. Commit changes in sh-secrets first, then bump the submodule pointer.

## sh-main

- `nginx/conf.d/shelf.music.conf`: upstream `ssh-server:10013`, `auth_basic` with the shared htpasswd, `proxy_buffering off` and `proxy_read_timeout 1h` for the event streams, `robots-disallow.conf`, and no CSP header, because shelf sends its own.
- Port 10013 in the `ssh-server` `expose` list in `compose.yaml` and on the `PermitListen` line of `ssh-server/sshd_config.d/tunnel-restrict.conf`.
- `shelf.music.tikhonnnnn.com` has its own DNS record, since `*.music` has no wildcard, and its own certificate from `DOMAINS` in the `Makefile`.
- A Music Shelf line in `nginx-contents/contents.html` and `contents-ru.html`.

## Music folder permissions

- shelf runs as `10001:10001` with umask 022, so new folders are 0755 and new files 0644. amdl and ffmpeg inherit the umask, and qobuz-dl-go passes the modes explicitly.
- shelf writes only into `qobuz/` and `apple-music/`, which belong to 10001. The genre folders keep their mixed owners, so the login user and Picard keep writing there. The login user needs sudo to write into the two source folders.
- shelf never changes the owner or mode of a file it did not create.
- Navidrome has a read-only mount and reads the files because they are world-readable.
- At startup shelf creates and removes a probe file in each source folder. The Status page reports a folder shelf can't write to or one owned by another UID.

This command prints nothing while the source folders are right:

```sh
M=~/.local/share/swingmusic/music
find "$M/qobuz" "$M/apple-music" \( ! -uid 10001 -o -type d ! -perm -0555 -o -type f ! -perm -0444 \) -print | head
```

To fix them:

```sh
sudo chown -R 10001:10001 "$M/qobuz" "$M/apple-music"
sudo chmod -R u+rwX,go+rX,go-w "$M/qobuz" "$M/apple-music"
```

## Backups

- shelf's `backup` job runs at 03:30 in the display time zone. It writes `VACUUM INTO` to a temp file on its own read-only connection, syncs it, and renames it to `/data/backups/shelf-YYYY-MM-DD.db`. It keeps 14.
- `backup.yaml`'s swingmusic task copies all of `${SWINGMUSIC_DATA}` to the HDD at 02:45, shelf's live database and the wrapper's data included. rsync can catch the live database mid-write, so a restore starts from a daily copy. The task runs before shelf's copy, so each copy reaches the HDD the next night. The HDD then goes to the remote node with the rest.

## Setting up from scratch

1. Give `qobuz/` and `apple-music/` to 10001 as above.
2. Create shelf's data folder for UID 10001, or Docker creates it as root and shelf can't write: `sudo install -d -o 10001 -g 10001 -m 755 ~/.local/share/swingmusic/shelf`.
3. Deploy sh-main and start `shelf.yaml`.
4. Sign `shelf-wrapper` in from the Status page.
5. To bring in the old bot's history, mount `${SWINGMUSIC_DATA}/music-downloader-bot` at `/import` and run `docker exec shelf shelf manage import-qobuz-db /import/qobuz_dl.db`. A second run changes nothing. Remove the mount afterwards.
6. Run [manual-checklist.md](manual-checklist.md).
