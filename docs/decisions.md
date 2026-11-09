# Decisions

The decisions in force and why. When one changes, edit its row and the docs it touches in the same commit. Git history has the earlier versions.

## Product

| Decision | Why |
| --- | --- |
| The name is shelf, shown as Music Shelf: Go module `github.com/tikhonp/shelf`, image `code.tikhonnnnn.com/tikhon/shelf`, env prefix `SHELF_`, host `shelf.music.tikhonnnnn.com`. | The owner's choice. |
| Qobuz writes to `music/qobuz` and Apple Music to `music/apple-music`, with the old tools' folder and file names. | Navidrome, Picard and AudioMuse see no change. |
| Every download writes `shelf.txt` into its release folder. | The owner asked for it, and the Library tab can tell an album's source from it without the database. |
| `SHELF_AUTH_PROVIDER` selects `none` by default or `simple` for CLI-managed accounts, passwords, and optional passkeys. All accounts share access. [auth.md](auth.md) describes sessions and recovery. | Opt-in authentication for deployments that replace gateway basic auth. Production made the cutover and the gateway dropped basic auth. |
| Account passwords contain 8 to 128 Unicode code points, with spaces allowed and no character-class rules. | The owner chose an eight-character minimum. |
| shelf is AGPL-3.0 like proxier, and qobuz-dl-go GPL-3.0 like qobuz-dl. | GPL-3.0 code can be combined with AGPL-3.0 code. |

## Stack

| Decision | Why |
| --- | --- |
| proxier's stack, copied and trimmed: Echo v5, modernc SQLite through sqlx, goose migrations per module, env config parsed by hand, slog JSON, the module pattern and the jobs platform. | The conventions of the owner's other Go apps. |
| `config.Load` takes a lookup function with `os.LookupEnv`'s signature. Unset takes the default, and empty is empty. | `SHELF_AMDL_CONFIG` and `SHELF_NAVIDROME_URL` have defaults, and an empty value must turn their part off. |
| A module with work that lasts as long as the server implements `module.Runner`. Only serve calls `Run`, and one that fails is logged while the server goes on. | `manage` opens the platform too, and a bot that can't reach Telegram must not stop downloads. |
| The jobs table has no subject columns. A download points at its job. | They served proxier's events and Jobs page. |
| Vue 3, Vite and TypeScript, embedded with `embed.FS`, under a strict CSP. | htmx would fall short for the library table. |
| The runtime image is `alpine:3.24` with ffmpeg, running as UID 10001 with umask 022. Existing files keep their owner and mode. | amdl's FLAC conversion and Qobuz's segmented fallback call ffmpeg, and 10001 owns the source folders. |
| A `fakesource` module with canned data, on with `SHELF_DEV_FAKE_SOURCE`. | Tests and UI work without real accounts. |
| A request the client left is logged as info. | A search aborts the one before it, and each abort logged an error for a failure that wasn't shelf's. |

## Downloads

| Decision | Why |
| --- | --- |
| A job request may name its queue. `downloads.download` runs on the queue named after its source, or on `system`. | One job type with its steps, and still one download per source at a time. |
| A source's error in `resolve` or `fetch` is recorded on the download, and the job goes on to `finish` and `notify`. Only shelf's own errors fail the job. | One path ends every download, and the bot's final message always comes from `notify`. |
| A download that is canceled, or whose job fails, gets a `downloads.notify` job. | Those never reach their own `notify` step. |
| Force fetches what is missing and leaves complete files alone. The bot's button says Fetch missing. | qobuz-dl-go and amdl skip complete files, and resume depends on that. |
| A track a cancel or a shutdown cut off goes back to `pending`. | `tracks_failed` then counts only real failures. |
| `/api/downloads` lists top-level downloads. An artist's or label's albums are in its detail. | An artist can have hundreds of albums. |
| A module may register its source switched off through `source.Switch`, and shelf still recognises its links. | The bot can say "Apple Music is not set up in Shelf" instead of "unknown link". |
| A source implements `source.LinkRefuser` for its links shelf can't download, such as music videos. | The bot names the reason without knowing the source. |
| `source.Unavailable` carries a reason the API may show, answered as 503. Other source errors stay in the log. | They may hold a URL with a token. |
| A rate-limited source wraps `source.ErrRateLimited`, and the API answers 429 with a readable message. | Apple Music knew it was limited, and a generic 502 hid that. |
| Navidrome that doesn't answer defers the scan job for a minute, up to ten times. A Subsonic error fails it at once. | A deferred job stays the one queued scan that requests merge into. |

