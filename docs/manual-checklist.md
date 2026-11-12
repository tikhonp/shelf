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
13. Library index. Before the deploy, touch a marker file outside the music folder. After the first walk, `GET /api/library/index` shows `ready`, `rules: navidrome` and no `error`. `navidrome_albums` equals Navidrome's album count, from `getAlbumList2` paged to the end, and `tracks` plus `unreadable` equals its song count, from `getScanStatus`. Only a folder the walk can't see, such as one under an `.ndignore` with patterns, may explain a difference. `find /music -newer <marker>` lists no file shelf wrote. An album opens with every track's tags, and a track plays in the browser.
14. Tag editing, after the chown in [deployment.md](deployment.md). `GET /api/library/index` shows `locked: 0` and every folder `writable`. Star an album and note its play count in Navidrome. Save a new album title on all its tracks through the API, with the files' revisions from `GET /api/library/albums/{id}`. The change ends `done`, Navidrome scans, and its Raw Tags view shows the new title. The album keeps its star and play count under its new ID. Undo the change: the old title and ID come back with the star. Replace its cover, then undo that: `cover.*` and the embedded cover are the old ones again. No `.shelf-tmp-` file is left in the folder, and the next night's backups copy the album's files.
15. The Library tab on a real album, on the desktop and on a real phone. Open an album with issues. Press Fix on No RELEASEDATE, read the amber fields, Save, and see Navidrome show the same year. Rename the album artist, confirm, and see the address follow the album after the walk. Undo both from History on the desktop. Replace a cover with Save cover alone while an edit stays staged. On the phone, edit a field from a suggestion, Save, and play a track. Play a file the browser refuses, and see it play through the transcode.
16. Bulk runs, on the desktop. In the Library tab press Select, and pick 20 albums with No RELEASEDATE. The preview's numbers match the albums, and Start with No RELEASEDATE and ReplayGain on. The Library tab's lamp lights, and a Save on another album finishes between two albums. Restart the shelf container during the run: the run goes on, and the album it was writing ends as `done` or starts over. Navidrome scans once when the run ends, shows the same years, and reports the new gains on a song. History shows one row. Undo it, and Navidrome shows the old values. Run it again, and the next night's backups finish with the 20 albums copied. Set `SHELF_ACOUSTID_KEY`, open an album that MusicBrainz can't find by text, and see its suggestions appear by AcoustID a few seconds later.

17. Delete, on a test download. Star the album in Navidrome and download it again in shelf to see it skipped. Press Delete in the Library tab. The dialog gives the file count and size, and for a playlist folder or a folder of singles it names the other albums. Confirm. The folder is gone from `/music`, the album leaves the list, and within a few minutes it leaves Navidrome with nothing under Missing Files. History shows the delete with Navidrome's count. Ask for the album again, and it downloads instead of being skipped.

18. Album sources, on the desktop and the phone. `shelf.music.tikhonnnnn.com/` opens the Library tab. After the first walk, the filter menu's Sources group counts about 759 Qobuz albums and 76 Apple Music albums, and No known source about 183, the genre folders' albums without a record. Kind of Blue under `jazz/` says it came from Qobuz by the old bot's history, recorded in `qobuz/`. An album shelf downloaded links to its download, and Open on links to its page when its folder has a `shelf.txt`. Filter by Qobuz, press Select and Select all, and the preview counts the same albums.

## Last run

Every check passed in production, except the run row and the hover date in Album History check 1.

