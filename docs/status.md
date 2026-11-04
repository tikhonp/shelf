# Status

## Now

Phase 1. Steps 0, 1a, 1b, 1c and 1f are done. 1a built the shelf skeleton: the trimmed platform from proxier, `serve`, `manage migrate`, `healthcheck` and `version`, a placeholder Vue page, the image and the Forgejo CI workflow. CI passed on its first run and pushed `code.tikhonnnnn.com/tikhon/shelf` with the tags `latest`, `main` and the commit SHA.

1b built the downloads core: the jobs system, the daily backup and the event hub in the platform, the source types and registry in `internal/source`, and the `downloads`, `catalog` and `fakesource` modules. With `SHELF_DEV_FAKE_SOURCE=true`, shelf searches, resolves and downloads canned items, writes `shelf.txt`, and streams progress and logs. `shelf manage download <url>` queues from the shell.

1f added the navidrome and telegram modules. Finished downloads ask Navidrome for one scan per burst, `/api/library/scan` shows and starts scans, and the bot queues links, asks before artists, searches albums by text, and keeps one status message per download up to date until the `notify` step gives it its final text. The platform gained `module.Runner`, the start hook that only serve runs.

1e built the applemusic module and shelf-wrapper. The module searches and reads Apple's catalog, downloads with amdl, and follows and signs in the wrapper through `/api/applemusic/wrapper`. shelf-wrapper is wrapper-lite with tini and a Go supervisor that runs service mode, signs in with the 2FA code file, and serves the control API. CI's image job now pushes shelf-wrapper too; it hasn't run yet. A local run of both images on this Mac signed in to the owner's Apple ID and downloaded real albums through shelf, and the fixtures now hold its amdl output. The staging run on blackberry is still to do.

The UI design is done. Its screens, stylesheets and rules are in [ui/design/](ui/design/README.md), exported from the Claude Design canvas on 2026-11-04. The brief and 1g's contract match it.

Step 1c is done. qobuz-dl-go `v0.1.0` is tagged, ports qobuz-dl 2.5.9, and resolves through the Go proxy. Its parity runs against Qobuz gave files byte for byte the same as Python's for a multi-disc hi-res album, a CD album and a playlist.

Next: 1d, the Qobuz module, on qobuz-dl-go `v0.1.0`; the notes for it are in the As built section of [build/1c.md](build/1c.md). 1e's staging run on blackberry still waits for the owner.

## Remotes

| Repo | Origin | Notes |
| --- | --- | --- |
| shelf | `ssh://git@code.tikhonnnnn.com/tikhon/shelf.git` | Forgejo. CI's image job pushes with the Actions secret `REGISTRY_TOKEN`, which has `write:package`. |
| qobuz-dl-go | `git@github.com:tikhonp/qobuz-dl-go.git` | GitHub only, public. CI runs on GitHub Actions. |

## Steps

| Step | What | State |
| --- | --- | --- |
| 0 | `CLAUDE.md` and docs in both repos | done |
| 1a | shelf skeleton | done |
| 1b | Downloads core | done |
| 1c | qobuz-dl-go | done: `v0.1.0` |
| 1d | Qobuz module | next |
| 1e | Apple Music module and shelf-wrapper | built and run locally; the staging run on blackberry is left |
| 1f | Navidrome and Telegram | done |
| 1g | Web UI | after 1b to 1f; the design is done |
| 1h | Deployment and cutover | last |

## Log

### 2026-11-04

- Step 1c: qobuz-dl-go `v0.1.0` is tagged and pushed to GitHub, and `go list -m` resolves it through proxy.golang.org. qobuz-dl-go's steps 6 to 11 are done: the fake Qobuz, the downloader with both transfers, core, the downloads database, the CLI and the parity runs against Qobuz. See the As built section of [build/1c.md](build/1c.md).
- Step 1e, local downloads: the owner signed the local wrapper in, and shelf downloaded a song, three albums and a Dolby Atmos single. The runs changed the module: amdl's bars give per-song progress, a failure printed on a bar's line reaches the log, a run in which a song failed runs once more, shelf refuses Dolby Atmos for a release without it, and the supervisor hides the token heads wrapper-lite prints. The fixtures now hold amdl's output for In Rainbows and Apple's answer for the album. See the As built section of [build/1e.md](build/1e.md) and [decisions.md](decisions.md).
- Step 1e, short of its staging run: the applemusic module, the amdl runner, the wrapper client, `internal/wrapperapi`, `cmd/shelf-wrapper` and the image's `wrapper` target. `make check`, `make lint`, `make web-check` and `docker build .` pass. A local compose run checked the wrapper under Rosetta, the routes, the event and the stop, then search and lookups against Apple's live catalog. The live artist views are recorded as a fixture. What differs from the contract is in the As built section of [build/1e.md](build/1e.md), and the decisions in [decisions.md](decisions.md).

