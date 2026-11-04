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
| shelf has its own bot, `@music_shelf_bot`. | Two processes can't poll one token, and the old bot still runs. |

## Web UI

The design's own decisions are in [ui/design/README.md](ui/design/README.md): the studio console look, phone first, Night by default, one result list across sources, the lamp on tabs, and no floating notices.

| Decision | Why |
| --- | --- |
| Shared parts live in one `parts.css` generated from the design's `components.css`. Parts one component owns are scoped. | A scoped copy of `.key` or `.row` would repeat in most components. |
| An open panel is `?open=`, the sign-in `?signin=1`, and the result type `?type=`. | A phone's back button closes them, and a link keeps its state. |
| Any `http` or `https` link goes to `/api/resolve`. | shelf knows its sources, and the UI shouldn't repeat that list. |
| Notices and table density are per-browser settings on Status. Notices default to Failures. | A failure needs a person, and phone and desktop may want different levels. |
| The fake source's covers are data URLs. | Any other host breaks the CSP. |

## Deployment

| Decision | Why |
| --- | --- |
| shelf, shelf-wrapper and `ssht-shelf` live in sh-blackberry's `shelf.yaml`, beside the old pipeline in `music.yaml`, which the owner removes later. | shelf could be checked while the old tools still worked, and stopping `shelf.yaml` is the rollback. |
| shelf-wrapper has its own data folder and `--device-info`. | wrapper-lite derives the device ID from the Apple ID, and `apple-music-wrapper` uses the same account. |
| Neither `shelf` nor `shelf-wrapper` updates nightly. Both are pulled together by hand. | They share the control API's types. |
| shelf's data lives in `${SWINGMUSIC_DATA}/shelf` and `${SWINGMUSIC_DATA}/shelf-wrapper`, and the swingmusic backup copies all of it. A restore starts from a daily copy. | The owner's choice, like the other music services. One task that copies everything is easier to keep right. |
| Only `qobuz/` and `apple-music/` belong to 10001. The genre folders keep their mixed owners. | shelf writes only there, and the login user and Picard keep writing elsewhere. |
