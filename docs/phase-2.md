# Phase 2: tags

## Context

Phase 1 is done and shelf runs in production. The owner tags albums by hand in Picard today, through a remote desktop that is poor on a phone. Phase 2 puts a full tag editor in shelf that follows Navidrome's tagging guide and has metadata-remote's functions, with one new tab and no heavy UI. After this plan, the owner designs the screens and the work is built as usual.

The library is healthy, so this is a tool, not a rescue: 11,447 tracks, 1,006 albums, 668 GB, almost all FLAC, 13 M4A, no MP3. 88% of tracks have MusicBrainz album and recording IDs, and only 2 folders split into several albums.

## Decisions

| Topic | Decision |
| --- | --- |
| Picard | No integration. Everything lives in shelf, and Picard stays as it is. |
| Suggestions | Hints beside tag fields. No album card, no Apply step, no release picker. |
| Hidden tags | Plain fields behind an All fields switch, each with its own suggestion. Nothing is written unless accepted. |
| Write access | The whole library. The owner runs one chown on the five genre folders. It costs nobody write access: no file owned by UID 1000 lacks world-write. |
| After a download | Nothing automatic. Files stay as the source wrote them. |
| Renames | None. shelf changes tags, never file or folder names. |
| Cleanup | shelf has checks and fixes, and only the owner starts them. Nothing runs by itself. |
| Phone | View albums and issues, edit main fields with suggestions. Custom fields, bulk work and history need a desktop. |
| Extras | All four: Qobuz and Apple Music data, ReplayGain, LRCLIB lyrics, AcoustID. |

## What the research changed

- **`RELEASEDATE` fix keeps what Navidrome shows.** No album has the tag. Navidrome's `mapDates` has a legacy rule: with `ORIGINALDATE` set and no release date, it reads `DATE` as the release date. `YEAR` and M4A's `©day` are release date aliases, so they turn that rule off. Step 0 measured 1,099 albums: 832 under the legacy rule get the pair (`RELEASEDATE` from the old `DATE`, `DATE` from `ORIGINALDATE`), which changed nothing in Navidrome. 108 with a release date from `YEAR` or `©day` copy that value. 158 others copy `DATE`, and 94 of those have no MusicBrainz ID, so their Navidrome album ID changes while stars stay. On 23 albums Navidrome already shows a reissue year, which is its own check with the owner choosing.
- **go-taglib fits, with care.** Step 0 confirmed it on real files: no loss on 249 tracks and 17 rare files, and the same values as Navidrome's reader on every shared key. WMA loses custom tags, so shelf refuses those writes. `Clear` must never be used, and its write call ignores rejected keys, so shelf verifies every write by reading it back. Read-back caught two silent failures: `ORIGINALDATE` on Picard-tagged M4A, and a FLAC cover over 16 MiB.
- **Navidrome prefers files over tags.** `cover.*` beats embedded art, and `.lrc` and other sidecars beat embedded lyrics. shelf has to account for both.
- **Navidrome decides.** The owner's rule since step 0: a write counts only if Navidrome shows it, so every doc and test judges by Navidrome's view.
- **Backups see every write.** The nightly HDD rsync recopies each changed file and keeps no history. The remote run keeps overwritten files for 30 days. A full-library rewrite could add up to 668 GB there.

## The editor

**Library tab.** One new tab, so the shell has Search, Downloads, Library and Status. Search stays the home screen. An album list has one search box and one filter menu, and a lamp marks albums with issues. The desktop shows the editor beside the list, in resizable panels. The phone opens it as its own page. A finished download links to its album.

**Fields.** The main fields are Title, Artist, Album, Album Artist, Track, Disc, Genre, dates, and the compilation flag. All fields shows every tag, and that is where custom fields are created and deleted. Editing at album level changes every track. A field whose tracks differ shows Mixed. Lyrics and any value over 100 characters open a long-form editor.

**Saving.** Edits and accepted suggestions stay staged. A Save bar appears only while something is staged, and one Save is one undo step.

**Suggestions.** Each one sits beside its field and shows its source and a confidence percentage. Confidence follows a documented formula, and agreement between sources raises it. Sources:

- Filename, folder, sibling files and `shelf.txt`.
- MusicBrainz, with classical, compilation and live strategies.
- Qobuz and Apple Music through `source.Searcher` and `source.Resolver`, so no source is named.
- Cover Art Archive for covers and LRCLIB for lyrics.
- AcoustID, when text lookups find nothing.

**One release per album, with no picker.** Accepting fields one at a time could mix IDs from two releases, so the server picks one anchor release per album. The order is: the ID in the files, a staged ID, barcode, ISRC overlap, text search, AcoustID. Track count and durations must pass a threshold, or release-scoped suggestions stay hidden. Those fields show only the anchor's value. Pasting an ID or URL into the album ID field changes the anchor. Save refuses suggestions from two anchors, while typed values stay allowed. One accept of a per-track ID fills every track, or shelf doesn't offer it.

