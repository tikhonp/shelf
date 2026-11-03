# Deployment

shelf runs on blackberry in the `music` compose group of `~/projects/sh-blackberry`, reached through the sh-main gateway at `shelf.music.tikhonnnnn.com`. Changes to those repos and to sh-secrets are drafted for the user to commit.

## Images

| Image | Built from | Runs as |
| --- | --- | --- |
| `code.tikhonnnnn.com/tikhon/shelf` | `Dockerfile`, target `prod`: `alpine:3.22` with `ca-certificates`, `ffmpeg`, `tzdata`, amdl copied from `ghcr.io/zhaarey/apple-music-downloader:0.1.4`, and the shelf binary | `10001:10001` |
| `code.tikhonnnnn.com/tikhon/shelf-wrapper` | `Dockerfile`, target `wrapper`: the pinned `ghcr.io/tikhonp/wrapper-lite` image plus tini and `shelf-wrapper`, amd64 only | root, privileged |

Forgejo CI builds both on every push to `main` and tags them with the SHA, `main` and `latest`.

## sh-blackberry

`.env` gains:

```
SHELF_DATA=${DATA_VOLUME}/shelf/
BACKUP_SHELF_LOCAL=${LOCAL_BACKUP_VOLUME}/shelf/
```

`music.yaml` gains the app and its tunnel, modeled on `proxier.yaml`:

```yaml
  shelf:
    image: code.tikhonnnnn.com/tikhon/shelf:latest
    container_name: shelf
    restart: unless-stopped
    user: "10001:10001"
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    tmpfs: [/tmp]
    env_file:
      - ./secrets/blackberry/shelf.env
    environment:
      SHELF_PUBLIC_URL: "https://shelf.music.tikhonnnnn.com"
    volumes:
      - "${SHELF_DATA}:/data"
      - "${SWINGMUSIC_DATA}/music:/music"
      - ./secrets/blackberry/apple-music-config.yaml:/config/amdl.yaml:ro
    expose:
      - "8080"
    healthcheck:
      test: ["CMD", "/usr/local/bin/shelf", "healthcheck"]
      interval: 30s
      timeout: 5s
      retries: 3
    stop_grace_period: 30s
    depends_on:
      - apple-music-wrapper
    logging:
      driver: "json-file"
      options:
        max-size: "100m"
        max-file: "10"
    labels:
      - dev.dozzle.group=music

  ssht-shelf:
    image: jnovack/autossh:${AUTOSSH_V}
    container_name: ssht-shelf
    restart: unless-stopped
    env_file:
      - ./secrets/ssh-tunnel.env
    environment:
      - SSH_TUNNEL_PORT=10013
      - SSH_TARGET_HOST=shelf
      - SSH_TARGET_PORT=8080
    volumes:
      - "./secrets/ssh-key:/id_rsa:ro"
      - "./secrets/ssh-known-hosts:/known_hosts:ro"
    healthcheck:
      test: ["CMD", "pgrep", "-f", "^/usr/bin/ssh "]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 30s
    depends_on:
      - shelf
    logging:
      driver: "json-file"
      options:
        max-size: "100m"
        max-file: "10"
    labels:
      - dev.dozzle.group=music
      - "dev.dozzle.icon=${DOZZLE_ICON_AUTOSSH}"
```

`apple-music-wrapper` keeps its name, `privileged: true` and its volume, and changes:

```yaml
  apple-music-wrapper:
    image: code.tikhonnnnn.com/tikhon/shelf-wrapper:latest
    env_file:
      - ./secrets/blackberry/shelf-wrapper.env
    expose:
      - "12340"
      - "12341"
    # command and stop_signal are removed: the supervisor is the entrypoint
    # and turns SIGTERM into the SIGINT wrapper-lite expects.
```

`backup.yaml` gains a cron task that rsyncs `${SHELF_DATA}backups/` to `${BACKUP_SHELF_LOCAL}`, in a free `CRON_TASK_N` slot after 03:30, when shelf writes its daily copy.

sh-secrets gains `blackberry/shelf.env`:

