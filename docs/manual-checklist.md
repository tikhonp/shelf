# Manual checklist

End-to-end checks that only the real deployment can answer: real accounts, the real wrapper, the real Navidrome and a phone. Run them on blackberry after big changes, and record the date and the result in Last run.

## Setup

- shelf and shelf-wrapper deployed as in [deployment.md](deployment.md).
- Check 2 signs shelf-wrapper in again, so run it when a code can reach the owner's phone.
- One Qobuz album and one Apple Music album the old tools downloaded, to compare against.
- Test downloads stay in the library. Delete them afterwards, from inside the shelf container, since the files belong to UID 10001.

## Checks

1. Status shows every part healthy, and the Apple Music card shows the storefront.
2. Sign in from the Status card. The code step appears, the right code signs in, and with no code the sign-in shows the timeout error after 60 seconds.
3. Search each source for albums, tracks, artists and playlists. Open an album panel, an artist panel and a playlist panel.
4. Download a Qobuz album and an Apple Music album at the same time. Progress updates live. Folder names, file names and tags match the old tools' output for the same albums. Navidrome scans once, about 30 seconds after the second download finishes.
5. Paste each link form: Qobuz `play`, `open`, and `www` with a locale and a slug, for album, track, playlist, artist and label, and the site's `/interpreter/`, `/playlists/` and `/label/<slug>/download-streaming-albums/<id>`; Apple Music album, song, album with `?i=`, playlist and artist. A music video link gets the refusal message.
6. Cancel a running download, retry a failed one, and restart the container during a download. The download continues where it stopped.
7. Ask for a downloaded album again: it is skipped. With force, it ends `done` with every track `skipped`. Delete a track and force again: only that track downloads.
8. An album the old bot downloaded is skipped.
9. Telegram: a link gets a status message with the cover that moves from queued to downloading to done. Cancel stops a running download, and Retry starts it again in the same message. A link already in the library says when it was downloaded, and Fetch missing fetches what is missing. An artist link asks before queuing. Plain text returns album buttons that queue on tap. `/queue` lists progress. A user outside the whitelist is refused. A restart of shelf while a download runs keeps its message moving, and it ends with one final edit.
10. Every finished download's folder has a `shelf.txt` with the right IDs and links, and a second download into the same folder appends a block.
11. New folders are 0755 and new files 0644, owned by 10001, and Navidrome lists the new albums.
12. The next morning a backup is in `${SWINGMUSIC_DATA}/shelf/backups`, and the morning after it is on the HDD.
13. Library index. Before the deploy, touch a marker file outside the music folder. After the first walk, `GET /api/library/index` shows `ready`, `rules: navidrome` and no `error`. `navidrome_albums` equals Navidrome's album count, from `getAlbumList2` paged to the end, and `tracks` plus `unreadable` equals its song count, from `getScanStatus`. Only a folder the walk can't see, such as one under an `.ndignore` with patterns, may explain a difference. `find /music -newer <marker>` lists no file shelf wrote. An album opens with every track's tags, and a track plays in the browser behind basic auth.

14. Tag editing, after the chown in [deployment.md](deployment.md). `GET /api/library/index` shows `locked: 0` and every folder `writable`. Star an album and note its play count in Navidrome. Save a new album title on all its tracks through the API, with the files' revisions from `GET /api/library/albums/{id}`. The change ends `done`, Navidrome scans, and its Raw Tags view shows the new title. The album keeps its star and play count under its new ID. Undo the change: the old title and ID come back with the star. Replace its cover, then undo that: `cover.*` and the embedded cover are the old ones again. No `.shelf-tmp-` file is left in the folder, and the next night's backups copy the album's files.

## Last run

Check 14, in part: 2026-11-04, in production on `cbba46f`, through the API from inside the shelf container. After the chown, the index showed `locked: 0` and all 7 top folders writable. The first Save failed as `locked` and wrote nothing, because the chown had kept the genre folders' groups, 1000 and root, which shelf can't keep on a copy. After a second chown to `10001:10001`, a Save renamed `electronic/Kraftwerk` to "Kraftwerk (shelf test)". Navidrome showed it under a new album ID with its star, its play and its last-played date, kept all 4 song IDs, and matched shelf's new album ID. The undo brought back the old ID with the star and the play, and every file's tags and pictures hashed as before. No temp file was left. The cover steps waited for a fix, because a replaced front cover lost its description, such as "Cover", and an undo didn't bring it back. The fix is in `main` and waits for a deploy before the cover steps run. The backup step waits for the next night.

Check 13: 2026-11-04, in production on `0bcd479`. The index read Navidrome's settings with production's account and matched its 11,461 songs and 1,008 albums, with no file unreadable. It formed 1,013 albums, because 5 Navidrome albums span two folders each. No file changed after the marker, an album opened with its tags, and a track played in the browser.

Checks 1 to 12: 2026-11-04, in production on `be76a2d`, through the API, over SSH and on the owner's phone and tablet. Every check passed. The owner accepted check 9 after one bot download and check 12 without waiting for the backup, and skipped one download through the old bot with shelf-wrapper signed in.