## Qobuz

| Decision | Why |
| --- | --- |
| Qobuz goes through qobuz-dl-go, a Go port of qobuz-dl 2.5.9 with one Go file per Python file, in its own repo on GitHub. | Go can't import the Python library, and matching files turn each upstream release into a diff to port. |
| One qobuz-dl-go instance per format, and one per format without a database for forced downloads. | Quality and `DownloadsDB` are fixed per instance, and `HandleID` can't override them. |
| Album and track downloads pass `IgnoreDB`, so only a playlist's tracks reach `Has`, which ignores the quality. | `Queue` has checked already, and asking again would skip a resumed job with no tracks. |
| Playlists go through `HandleID`. | Only `handle_url` writes the m3u8. |
| The module's HTTP client has no `Timeout`, and qobuz-dl-go times each call. | A client timeout cuts long transfers. |
| Search, lookups and downloads wait up to a minute for the first login attempt. | A download resumed after a restart would otherwise fail with "starting". |
| `ParseLink` also reads the site's `/interpreter/`, `/playlists/` and `/label/<slug>/download-streaming-albums/<id>` links, and refuses a link unless the ID is its whole last segment. | Without that the label form reads as the ID `download`. |
| The import maps `/downloads/<folder>` to `<SHELF_QOBUZ_DIR>/<folder>`, keeps the highest quality per item, keeps the old artist and album, and stays offline. A skip asks the source for what it lacks. | The old container mounted `music/qobuz` at `/downloads`, and the old table has no covers. |
| MP3 files differ byte for byte between runs. | `bogem/id3v2` keeps frames in a map. The audio and tags are the same, and FLAC isn't affected. |

## Apple Music

| Decision | Why |
| --- | --- |
| shelf's image carries the upstream amdl 0.1.4 binary and runs it once per download. No amdl code is copied. | amdl has no cancel or progress API, and its repo has no licence. |
| wrapper-lite runs in its own privileged container, shelf-wrapper, with tini as PID 1 and a Go supervisor that serves a control API for sign-in. | It needs privileges and an Android rootfs the app shouldn't have, tini reaps orphans, and the owner wanted sign-in in the UI. |
| shelf-wrapper starts from upstream's `ghcr.io/worldobservationlog/wrapper-lite` image, pinned by digest. | Upstream now publishes its `lite` branch to GHCR, so the temporary fork is no longer needed. The digest keeps builds reproducible. |
| `login complete, exiting` decides whether a sign-in worked. | The launcher exits 0 however wrapper-lite ends. |
| The wrapper has a `starting` state. | A new or restarted wrapper has no answer yet. |
| The supervisor drops `request: GET /status` lines from its log and redacts the password and token heads. | The log reaches the Status page. |
| The renderer parses the base amdl config as YAML nodes and writes the rendered file before a run. | Comments and key order stay, and Init must not write. |
| A run that ends with `Error detected, exiting...` runs once more. Another failure fails the download. | amdl skips finished files, so the second run is cheap, and a song often passes on it. |
| A Dolby Atmos download fails before amdl runs when no song has the `atmos` trait. | amdl would fail every song, exit 0, and leave a folder with only lyrics and a cover. |
| Catalog tests use hand-written answers in the API's shape, plus recorded answers in `testdata/live` without Apple's editorial notes. | Editorial prose has no place in the repo. |

## Telegram