```
SHELF_QOBUZ_TOKEN=
SHELF_WRAPPER_TOKEN=
SHELF_NAVIDROME_USER=
SHELF_NAVIDROME_PASSWORD=
SHELF_TELEGRAM_TOKEN=
SHELF_TELEGRAM_USERS=
```

and `blackberry/shelf-wrapper.env` with the same `SHELF_WRAPPER_TOKEN`, made with `openssl rand -base64 32`. Commit in sh-secrets first, then bump the submodule pointer.

`CLAUDE.md` in sh-blackberry gets the new music pipeline: shelf, shelf-wrapper, Picard, Navidrome, AudioMuse.

Before the first start, `${SHELF_DATA}` and `${SHELF_DATA}backups` must exist and belong to UID 10001. Otherwise Docker creates them as root and shelf can't write; that broke proxier's backups once.

## sh-main

- `nginx/conf.d/shelf.music.conf`, copied from `audiomuse.music.conf`: upstream `ssh-server:10013`, `server_name shelf.music.tikhonnnnn.com`, its certificate, `auth_basic` with the shared htpasswd, `proxy_buffering off` and `proxy_read_timeout 1h` for the event streams, `robots-disallow.conf`, and no CSP header, because shelf sends its own.
- Port 10013 in the `ssh-server` `expose` list in `compose.yaml`, with a `# shelf.music.tikhonnnnn.com` comment, and on the `PermitListen` line of `ssh-server/sshd_config.d/tunnel-restrict.conf`. Then `docker restart ssh-server`.
- The domain in `DOMAINS` in the `Makefile`, then `make shelf.music.tikhonnnnn.com` and `docker exec nginx nginx -s reload`. Add a DNS record first if `*.music` has no wildcard.
- A Music Shelf line in `nginx-contents/contents.html` and `contents-ru.html`.

## Music folder permissions

- shelf runs as `10001:10001`, the user that owns the music folder today. The old bot, `apple-music-api` and Picard all write as 10001.
- At startup shelf sets umask 022, so new folders are 0755 and new files 0644. amdl and ffmpeg inherit the umask. qobuz-dl-go passes 0755 and 0644 explicitly.
- shelf never changes the owner or mode of a file it did not create.
- Navidrome keeps its read-only mount. The files are world-readable, so Navidrome reads them whatever user it runs as.
- At startup shelf creates and removes a probe file in each source folder. The Status page reports a folder shelf can't write to or one owned by another UID.

Before the cutover, run this on blackberry and fix what it lists once, by hand:

```sh
find "${SWINGMUSIC_DATA}/music" \( ! -uid 10001 -o -type d ! -perm -0555 -o -type f ! -perm -0444 \) -print | head -50
```

## Backups

- shelf's `backup` job runs at 03:30 in the display time zone. It writes `VACUUM INTO` to a temp file on its own read-only connection, syncs it, and renames it to `/data/backups/shelf-YYYY-MM-DD.db`. It keeps 14.
- `backup.yaml` copies only `backups/` to the HDD, so rsync never reads the live database. The HDD then goes to the remote node with the rest.

## Cutover

1. CI publishes `shelf` and `shelf-wrapper`.
2. Run the permission check above and fix what it lists.
3. Switch `apple-music-wrapper` to shelf-wrapper. The lite API stays on port 12340, so the old bot and `apple-music-api` keep working while shelf is tested.
4. Start shelf with `SHELF_TELEGRAM_TOKEN` empty, because two processes can't poll one bot token.
5. Mount the old bot's data read-only, `${SWINGMUSIC_DATA}/music-downloader-bot:/import:ro`, and run `docker exec shelf shelf manage import-qobuz-db /import/qobuz_dl.db`.
6. Run the web part of [manual-checklist.md](manual-checklist.md).
7. Stop `music-download-bot`, move its token into `SHELF_TELEGRAM_TOKEN`, restart shelf, and run the Telegram part of the checklist.
8. Remove `music-download-bot`, `apple-music-api` and the import mount from `music.yaml`. Keep `${SWINGMUSIC_DATA}/music-downloader-bot` until it is no longer needed.

Rollback before step 8: stop shelf, put the old bot's token back, and start `music-download-bot` again. shelf-wrapper serves the old setup too, so it can stay.