- Checks 1 to 12: 2026-11-04 on `be76a2d`, through the API, over SSH and on the owner's phone and tablet. The owner accepted check 9 after one bot download and check 12 without waiting for the backup.
- Check 13: 2026-11-04 on `0bcd479`. The index matched Navidrome's 11,461 songs and 1,008 albums, and no file changed after the marker.
- Redesign checks 1 to 10: 2026-11-10 on `main-9014241` and `main-e561e16`, on the desktop and the owner's vivo phone. Check 3 failed on the phone, and `main-e561e16` holds its fix. On the phone, the owner ran the passkey steps of checks 5 to 7, because the test phone has no screen lock and Android creates no passkey without one.
- The phone's add confirm: 2026-11-11 on `main-b8a2bf6`, on the vivo with the test account `claude-check`. The confirm sits 14px below Add passkey, the unit's usual spacing, and offers only Cancel and Add passkey on an account without passkeys.
- Check 14: 2026-11-04 on `cbba46f` and `c021044`, on `electronic/Kraftwerk`, with the backup on 2026-11-05. The first Save failed as `locked` until a second chown gave the genre folders group 10001 too.
- Check 15: 2026-11-04 on `f0fadf1`, on the desktop and on a real phone. The owner skipped the transcode step, because the library holds only FLAC and AAC.
- Check 16: 2026-11-04 on `0fb5a7f`, on the 20 smallest 16/44.1 albums with No RELEASEDATE and a MusicBrainz ID, with the backup on 2026-11-05. AcoustID found an anchor for *Cookin' With The Miles Davis Quintet*.
- Check 17: 2026-11-05 on `a7359e1`, in production.
- Check 18: 2026-11-05 on `d228255`, through the API and Playwright on the desktop and a phone viewport. The counts matched exactly: 759 Qobuz, 76 Apple Music, 183 without a source. Each of the 13 albums shelf downloaded links to its download and its page.
- Album History checks 1 to 4: 2026-11-12 on `v1.0.4`, through Playwright on the desktop and a phone viewport, not a real phone, with a test account. Check 1 skipped the run's row, because production's History holds no run. Its hover date failed, because the row's button covered the time, and the fix is on main. In check 2 the Save finished before the first screenshot, so the screenshots missed Saving. Check 2 changed Darkside EP's LABEL, and the cleanup's Undo put back `Clown & Sunset`, which Navidrome shows too.

## Optional accounts

Last run: passed in production, run by the owner before 2026-11-10. The gateway's basic auth came off afterwards.


- Keep provider none and verify the current app and gateway still work with no account units on Status.
- Enable simple with the public HTTPS origin. Verify logged-out requests cannot read the API, media, logs, or events.
- On a fresh install with no accounts, create the first account in the browser. A short password and a wrong repeat turn the display red, and the new account opens the app. With `SHELF_AUTH_SETUP_UI=false` the unit names `shelf manage users add` instead.
- Sign in with a case-sensitive username. Verify a different case fails unless that separate account exists.
- Add a named passkey on a phone and a desktop. Sign out and sign in with each. Verify canceling the browser prompt keeps password login available.
- Rename and remove a passkey. Removal must end the current session and other sessions.
- Change a password and test CLI password and passkey resets. Check another browser's event and log streams close within the next 30-second check.
- Expire a session with staged edits. The same account resumes without an automatic Save. A different account reloads the page.
- Save as one user and undo as another. Verify the two authors in History. Check download and bulk-run authors.
- Delete an account and verify its old authors remain. Check the Telegram allowlist still works independently.
- Remove gateway basic auth only after password and real-device passkey checks pass over HTTPS.

## Optional agents

This checklist ran in production on 2026-11-10 on `main-846ae08`, with agents already on. The writes went to Darkside EP, and undo restored its files byte for byte afterwards. A temporary account, `checklist`, acted as a second user, and the owner's CLI deleted it at the end. The phone items ran on a real Android phone. Every item passed. The owner first skipped the Clear history item to keep History's undo entries, and it passed later that day on `main-de2180a`. An Inbox acceptance wrote Album artists to Darkside EP, and about 30 seconds later Clear history deleted 19 changes. It kept the acceptance's change, and the acceptance still showed Written. Older acceptances whose changes it deleted still showed Undone. Undoing the kept change restored the files. The run found five bugs, fixed afterwards:

- An agent request to a path with no route answered 500 instead of 404.
- The design contract listed `/library/albums` for agents; the path is `/albums`.
- Evidence tape named a field by its ID, such as `originaldate`.
- On the phone, "Change N in History" led to the album list, because the phone has no History.
- A proposal for a field no file held, such as Album artists, showed under Proposed but had no tape.