- Step 1f: the navidrome and telegram modules. `make check`, `make lint`, `make web-check` and `docker build .` pass, and a manual run of serve answered the new routes and queued one scan for a finished download. The bot hasn't met the real Telegram yet; the manual checklist covers it in 1h. What differs from the contract is in the As built section of [build/1f.md](build/1f.md), and the decisions in [decisions.md](decisions.md).

- The UI design: the Studio redesign page of the canvas exported to [ui/design/](ui/design/README.md) as `tokens.css`, `components.css`, 29 screens and a README with the rules. The brief and 1g's contract now follow it: phone first, Night by default, and search across every source that is on. See [decisions.md](decisions.md).
- The export found secondary text and links below WCAG AA on parts of both themes. Their new colors pass, on the canvas and in the export.

### 2026-11-03

- Step 1b: the downloads core. `make check`, `make lint`, `make web-check` and `docker build .` pass, and a manual run against the fake source downloaded an artist and a failing album over the API. What differs from the contract is in the As built section of [build/1b.md](build/1b.md), and the decisions in [decisions.md](decisions.md).
- CI passed on its first run, for 1a, and pushed the first image.
- The project folder is now `~/projects/shelf`, not `~/projects/proj-x`.
- qobuz-dl-go's step 5, the taggers from `metadata.py`, is done.
- Step 1a: the shelf skeleton. `make check`, `make lint`, `make web-check` and `docker build .` pass, and a manual run checked the image and `make dev`. `config.Load` takes a lookup function, so an empty value can turn a part off. See [decisions.md](decisions.md).
- The phase 1 plan was approved. Its decisions are in [decisions.md](decisions.md).
- Step 0: both repos initialised on `main`, docs written, one commit each.
- Remotes added and pushed: shelf on Forgejo, qobuz-dl-go on GitHub only. See [decisions.md](decisions.md).
- qobuz-dl-go got its requirements, design questions, references and session prompts, so its design session can start in that repo.
- qobuz-dl-go's design session and its steps 1 to 4 are done. Their findings changed shelf's docs: playlists go through `HandleID`, the module's HTTP client has no `Timeout`, the port keeps dicts inside with typed views outside, and there is no `PartialFormatter`. See [decisions.md](decisions.md). sources/qobuz.md now covers the second bundle fetch, `GetURLInfo`'s gaps, the `Result`, and the `DownloadsDB` signature.

## Notes for the next step

- 1e's staging run: the steps are at the end of the As built section of [build/1e.md](build/1e.md). Sign in only a wrapper with its own data folder and its own `--device-info`: wrapper-lite's device ID comes from the Apple ID. The fixtures are recorded already.
- The As built sections of [build/1b.md](build/1b.md) and [build/1f.md](build/1f.md) end with notes for the later steps. The main ones: a source module logs in and starts goroutines in `Run` from `module.Runner`, never in `Init`, because `manage` opens the platform too; a source registers with `Switch` when its settings are missing; Apple Music implements `source.LinkRefuser` for music videos and stations; and the Status page reads `/api/telegram` and the health fields of `/api/library/scan`.
- The values the old bot runs with in production are in [sources/qobuz.md](sources/qobuz.md), so the Qobuz settings keep parity.
- The As built section of [build/1c.md](build/1c.md) ends with notes for 1d on how an expired token surfaces, what a playlist's progress and `Result` look like, the ffmpeg the host needs, and what `qobuzdltest` offers for the checklist.
- For 1d: `GetURLInfo` reads `/label/<slug>/download-streaming-albums/<id>` as the ID `download` and doesn't match `/playlists/`. Decide in 1d whether `ParseLink` handles these, as it handles `/interpreter/`.
