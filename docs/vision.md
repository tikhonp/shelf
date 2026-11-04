# Vision

Music Shelf is one place to grow and look after a personal Navidrome library. Navidrome streams the music. shelf decides what goes into the library, fetches it, and records where every file came from.

## Phase 1: downloads

Done. shelf replaces `music-download-bot` and `apple-music-api` on blackberry:

- The web UI searches Qobuz and Apple Music, and downloads from the results or from a pasted link.
- The Telegram bot takes links and text searches, and keeps one message per download up to date.
- Downloads run in a queue that survives a restart, with progress, cancel, retry and history.
- Every release folder gets a `shelf.txt`.
- Navidrome scans after downloads finish.
- The Apple Music wrapper signs in from the UI, 2FA code included.

Files keep the old tools' folder and file names, so Navidrome, Picard and AudioMuse see no change.

## Later

Directions, not commitments. Each phase gets its own plan.

- A library index read from Navidrome over the Subsonic API, and the library table as the home screen: every album with its source, quality and provenance.
- Editing from that table: tags, file moves, one naming scheme for every source, and the work Picard does today.
- Settings in the UI, such as the Qobuz token, and a login in the app instead of the gateway's basic auth.
- More sources behind the same interfaces, and lyrics from qobuz-dl's newer releases.
