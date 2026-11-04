# Vision

Music Shelf is one place to grow and look after a personal Navidrome library. Navidrome streams the music. shelf decides what goes into the library, fetches it, and records where every file came from.

## Phase 1: downloads

Done. shelf replaced `music-download-bot` and `apple-music-api` on blackberry:

- The web UI searches Qobuz and Apple Music, and downloads from the results or from a pasted link.
- The Telegram bot takes links and text searches, and keeps one message per download up to date.
- Downloads run in a queue that survives a restart, with progress, cancel, retry and history.
- Every release folder gets a `shelf.txt`.
- Navidrome scans after downloads finish.
- The Apple Music wrapper signs in from the UI, 2FA code included.

Files keep the old tools' folder and file names, so Navidrome, Picard and AudioMuse see no change.

## Phase 2: tags

Done, as [status.md](status.md) says. shelf has a Library tab with a full tag editor, so tagging no longer needs Picard:

- The editor reads and writes every tag in the library, with custom fields, covers, lyrics and in-browser playback.
- Suggestions sit beside the tag fields, from the file and folder names, MusicBrainz, Qobuz, Apple Music, LRCLIB and AcoustID.
- Checks follow Navidrome's tagging guide. Their fixes run only when the owner starts them, on one album or on many.
- History keeps every Save, and any Save can be undone, after a restart too.
- ReplayGain is calculated in the background.

shelf changes tags and covers only, and never renames or moves a file. Nothing changes after a download on its own.

## Later

Directions, not commitments. Each phase gets its own plan.

- The library table as the home screen, with every album's source, quality and provenance.
- Settings in the UI, such as the Qobuz token, and a login in the app instead of the gateway's basic auth.
- More sources behind the same interfaces, and lyrics from qobuz-dl's newer releases.
