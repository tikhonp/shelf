# Status

Phase 1 is done. shelf has run in production on blackberry since 2026-11-04: the web UI at `shelf.music.tikhonnnnn.com`, the bot `@music_shelf_bot`, Qobuz through qobuz-dl-go `v0.1.0`, Apple Music through amdl and shelf-wrapper, and scans in Navidrome. The old bot's 169 downloads are imported. The last run of [manual-checklist.md](manual-checklist.md) passed.

Phase 2, tags, is planned and nothing of it is built. The approved plan is `~/.claude/plans/lexical-wobbling-russell.md`, its decisions are in [decisions.md](decisions.md), and the screens to design are in [ui/design-brief.md](ui/design-brief.md).

| Step | What | State |
| --- | --- | --- |
| 0 | Evidence: real files, a scratch Navidrome, key census, ReplayGain speed | done |
| 1 | Tag IO and the safe writer | next |
| 2 | Read-only library: index, album API, checks, audio | |
| 3 | Editing backend: Save, history, undo, covers. The owner runs the chown here. | |
| 4 | Suggestions | |
| 5 | Web UI, after the design | |
| 6 | Bulk runs, ReplayGain and AcoustID | |

Steps 1 to 4 need no screens, so they can run while the owner designs. Step 0's results are in [decisions.md](decisions.md).

## Open

- The owner removes the old pipeline's data, `${SWINGMUSIC_DATA}/music-downloader-bot` and `${SWINGMUSIC_DATA}/apple-music-lite-data`.
- The checklist run left Third, Portishead and Wish You Were Here 50 in the library.
- The wrapper-lite pin moved to `0f06738`, which fixes `/key` failing after 1024 calls. The owner deploys `shelf` and `shelf-wrapper` and runs one Apple Music download to check it.

## Remotes

| Repo | Origin | Notes |
| --- | --- | --- |
| shelf | `ssh://git@code.tikhonnnnn.com/tikhon/shelf.git` | Forgejo. CI's image job pushes with the Actions secret `REGISTRY_TOKEN`, which has `write:package`. |
| qobuz-dl-go | `git@github.com:tikhonp/qobuz-dl-go.git` | GitHub only, public. CI runs on GitHub Actions. |

## Notes for later work

- A source module logs in and starts goroutines in `Run`, never in `Init`, and registers with `Switch` when its settings are missing.
- A wrapper signed in to the owner's Apple ID needs its own data folder and `--device-info`.
- Unset `SHELF_QOBUZ_TOKEN` for a local run with only the fake source, or it logs in to the owner's Qobuz account.
- Files in the music folder belong to UID 10001. Delete test downloads from inside the shelf container.