| Decision | Why |
| --- | --- |
| One status message per download that the bot edits, with Cancel, Retry and Fetch missing, a confirmation before artist and label downloads, and album search by plain text. | One edited message replaces two separate replies per link. |
| The bot sends the status message before it queues the download. | A download that ends at once would reach `notify` before the message existed. |
| The editor keeps one edit per message in the send queue, and the edit reads the download's state when its turn comes. | A message never goes back to an older state. |
| shelf has its own bot, `@music_shelf_bot`. | Two processes can't poll one token, and the old bot ran beside shelf at first. |

## Web UI

The design's own decisions are in [ui/design/README.md](ui/design/README.md): the studio console look, phone first, Night by default, one result list across sources, the lamp on tabs, and no floating notices.

| Decision | Why |
| --- | --- |
| Shared parts live in one `parts.css` generated from the design's `components.css`. Parts one component owns are scoped. | A scoped copy of `.key` or `.row` would repeat in most components. |
| An open panel is `?open=`, the sign-in `?signin=1`, and the result type `?type=`. | A phone's back button closes them, and a link keeps its state. |
| Any `http` or `https` link goes to `/api/resolve`. | shelf knows its sources, and the UI shouldn't repeat that list. |
| Notices and table density are per-browser settings on Status. Notices default to Failures. | A failure needs a person, and phone and desktop may want different levels. |
| The fake source's covers are data URLs. | Any other host breaks the CSP. |
| The Library tab is the home screen and the first tab. `/` and unknown paths open it. | The owner asked for it. The list shows every album's source and quality. |

## Tags

[tags.md](tags.md) describes how the tag editor works.

