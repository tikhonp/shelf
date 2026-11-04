# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Music Shelf, short name shelf, manages a Navidrome music library. It downloads releases from Qobuz and Apple Music into the library, searches both catalogs, and keeps a record of every download. One Go binary with one SQLite file and a Vue web UI, deployed on blackberry next to Navidrome behind the sh-main gateway. Docs, code, logs and UI are in English.

State: phase 1 is done, and shelf runs in production on blackberry. Steps 0 and 1a to 1h built the docs, the shelf skeleton with the platform, the image and CI, the downloads core with the jobs system, the source interfaces, the downloads, catalog, fake source, qobuz, applemusic, navidrome and telegram modules, shelf-wrapper, the Vue web UI, and the deployment. Step 1c is qobuz-dl-go `v0.1.0`. The UI design is exported to `docs/ui/design/`. No phase 2 is planned yet. `docs/status.md` says what is done and what comes next.

## Read first

- `docs/status.md`: where the work stands, and notes for the next step.
- `docs/README.md`: the map of the docs.
- `docs/decisions.md`: what was decided and why.
- `docs/architecture.md`, then `docs/data-model.md`, then the doc of the part you touch.
- The step's contract in `docs/build/` before starting the step.
- For the UI: `docs/ui/design/README.md` and its screens, then `docs/ui/design-brief.md`. The design wins where they differ.

## Rules for all code

- Every edge case in a doc or a step contract gets a test. A behavior change updates the docs in the same commit.
- Modules live under `internal/modules/<name>` and are enabled in `cmd/shelf/main.go`. A module never reads another module's tables. It talks to others through ports, which are small interfaces, and through the event hub.
- `Init` reaches no network and starts no goroutine, because `manage` opens the platform too. Work that lasts as long as the server goes in `Run` from `module.Runner`.
- A source is a module that implements the interfaces in `internal/source`. Code outside that module never knows which source it talks to.
- Nothing slow or remote happens inside a database transaction. Read, act, then write.
- Schema changes go only into new goose migrations. Tables are STRICT. Stored times are UTC text.
- The music folder belongs to UID 10001. shelf runs as that user with umask 022, never changes the owner or mode of a file it did not create, and deletes only its own temp files.
- Secrets never reach logs, job logs, `shelf.txt` or API answers. That covers the Qobuz token, the Telegram token, the Navidrome password, the wrapper token, and Apple ID passwords and codes.
- Comments say why, not what. Match the surrounding code.
- There is no error reporting service. Errors are logged with slog.

## How to work

- Run `/tikhon:unslop` on all prose before committing: docs, this file, READMEs, code comments, UI texts and bot texts.
- Commit messages are one short line, such as `Add Qobuz search`, with no body. Commit on `main` and create no branches.
- At the end of each step, update `docs/status.md`, fill in the As built section of the step's contract, and update `docs/decisions.md` if a decision changed.
- When tests, lint, a build or a subagent's work fails, fix it and go on. Ask only when it can't be solved, and mention the fix in the summary.
- Never run `caffeinate` or `pkill caffeinate`.
- Changes to `~/projects/sh-blackberry`, `~/projects/sh-main` and sh-secrets are drafted for the user to commit.

## Commands

```sh
make check        # gofmt, vet, go test -race
make lint         # golangci-lint and govulncheck in Docker
make web-check    # npm ci, vue-tsc, Vitest and the Vite build
make dev          # docker compose with air hot reload on :8080
make manage ARGS="migrate status"   # shelf manage inside the dev container
make manage ARGS="download https://fake.test/album/album-1"   # with SHELF_DEV_FAKE_SOURCE=true
make manage ARGS="import-qobuz-db /path/to/qobuz_dl.db"   # the old bot's history
make build-image  # code.tikhonnnnn.com/tikhon/shelf:local
make build-wrapper-image  # code.tikhonnnnn.com/tikhon/shelf-wrapper:local, amd64 under emulation on a Mac
SHELF_DATA_DIR=./data go run ./cmd/shelf serve   # /data can't be created on macOS; the shell's SHELF_QOBUZ_TOKEN reaches it
npm --prefix web run dev   # Vite on :5173, with /api sent to :8080
```

`npm --prefix web run build` writes the UI into `internal/platform/ui/dist`, which the binary embeds. Without a build, shelf serves a placeholder page.

Before committing: `make check`, `make lint`, `make web-check`, `docker build .`.

## Layout

```
cmd/shelf/              serve | manage <command> | healthcheck | version
cmd/shelf-wrapper/      supervisor for the wrapper-lite image: runs it, signs it in, serves the control API
internal/platform/      config, obs, db, migrations, httpx, module, web, ui, jobs, backup, hub, testwait
internal/source/        catalog types, the source interfaces and the registry
internal/wrapperapi/    the control API's types, shared by shelf and shelf-wrapper
internal/modules/       downloads, catalog, fakesource, navidrome, telegram, qobuz, applemusic
web/                    the Vue app
docs/                   all project information
```

## Related repositories

- `~/projects/qobuz-dl-go`: the Go port of qobuz-dl that the Qobuz module imports.
- `~/projects/proxier`: where the platform packages and most conventions come from.
- `~/projects/vk2tg`: the Telegram client and its send queue.
- `~/projects/sh-blackberry` and `~/projects/sh-main`: the production compose files and the gateway.
- `~/projects/wrapper`: wrapper-lite sources on the `lite` branch.
- `~/projects/apple-music-downloader`: amdl sources, for reading only. The repo has no licence, so no code is copied from it.
- `~/projects/music-download-bot` and `~/projects/apple-music-dl-http-wrapper`: the services shelf replaces.

## Glossary

Use these words in code, UI, docs and conversation.

**Source.** A music service shelf searches and downloads from: Qobuz or Apple Music. _Avoid_: provider, service, backend.

**Item.** A track, album, playlist, artist or label in a source's catalog, named by a ref made of the source, the kind and the ID. _Avoid_: entity, object.

**Download.** One request to fetch an item, with its progress and result. A job is the background work behind a download. _Avoid_: task.

**Release folder.** The folder a download writes into: an album folder, a playlist folder, or the folder a single track lands in.

**Provenance file.** `shelf.txt` in a release folder, which records where the files came from. _Avoid_: metadata file, info file.

**Format.** What a download asks the source for, such as Qobuz 27 or Apple Music ALAC.

**Quality.** What an item offers or a file has, such as FLAC 24-bit 96 kHz. _Avoid_: bitrate, for lossless files.

**Wrapper.** wrapper-lite in its own container, which amdl needs to decrypt Apple Music.

**Supervisor.** The shelf-wrapper process that runs the wrapper and serves the control API.

**Sign-in.** Logging the wrapper into an Apple ID, including the 2FA code step. _Avoid_: login, in the UI.

**Scan.** The Navidrome library scan that shelf starts after downloads.

**Status message.** The Telegram message the bot edits while one download moves.
