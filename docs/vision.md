# Vision

Music Shelf is one place to grow and look after a personal Navidrome library. Navidrome streams the music; shelf decides what goes into the library, fetches it, and keeps track of where every file came from.

## Phase 1: downloads

Phase 1 replaces two containers on blackberry, `music-download-bot` and `apple-music-api`, with shelf:

- A web UI searches Qobuz and Apple Music for albums, tracks, artists and playlists, and downloads from the results or from a pasted link.
- A Telegram bot takes links and plain-text searches from the phone and shows each download in one message that it keeps up to date.
- Downloads run in a persistent queue with progress, cancel, retry and history, and survive a restart.
- Every release folder gets a `shelf.txt` with the source, the catalog IDs and links, the formats and the tracks.
- Navidrome scans the library after downloads finish.
- The Apple Music wrapper signs in to an Apple ID from the UI, with the 2FA code step, instead of a terminal session on the server.

Files keep the folder and file names the current tools produce, so Navidrome, Picard and AudioMuse see no change.

## Later phases

These are directions, not commitments. Each phase gets its own plan.

- A library index read from Navidrome over the Subsonic API, and the library table as the home screen: every album with its source, quality and provenance.
- Editing from the table: tags, file moves, one naming scheme for every source, and the work Picard does today.
- Settings in the UI, such as the Qobuz token, and a login inside the app instead of the gateway's basic auth.
- More sources behind the same interfaces, and lyrics from qobuz-dl's newer releases.
