# Deployment

shelf runs on blackberry from `shelf.yaml` in `~/projects/sh-blackberry`. The sh-main gateway serves it at `shelf.music.tikhonnnnn.com`, and the bot is `@music_shelf_bot`. Changes to sh-blackberry, sh-main and sh-secrets are drafted for the user to commit.

## Images

| Image | Built from | Runs as |
| --- | --- | --- |
| `code.tikhonnnnn.com/tikhon/shelf` | `Dockerfile`, target `prod`: `alpine:3.24` with `ca-certificates`, `chromaprint` for `fpcalc`, `ffmpeg`, `tzdata`, amdl copied from `ghcr.io/zhaarey/apple-music-downloader:0.1.4`, and the shelf binary | `10001:10001` |
| `code.tikhonnnnn.com/tikhon/shelf-wrapper` | `Dockerfile`, target `wrapper`: the pinned `ghcr.io/worldobservationlog/wrapper-lite` image plus tini and `shelf-wrapper`, amd64 only | root, privileged |

Forgejo CI builds both on every push to `main`, tags them with the SHA, `main` and `latest`, and sets the version to `main-<sha7>`. A pushed `v*` git tag, such as `v1.0.0`, builds both as `:v1.0.0`, sets the version to `v1.0.0`, and leaves `latest` alone. To release:

