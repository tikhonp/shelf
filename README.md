# Music Shelf

Music Shelf manages a Navidrome music library from a single Go binary with SQLite and an embedded web UI.

- Edit tags, covers, and lyrics in the browser with suggestions from file names, MusicBrainz, Cover Art Archive, LRCLIB, AcoustID, and optional sources or agents.
- Check albums against Navidrome's tagging guide, run ReplayGain in bulk, and undo any save.
- Trigger Navidrome scans after edits, and remove missing tracks after deleting a release folder.
- Enable optional accounts and passkeys with `SHELF_AUTH_PROVIDER=simple`.
- Enable optional Qobuz and Apple Music downloads, catalog search, and the Telegram bot with `SHELF_DOWNLOADS_ENABLED=true`.
- Enable optional external library agents and the Inbox review workflow with `SHELF_AGENT_ENABLED=true`.

## Quick start

Copy [compose.example.yaml](compose.example.yaml) to `compose.yaml` (and [.env.example](.env.example) to `.env` for secrets), create `./data` owned by `10001:10001`, point the `/music` volume at your Navidrome music folder, set your Navidrome URL and credentials, and start the container:

```sh
sudo install -d -o 10001 -g 10001 -m 755 ./data
docker compose up -d
```

The container image runs as UID `10001` and GID `10001`. Either set `user: "<uid>:<gid>"` in `compose.yaml` to match both the UID and GID of your library and data folders, or change ownership of those folders to `10001:10001`.

With `SHELF_AUTH_PROVIDER=simple` and an HTTPS `SHELF_PUBLIC_URL`, `compose.example.yaml` disables browser sign-up (`SHELF_AUTH_SETUP_UI=false`) so nobody else can claim the first account on a public URL. Create your first account from the CLI:

```sh
docker exec -it shelf shelf manage users add <username>
```

For a local HTTP trial without a reverse proxy, set `SHELF_AUTH_PROVIDER=none` and leave `SHELF_PUBLIC_URL` empty.

See [docs/deployment.md](docs/deployment.md) for reverse proxy settings, folder permissions, backups, and optional modules.

## Packages

Container images are published to GitHub Container Registry:

- `ghcr.io/tikhonp/shelf` (`linux/amd64`, `linux/arm64`)
- `ghcr.io/tikhonp/shelf-wrapper` (`linux/amd64`, optional, required only for Apple Music downloads)

Prebuilt static `shelf` binaries and SHA-256 checksums for Linux and macOS are attached to each [GitHub Release](https://github.com/tikhonp/shelf/releases). Native runs need `ffmpeg` on `PATH` for ReplayGain, transcoding, and cover checks, plus `fpcalc` (Chromaprint) if you set `SHELF_ACOUSTID_KEY`.

## Configuration

Every setting is an optional `SHELF_*` environment variable. See [.env.example](.env.example) and [docs/architecture.md#configuration](docs/architecture.md#configuration) for the full list.

- **Library tagging** is always on. Set `SHELF_NAVIDROME_URL`, `SHELF_NAVIDROME_USER`, and `SHELF_NAVIDROME_PASSWORD` (an admin account) so Shelf reads Navidrome's tag settings and triggers scans.
- **Accounts** are off by default (`SHELF_AUTH_PROVIDER=none`). Set `SHELF_AUTH_PROVIDER=simple` and an HTTPS `SHELF_PUBLIC_URL` for built-in password and passkey sign-in.
- **Downloads** are off by default (`SHELF_DOWNLOADS_ENABLED=false`). Set `SHELF_DOWNLOADS_ENABLED=true` to enable Qobuz, Apple Music, catalog search, and the Telegram bot.
- **Agents** are off by default (`SHELF_AGENT_ENABLED=false`). Set `SHELF_AGENT_ENABLED=true` to connect external agents through scoped tokens and review their proposals in the Inbox.

## Development

```sh
make dev          # Docker Compose with air hot reload on :8080
make check        # gofmt, go vet, go test -race
make lint         # golangci-lint and govulncheck in Docker
make web-check    # npm ci, vue-tsc, Vitest, and Vite build
make contract     # verify writes against a scratch Navidrome container
```

## Documentation

Full documentation starts at [docs/README.md](docs/README.md).

## License

AGPL-3.0. See [LICENSE.md](LICENSE.md).
