# Manual checklist

End-to-end checks that need a live deployment: real accounts, `shelf-wrapper`, Navidrome, and a phone.

## Setup

- Deploy `shelf` and `shelf-wrapper` as in [deployment.md](deployment.md).
- Check 2 re-signs `shelf-wrapper` in, so run it when a 2FA code can reach your phone.
- Delete test downloads afterwards from inside the `shelf` container (files belong to shelf's UID, `10001` in the default image).

## Core checks

With `SHELF_DOWNLOADS_ENABLED=false`, the default, check that the Search and Downloads tabs and the source, Telegram, and Notices cards on Status stay hidden, and that download, catalog, Telegram, and wrapper API routes answer 404. Check that Library tagging, Navidrome scans, and `/api/events` still work while downloads are off. Then turn `SHELF_DOWNLOADS_ENABLED=true` back on and check that existing download history is intact.

With `SHELF_DOWNLOADS_ENABLED=true`:

1. **Status.** Every part is healthy and Apple Music shows the storefront.
2. **Apple Music sign-in.** Sign in from Status. The 2FA step appears, the right code signs in, and no code times out after 60 seconds.
3. **Search.** Search Qobuz and Apple Music for albums, tracks, artists, and playlists. Open album, artist, and playlist panels.
4. **Downloads and scan.** Download a Qobuz album and an Apple Music album together. Progress updates live, folder/file names and tags match legacy output, and Navidrome scans once about 30 seconds after the second finishes.
5. **Links.** Paste Qobuz (`play`, `open`, `www`, `/interpreter/`, `/playlists/`, `/label/.../download-streaming-albums/`) and Apple Music (album, song, `?i=`, playlist, artist) links. Music video links return the refusal message.
6. **Cancel, retry, restart.** Cancel a running download, retry a failed one, and restart the container mid-download; it resumes cleanly.
7. **Duplicates and force.** Re-requesting a downloaded album skips it. Force finishes `done` with all tracks `skipped`. Delete one track and force again to fetch only that track.
8. **Imported history.** An album downloaded by the old bot is skipped.
9. **Telegram.** Links send an edited status message with cover, Cancel, Retry, and Fetch missing. Artist links confirm first. Plain text searches albums. `/queue` lists active items. Unlisted users are refused. Container restarts keep status messages updating.
10. **Provenance.** Every finished release folder gets a `shelf.txt`, and a second download appends a new block.
11. **Permissions.** New folders are `0755`, files `0644`, owned by `10001`, and visible in Navidrome.
12. **Backups.** Daily DB backup appears in `/data/backups`.
13. **Library index.** `GET /api/library/index` shows `ready`, `rules: navidrome`, no `error`, and counts matching Navidrome's songs and albums. No files are modified by a walk. Tracks play in the browser.
14. **Tag editing and covers.** `locked: 0` and all top folders `writable`. Star an album in Navidrome, rename it via API/UI, verify star and play count move to the new ID, then undo. Replace and undo a cover. No `.shelf-tmp-` files remain.
15. **Editor UI (desktop and phone).** Apply Fix on `no_releasedate` and verify Navidrome keeps the year. Rename album artist with confirmation. Save cover alone while keeping staged tag edits. Edit from suggestions on phone and play a track.
16. **Bulk runs and AcoustID.** Select 20 albums with `no_releasedate`, run fix + ReplayGain, verify single Saves still fit between slices, restart mid-run, verify Navidrome gains and single History row, then undo. Verify AcoustID finds an anchor for an album MusicBrainz misses by text.
17. **Delete.** Delete a test download from Library (confirming shared-folder warnings if applicable). Folder disappears from `/music`, album leaves Navidrome with no Missing Files, History records it, and re-requesting downloads it again.
18. **Album sources.** `/` opens Library. Filter menu lists Qobuz, Apple Music, and No known source. Downloaded albums link to their download and `shelf.txt` page.

## Optional accounts

- With `provider=none`, app works with no account units.
- With `provider=simple` and HTTPS `SHELF_PUBLIC_URL`, logged-out API, media, logs, and events return 401.
- First-account UI setup validates passwords and opens the app; `SHELF_AUTH_SETUP_UI=false` shows the CLI command.
- Case-sensitive usernames work.
- Add, rename, and remove passkeys on desktop and phone. Canceling browser prompt keeps password login available. Removing a passkey or changing password ends all sessions.
- CLI password/passkey resets close open event/log streams within 30 seconds.
- Expired session preserves staged edits for the same account and reloads for a different account.
- Save as one user and undo as another; verify authors in History and after account deletion.

## Optional agents and Inbox

- Toggle `SHELF_AGENT_ENABLED=true` and `false`; verify Inbox/Agents appear and hide while data persists.
- Create a connection, copy the one-time setup prompt (Done warns if not copied), and verify secret never reappears.
- Run an external agent to read memory and create tag and artwork proposals without modifying files.
- Edit a proposed value, accept one field, and verify History links to the proposal.
- Modify a reviewed field externally and confirm Shelf requires re-review; modify an unrelated field and confirm identical diff still accepts.
- Decline selected/all fields, send comment and Ask for changes replies, verify dependency rockers move together, and confirm the agent reads feedback on its next run.
- Accept a proposal field via Library suggestion tape + ordinary Save, then run Clear history immediately; verify Inbox shows Written.
- Edit/delete learned preferences and verify tombstones and atomic version checks.
- Revoke a connection (or delete its creating account) and verify requests fail.
- Verify agent tokens cannot call ordinary write routes, cookies cannot call agent routes, and traversal paths (`/api/agent/v1/../../library/albums/1/save`, literal or encoded) are rejected.

## Album History and tag editor

- **Album History.** Desktop History cap opens `/library/<id>?view=history` with relative times, full date on hover, Undo run showing only that album's files, and Download row at the bottom. Phone opens History page with Undo in the bottom slot and Inbox deep-links.
- **Multi-value fields and Release type.** Artists, Album artists, and Release type appear in main rows on desktop and phone. MusicBrainz credits replace artist/type lists while Genre adds. Filter menu counts Release types.
- **Rivals and split suggestions.** Taking one suggestion on a single-value field folds rivals into one chip before and after Save. Split `ARTIST` without `ARTISTS` shows "Navidrome's split" at 50%.
- **Infinite scroll and formats.** Long lists load pages on scroll without duplicates. Albums offer only the lowest lossless format covering their quality.
