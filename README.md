# Music Shelf

Music Shelf manages a Navidrome music library. Phase 1 downloads releases from Qobuz and Apple Music into the library, from a web UI that searches both catalogs or from a Telegram bot. Every release folder gets a `shelf.txt` that records where its files came from.

Status: in development, see [docs/status.md](docs/status.md).

- One Go binary with a SQLite database and an embedded Vue UI.
- Qobuz through [qobuz-dl-go](https://github.com/tikhonp/qobuz-dl-go), a Go port of qobuz-dl.
- Apple Music through the amdl binary and wrapper-lite, with Apple ID sign-in from the UI.
- A Navidrome scan after downloads finish.

The docs start at [docs/README.md](docs/README.md). Licence: AGPL-3.0.
