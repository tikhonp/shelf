# Music Shelf

Music Shelf manages a Navidrome music library.

- Search and download releases from Qobuz and Apple Music via the web UI or Telegram bot.
- Edit tags, covers, and lyrics with suggestions from MusicBrainz, sources, and LRCLIB.
- Check albums against Navidrome's tagging guide, run ReplayGain, and undo any save.
- Record every download's origin in a `shelf.txt` file inside its release folder.
- Trigger a Navidrome scan after downloads and edits.

Single Go binary with SQLite and an embedded web UI.

Docs: [docs/README.md](docs/README.md). Licence: AGPL-3.0.