```sh
git tag v1.0.0 && git push origin v1.0.0
```

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
SHELF_ACOUSTID_KEY=         an application key from acoustid.org, optional
```

and `blackberry/shelf-wrapper.env` with the same `SHELF_WRAPPER_TOKEN`. Commit changes in sh-secrets first, then bump the submodule pointer.

## sh-main

- `nginx/conf.d/shelf.music.conf`: upstream `ssh-server:10013`, no basic auth, because Shelf's accounts guard it, `proxy_buffering off` and `proxy_read_timeout 1h` for the event streams, `robots-disallow.conf`, and no CSP header, because shelf sends its own.
- Port 10013 in the `ssh-server` `expose` list in `compose.yaml` and on the `PermitListen` line of `ssh-server/sshd_config.d/tunnel-restrict.conf`.
- `shelf.music.tikhonnnnn.com` has its own DNS record, since `*.music` has no wildcard, and its own certificate from `DOMAINS` in the `Makefile`.
- A Music Shelf line in `nginx-contents/contents.html` and `contents-ru.html`.

## Music folder permissions

- shelf runs as `10001:10001` with umask 022, so new folders are 0755 and new files 0644. amdl and ffmpeg inherit the umask, and qobuz-dl-go passes the modes explicitly.
- Downloads write only into `qobuz/` and `apple-music/`. The tag editor writes in the whole library, so after the chown below every file and folder belongs to 10001. shelf refuses a Save of a file another UID or group owns as locked, and writes nothing. The login user and Picard keep writing in the genre folders, because no file of UID 1000 there lacks world-write. The login user needs sudo to write into the two source folders.
- shelf never changes the owner or mode of a file it did not create. Besides its own temp files, it deletes a release folder's `cover.*` when the owner replaces or deletes a cover, keeping a copy under `/data/library/covers` for undo, and a whole release folder the owner deletes from the Library tab, when 10001 owns everything in it. A deleted folder has no copy, so only the backups can bring it back.
- Navidrome has a read-only mount and reads the files because they are world-readable.
- At startup shelf creates and removes a probe file in each top folder of the music folder, and each walk of the index counts the files another UID owns. `GET /api/library/index` reports a folder shelf can't write to and its locked files.

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

The tag editor needs the genre folders too, the top folders other than `qobuz` and `apple-music`. This prints them, then anything in them that UID 1000 owns without world-write, which the chown would take from the login user. The second list should be empty:

```sh
find "$M" -mindepth 1 -maxdepth 1 -type d ! -name qobuz ! -name apple-music ! -name '.*'
find "$M" -mindepth 1 -maxdepth 1 -type d ! -name qobuz ! -name apple-music ! -name '.*' -exec find {} -uid 1000 ! -perm -0002 \; | head
```

The chown gives them owner and group 10001. A write keeps a file's group, and shelf can give a file only a group it is in, so a file of group 1000 or root is locked. Modes stay, and the login user keeps access through world-write:

```sh
sudo find "$M" -mindepth 1 -maxdepth 1 -type d ! -name qobuz ! -name apple-music ! -name '.*' -exec chown -R 10001:10001 {} +
```

Afterwards `GET /api/library/index` shows `locked: 0` and every folder `writable` once shelf restarts.

## Backups

- shelf's `backup` job runs at 03:30 in the display time zone. It writes `VACUUM INTO` to a temp file on its own read-only connection, syncs it, and renames it to `/data/backups/shelf-YYYY-MM-DD.db`. It keeps 14.
- `backup.yaml`'s swingmusic task copies all of `${SWINGMUSIC_DATA}` to the HDD at 02:45, shelf's live database and the wrapper's data included. rsync can catch the live database mid-write, so a restore starts from a daily copy. The task runs before shelf's copy, so each copy reaches the HDD the next night. The HDD then goes to the remote node with the rest.

## Setting up from scratch

1. Give the music folder to 10001 as above: `qobuz/` and `apple-music/`, then the genre folders.
2. Create shelf's data folder for UID 10001, or Docker creates it as root and shelf can't write: `sudo install -d -o 10001 -g 10001 -m 755 ~/.local/share/swingmusic/shelf`.
3. Deploy sh-main and start `shelf.yaml`.
4. Sign `shelf-wrapper` in from the Status page.
5. To bring in the old bot's history, mount a folder holding `qobuz_dl.db` at `/import` and run `docker exec shelf shelf manage import-qobuz-db /import/qobuz_dl.db`. A second run changes nothing. Remove the mount afterwards. Production has imported it, and the host no longer keeps the file.
6. Run [manual-checklist.md](manual-checklist.md).

## Optional account authentication

Production made this cutover and no longer has gateway basic auth. The steps stay for a new deployment. The default provider none keeps a deployment behind gateway basic auth working.

1. Back up `shelf.db` and deploy the new image with provider none. Starting the server applies migrations. If creating accounts before the first start, run `shelf manage migrate up`.
2. Run `docker exec -it shelf shelf manage users add <username>` and enter the password twice. Create another account if needed.
3. Set `SHELF_AUTH_PROVIDER=simple` and the public HTTPS origin in `SHELF_PUBLIC_URL`, then restart Shelf. Keep gateway basic auth during the check.
4. Verify password access through the public address, add a passkey on Status, sign out, and verify passkey access. Verify logged-out audio, covers, logs, and events return 401.
5. Remove `auth_basic` from the Shelf gateway once those checks pass. Keep TLS, streaming proxy settings, and the existing security headers. Gateway repository changes remain a draft for the owner to commit.

The CLI can reset a password or all passkeys without the server running. Both revoke sessions. Switching serve back to none opens app access and clears sessions, so keep gateway protection if using that rollback. A database restore restores credentials and sessions from that backup. Treat backups as secrets.

[auth.md](auth.md) lists commands, cookie behavior, and the effect of changing the hostname. Local HTTP development keeps provider none.


## Optional agent API

Agent support is off unless `SHELF_AGENT_ENABLED=true`. Production sets it in `sh-blackberry/shelf.yaml`. [agents.md](agents.md) describes credentials and review.

Turn it on only with `SHELF_AUTH_PROVIDER=simple` or behind gateway basic auth. The gateway needs no rule for agents once Shelf's accounts guard it: Shelf checks a scoped bearer token on `/api/agent/` and a session on every other route. A deployment that keeps gateway basic auth must exempt `/api/agent/` from it and check the raw request URI too, because nginx matches the normalized path. Never give an agent a gateway password.
