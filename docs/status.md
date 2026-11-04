# Status

Phase 1 is done. shelf has run in production on blackberry since 2026-11-04: the web UI at `shelf.music.tikhonnnnn.com`, the bot `@music_shelf_bot`, Qobuz through qobuz-dl-go `v0.1.0`, Apple Music through amdl and shelf-wrapper, and scans in Navidrome. The old bot's 169 downloads are imported. The last run of [manual-checklist.md](manual-checklist.md) passed.

Phase 2, tags, is being built. The approved plan is [phase-2.md](phase-2.md), its decisions are in [decisions.md](decisions.md), and the screens to design are in [ui/design-brief.md](ui/design-brief.md). Step 1 is done. `internal/modules/library/tagio` reads tags and writes them through verified temp copies, as [tags.md](tags.md) describes, `shelf manage tags <file>` shows a file, and the library module sweeps leftover temp files at start. Nothing in serve writes tags yet.

| Step | What | State |
| --- | --- | --- |
| 0 | Evidence: real files, a scratch Navidrome, key census, ReplayGain speed | done |
| 1 | Tag IO and the safe writer | done |
| 2 | Read-only library: index, album API, checks, audio | next |
| 3 | Editing backend: Save, history, undo, covers. The owner runs the chown here. | |
| 4 | Suggestions | |
| 5 | Web UI, after the design | |
| 6 | Bulk runs, ReplayGain and AcoustID | |

Steps 1 to 4 need no screens, so they can run while the owner designs. Step 0's results are in [decisions.md](decisions.md).

## Open

Nothing. An Apple Music download on 2026-11-04 confirmed that the wrapper-lite pin `0f06738` fixes `/key` failing after 1024 calls.

## Remotes

| Repo | Origin | Notes |
| --- | --- | --- |
| shelf | `ssh://git@code.tikhonnnnn.com/tikhon/shelf.git` | Forgejo. CI's image job pushes with the Actions secret `REGISTRY_TOKEN`, which has `write:package`. |
| qobuz-dl-go | `git@github.com:tikhonp/qobuz-dl-go.git` | GitHub only, public. CI runs on GitHub Actions. |

## Notes for later work

- The writer's journal and busy check are ports with no implementation yet. Step 3 gives the journal a table, reconciles rows left pending at start, and wires the busy check to the working-folder port.
- Run `shelf manage tags` on blackberry once, on a real FLAC and a real M4A, before step 2's index reads every file the same way.

- A source module logs in and starts goroutines in `Run`, never in `Init`, and registers with `Switch` when its settings are missing.
- A wrapper signed in to the owner's Apple ID needs its own data folder and `--device-info`.
- Unset `SHELF_QOBUZ_TOKEN` for a local run with only the fake source, or it logs in to the owner's Qobuz account.
- Files in the music folder belong to UID 10001. Delete test downloads from inside the shelf container.
- Third, Portishead and Wish You Were Here 50 came from the checklist run, and the owner keeps them in the library.
