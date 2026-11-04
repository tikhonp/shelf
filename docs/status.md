# Status

Phase 1 is done. shelf has run in production on blackberry since 2026-11-04: the web UI at `shelf.music.tikhonnnnn.com`, the bot `@music_shelf_bot`, Qobuz through qobuz-dl-go `v0.1.0`, Apple Music through amdl and shelf-wrapper, and scans in Navidrome. The old bot's 169 downloads are imported. The last run of [manual-checklist.md](manual-checklist.md) passed.

Phase 2, tags, is being built. The approved plan is [phase-2.md](phase-2.md), its decisions are in [decisions.md](decisions.md), and the screens to design are in [ui/design-brief.md](ui/design-brief.md). Steps 1 and 2 are done. `internal/modules/library/tagio` reads tags and writes them through verified temp copies, as [tags.md](tags.md) describes, `shelf manage tags <file>` shows a file, and the library module sweeps leftover temp files at start. The library module keeps an index of the music folder, read as Navidrome reads it under the owner's settings, groups it into albums with Navidrome's album IDs, runs the checks, and serves the album list, an opened album and the audio under `/api/library`.

Step 3 is under way. `POST /api/library/albums/{id}/save` checks a Save at once, and a `library.write` job on the `tags` queue writes it. A Save refuses conflicts, locked files and folders a download works in, which sources name through `source.Reporter.Working`. It asks first before a split, a merge or an album artist rename. `library_changes` keeps every change with each file's tags before and after, shelf settles a change a stop cut off, and a write asks Navidrome for a scan. The write probe reports each top folder. Left in step 3: history with undo and redo, covers, the Navidrome contract test, the rule changes and the owner's chown, and a real album saved and undone on blackberry.

| Step | What | State |
| --- | --- | --- |
| 0 | Evidence: real files, a scratch Navidrome, key census, ReplayGain speed | done |
| 1 | Tag IO and the safe writer | done |
| 2 | Read-only library: index, album API, checks, audio | done |
| 3 | Editing backend: Save, history, undo, covers. The owner runs the chown here. | in progress: Save done |
| 4 | Suggestions | |
| 5 | Web UI, after the design | |
| 6 | Bulk runs, ReplayGain and AcoustID | |

Steps 1 to 4 need no screens, so they can run while the owner designs. Step 0's results are in [decisions.md](decisions.md).

## Open

Nothing. Check 13 passed in production on 2026-11-04.

## Remotes

| Repo | Origin | Notes |
| --- | --- | --- |
| shelf | `ssh://git@code.tikhonnnnn.com/tikhon/shelf.git` | Forgejo. CI's image job pushes with the Actions secret `REGISTRY_TOKEN`, which has `write:package`. |
| qobuz-dl-go | `git@github.com:tikhonp/qobuz-dl-go.git` | GitHub only, public. CI runs on GitHub Actions. |

## Notes for later work

- In production the index holds 11,461 tracks and 1,008 Navidrome album IDs, the same as Navidrome. They form 1,013 albums, because 5 Navidrome albums span two folders each.
- A source module logs in and starts goroutines in `Run`, never in `Init`, and registers with `Switch` when its settings are missing.
- A wrapper signed in to the owner's Apple ID needs its own data folder and `--device-info`.
- Unset `SHELF_QOBUZ_TOKEN` for a local run with only the fake source, or it logs in to the owner's Qobuz account.
- Files in the music folder belong to UID 10001. Delete test downloads from inside the shelf container.
- Third, Portishead and Wish You Were Here 50 came from the checklist run, and the owner keeps them in the library.