Undo requests sent within a second of each other on one album answer 202, and only the first writes. The writer refuses the others, because the files changed under them. It writes nothing wrong, but an API client can't tell. Pressing Undo in History one change at a time works.

- Enable `SHELF_AGENT_ENABLED=true` on a disposable library. The Inbox and Agents panel appear; disable it and confirm both disappear while saved records remain.
- Create a connection from the CLI, or from Status with Shelf sign-in enabled. Copy the prompt that holds its secret and start an agent with it. Done must ask when nothing was copied. The Status units must never show that secret again.
- Use a fresh external run to retrieve shared rules, context and pending work. Create a tag proposal and an artwork proposal. Confirm the music files stay unchanged before acceptance.
- Edit one proposed value, save its proposal revision, and accept just that field. Confirm the other fields remain pending and History names the reviewer and links to the proposal.
- Change a reviewed field using another tool, then accept the old preview. Confirm Shelf requires another review. Change an unrelated field and confirm an identical selected diff can still be accepted.
- Switch a field off and Decline the rest, then decline every field of another proposal. Send a Reply with Send comment, and another with Ask for changes on, so the key reads Ask for changes. Confirm the external agent retrieves those actions on its next run and no file changes occur.
- On a proposal where one field requires another, switch the required field off. Confirm the field that needs it switches off too and the line under the fields names both.
- On that proposal, confirm a cable joins the two fields and each value has a "Was ..." line that matches the files. On a phone, confirm Accept sits in a slot above the dock, pressing a field's name opens it, and the ? cap opens the page note above the review slot. On the desktop, drag the divider, reload, and confirm the list keeps its width. Rest the pointer on Accept and confirm its note opens.
- Open the proposal's album in the Library. Confirm the Proposed row and the agent's tape beside each pending field. Press a tape, Save, and confirm the Inbox shows that field accepted by you, linked to the Save in History.
- Clear history right after an Inbox acceptance finishes writing. Confirm the acceptance shows Written, not "Left the history".
- Correct or delete a learned preference. Start a new agent run and confirm the correction or tombstone is still present. A learning commit with an old version must fail atomically.
- Revoke a connection and confirm its next request fails. With account sign-in enabled, delete the account that created a connection and confirm that token is revoked on its next call.
- Verify machine credentials cannot call ordinary Save, review acceptance, undo or delete. Verify a browser cookie alone cannot authenticate a machine endpoint.
- Without a session, request `/api/library/x/../../agent/v1/runs`, `/api/agent/v1/../../library/albums/1/save` and `/api/agent/v1/%2e%2e/%2e%2e/library/albums/1/save` through the public address. None may reach an ordinary API handler.

Automated tests cover proposal isolation, selected tag/artwork writes against real fixtures, concurrent acceptance, replay, rollback, changed previews, confirmations, memory versions and tombstones, learned feedback retention, auth isolation and CLI persistence. The checklist above covers the live external-agent and gateway setup, which local tests don't deploy.

## Sign-in and agent review redesign

Run these after deploying the redesign, on the desktop and a real phone over HTTPS. Accounts are on as in [auth.md](auth.md) and agents as in [agents.md](agents.md). You need an account with a passkey, a test connection and a proposal with pending fields. Run check 10 only on a scratch instance, never in production.

