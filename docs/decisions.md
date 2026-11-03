# Decisions

Newest first. A changed decision gets a new entry that names the one it replaces.

## 2026-11-04: step 1f

| Topic | Decision | Why |
| --- | --- | --- |
| Start hook | A module with work that lasts as long as the server implements `module.Runner`. Only serve calls `Run`; one that fails is logged and the server goes on. | `manage` opens the platform too, and a bot that can't reach Telegram must not stop downloads. |
| Status message first | The bot sends a download's status message before it queues the download, and passes the message's ID in `origin_ref`. Replaces 1b's note about a service method that saves `status_message_id` later. | A download that ends at once would reach its `notify` step before the message existed. |
| Edits read late | The editor keeps one edit per message in the send queue, and the edit reads the download's state when its turn comes. It never shows a state that has ended. | Whichever edit runs later reads the newer state, so a message never shows an older state after a newer one. |
| Refused links | A source implements `source.LinkRefuser` for its links shelf can't download, such as music videos. | The bot names the reason without knowing the source. |
| Failed scans | Navidrome that doesn't answer defers the scan job for a minute, up to ten times; a Subsonic error fails it at once. | A deferred job stays the one queued scan that requests merge into; retries with backoff left one job per request. |
| Health on the Status page | `GET /api/library/scan` reads Navidrome live and says whether it answers. `GET /api/telegram` says whether the bot is connected. | The Status screen shows both, and 1g's contract listed no route for the bot. |

## 2026-11-04: UI design

Made in the Claude Design session. They change what `ui/design-brief.md` and 1g's contract said, and both now match. The screens and rules are in [ui/design/](ui/design/README.md).

| Topic | Decision | Why |
| --- | --- | --- |
| Look | The look is a recording studio built around a British mixing console: blue-grey modules, ivory push buttons, red and blue knobs, lamps, small displays. It is only visual, and labels use the glossary. Replaces the brief's neutral chrome with one accent color. | The owner's choice, with usability first: WCAG AA text in both themes and 44px touch targets. |
| Devices | The UI is phone first for searching and downloading; the desktop uses the same parts with more room. Replaces the brief's "mostly a desktop browser". | The owner's stated priority. |
| Theme | Night, the dark theme, is the default. A switch turns on Day, and the app remembers the choice. Replaces the brief's and 1g's "follows the system". | The owner's stated priority. |
| Search sources | Search covers every source that is on, and the results come in one list with the source named on each row. A Sources switch opens one on and off switch per source. Replaces the brief's switch between Qobuz and Apple Music. | More sources may come, and a switch between two would not grow with them. |
| Shell | The desktop has a menubar and the phone a tab bar. A tab shows one amber lamp while there is something to look at, and never a number. Replaces the brief's left sidebar and the count on Downloads. | The owner asked for a lamp in place of the count. |
| Notices | Nothing floats over the content. On the phone a notice such as Queued takes its own place between the window and the tab bar. Replaces the brief's toasts in a corner. | The owner said a notice on top of the content doesn't look like a real device. |
| Source names | Source names are plain text on each row, with no color dot or logo. Replaces the brief's source marker with a dot. | The owner asked for text only. |
| Lamp colors | Only `ui/design/README.md` explains them; the UI has no legend. | The owner asked for that. |

## 2026-11-03: step 1b

| Topic | Decision | Why |
| --- | --- | --- |
| Job queue | A job request may name its queue. `downloads.download` runs on the queue named after its source, or on `system`. | One job type with its steps, and still one download per source at a time. |
| Source errors | The step records a source's error in `resolve` or `fetch` on the download, and the job goes on to `finish` and `notify`, which end the download `failed` or `partial`. Only shelf's own errors fail the job. | One path ends every download, and the bot's final message always comes from `notify`. |
| Notify job | A download that is canceled, or whose job fails, gets a `downloads.notify` job. | Those never reach their own `notify` step. |
| Switched-off sources | A module may register a source switched off through `source.Switch`. shelf still recognises its links and refuses them by name. | The bot answers "Apple Music is not set up in Shelf" instead of "not from Qobuz or Apple Music". |
| Downloads list | `/api/downloads` lists top-level downloads; an artist's or label's albums are in its detail. | An artist can have hundreds of albums. |
| Job subjects | The jobs table has no subject columns. | They served proxier's events and Jobs page; a download points at its job instead. |

## 2026-11-03: step 1a

| Topic | Decision | Why |
| --- | --- | --- |
| Config | `config.Load` takes a lookup function with `os.LookupEnv`'s signature. An unset variable takes its default, and one set to an empty value is empty. Replaces 1a's `Load(getenv)`. | `getenv` can't tell unset from empty, yet `SHELF_AMDL_CONFIG` and `SHELF_NAVIDROME_URL` have defaults and an empty value must turn their part off. |

