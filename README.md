# Music Shelf

Music Shelf manages a Navidrome music library.

- Edit tags, covers, and lyrics with suggestions from MusicBrainz, LRCLIB, AcoustID, and optional sources or agents.
- Check albums against Navidrome's tagging guide, run ReplayGain, and undo any save.
- Trigger a Navidrome scan after edits, and after downloads when downloads are enabled.
- Set `SHELF_DOWNLOADS_ENABLED=true` to search and download from Qobuz and Apple Music in the web UI or Telegram bot, and record each download's origin in `shelf.txt`.
- Set `SHELF_AGENT_ENABLED=true` to connect external library agents that submit scoped tag and artwork proposals for review in the Inbox.

Single Go binary with SQLite and an embedded web UI.

Docs: [docs/README.md](docs/README.md). Licence: AGPL-3.0.