1. Sign out. Sign-in shows only its unit, with a dim MUSIC SHELF line and no title bar, menubar or dock. Enter a wrong password: the display turns red and reads WRONG USERNAME / OR PASSWORD. Sign in with the right password, and the app opens.
2. Sign out and slide Passkey to ON. Cancel the browser prompt: the slide returns to OFF, and the display turns amber with PASSKEY CANCELED / USE THE PASSWORD. Slide again and sign in with the passkey. On the desktop, sign out and sign in with a passkey from the key switch.
3. On an iPhone, open sign-in with the keyboard closed. The unit is centered. Focus Username, then Password: the unit moves up and Sign in stays visible above the keyboard.
4. On Status, press the Account key switch with no staged edits: it turns to OFF and signs out. Sign in and stage two Library edits. Press the switch again: it stops halfway and asks "Sign out with 2 staged edits?". Keep editing returns it to ON and keeps the edits. Press it again and confirm Sign out: sign-in opens.
5. Checks 5 to 7 need a proof older than five minutes, so wait that long after signing in or confirming before each one. On Status, press Rename beside a passkey, change Passkey name and press Save. The confirm asks `Rename <name>?`, and Cancel keeps the old name. Rename again, enter Password and press Rename: the new name appears.
6. Fill New passkey name and press Add passkey: "Add a passkey?" opens under the form, with no blank space above its question. On an account with no passkeys, the confirm has no Confirm with a passkey key. Enter Password, press Add passkey and register one. The new passkey appears.
7. Press Remove beside that passkey: `Remove <name>?` opens beside it. The confirm's Remove key stays disabled with an empty Password. Enter the password and press Remove. Sign in again opens, and after sign-in the passkey is gone.
8. On Status, fill Name under Connect and press Connect. The form locks and `Prompt for <name>` appears. Before copying anything, press Done: "Nothing copied yet, and Shelf shows it only once." appears with Close anyway. Press Copy prompt: it says "Prompt copied.", and Done now closes the prompt. Press the connection's Revoke jack: the plug stops halfway and asks first. Confirm Revoke: the plug comes out, the lamp goes dark and the display reads REVOKED JUST NOW.
9. In the desktop Inbox, open a pending proposal. Drag the divider, then focus it and press the left and right arrows. Both move it, and a reload keeps the width. No help note shows at rest. Rest the pointer on Accept: its note opens after 350ms and closes when the pointer leaves. Type a comment in Reply. With Ask for changes off, Send reads Send comment, and with the rocker on it reads Ask for changes. The rocker stays in place when it flips. Scroll the list: the menubar stays at the top and the divider's grip stays in the middle of the window. On the phone, turn one field off: the decline key reads Decline 1 and follows the count. The ? cap opens the page's note above the review slot.
10. On a scratch instance with `SHELF_AUTH_PROVIDER=simple` and no accounts, see NO ACCOUNTS YET / CREATE THE FIRST ONE. A short password turns the display red, and so does a Repeat password that differs. Enter matching passwords of eight characters or more and press Create account: the app opens signed in. On another scratch instance with no accounts and `SHELF_AUTH_SETUP_UI=false`, the unit shows NO ACCOUNTS / ADD ONE ON THE SERVER and `shelf manage users add <username>` instead of the form.

## Album History

Run these after deploying [Album History](plans/album-history.md), on the desktop and a real phone. Check 1's run steps need an album with a run in its History. Someone cleared production's History before 2026-11-10, and it has held no run since, so those steps wait for the next run. Never press Undo run in production, because it undoes every album in the run. Checks 2 and 3 write to Darkside EP, and check 4 needs the Inbox's accepted proposal for it. Undo check 2's Save at the end.

1. On the desktop, open one of check 16's albums and press the History cap in the Editor's title bar. The cap stays lit, the address ends in `?view=history`, and History takes the place of the Editor and the Tracks window. The run's row says "Run on 20 albums, undone together" and has an Undo run key. Open the row, and it lists only this album's files. Rest the pointer on a time, and the full date shows. Press Esc, and the Editor comes back.
2. On the desktop, Save one tag on Darkside EP and press the History cap. The new row shows Saving, then Saved without a reload. Press Undo. The row turns Undone, the Editor shows the old value, and Navidrome's Raw Tags view shows it too. Press Redo. If a download wrote the album, it comes last under Download, and Open opens that download.
3. On the phone, open Darkside EP. History is before Delete in the title bar. The History page has a back button and rows without keys. Open the Save from check 2. Each line shows Before over After, and Undo is in the slot above the tab bar. Press it, and the slot's key turns to Redo. Press Redo. The `?` note gives the change's full date. If the album has a download row, tap it, and the download opens.
4. On the phone, open the Inbox and the accepted proposal for Darkside EP. Its change links to the album's History, and the link opens the History page.