## 2026-11-03: qobuz-dl-go interface

Made in qobuz-dl-go's design session. They change what shelf's 1c and 1d contracts said.

| Topic | Decision | Why |
| --- | --- | --- |
| Types | Dicts inside the port, typed views outside, each with `Raw`. Replaces 1c's typed structs throughout. | Names and tags must see values with Python's types. |
| Templates | A port of `str.format`, without `PartialFormatter`, which upstream never uses. Replaces 1c's template row. | The source. |
| Playlists | shelf downloads playlists through `HandleID`, not `DownloadFromID`. Replaces that part of 1d's download row. | Only `handle_url` writes the m3u8. |
| HTTP client | shelf's client has no `Timeout`; qobuz-dl-go times each call. Replaces 1d's 30-second client. | A client timeout cuts long transfers. |

## 2026-11-03: qobuz-dl-go hosting

| Topic | Decision | Why |
| --- | --- | --- |
| qobuz-dl-go hosting | Only on GitHub, `github.com/tikhonp/qobuz-dl-go`, public, with CI on GitHub Actions. Replaces the phase 1 plan's Forgejo repo with a GitHub push mirror. | The user created it there. `go get` fetches the module from GitHub either way. |

## 2026-11-03: phase 1 plan

| Topic | Decision | Why |
| --- | --- | --- |
| Name | shelf, shown as Music Shelf. Module `github.com/tikhonp/shelf`, image `code.tikhonnnnn.com/tikhon/shelf`, env prefix `SHELF_`, host `shelf.music.tikhonnnnn.com`. | The user's choice. |
| Qobuz | A Go port of qobuz-dl 2.5.9 in its own repo, qobuz-dl-go, with one Go file per Python file. shelf imports it as a module. | Go cannot import the Python library. Matching files turn each upstream release into a diff to port, and qobuz-dl is well maintained. |
| Apple Music | shelf's image carries the upstream amdl 0.1.4 binary and ffmpeg and runs amdl once per download. | amdl's download loop has no cancel or progress API, and its repo has no licence. The binary is what runs today, so the files come out the same. |
| Wrapper | wrapper-lite stays a separate privileged container, rebuilt as shelf-wrapper: the upstream image plus a Go supervisor with a control API for sign-in and 2FA. | It needs privileges and an Android rootfs that the main app should not have. The supervisor brings sign-in into the UI, which the user asked for. |
| Telegram | The bot stays as the phone entry point with a new flow: one status message per download that the bot edits, buttons for cancel, retry and download again, a confirmation before artist and label downloads, and album search by plain text. | The user doesn't need the old flow kept. One edited message replaces two separate replies per link. |
| Provenance | Every download writes `shelf.txt` into its release folder. | The user asked for it. It also lets a later phase link folders to catalog items again without the database. |
| Permissions | shelf runs as UID 10001 with umask 022. New folders are 0755 and new files 0644. Existing files keep their owner and mode. | Navidrome keeps reading through its read-only mount, and shelf and Picard keep write access. |
| Docs and commits | `CLAUDE.md` and `docs/` in each repo hold all project information, with `docs/status.md` as the running log. `/tikhon:unslop` on all prose. One-line commit messages on `main`. | The user's rules for this project. |
| Backend | proxier's stack: Echo v5, modernc SQLite through sqlx, goose migrations per module, env config parsed by hand, slog JSON, and proxier's module pattern and jobs platform, copied and trimmed. | The conventions of the user's other Go apps. |
| Frontend | Vue 3, Vite and TypeScript, built in a Docker stage and embedded with `embed.FS`, under a strict CSP. | htmx would fall short for the library table of later phases. |
| Access | nginx `auth_basic` on the gateway, as for audiomuse and Picard. No login inside the app in phase 1. | The gateway's pattern for apps without their own login. |
| Files | Qobuz still writes to `music/qobuz` and Apple Music to `music/apple-music`, with today's folder and file names. | Navidrome, Picard and AudioMuse see no change. |
| Runtime image | `alpine:3.22` with ffmpeg, running as UID 10001. | amdl's FLAC conversion and the Qobuz segmented fallback call ffmpeg. UID 10001 owns the music folder. |
| Data | `${DATA_VOLUME}/shelf`, a daily `VACUUM INTO` copy in `backups/`, and only `backups/` rsynced to the HDD. | proxier's backup pattern. Today the bot's database is rsynced while it is open. |
| Licence | shelf is AGPL-3.0 like proxier. qobuz-dl-go is GPL-3.0 because qobuz-dl is. | GPL-3.0 code can be combined with AGPL-3.0 code. |
| Dev source | A `fakesource` module with canned catalog data and fake downloads, enabled by `SHELF_DEV_FAKE_SOURCE`, used by tests and by UI work without real accounts. | Steps 1b and 1g need a source before the real ones exist. |