**Covers.** Upload, preview, save alone, apply to the album, delete, and detect and repair corrupt art, using ffmpeg as the judge. A cover edit changes the embedded front cover and the folder's `cover.*`, because Navidrome prefers the file. Embedded art is capped at 1000x1000 and the folder file stays full size. Delete removes both.

**History.** A timeline of Saves that survives restarts, with undo and redo in any order, up to 1,000 operations, and Clear history. Undo restores only fields that still hold the saved value and reports the rest as conflicts. Release ID fields undo as one group.

**Checks.** The issues list holds what Navidrome gets wrong, plus missing `RELEASEDATE`:

- A missing required tag, or an album that splits.
- No `RELEASEDATE`. This shows on nearly every album until the owner runs its fix.
- Values that differ across an album's tracks, or stale aliases that disagree.
- Various Artists without the compilation flag, the 86 tracks found so far.
- A wrong artist split, judged by Navidrome's real rules.
- IDs from two releases, or no cover.

Guide style, such as `ALBUMARTISTS` missing or no ReplayGain, stays behind a filter. Each check has a Fix that stages edits for review.

**Bulk.** The owner picks albums and fix kinds. A preview shows the changes, the files and GB touched, and what the backups will recopy. All chosen fixes go through an album in one pass, and a run is one undo step. ReplayGain starts the same way.

**Playback and keyboard.** A play button on each track, with an ffmpeg transcode where the browser can't play the file. Every action has a shortcut on the desktop.

**Dropped from metadata-remote.** File and folder rename, by decision. An album list with a folder filter replaces its folder tree, because the library has 7 top folders and about 1,000 albums.

## Safe write

One rule covers every write, whether from Save, undo, a fix or ReplayGain.

1. Compare a revision token (size, mtime, tag hash) with what the editor loaded. A difference is a conflict, which catches Picard and the old tools.
2. Refuse a file owned by another UID as locked, and a folder a download is working in as busy.
3. Write a pending history row.
4. For every file of the album, copy it to a hidden temp name in the same folder, write tags without `Clear`, then read back the full tag map and picture list. Check the audio is unchanged: a byte hash during the copy for FLAC, ffmpeg for other formats. Any difference aborts the album with the originals untouched.
5. Re-stat the originals, rename the temp files over them in one burst, fsync the folder, and mark the row done with the read-back values.
6. Ask for a scan.

At start, shelf reconciles pending rows by reading the files and sweeps its temp files by name.

**Album identity.** Album, Album Artist, album version, release date and the album ID feed Navidrome's album ID. The tracks of an album must end with identical values. A retag in place keeps track IDs, stars, plays and playlists, and a whole-album change keeps the album's star and rating under the new ID. Three cases lose something, and Save asks first in each: moving only some tracks takes the star and rating with them, and an undo leaves them on neither album. Merging loses the source's when the target has its own. Renaming an album artist leaves the star with the old name until an undo.

## Architecture

- **Module.** `internal/modules/library`, enabled in `cmd/shelf/main.go`, with tag file IO and the MusicBrainz, LRCLIB and AcoustID clients as sub-packages. A field table built from Navidrome's `mappings.yaml` maps each logical field to its keys per format and updates every alias present. It covers known traps: file tag `MUSICBRAINZ_RELEASETRACKID` is Navidrome's track ID and `MUSICBRAINZ_TRACKID` is the recording, and `ARTISTS` pairs by index with `MUSICBRAINZ_ARTISTID`.
- **Index.** A walk reads only files whose size or mtime changed, skips dot folders, and feeds the list, search and checks. An opened album always re-reads its files. Rows group by album inside a release folder, since playlist and single-track folders exist, and `CD NN` folders merge when their tags agree. The API uses row IDs, so file names never travel in URLs.
- **Tables.** Library files and albums, changes with full before and after snapshots, batches, raw lookup answers, and ReplayGain results keyed to the audio. History has no foreign key to the index. A `library.retention` schedule trims history and old covers, which live under `/data`.
- **Queues.** A new `tags` queue takes every write at concurrency 1. A new `analysis` queue takes ReplayGain and fingerprints. `library` stays for scans. Save runs as a job, so a dropped phone connection can't cut it off. Bulk work runs one album per slice through `jobs.Defer(0, ...)`, so a Save waits for one album at most. Selections live in a table, because job payloads stop at 64 KiB.
- **Ports.** `RequestScan` from `internal/modules/navidrome/scan.go`, with its `CreatedBy` label fixed. A new `source.Reporter` method reports a source's working folder, because Qobuz renames an album to `[IN PROGRESS]` during a run and `downloads.folder` is written only afterwards. The navidrome module also reads the effective split rules and priorities from Navidrome's `GET /api/config/`, with the trailing slash and an admin login, and shelf caches them. No TOML parsing or new mount is needed.
- **Lookups.** One MusicBrainz limiter at 1 request a second, with interactive lookups ahead of bulk, and a User-Agent with shelf's version and public URL. A separate suggestions endpoint lets the editor open at once. Remote images go through shelf, so the CSP doesn't change.
- **Platform changes.** A body limit option in `internal/platform/httpx/httpx.go` for cover upload and long lyrics, a `library` topic on `/api/events`, `SHELF_ACOUSTID_KEY` as a `config.Secret`, and fpcalc in the prod image if Alpine's `chromaprint` package ships it. Per-file panic recovery around go-taglib.
- **ReplayGain.** One ffmpeg `ebur128=metadata=1` decode per track gives track gain and the loudness blocks. shelf pools an album's blocks across discs for album gain, and writes track and album values as one set. Step 0: pooling matched one continuous decode, and the whole library takes about 12 hours on one CPU.

