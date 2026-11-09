# Status

shelf is finished and runs in production on blackberry. [vision.md](vision.md) says what it is for.

- **Downloads.** The web UI at `shelf.music.tikhonnnnn.com` and the bot `@music_shelf_bot` search and download from Qobuz, through qobuz-dl-go `v0.1.0`, and from Apple Music, through amdl and shelf-wrapper. Navidrome scans after downloads. shelf imported the old bot's 169 downloads.
- **Tags.** The Library tab edits every tag in the library, with suggestions, checks and fixes, covers, playback, history with undo, and bulk runs with ReplayGain and AcoustID. [tags.md](tags.md) describes it. `make contract` passes against Navidrome 0.64.2.
- **Home.** The Library tab is the home screen and the first tab. Each album shows its source, from the downloads' records, `shelf.txt` or its source folder, and the filter menu filters by source. [tags.md](tags.md#album-sources) describes it.
- **Delete.** The Library tab deletes an album's release folder and has Navidrome forget its tracks, as [tags.md](tags.md#deleting) describes. `make contract` covers it.

Every check in [manual-checklist.md](manual-checklist.md) passed in production, the last on 2026-11-05.

## Remotes

| Repo | Origin | Notes |
| --- | --- | --- |
| shelf | `ssh://git@code.tikhonnnnn.com/tikhon/shelf.git` | Forgejo. CI's image job pushes with the Actions secret `REGISTRY_TOKEN`, which has `write:package`. |
| qobuz-dl-go | `git@github.com:tikhonp/qobuz-dl-go.git` | GitHub only, public. CI runs on GitHub Actions. |

## Notes for later work

- The owner approved a redesign of account sign-in, Account and Agents on Status, and agent proposal review. [The plan](plans/signin-and-agent-review-design.md) has the owner's decisions, the behavior of each screen, and 24 bugs in the screens as they were. The 24 bugs were fixed on 2026-11-09, and the plan lists the parts of the redesign those fixes built. The rest isn't built: the sign-in rack unit with the key switch and the power-up, the session-ended display, the drawn patch cable, "Was ..." under each proposed value, the phone's review slot above the dock, and proposal values as suggestions in the Library editor. The exports are in `docs/ui/design/screens`, and the plan links the canvas.

- Optional [library agents](agents.md) are off by default and on in production. The Inbox reviews tags and artwork by field, with user edits, comments, history links, and shared memory for fresh agent runs. Production runs `main-6210916`, which predates the fixes below, and the [agents checklist](manual-checklist.md#optional-agents) hasn't run yet. Claude supplied the design and initial implementation. The final review on 2026-11-10 found no way past the access boundary. It fixed 11 bugs. Clear history could let the learning cursor skip an unfinished Save. A failed or undone dependency still counted as accepted. Memory versions repeated after a deletion. Deleted context kept its quota. Memory events were never retained, and history did not say which files were written. In the Inbox, a late answer could restore a connection secret, overwrite another proposal's drafts, or attach a preview to the wrong selection. A new revision's requirements were not applied to the selection, and link evidence could not be opened. A history Clear in the 2 seconds before an outcome is recorded still loses that outcome; the acceptance shows it as expired.

- shelf-wrapper now builds from upstream's `ghcr.io/worldobservationlog/wrapper-lite` image, pinned by digest. Production needs a rebuild and an update of both Shelf images to pick it up.

- Production signs in with Shelf accounts and records change authors. [auth.md](auth.md) describes setup. The owner ran the accounts checklist over HTTPS with passkeys on real devices, then removed the gateway's basic auth.

- In production the index holds 11,461 tracks and 1,008 Navidrome album IDs, the same as Navidrome. They form 1,013 albums, because 5 Navidrome albums span two folders each.
- A source module logs in and starts goroutines in `Run`, never in `Init`, and registers with `Switch` when its settings are missing.
- A wrapper signed in to the owner's Apple ID needs its own data folder and `--device-info`.
- Unset `SHELF_QOBUZ_TOKEN` for a local run with only the fake source, or it logs in to the owner's Qobuz account.
- Files in the music folder belong to UID 10001. Delete test downloads from inside the shelf container.
- Third, Portishead and Wish You Were Here 50 came from the checklist run, and the owner keeps them in the library.

Claude Opus 5.5 reviewed the authentication implementation. The review found no confirmed bypass. Follow-up fixes cover login queueing, independent passkey limits, data refresh after reauthentication, Telegram author labels in events, full account-switch reloads, and registration proof timing.

A focused Claude recheck confirmed both medium findings were addressed and caught a reconnect refresh that cleared Save results. Reconnect now rechecks pending writes without a second album reload and preserves existing results when refreshing an idle editor.

The account login dialog now fits its content and stays centered. Its styles override the app root's full-viewport height, which stretched the dialog into a narrow column.

Account passwords now accept 8 to 128 Unicode code points. The same limits apply to account creation, CLI resets, and password changes in the app.

Dependencies were refreshed on 2026-11-07, including Go modules, Vue, Vite, Vitest 5, jsdom 31, Node 26 LTS, and Alpine 3.24. TypeScript stays on 6.0.3. On 2026-11-10, vue-tsc 3.3.14, still the newest release, failed to start with TypeScript 7.0.2, because TypeScript 7 no longer exports `lib/tsc`. The test-only chain from Vue Test Utils through js-beautify had five moderate npm audit findings in glob and minimatch. `overrides` in `web/package.json` now require glob 14.0.4 or later under js-beautify and minimatch 11.0.5 or later under editorconfig, so npm audit reports none. Drop the overrides once js-beautify depends on fixed versions.

The dependency refresh passes `make check`, `make lint`, `make web-check`, and the production Docker build. npm reports no vulnerabilities in production dependencies.