| Decision | Why |
| --- | --- |
| Navidrome is the test of every write. shelf follows its mappings, split rules and grouping, and a write that reads back but shows wrong in Navidrome has failed. | The owner's rule: shelf exists for Navidrome. |
| No Picard integration. Everything lives in shelf, and Picard stays as it is. | The owner's choice. Picard's HTTP port can't load files, and its web view is a remote desktop that is poor on a phone. |
| Suggestions are hints beside tag fields, with no album card, Apply step or release picker. | The owner asked for a radically simple interface. |
| MusicBrainz IDs and other hidden tags are plain fields behind an All fields switch. Nothing is written unless accepted. | The owner's choice. |
| The server picks one anchor release per album. Release-scoped fields show only its values, and Save refuses suggestions from two anchors. | Accepting fields one at a time could otherwise mix IDs from two releases. |
| shelf edits the whole library, after one chown of the five genre folders to 10001:10001 that keeps their modes. | shelf can't write 184 album folders there, which are root's with mode 0755, or 73 files, which are root's with mode 0644. No file owned by UID 1000 lacks world-write, so nobody loses access. |
| Nothing changes after a download on its own. | The owner's choice. |
| Delete removes the album's whole release folder, with every file in it, after a confirmation. When other albums share the folder, the confirmation names them and they go too. | The owner's choice. A release folder holds the cover, `shelf.txt` and lyrics beside the audio, and a folder left half empty would still show in Navidrome. |
| Delete refuses a top folder, a folder with anything another UID owns or shelf can't write, and a folder a download works in. | A top folder holds a whole source's downloads. shelf never removes what isn't its own, and a download would write into the folder again. |
| A delete is a row in History, of kind `delete`, and nothing undoes it. Retention counts it as a save. | The owner wanted a record of what went. The files are gone, so there is nothing to put back. |
| An album's source comes from the downloads' records by folder, then by folder name, then `shelf.txt`, then the source folder it sits in. The walk stores it on the album. | In production only 13 folders had a `shelf.txt`, while the records named 179 folders, 33 of them moved to `jazz/` under the same name. The source folders account for the albums the tools before shelf wrote with no history. Stored on the album, it lets the list filter in SQL. |
| A source names its folder through the optional `source.Folderer`. | The library never knows which source it talks to, and a switched-off source still names its folder. |
| `00007_deletes.sql` builds `library_changes` again with foreign keys off, in one transaction on the write connection. | SQLite can't change a CHECK in place. With foreign keys on, dropping the old table would delete every change's files, runs and undos. |
| After a delete, shelf asks Navidrome for a scan and removes the folder's missing tracks through `/api/missing`, looking every 30 seconds up to ten times. It leaves them when `Scanner.PurgeMissing` is `always`. | Navidrome keeps a missing track, with its star and plays, until an admin removes it, and its API removes only tracks its scan marked missing. Navidrome then drops the empty album, as the contract test shows. |
| A delete forgets the folder's downloaded items, and the downloads keep their rows. | Otherwise a request for the deleted album would be skipped as already in the library. |
| shelf never renames or moves a file. Delete removes a whole release folder. | The owner's choice. It also avoids Navidrome's limit that retagging and moving can't share a scan. |
| Checks and fixes exist, and only the owner starts them, on one album or on many. | The owner's choice. |
| Missing `RELEASEDATE` is a check, and its fix keeps what Navidrome shows today. Where Navidrome's legacy date rule applies (832 of 1,099 albums), it writes `RELEASEDATE` from the old `DATE` and `DATE` from `ORIGINALDATE`. Where `YEAR` or M4A's `©day` already sets the release date (108), `RELEASEDATE` copies that value. Elsewhere (158), `RELEASEDATE` copies `DATE`. | The owner wants the tag. `mapDates` reads `DATE` as the release date while `ORIGINALDATE` is set and no release date exists, so `RELEASEDATE` alone moved a reissue from 1956 to 2006 in a test. The pair changed nothing. |
| Navidrome showing a reissue year is a check of its own, and the owner picks each fix. | On 23 albums `YEAR` or `©day` turns the legacy rule off, so Navidrome shows `DATE`, such as 1984 for Moanin', whose `ORIGINALDATE` is 1958. No rule can tell which date the owner wants. |
| A cover edit changes the folder's `cover.*` and replaces the embedded front cover in files that have one. The folder keeps one `cover.*`. Delete removes both. Embedded art stops at 1000x1000. | Navidrome shows `cover.*` first, and `cover.jpg` hid a `cover.png` beside it. A track without embedded art shows the album's cover, so 58% of files need no rewrite. FLAC drops a picture over 16 MiB without an error. The guide calls 1000x1000 plenty. |
| Save cover alone writes the cover at once as its own undo step, and staged tag edits keep waiting. Apply to album and Delete stage for the next Save. Apply to album sends the same edit, without `every_track`. | The owner's choice. A staged Delete can still be dropped. A track without embedded art shows the album's cover, so rewriting it changes nothing Navidrome shows. |
| A cover edit that changes a cover file asks first when other albums share the release folder. Its undo and redo ask nothing. | The owner's choice. Navidrome 0.64.2 shows a folder's cover for every album in it, so a Delete on one album took the cover off the other. |
| A replace leaves a disc folder's `cover.*`, unless ffmpeg can't decode it. Delete removes them all. | Navidrome 0.64.2 serves that file as the disc's art, and the release folder's cover outranks it for the album. After a Delete that missed them, Navidrome fell back to the first disc's cover. A damaged one left behind would keep `bad_cover` open after a repair. |
| The web app works out each Fix from the tracks and the suggestions. | An issue names a kind and its fields, and a Fix only stages edits for review, so the server has nothing to decide. Bulk runs have their own copy of the rules on the server, without the suggestions. |
| Staged edits live in the browser per album and don't survive a reload of the page. | The owner edits one album at a time, and a server-side draft needs its own conflict rules. |
| All fields edits the field when its key is the one a main row shows, and a raw key otherwise. | Two rows must never stage two edits on one tag. |
| Only Extended rows have a delete button. Emptying a Standard tag removes it. | The design shows a delete only on custom tags. |
| Esc leaves a field and keeps its edit, and the keyboard then acts on that field. R reverts it. | An input takes every key as typing, so the keyboard's actions work only outside it. Reverting on Esc would lose a Fix the owner is reading. |
| The transcode is MP3 at 256 kbps, as a stream the player can't seek in, and the player asks for it only after the browser refuses a file. | Every browser plays MP3. Seeking needs a known length, and only 13 files are M4A, so a simple stream is enough. |
| A tag that belongs to a single track, such as Title, Track, ISRC, a track ID, track gain or lyrics, is read-only with no track selected. A suggestion may still fill every track with its own ID. | Typing at album level would give every track the same value. |
| A multi-value tag has one input per value, and pressing a suggestion adds it as another value. | Navidrome's guide prefers `ARTISTS` and `ALBUMARTISTS` with one artist per value, and it never splits those. A single input with a separator breaks names that hold the separator. Adding loses nothing, and each value can be removed. |
| Adding a custom field whose key already has a row goes to that row. A new key Navidrome reads goes under Standard. | Two rows must never edit one tag. |
| The Library tab shows the amber lamp only while a bulk run works. | A run can take about 12 hours, and issues would light the lamp on nearly every album. |
| On a phone the editor covers albums, issues and main fields with suggestions. Custom fields, bulk runs and history need a desktop. | The owner's choice. |
| go-taglib reads and writes tags, and `Clear` is never used. Every write is read back. | On 249 tracks it read the same values as Navidrome on every shared key and lost nothing in a write. `Clear` drops every field not passed, and its write call ignores rejected keys. |
| shelf accepts TagLib's limits on formats the library doesn't hold. WMA refuses custom fields, multi-value writes and files with two pictures, and MP3 writes gain an ID3v1 copy of the tags. | go-taglib lost custom keys and reversed multi-value order in WMA, TagLib reorders WMA pictures on save, and go-taglib can't stop the ID3v1 copy. The library has no WMA or MP3 files. |
| Every write goes through a temp copy, a verify and a rename, for all of an album's files together. | A crash or a failed tag write must leave the originals whole. |
| Writes run on a `tags` queue, and ReplayGain and fingerprints on an `analysis` queue. `library` keeps scans. | A long run on `library`, whose one slot scans share, would hold back scans for new downloads. |
| A write updates every alias of a field that the file holds. | Navidrome takes the first alias of a single value, so `DISC` beats `DISCNUMBER`, and merges aliases of a multi-value tag, so a stale `ALBUM ARTIST` adds a second album artist. |
| A Save that moves only some of an album's tracks to a new Navidrome album ID asks first, and says the star and rating leave with them, even after an undo. | Navidrome moves the album's star and rating to the new album. Undo leaves them on neither album. shelf can't restore them, because they belong to each Navidrome user. |
| A Save that merges two albums says the first album's star and rating are lost when the other has its own. A Save that renames an album artist says their star and rating stay with the old name. | Both happen in Navidrome's scan, and shelf can't prevent them. An undo of an artist rename brings the star back. |
| On M4A files with Picard's lowercase atoms, `ORIGINALDATE` and `ORIGINALYEAR` are read-only. | go-taglib can neither change nor remove `----:com.apple.iTunes:originaldate`, and the uppercase atom it adds is read by nobody. This is one album of 13 files. |
| `.mp4` files are never audio to shelf. | The 10 in the library are amdl's animated artwork, and Navidrome skips them. |
| All fields groups tags by whether Navidrome reads them: Standard holds the ones it reads, Extended the rest. | 28 of the library's 89 keys, such as `ENSEMBLE` and `UPC`, never reach Navidrome. The owner should see that before editing one. |
| A ReplayGain run writes track and album values to -18 LUFS with one ffmpeg `ebur128` decode per track, and pools the loudness blocks for album gain. | Pooled blocks gave -21.21 LUFS where one continuous decode gave -21.2. The whole library takes about 12 hours on one CPU. No file has album gain today, and on the album tested the 2,443 existing track gains differ from `ebur128` by 0.5 to 1.2 dB, so a run replaces them. |
| The field table is Navidrome's `mappings.yaml`, copied unchanged with a header naming the release. Format tables add the raw atoms TagLib renames, such as M4A's `©day` as `DATE`. | Navidrome reads by these aliases, so a copy can't drift from them. A newer release is a new copy and a diff to read. |
| A temp copy is `.shelf-tmp-<16 hex>` beside its file, and the sweep at start deletes only regular files with that name that shelf's UID owns, outside dot folders. | Navidrome skips dot files, a rename stays in one folder, and the name can't match anyone else's file. |
| The writer refuses symlinks and files with more than one hard link, and gives a temp copy the original's mode and group. | A rename would replace the link and split the hard link. The census found neither in the library. |
| Tests that check audio through ffmpeg skip where ffmpeg is missing. The FLAC tests never need it. | The image and the dev machines have ffmpeg. CI's runner may not. |
| The index groups files by Navidrome album ID inside a release folder, computing the ID as Navidrome 0.64.2 does under the owner's settings. A disc folder such as `CD 2` belongs to the folder above. | Navidrome's grouping is the measure, so the counts must match. A real Navidrome computed the test IDs. Grouping inside a folder keeps playlist folders and single tracks apart. |
| The navidrome module reads Navidrome's tag settings from `GET /api/config/` at each walk, and shelf keeps the last answer. New settings make the index read every file again. | The owner's split rules and album ID spec decide what Navidrome shows. Reading them needs no TOML parsing and no new mount. |
| shelf judges an artist split by the MusicBrainz artist IDs. Their number must match the number of artists Navidrome splits. Without IDs there is no check. | The IDs are the only evidence of the intended artists that shelf has offline. |
| The index walks at start, every 15 minutes, after each finished download and on request. An opened album always reads its files again. | A walk reads only changed files, and the editor never works from what the index read. |
| The index keeps facts, about 1 KB a file, and no tags. | An opened album reads its files anyway. Tags with lyrics would make the database, and its 14 daily copies, many times larger. |
| An `.ndignore` with no pattern skips its folder, as in Navidrome. One with patterns is logged and its folder indexed. | shelf doesn't match Navidrome's patterns, and a missed ignore shows as a count that differs. |
| The audio endpoint serves the file as it is, and `?transcode=1` streams it for browsers that can't play a format. | Browsers play FLAC. The UI knows what it can play, and the library has 13 M4A files. |
| A Save is checked against the files when it is asked for, runs as a `library.write` job, and is checked again there. The job's payload names only the change, whose row holds the edits. | The editor learns of a conflict at once, a phone that drops its connection can't cut a write off, and payloads stop at 64 KiB. |
| A Save that splits, merges or renames album artists answers 409 until its `confirm` names each of them. | A confirmation given for one warning can't let another through. |
| A source names each folder it writes in through `source.Reporter.Working`. Qobuz names its work folder and both names it ends with, and Apple Music names its whole folder. | `downloads.folder` is written only after a download, Qobuz renames the album folder while it runs, and amdl names its folders only when it ends. |
| Undo and redo work key by key from the full tag maps a save stored. A key goes back only while it holds what the save wrote. | Another tool's later edit is the owner's newer intent, and full maps restore keys the save added or removed. |
| The MusicBrainz release IDs undo and redo as one group. | Restoring some of them could leave a file with IDs from two releases. |
| An undo or redo is checked and confirmed as a Save is. | Putting tracks back moves stars between Navidrome albums too. |
| The history keeps the newest 1,000 saves, trimmed at 05:15. Clear history keeps a save whose undo or redo is still to run. | The owner's limit. Deleting such a save would delete its queued undo through the foreign key. |
| A cover edit names every track, and its cover files are written only after every audio file was replaced, the new file before the old ones go. | It may change each file, and a failure never leaves the folder without a cover. |
| The cover store under `/data` keeps uploads and every cover a change replaces, named by hash, in any format. | Undo needs the old bytes, and a damaged cover is still the one to put back. |
| shelf scales embedded covers down with its own area average, in the image's own format. | Downscaling needs only the standard library, so shelf takes on no new dependency for it. |
| `bad_cover` is found when an album opens, by ffmpeg. Repair stages a cover and writes nothing. | The walk reads no picture bytes. A Fix only stages edits for review. |
| Modules raise a route's body limit through `module.BodyLimiter`. | The platform's 1 MiB limit runs before routing would let a route raise it. |
| A run is a change at the head with a child change per album. | History, retention and undo already work on changes, so a run stays one row and one undo step. A single change over all albums would need temp copies of the whole selection at once. |
| A run's fixes use only what the files hold, and ask no service. | Nobody reads each album before it writes. A wrong MusicBrainz match on an unread album is worse than a tag left alone, and the preview can show what will happen. |
| A run leaves a tie in `values_differ` alone, and doesn't offer `missing_tag`, `two_releases` or `artist_split`. | No rule can tell which value is right, and those three Fixes are only suggestions. |
| A run skips what a Save would ask about: a split, a merge, an album artist rename. | Start asks nothing more, and each of these can lose a star or a rating. The owner settles them one album at a time. |
| One run works at a time. | Two would take turns on the `tags` queue and double the wait of a Save. |
| A run plans each album when its turn comes, not at Start. | A run can take hours, and files change meanwhile. Planning late means no revision goes stale in between. |
| A run asks for one walk and one scan when it ends. | One per album would keep Navidrome scanning for hours. The 15-minute walk still runs. |
| ReplayGain's analysis works ahead of the writes on its own job, and stores results by the hash of the audio. | A decode takes far longer than a write, and a 12-hour run can be cut off. A retag keeps the hash, so nothing decodes twice. |
| A track keeps its loudness blocks as a histogram in 0.1 LU bins. | Whole blocks take 12 KB a track. Bins move a result by under 0.01 LU, and the album's gain still needs the blocks. |
| AcoustID runs as a job when the text lookups found nothing, and the editor asks again when it ends. | `fpcalc` takes seconds a track, and an album must open at once. It stays off without `SHELF_ACOUSTID_KEY`. |
| A deferred job takes in a follow-up that coalesced while it ran. | Defer puts a job back to never having started, and only one such job may hold a coalescing key. The requeue failed on the index, and left the job `running` with nothing behind it. |
| A bulk run has no nightly limit. Its preview shows the files and GB touched, and the owner sizes each run. | Nothing runs by itself. The HDD backup recopies each changed file and keeps no history, and the remote one keeps overwritten files for 30 days. |
| A pasted release ID is the anchor ahead of the one in the files, then barcode, ISRC and search. | Pasting an ID is how the owner moves the album to another release, and the files still name the old one until the Save. |
| A release named by ID, pasted or in the files, stays the anchor when it fails the threshold, and then suggests nothing. A release found by barcode, ISRC or search must pass. | The owner or the files chose the first. Searching for another release would offer IDs the files don't name. |
| A release passes with the album's number of tracks, each within 10 seconds of the release's length and 3 seconds on average. | Editions differ by tracks or by seconds of silence, and a remaster within a second or two is still the same tracklist. |
| A Save refuses edits that name two anchors, and a track that would keep another release's album ID beside the anchor's fields. Only release fields carry an anchor, and typed values never do. | Release fields are what mixes two releases. A title from another edition mixes nothing, so it needs no rule. |
| Without a matched anchor, a release field shows the most confident value, from a catalog or `shelf.txt`. | 12% of tracks have no MusicBrainz ID, and a catalog's label and UPC still help there. |
| Confidence is 1 − (1 − p1)(1 − p2)… over the sources that give a value, each with a base p from a table in [tags.md](tags.md#sources), shown from 1% to 99%. | A documented formula the owner can check, in which agreement raises confidence. |
| File and folder names suggest a field only where the tracks lack it. | Names are cut and cleaned copies of the tags, so where a tag exists a name only offers a worse version. |
| A catalog reads the album its own `shelf.txt` block names, and searches otherwise. | The download's ID is certain, and a search can find another edition. |
| Lyrics are asked per track, when the editor opens them, not with the album. | In a test LRCLIB answered 503 or timed out after 15 seconds on 8 of 10 tracks, which held the album's other suggestions for 30 seconds. One opening also shouldn't make 20 calls to a free service. |
| shelf stores a lookup for 30 days, a "not found" for a day, and a failure not at all. | Releases change little, new ones appear daily, and the next opening should try a failed service again. |
| Suggested covers reach the editor through `/api/library/remote/{id}`, by an ID shelf gave out. | The CSP stays `'self'`, and shelf never fetches a URL the browser chose. |
| MusicBrainz tests use answers written by hand in the API's shape, with made-up IDs, checked once against real answers. | MusicBrainz answered "busy" while the tests were written, and made-up IDs can't be mistaken for real data. |
| The ISRC way uses a recording search by `isrc:`, not the ISRC lookup. | The lookup's JSON leaves out releases, even with `inc=releases`. |
| Suggestions don't use `SHELF_PROXY_URL`. | MusicBrainz, the Cover Art Archive and LRCLIB need no account and serve every country. |

## Deployment

| Decision | Why |
| --- | --- |
| shelf, shelf-wrapper and `ssht-shelf` live in sh-blackberry's `shelf.yaml`, apart from `music.yaml`. | shelf could be checked while the old tools still worked. They were removed once it passed the checklist. |
| shelf-wrapper has its own data folder and `--device-info`. | wrapper-lite derives the device ID from the Apple ID, and `apple-music-wrapper` used the same account. |
| Neither `shelf` nor `shelf-wrapper` updates nightly. Both are pulled together by hand. | They share the control API's types. |
| shelf's data lives in `${SWINGMUSIC_DATA}/shelf` and `${SWINGMUSIC_DATA}/shelf-wrapper`, and the swingmusic backup copies all of it. A restore starts from a daily copy. | The owner's choice, like the other music services. One task that copies everything is easier to keep right. |
| `qobuz/` and `apple-music/` belong to 10001:10001, and the genre folders too after one chown that keeps their modes. Downloads write only into the two source folders. | The tag editor writes in the whole library. A write keeps each file's group, and shelf can give a file only one of its own groups: the first real Save failed on group 1000. Nothing in the genre folders relies on group-write without world-write, so the chown takes no access from the login user or Picard. |


## Library agents

| Decision | Why |
| --- | --- |
| Agent support is off by default and independent of account sign-in. | The owner wants it to be opt-in. |
| External agents connect through scoped tokens on a separate API prefix. Bearer credentials fail on every ordinary API route. | Reading metadata and proposing work must never grant permission to accept or write library changes. |
| The first version proposes tags and artwork. Every selected write requires human acceptance in Shelf. | This is the confirmed initial scope. |
| Agent proposals are persistent server-side drafts with immutable revisions, separate from the editor's browser-only staged edits. | A fresh agent session and another reviewer must see the same pending work. |
| Users can edit values and accept or decline individual operations. The existing library Save checks validate subsets. | Field review needs the same alias, anchor, format and confirmation rules as ordinary editing. |
| Acceptance covers an exact selected diff and confirmations. It may use a newer file revision only while every reviewed effect remains identical. | An unrelated accepted field should not invalidate the remaining review. The library writer still checks revisions when the queued change runs. |
| An acceptance, its decisions and its library change/job commit in one transaction. Request IDs make repeats idempotent. | Crashes and network retries must not duplicate writes or leave accepted work without its job. |
| Ordinary Saves accept proposal tag operations only after writing their exact values on every target file. Agents reconcile retained Save records through the library port. | Clicking a suggestion stages an ordinary edit. A durable cursor recovers missed events without reading the library's tables. |
| Memory and learning progress are shared by the library. Inferred preferences can guide proposals immediately; users can correct or delete them. | The owner wants useful context across fresh runs and replacement agents. No memory rule bypasses acceptance. |
| Manual-tag learning reads existing history. Proposal feedback has its own durable feed. | The agent can learn tagging behavior without recording general UI activity. |
| Proposal artwork uses validated bytes or existing offers, with cover-store pins. | Arbitrary agent URLs must not trigger server fetches, and pending reviews must outlive upload cleanup. |
| The gateway has no agent rule. Shelf checks scoped tokens on the agent prefix, sessions everywhere else, and rejects ambiguous paths. | Production dropped gateway basic auth after the account cutover, so the planned exemption for the agent prefix was never applied. A gateway password would also open ordinary writes, so an agent never gets one. |
