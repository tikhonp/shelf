# Status

Phase 1 is done. shelf has run in production on blackberry since 2026-11-04: the web UI at `shelf.music.tikhonnnnn.com`, the bot `@music_shelf_bot`, Qobuz through qobuz-dl-go `v0.1.0`, Apple Music through amdl and shelf-wrapper, and scans in Navidrome. The old bot's 169 downloads are imported. The last run of [manual-checklist.md](manual-checklist.md) passed.

Phase 2, tags, is being built. The approved plan is [phase-2.md](phase-2.md), its decisions are in [decisions.md](decisions.md), [ui/design-brief.md](ui/design-brief.md) says what each screen does, and the screens are exported to [ui/design/](ui/design/README.md). Steps 1, 2 and 4 are done, and step 3 is built. `internal/modules/library/tagio` reads tags and writes them through verified temp copies, as [tags.md](tags.md) describes, `shelf manage tags <file>` shows a file, and the library module sweeps leftover temp files at start. The library module keeps an index of the music folder, read as Navidrome reads it under the owner's settings, groups it into albums with Navidrome's album IDs, runs the checks, and serves the album list, an opened album and the audio under `/api/library`.

Step 3 is built. `POST /api/library/albums/{id}/save` checks a Save at once, and a `library.write` job on the `tags` queue writes it. A Save refuses conflicts, locked files and folders a download works in, which sources name through `source.Reporter.Working`. It asks first before a split, a merge or an album artist rename. `library_changes` keeps every change with each file's tags before and after, shelf settles a change a stop cut off, and a write asks Navidrome for a scan. The write probe reports each top folder. The history lists saves, and undo and redo restore only the keys that still hold what a save wrote, with the release IDs as one group. A Save can replace or delete the album's cover, in the files that have one and in the folder's `cover.*`, with embedded copies scaled to 1000 pixels, and an opened album reports a cover ffmpeg can't decode, with a repair to stage. `make contract` runs these writes against a scratch Navidrome and passes on 0.64.2. The rules and deployment docs now let shelf write the whole library.

On blackberry the genre folders belong to `10001:10001` since 2026-11-04, and check 14 of [manual-checklist.md](manual-checklist.md) passed its rename and undo on a real album. A replaced front cover now keeps its type and description, and an undo of a Delete puts the description back. With that fix deployed as `c021044`, the check's cover replace and undo passed on Kraftwerk. Left in step 3: the check's backup step, after the night of 2026-11-04.

Step 4 is built. `GET /api/library/albums/{id}/suggestions` suggests values from file and folder names, other tracks, `shelf.txt`, MusicBrainz with the Cover Art Archive, and every catalog, each with its source and a confidence. One MusicBrainz release anchors an album's release fields. shelf finds it by a pasted ID, the files' ID, barcode, ISRC or search, and a Save refuses suggestions from two releases. Lyrics come per track from LRCLIB. shelf stores answers for 30 days and calls MusicBrainz at most once a second. On 2026-11-04 a live run found Blue's release by search, with its cover, and LRCLIB's lyrics for its first tracks.

| Step | What | State |
| --- | --- | --- |
| 0 | Evidence: real files, a scratch Navidrome, key census, ReplayGain speed | done |
| 1 | Tag IO and the safe writer | done |
| 2 | Read-only library: index, album API, checks, audio | done |
| 3 | Editing backend: Save, history, undo, covers. The owner runs the chown here. | built; check 14's backup step is left |
| 4 | Suggestions | done |
| 5 | Web UI, after the design | |
| 6 | Bulk runs, ReplayGain and AcoustID | |

Steps 1 to 4 need no screens, so they could run while the owner designed. Step 5 waits for the design. Step 0's results are in [decisions.md](decisions.md).

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
