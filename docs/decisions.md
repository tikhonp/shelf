# Decisions

The decisions in force and why. When one changes, edit its row and the docs it touches in the same commit. Git history has the earlier versions.

## Product

| Decision | Why |
| --- | --- |
| The name is shelf, shown as Music Shelf: Go module `github.com/tikhonp/shelf`, image `code.tikhonnnnn.com/tikhon/shelf`, env prefix `SHELF_`, host `shelf.music.tikhonnnnn.com`. | The owner's choice. |
| Qobuz writes to `music/qobuz` and Apple Music to `music/apple-music`, with the old tools' folder and file names. | Navidrome, Picard and AudioMuse see no change. |
| Every download writes `shelf.txt` into its release folder. | The owner asked for it, and a later phase can link folders to catalog items again without the database. |
| Access is nginx `auth_basic` on the gateway, with no login in the app. | The gateway's pattern for apps without their own login. |
| shelf is AGPL-3.0 like proxier, and qobuz-dl-go GPL-3.0 like qobuz-dl. | GPL-3.0 code can be combined with AGPL-3.0 code. |

## Stack

| Decision | Why |
| --- | --- |
| proxier's stack, copied and trimmed: Echo v5, modernc SQLite through sqlx, goose migrations per module, env config parsed by hand, slog JSON, the module pattern and the jobs platform. | The conventions of the owner's other Go apps. |
| `config.Load` takes a lookup function with `os.LookupEnv`'s signature. Unset takes the default, and empty is empty. | `SHELF_AMDL_CONFIG` and `SHELF_NAVIDROME_URL` have defaults, and an empty value must turn their part off. |
| A module with work that lasts as long as the server implements `module.Runner`. Only serve calls `Run`, and one that fails is logged while the server goes on. | `manage` opens the platform too, and a bot that can't reach Telegram must not stop downloads. |
| The jobs table has no subject columns. A download points at its job. | They served proxier's events and Jobs page. |
| Vue 3, Vite and TypeScript, embedded with `embed.FS`, under a strict CSP. | htmx would fall short for the library table of later phases. |
| The runtime image is `alpine:3.22` with ffmpeg, running as UID 10001 with umask 022. Existing files keep their owner and mode. | amdl's FLAC conversion and Qobuz's segmented fallback call ffmpeg, and 10001 owns the source folders. |
| A `fakesource` module with canned data, on with `SHELF_DEV_FAKE_SOURCE`. | Tests and UI work without real accounts. |

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

## Tags

Planned for phase 2 and not built yet. The rows under Deployment stay in force until the editing backend ships.