## Steps

Steps 0 to 4 need no screens, so they run while the owner designs.

| Step | What | Done when |
| --- | --- | --- |
| Plan docs | `status.md`, `vision.md`, `decisions.md`, and the Library section of `design-brief.md`. | The owner can start designing. |
| 0. Evidence | Done on 2026-11-04. | Results are in `decisions.md`, and the plan is adjusted. |
| 1. Tag IO | Field and format tables, safe writer, temp sweep, a read-only `shelf manage` view of a file. | Fixture tests pass for every format, with a failure injected at each stage. |
| 2. Read-only library | Migrations, walk, album API, checks, audio endpoint. | It runs in production without touching files, and its counts match Navidrome's. |
| 3. Editing backend | `tags` queue, Save, conflicts, working-folder port, history, undo, covers, write probe, a Navidrome contract test, rule and doc changes. The owner runs the chown. | One album saved and undone through the API shows correctly in Navidrome, and the contract test passes. |
| 4. Suggestions | Local sources, anchor logic, MusicBrainz, catalogs, Cover Art Archive, LRCLIB. Done on 2026-11-04. | Recorded-answer tests pass, including refused mixed anchors. |
| 5. UI | After the design: Library tab, editor, suggestions, covers, playback on both widths, then custom fields, history, keyboard and panels on desktop. | The phone and desktop limits hold on a real album. |
| 6. Bulk and analysis | Batches with preview, `analysis` queue, ReplayGain, AcoustID. Last, so the writer proves itself on single albums first. | A 20-album batch survives an interruption, an undo and the next night's backups. |

## What the design needs

Phone: album list, album view, field edit with suggestions, an album's issues. Desktop: list with editor, All fields, a selected track, long-form editor, cover dialog, history, bulk preview, keyboard help. States on both: conflict, locked, busy, saving, no suggestions, Navidrome date or split warning. All in the Studio direction, using existing parts where they fit.

## Rules and docs to change

- `CLAUDE.md`: the music folder rule, which says shelf deletes only its own temp files, the layout, and the glossary. New words: Suggestion, Check, Change.
- `docs/decisions.md` and `docs/deployment.md`: replace the rule that shelf writes only into `qobuz/` and `apple-music/`, and add the chown. `docs/deployment.md` also describes a start-up write probe that step 3 has to build.
- `docs/vision.md`: it plans an index read from Navidrome, file moves and one naming scheme. Phase 2 reads files and moves nothing.
- `docs/architecture.md` and `docs/data-model.md` as each step ships, and a new `docs/tags.md` with fields, checks and the write rule.

## Verification

- Before every commit: `make check`, `make lint`, `make web-check`, `docker build .`. Every edge case in `docs/tags.md` gets a test.
- A Navidrome contract test: the scenarios under Evidence from step 0 in `decisions.md`, kept as a repeatable run against a real Navidrome container. Production uses `deluan/navidrome:latest`, so its rules can change under shelf, and this run shows when they do.
- Fixture tests per format, covering preservation of unknown fields, multi-value order, pictures, and audio hashes.
- Failure injection at each write stage, and a restart with a pending row.
- On blackberry: a real album through Save, undo, scan and Navidrome's Raw Tags view, with a starred album and play counts checked before and after. New lines in `docs/manual-checklist.md`.
- shelf's artist split compared with Navidrome's `/api/inspect` on real tracks.
- A real phone for the Library tab and for playback behind basic auth.

## Assumptions

`decisions.md` records 1, 2 and 4, and `ui/design-brief.md` records 3.

1. "Add ability, I will do that manually" means bulk fixes exist and only the owner starts them.
2. Cover edits also write `cover.*`, the one file shelf changes beyond tags. Without this Navidrome keeps showing the old cover. Embedded art changes only in files that have it, because tracks without it show the album's cover.
3. Edits are staged with one Save, not saved field by field as in metadata-remote.
4. Bulk has no nightly limit. The owner sizes each run from the preview.
