# Manual checklist

End-to-end checks that only the real deployment can answer: real accounts, the real wrapper, the real Navidrome and a phone. Run them on blackberry during step 1h, and again after big changes. Record the date and the result under each check.

## Setup

- shelf and shelf-wrapper deployed as in [deployment.md](deployment.md).
- A staging shelf-wrapper with its own empty data folder for check 2, so the production session stays untouched.
- One known Qobuz album and one Apple Music album that the old tools downloaded before, to compare against.

## Checks

1. Status shows every part healthy, and the Apple Music card shows the storefront.
2. Sign in from the Status card against the staging shelf-wrapper. The code step appears, the right code signs in, and a code sent after 60 seconds shows the timeout error.
3. Search each source for albums, tracks, artists and playlists. Open an album panel, an artist panel and a playlist panel.
4. Download a Qobuz album and an Apple Music album at the same time. Progress updates live. Folder names, file names and tags match the old tools' output for the same albums. Navidrome scans once, about 30 seconds after the second download finishes.
5. Paste each link form: Qobuz `play`, `open`, and `www` with a locale and a slug, for album, track, playlist, artist and label; Apple Music album, song, album with `?i=`, playlist and artist. A music video link gets the refusal message.
6. Cancel a running download, retry a failed one, and restart the container during a download. The download continues where it stopped.
7. Ask for a downloaded album again: it is skipped. With force, it downloads.
8. An album the old bot downloaded before the cutover is skipped after the import.
9. Telegram: a link gets a status message with the cover that moves from queued to downloading to done. Cancel stops a running download, and Retry starts it again in the same message. A link already in the library says when it was downloaded, and Download again fetches it. An artist link asks before queuing. Plain text returns album buttons that queue on tap. `/queue` lists progress. A user outside the whitelist is refused. A restart of shelf while a download runs keeps its message moving, and it ends with one final edit.
10. Every finished download's folder has a `shelf.txt` with the right IDs and links, and a second download into the same folder appends a block.
11. New folders are 0755 and new files 0644, owned by 10001, and Navidrome lists the new albums.
12. The next morning a backup is in `${SHELF_DATA}/backups` and on the HDD.

## Results

Not run yet.