| Decision | Why |
| --- | --- |
| Navidrome is the test of every write. shelf follows its mappings, split rules and grouping, and a write that reads back but shows wrong in Navidrome has failed. | The owner's rule: shelf exists for Navidrome. |
| No Picard integration. Everything lives in shelf, and Picard stays as it is. | The owner's choice. Picard's HTTP port can't load files, and its web view is a remote desktop that is poor on a phone. |
| Suggestions are hints beside tag fields, with no album card, Apply step or release picker. | The owner asked for a radically simple interface. |
| MusicBrainz IDs and other hidden tags are plain fields behind an All fields switch. Nothing is written unless accepted. | The owner's choice. |
| The server picks one anchor release per album. Release-scoped fields show only its values, and Save refuses suggestions from two anchors. | Accepting fields one at a time could otherwise mix IDs from two releases. |
| shelf edits the whole library, after one chown of the five genre folders to 10001 that keeps their modes. | shelf can't write 184 album folders there, which are root's with mode 0755, or 73 files, which are root's with mode 0644. No file owned by UID 1000 lacks world-write, so nobody loses access. |
| Nothing changes after a download on its own. | The owner's choice. |
| shelf never renames or moves a file. | The owner's choice. It also avoids Navidrome's limit that retagging and moving can't share a scan. |
| Checks and fixes exist, and only the owner starts them, on one album or on many. | The owner's choice. |
| Missing `RELEASEDATE` is a check, and its fix keeps what Navidrome shows today. Where Navidrome's legacy date rule applies (832 of 1,099 albums), it writes `RELEASEDATE` from the old `DATE` and `DATE` from `ORIGINALDATE`. Where `YEAR` or M4A's `©day` already sets the release date (108), `RELEASEDATE` copies that value. Elsewhere (158), `RELEASEDATE` copies `DATE`. | The owner wants the tag. `mapDates` reads `DATE` as the release date while `ORIGINALDATE` is set and no release date exists, so `RELEASEDATE` alone moved a reissue from 1956 to 2006 in step 0. The pair changed nothing. |
| Navidrome showing a reissue year is a check of its own, and the owner picks each fix. | On 23 albums `YEAR` or `©day` turns the legacy rule off, so Navidrome shows `DATE`, such as 1984 for Moanin', whose `ORIGINALDATE` is 1958. No rule can tell which date the owner wants. |
| A cover edit changes the folder's `cover.*` and replaces the embedded front cover in files that have one. The folder keeps one `cover.*`. Delete removes both. Embedded art stops at 1000x1000. | Navidrome shows `cover.*` first, and `cover.jpg` hid a `cover.png` beside it. A track without embedded art shows the album's cover, so 58% of files need no rewrite. FLAC drops a picture over 16 MiB without an error. The guide calls 1000x1000 plenty. |
| On a phone the editor covers albums, issues and main fields with suggestions. Custom fields, bulk runs and history need a desktop. | The owner's choice. |
| go-taglib reads and writes tags, and `Clear` is never used. Every write is read back. | In step 0 it read the same values as Navidrome on every shared key of 249 tracks and lost nothing in a write. `Clear` drops every field not passed, and its write call ignores rejected keys. |
| WMA refuses custom fields and multi-value writes. | go-taglib lost custom keys and reversed multi-value order in WMA. The library has no WMA files. |
| Every write goes through a temp copy, a verify and a rename, for all of an album's files together. | A crash or a failed tag write must leave the originals whole. |
| Writes run on a `tags` queue, and ReplayGain and fingerprints on an `analysis` queue. `library` keeps scans. | A long run on `library`, whose one slot scans share, would hold back scans for new downloads. |
| A write updates every alias of a field that the file holds. | Navidrome takes the first alias of a single value, so `DISC` beats `DISCNUMBER`, and merges aliases of a multi-value tag, so a stale `ALBUM ARTIST` adds a second album artist. |
| A Save that moves only some of an album's tracks to a new Navidrome album ID asks first, and says the star and rating leave with them, even after an undo. | Navidrome moves the album's star and rating to the new album. Undo leaves them on neither album. shelf can't restore them, because they belong to each Navidrome user. |
| A Save that merges two albums says the first album's star and rating are lost when the other has its own. A Save that renames an album artist says their star and rating stay with the old name. | Both happen in Navidrome's scan, and shelf can't prevent them. An undo of an artist rename brings the star back. |
| On M4A files with Picard's lowercase atoms, `ORIGINALDATE` and `ORIGINALYEAR` are read-only. | go-taglib can neither change nor remove `----:com.apple.iTunes:originaldate`, and the uppercase atom it adds is read by nobody. This is one album of 13 files. |
| `.mp4` files are never audio to shelf. | The 10 in the library are amdl's animated artwork, and Navidrome skips them. |
| All fields groups tags by whether Navidrome reads them: Standard holds the ones it reads, Extended the rest. | 28 of the library's 89 keys, such as `ENSEMBLE` and `UPC`, never reach Navidrome. The owner should see that before editing one. |
| A ReplayGain run writes track and album values to -18 LUFS with one ffmpeg `ebur128` decode per track, and pools the loudness blocks for album gain. | Pooled blocks gave -21.21 LUFS where one continuous decode gave -21.2. The whole library takes about 12 hours on one CPU. No file has album gain today, and on the album tested the 2,443 existing track gains differ from `ebur128` by 0.5 to 1.2 dB, so a run replaces them. |
| A bulk run has no nightly limit. Its preview shows the files and GB touched, and the owner sizes each run. | Nothing runs by itself. The HDD backup recopies each changed file and keeps no history, and the remote one keeps overwritten files for 30 days. |

