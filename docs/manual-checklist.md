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
- Check 18: 2026-11-05 on `d228255`, through the API and Playwright on the desktop and a phone viewport. The counts matched exactly: 759 Qobuz, 76 Apple Music, 183 without a source. Each of the 13 albums shelf downloaded links to its download and its page.

## Optional accounts, pending production check

- Keep provider none and verify the current app and gateway still work with no account units on Status.
- Enable simple with the public HTTPS origin. Verify logged-out requests cannot read the API, media, logs, or events.
- Sign in with a case-sensitive username. Verify a different case fails unless that separate account exists.
- Add a named passkey on a phone and a desktop. Sign out and sign in with each. Verify canceling the browser prompt keeps password login available.
- Rename and remove a passkey. Removal must end the current session and other sessions.
- Change a password and test CLI password and passkey resets. Check another browser's event and log streams close within the next 30-second check.
- Expire a session with staged edits. The same account resumes without an automatic Save. A different account reloads the page.
- Save as one user and undo as another. Verify the two authors in History. Check download and bulk-run authors.
- Delete an account and verify its old authors remain. Check the Telegram allowlist still works independently.
- Remove gateway basic auth only after password and real-device passkey checks pass over HTTPS.

## Optional agents

Keep production disabled until the agent-only gateway route is applied and checked.

- Enable `SHELF_AGENT_ENABLED=true` on a disposable library. The Inbox and Agents panel appear; disable it and confirm both disappear while saved records remain.
- Create a connection from the CLI, or from Status with Shelf sign-in enabled. Copy its secret separately from the setup prompt. Listing connections must never show that secret again.
- Use a fresh external run to retrieve shared rules, context and pending work. Create a tag proposal and an artwork proposal. Confirm the music files stay unchanged before acceptance.
- Edit one proposed value, save its proposal revision, and accept just that field. Confirm the other fields remain pending and History names the reviewer and links to the proposal.
- Change a reviewed field using another tool, then accept the old preview. Confirm Shelf requires another review. Change an unrelated field and confirm an identical selected diff can still be accepted.
- Switch a field off and Decline the rest, then decline every field of another proposal. Send a Reply as a comment, and another with Ask for changes on. Confirm the external agent retrieves those actions on its next run and no file changes occur.
- On a proposal where one field requires another, switch the required field off. Confirm the field that needs it switches off too and the line under the fields names both.
- Correct or delete a learned preference. Start a new agent run and confirm the correction or tombstone is still present. A learning commit with an old version must fail atomically.
- Revoke a connection and confirm its next request fails. With account sign-in enabled, delete the account that created a connection and confirm that token is revoked on its next call.
- Verify machine credentials cannot call ordinary Save, review acceptance, undo or delete. Verify a browser cookie alone cannot authenticate a machine endpoint.
- At the gateway, try both encoded and literal traversal paths from the configuration draft without credentials. They must not reach an ordinary API handler.

Automated tests cover proposal isolation, selected tag/artwork writes against real fixtures, concurrent acceptance, replay, rollback, changed previews, confirmations, memory versions and tombstones, learned feedback retention, auth isolation and CLI persistence. The checklist above covers the live external-agent and gateway setup, which local tests do not deploy.
