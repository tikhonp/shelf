# Decisions

Newest first. A changed decision gets a new entry that names the one it replaces.

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
