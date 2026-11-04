# Manual checklist

End-to-end checks that only the real deployment can answer: real accounts, the real wrapper, the real Navidrome and a phone. Run them on blackberry during step 1h, and again after big changes. Record the date and the result under each check.

## Setup

- shelf and shelf-wrapper deployed as in [deployment.md](deployment.md).
- Check 2 is shelf-wrapper's first sign-in. Its data folder starts empty and is apart from `apple-music-wrapper`'s. Send no code once to see the timeout, then sign in.
- One known Qobuz album and one Apple Music album that the old tools downloaded before, to compare against.

## Checks

1. Status shows every part healthy, and the Apple Music card shows the storefront.
2. Sign in from the Status card. The code step appears, the right code signs in, and a code sent after 60 seconds shows the timeout error.
3. Search each source for albums, tracks, artists and playlists. Open an album panel, an artist panel and a playlist panel.
4. Download a Qobuz album and an Apple Music album at the same time. Progress updates live. Folder names, file names and tags match the old tools' output for the same albums. Navidrome scans once, about 30 seconds after the second download finishes.
5. Paste each link form: Qobuz `play`, `open`, and `www` with a locale and a slug, for album, track, playlist, artist and label, and the site's `/interpreter/`, `/playlists/` and `/label/<slug>/download-streaming-albums/<id>`; Apple Music album, song, album with `?i=`, playlist and artist. A music video link gets the refusal message.
6. Cancel a running download, retry a failed one, and restart the container during a download. The download continues where it stopped.
7. Ask for a downloaded album again: it is skipped. With force, it ends `done` with every track `skipped`. Delete a track and force again: only that track downloads.
8. An album the old bot downloaded before the cutover is skipped after the import.
9. Telegram: a link gets a status message with the cover that moves from queued to downloading to done. Cancel stops a running download, and Retry starts it again in the same message. A link already in the library says when it was downloaded, and Download again fetches it. An artist link asks before queuing. Plain text returns album buttons that queue on tap. `/queue` lists progress. A user outside the whitelist is refused. A restart of shelf while a download runs keeps its message moving, and it ends with one final edit.
10. Every finished download's folder has a `shelf.txt` with the right IDs and links, and a second download into the same folder appends a block.
11. New folders are 0755 and new files 0644, owned by 10001, and Navidrome lists the new albums.
12. The next morning a backup is in `${SHELF_DATA}/backups` and on the HDD.

## Results

### 2026-11-04, staging on blackberry

Apple Music only, through the API, on images from `d54a2f9`. Qobuz, Navidrome and Telegram were off, and there is no web UI yet, so the full run stays for 1h.

| Check | Result |
| --- | --- |
| 2 | Passed through the API, not the Status card. The code step and the right code signed in. With no code, the state became `error` with `2FA code timeout (60s), aborting login` after 60 seconds, and a late code got a 409. A wrong password and the timeout both left the old session working: the wrapper went back to `running`. |
| 3 | Passed for all four kinds and the album, artist and playlist panels. |
| 5 | Album, song, album with `?i=`, playlist and artist passed. Music video and station links failed, with the generic error, then passed on `e95e5ad`. |
| 6 | Cancel, retry and a restart during a download passed, and the restart went on with 2 tracks skipped and 10 downloaded. A cancel left the current track `running`, and on `e95e5ad` it went back to `pending`. |
| 7 | Passed: skipped without force, 10 skipped with force, and only the deleted track downloaded. |
| 10 | Passed: `shelf.txt` holds the right IDs and links, and later downloads appended their blocks. |
| 11 | Passed for modes and owner. Navidrome was off. |
