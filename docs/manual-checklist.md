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
12. The next morning a backup is in `${SWINGMUSIC_DATA}/shelf/backups`, and the morning after it is on the HDD.

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

### 2026-11-04, production on blackberry

shelf and shelf-wrapper run from `shelf.yaml` beside the old pipeline: `114f699` for Blue Lines and Mezzanine, `be76a2d` from 11:41 UTC on. Claude ran its checks through the API behind the gateway and over SSH on blackberry, except for the checks the owner ran.

| Check | Result |
| --- | --- |
| 1 | Passed. Qobuz is healthy with the Studio label, Apple Music says `Signed in, storefront RU`, Navidrome answers in 9 ms with 11385 songs, and the bot is connected as `@music_shelf_bot` with 2 users. |
| 2 | Passed, run by the owner from the Status card. The timeout part passed on staging. |
| 3 | Passed on both sources for albums, tracks, artists and playlists, and for the album, artist and playlist panels of Portishead. |
| 4 | Passed. Third from Qobuz at 27 and Portishead from Apple Music in ALAC started at 12:01:19 together and ended `done`, 11 of 11 each. The event stream carried 164 progress events. shelf's scan started at 12:02:14, 30 seconds after the second finished; Navidrome's own folder watcher had scanned at 12:01:50. The comparison used two albums the old tools wrote: Blue Lines against Highway 61 Revisited, and Mezzanine against Camel's Mirage. Folder and file names follow the same patterns, and the tags are the same fields. The old Qobuz files also carry `TOTALDISCS` and `TOTALTRACKS`. The old Apple Music files have upper-case field names, no embedded cover and no `.lrc` files, while shelf embeds the cover and saves `.lrc` as its amdl config says. 1d showed byte-for-byte parity for Qobuz. |
| 5 | Passed. Every Qobuz form listed resolved for album, track, playlist, artist and label, and so did every Apple Music form. The music video link got `Music videos and stations are not supported yet.`, and a foreign link got `link not from a known source`. |
| 6 | Passed on Wish You Were Here 50, 30 tracks. A cancel after 3 tracks left 27 `pending` and none `running`. Retry went on, and `docker restart shelf` during it resumed the download. It ended `done` with 21 tracks downloaded and 9 skipped, 30 files and no temp files. |
| 7 | Passed on Third: skipped without force, `done` with 11 skipped with force, and after deleting `04 - The Rip.flac` only that track downloaded. |
| 8 | Passed. Highway 61 Revisited, downloaded by the old bot on 2026-10-16, shows `in_library` in search and is skipped when queued. Its folder is untouched. |
| 9 | Accepted by the owner without a separate run of every line. The logs show one bot download, Protection, which ended `done`. The owner chose to fix any failure later. |
| 10 | Passed. Every new folder has `shelf.txt` with the right IDs and links. Third's holds one block for each of downloads 5, 8 and 9, and the skip without force added none. |
| 11 | Passed. New folders are 0755 and files 0644, all owned by 10001:10001. Navidrome went from 11385 to 11417 songs, the 32 tracks of Protection, Third and Portishead. |
| 12 | Waits for the next mornings. |

The extra checks in [build/1h.md](build/1h.md): the owner checked that the `find` on `qobuz/` and `apple-music/` prints nothing and that the tunnel is healthy. The vhost answers 401 without basic auth. The owner skipped the download through `music-download-bot`.

The owner checked the UI on a phone and a tablet. Claude then ran headless Chrome against the live UI at 320, 390 and 820 pixels wide, Night and Day, on Search with results, Downloads, a download, Status and the sign-in dialog. No page scrolls sideways, no image fails to load, the console shows no error, and the dialog fits each screen.

Qobuz delivered Wish You Were Here 50 at 24/96, while its search badge says 24/192. The folder name and `shelf.txt` follow what was delivered.