### Evidence from step 0

Run on 2026-11-04 on blackberry in the shelf image as UID 10001, on 25 albums copied from the library (249 tracks) and 17 rare files, with a scratch Navidrome 0.64.2 and the owner's split rules. The library was mounted read-only, and the copies are deleted.

| Test | Result |
| --- | --- |
| Preservation | Adding a field, removing it, writing hard values, replacing a cover and deleting it lost no tag, picture or audio byte. That covers an ID3 prefix, zero padding, APPLICATION blocks, lowercase keys and pictures without a MIME type. Deliberate damage was caught, so the checks can fail. TagLib sorts FLAC comments and uppercases their keys, and Navidrome ignores both. |
| Readers | go-taglib 0.15.0 and Navidrome agree on every shared key of all 249 tracks. Only Navidrome splits `3/9` track numbers, adds a language to `lyrics` and lists raw MP4 atoms. |
| Tracks | A retag in place keeps a track's ID, star, rating, plays and playlists, even when its persistent ID changes. |
| Albums | Renaming an album, changing its MusicBrainz ID or adding MusicBrainz IDs changes the album ID, and the star, rating and plays follow. Undo restores the old ID. A scan that lands between two halves of a rename ends the same. |
| Split | Moving one track off an album moved the star and rating to the new album. Undoing it left them on neither. |
| Merge | A source album's star and rating moved to an unmarked target. A target with its own rating kept it, and the source's was lost. |
| Artists | An artist renamed in tags starts with no star or rating. They stay under the old name's ID, so an undo brings them back. |
| Recently Added | Rewriting all of an album's files through temp files left its created time and position alone. |
| Dates | `YEAR` and M4A's `©day` are release date aliases, and `RELEASEDATE` beats `YEAR`. On an album without a MusicBrainz ID, a new release date changes the album ID and keeps the star. |
| Covers | An embedded change alone left the album cover as it was and changed the track's. With `cover.*` gone, the embedded cover showed. Tracks without embedded art showed the album's cover on 19 of 19 albums. |
| Lyrics | An `.lrc` hides embedded lyrics. Without a sidecar the embedded text shows. |
| Temp files | Navidrome skips dot files. TagLib detects the format by content, so a temp name needs no extension. |
| Watcher | Navidrome showed a retag 5.6 seconds after the write, with no scan request. |
| Config | `GET /api/config/` returns the split rules, exceptions, persistent ID specs and priorities to an admin. Without the trailing slash it is a 404. |
| Census | 89 keys in 11,457 files, read in 26 seconds with no errors or lost keys. No file has two pictures, and no file is a hard link or a symlink. The longest file name is 187 bytes. |
| Broken files | Empty, garbage and damaged-header files return an error, with no panic. A half-truncated file reads and writes without one. |
| AcoustID | Alpine's `chromaprint` ships `fpcalc`. |

Not tested: that `RELEASEDATE` copying `YEAR`'s exact value keeps an album ID, which follows from the persistent ID code, and `/api/config/` with production's account. Step 3 covers both.

## Deployment

| Decision | Why |
| --- | --- |
| shelf, shelf-wrapper and `ssht-shelf` live in sh-blackberry's `shelf.yaml`, apart from `music.yaml`. | shelf could be checked while the old tools still worked. They were removed once it passed the checklist. |
| shelf-wrapper has its own data folder and `--device-info`. | wrapper-lite derives the device ID from the Apple ID, and `apple-music-wrapper` used the same account. |
| Neither `shelf` nor `shelf-wrapper` updates nightly. Both are pulled together by hand. | They share the control API's types. |
| shelf's data lives in `${SWINGMUSIC_DATA}/shelf` and `${SWINGMUSIC_DATA}/shelf-wrapper`, and the swingmusic backup copies all of it. A restore starts from a daily copy. | The owner's choice, like the other music services. One task that copies everything is easier to keep right. |
| Only `qobuz/` and `apple-music/` belong to 10001. The genre folders keep their mixed owners. | shelf writes only there, and the login user and Picard keep writing elsewhere. |
