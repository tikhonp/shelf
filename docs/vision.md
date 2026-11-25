# Vision

Music Shelf manages a personal Navidrome library. Navidrome streams the music. Library tagging is always on. Downloads and external library agents are opt-in.

## Library

The Library tab is the home screen. It lists every album with its quality, plus its source when known, and handles tag editing:

- Edit all tags, custom fields, covers, and lyrics, with in-browser playback.
- Show suggestions beside tag fields from file and folder names, MusicBrainz, LRCLIB, AcoustID, and optional download sources or agents.
- Check albums against Navidrome's tagging guide and apply fixes only when started by the user.
- Keep undoable history for every save, across restarts.
- Measure and write ReplayGain in the background.
- Trigger a Navidrome scan after edits.

Shelf edits tags and covers, deletes a release folder only when asked, and never renames or moves files. Nothing changes automatically after a download.

## Optional downloads

Enabled with `SHELF_DOWNLOADS_ENABLED=true`:

- Search Qobuz and Apple Music from the web UI, or paste a link to download.
- Send links or search queries to the Telegram bot and track progress in one updated message per download.
- Run downloads in a persistent queue with progress, cancel, retry, and history.
- Write a `shelf.txt` provenance file into every release folder.
- Trigger a Navidrome scan when downloads finish.
- Sign the Apple Music wrapper in from the UI, including the 2FA code step.

Files keep the folder and file names of `music-download-bot` and `apple-music-api`, so Navidrome, Picard, and AudioMuse see no layout change.
