# Status

Phase 1 is done. shelf has run in production on blackberry since 2026-11-04: the web UI at `shelf.music.tikhonnnnn.com`, the bot `@music_shelf_bot`, Qobuz through qobuz-dl-go `v0.1.0`, Apple Music through amdl and shelf-wrapper, and scans in Navidrome. The old bot's 169 downloads are imported. The last run of [manual-checklist.md](manual-checklist.md) passed.

Phase 2, tags, is being built. The approved plan is [phase-2.md](phase-2.md), its decisions are in [decisions.md](decisions.md), [ui/design-brief.md](ui/design-brief.md) says what each screen does, and the screens are exported to [ui/design/](ui/design/README.md). Steps 1, 2 and 4 are done, and steps 3, 5 and 6 are built. `internal/modules/library/tagio` reads tags and writes them through verified temp copies, as [tags.md](tags.md) describes, `shelf manage tags <file>` shows a file, and the library module sweeps leftover temp files at start. The library module keeps an index of the music folder, read as Navidrome reads it under the owner's settings, groups it into albums with Navidrome's album IDs, runs the checks, and serves the album list, an opened album and the audio under `/api/library`.

Step 3 is built. `POST /api/library/albums/{id}/save` checks a Save at once, and a `library.write` job on the `tags` queue writes it. A Save refuses conflicts, locked files and folders a download works in, which sources name through `source.Reporter.Working`. It asks first before a split, a merge, an album artist rename or a cover edit that reaches other albums in the folder. `library_changes` keeps every change with each file's tags before and after, shelf settles a change a stop cut off, and a write asks Navidrome for a scan. The write probe reports each top folder. The history lists saves, and undo and redo restore only the keys that still hold what a save wrote, with the release IDs as one group. A Save can replace or delete the album's cover, in the files that have one and in the folder's `cover.*`, with embedded copies scaled to 1000 pixels, and an opened album reports a cover ffmpeg can't decode, with a repair to stage. A replace keeps each disc folder's art, and Delete removes it too. A change reports each file's new revision and whether it wrote a cover, which step 5 needs for Save cover alone and the history. `make contract` runs these writes against a scratch Navidrome and passes on 0.64.2. The rules and deployment docs now let shelf write the whole library.

On blackberry the genre folders belong to `10001:10001` since 2026-11-04, and check 14 of [manual-checklist.md](manual-checklist.md) passed its rename and undo on a real album. A replaced front cover now keeps its type and description, and an undo of a Delete puts the description back. With that fix deployed as `c021044`, the check's cover replace and undo passed on Kraftwerk. Left in step 3: the check's backup step, after the night of 2026-11-04.

Step 4 is built. `GET /api/library/albums/{id}/suggestions` suggests values from file and folder names, other tracks, `shelf.txt`, MusicBrainz with the Cover Art Archive, and every catalog, each with its source and a confidence. One MusicBrainz release anchors an album's release fields. shelf finds it by a pasted ID, the files' ID, barcode, ISRC or search, and a Save refuses suggestions from two releases. Lyrics come per track from LRCLIB. shelf stores answers for 30 days and calls MusicBrainz at most once a second. On 2026-11-04 a live run found Blue's release by search, with its cover, and LRCLIB's lyrics for its first tracks.

Step 5 is built. The Library tab has the album list, the editor with suggestions, issues and Fixes, covers, the player and the Save states at both widths, and All fields, the long-form editor, History, the Help sheet, the keys and the resizable panels on the desktop. Status has a Library part, and a finished download has a button that opens its album. [tags.md](tags.md#the-editor) says how the editor works. shelf ran it on a scratch music folder in Chrome at 1440, 1050, 760 and 390 pixels, and each write was read back from the files. It hasn't run on a real album or a real phone yet. Step 5 is done when it has, which is check 15 of [manual-checklist.md](manual-checklist.md). Status doesn't say whether MusicBrainz answers, because the API has no such check yet.

Step 6 is built. Select in the Library list picks albums, a preview shows what a run would touch, and a run applies fixes or ReplayGain one album at a time, as one undo step. A run skips what a Save would ask about, survives a restart, and can be cancelled. ReplayGain measures on the new `analysis` queue, and AcoustID finds an anchor when text lookups find nothing. [tags.md](tags.md#bulk-runs) has the rules. On 2026-11-04 shelf ran this on a scratch music folder in Chrome at 1440, 1050, 760 and 390 pixels, and `make contract` passed six times in a row with a run's dates and gains read back from Navidrome. None of it has run on blackberry yet, and AcoustID has never reached the real service, because shelf has no key. Step 6 is done when check 16 of [manual-checklist.md](manual-checklist.md) passes. The plan puts this step after the writer has proved itself on real albums, so checks 14 and 15 come first.

| Step | What | State |
| --- | --- | --- |
| 0 | Evidence: real files, a scratch Navidrome, key census, ReplayGain speed | done |
| 1 | Tag IO and the safe writer | done |
| 2 | Read-only library: index, album API, checks, audio | done |
| 3 | Editing backend: Save, history, undo, covers. The owner runs the chown here. | built; check 14's backup step is left |
| 4 | Suggestions | done |
| 5 | Web UI: Library tab, editor, suggestions, covers, playback, All fields, History, keyboard, panels | built; check 15 is left |
| 6 | Bulk runs, ReplayGain and AcoustID | built; check 16 is left |

Steps 1 to 4 needed no screens, so they ran while the owner designed. Step 0's results are in [decisions.md](decisions.md).

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
- `make contract` used to fail about one run in three, also on the code before step 6, because it read a cover once while Navidrome still served its placeholder. It now waits for the cover.
- Third, Portishead and Wish You Were Here 50 came from the checklist run, and the owner keeps them in the library.
