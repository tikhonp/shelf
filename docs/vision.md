# Vision

Music Shelf is one place to grow and look after a personal Navidrome library. Navidrome streams the music. shelf decides what goes into the library, fetches it, tags it, and records where every file came from.

## Downloads

- The web UI searches Qobuz and Apple Music, and downloads from the results or from a pasted link.
- The Telegram bot takes links and text searches, and keeps one message per download up to date.
- Downloads run in a queue that survives a restart, with progress, cancel, retry and history.
- Every release folder gets a `shelf.txt`.
- Navidrome scans after downloads finish.
- The Apple Music wrapper signs in from the UI, 2FA code included.

Files keep the folder and file names of the tools shelf replaced, `music-download-bot` and `apple-music-api`, so Navidrome, Picard and AudioMuse see no change.

## Library

The Library tab is the home screen. Its list shows every album's source and quality, and its tag editor does the work Picard used to do:

- The editor reads and writes every tag in the library, with custom fields, covers, lyrics and in-browser playback.
- Suggestions sit beside the tag fields, from the file and folder names, MusicBrainz, Qobuz, Apple Music, LRCLIB and AcoustID.
- Checks follow Navidrome's tagging guide. Their fixes run only when the owner starts them, on one album or on many.
- History keeps every Save, and any Save can be undone, after a restart too.
- ReplayGain runs in the background.

shelf changes tags and covers, deletes a release folder when the owner asks, and never renames or moves a file. Nothing changes after a download on its own.
