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
15. The Library tab on a real album, on the desktop and on a real phone behind basic auth. Open an album with issues. Press Fix on No RELEASEDATE, read the amber fields, Save, and see Navidrome show the same year. Rename the album artist, confirm, and see the address follow the album after the walk. Undo both from History on the desktop. Replace a cover with Save cover alone while an edit stays staged. On the phone, edit a field from a suggestion, Save, and play a track. Play a file the browser refuses, and see it play through the transcode.
16. Bulk runs, on the desktop. In the Library tab press Select, and pick 20 albums with No RELEASEDATE. The preview's numbers match the albums, and Start with No RELEASEDATE and ReplayGain on. The Library tab's lamp lights, and a Save on another album finishes between two albums. Restart the shelf container during the run: the run goes on, and the album it was writing ends as `done` or starts over. Navidrome scans once when the run ends, shows the same years, and reports the new gains on a song. History shows one row. Undo it, and Navidrome shows the old values. Run it again, and the next night's backups finish with the 20 albums copied. Set `SHELF_ACOUSTID_KEY`, open an album that MusicBrainz can't find by text, and see its suggestions appear by AcoustID a few seconds later.

17. Delete, on a test download. Star the album in Navidrome and download it again in shelf to see it skipped. Press Delete in the Library tab. The dialog gives the file count and size, and for a playlist folder or a folder of singles it names the other albums. Confirm. The folder is gone from `/music`, the album leaves the list, and within a few minutes it leaves Navidrome with nothing under Missing Files. History shows the delete with Navidrome's count. Ask for the album again, and it downloads instead of being skipped.

18. Album sources, on the desktop and the phone. `shelf.music.tikhonnnnn.com/` opens the Library tab. After the first walk, the filter menu's Sources group counts about 759 Qobuz albums and 76 Apple Music albums, and No known source about 183, the genre folders' albums without a record. Kind of Blue under `jazz/` says it came from Qobuz by the old bot's history, recorded in `qobuz/`. An album shelf downloaded links to its download, and Open on links to its page when its folder has a `shelf.txt`. Filter by Qobuz, press Select and Select all, and the preview counts the same albums.

## Last run

Every check passed in production.

- Checks 1 to 12: 2026-11-04 on `be76a2d`, through the API, over SSH and on the owner's phone and tablet. The owner accepted check 9 after one bot download and check 12 without waiting for the backup.
- Check 13: 2026-11-04 on `0bcd479`. The index matched Navidrome's 11,461 songs and 1,008 albums, and no file changed after the marker.
- Check 14: 2026-11-04 on `cbba46f` and `c021044`, on `electronic/Kraftwerk`, with the backup on 2026-11-05. The first Save failed as `locked` until a second chown gave the genre folders group 10001 too.
- Check 15: 2026-11-04 on `f0fadf1`, on the desktop and on a real phone. The owner skipped the transcode step, because the library holds only FLAC and AAC.
- Check 16: 2026-11-04 on `0fb5a7f`, on the 20 smallest 16/44.1 albums with No RELEASEDATE and a MusicBrainz ID, with the backup on 2026-11-05. AcoustID found an anchor for *Cookin' With The Miles Davis Quintet*.
- Check 17: 2026-11-05 on `a7359e1`, in production.
