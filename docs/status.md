# Status

## Now

Phase 1. Steps 0 and 1a are done. 1a built the shelf skeleton: the trimmed platform from proxier, `serve`, `manage migrate`, `healthcheck` and `version`, a placeholder Vue page, the image and the Forgejo CI workflow. CI hasn't run yet, because 1a isn't pushed. Its image job needs the `REGISTRY_TOKEN` Actions secret.

The Claude Design canvas exists, and its Studio redesign page is the current direction. Before step 1g, its screens and tokens go into [ui/design/](ui/design/).

Step 1c is under way in qobuz-dl-go: its design is done, and steps 1 to 4 of its 11 (goldens and fixtures, the Python compatibility layer, utils and settings, the API client) are done. Its step 5, tags, is next. Its `docs/status.md` has the details.

Next: step 1b in shelf, and step 5 in qobuz-dl-go. Neither depends on the other.

## Remotes

| Repo | Origin | Notes |
| --- | --- | --- |
| shelf | `ssh://git@code.tikhonnnnn.com/tikhon/shelf.git` | Forgejo. The image job in 1a needs the Actions secret `REGISTRY_TOKEN` with `write:package`. |
| qobuz-dl-go | `git@github.com:tikhonp/qobuz-dl-go.git` | GitHub only, public. CI runs on GitHub Actions. |

## Steps

| Step | What | State |
| --- | --- | --- |
| 0 | `CLAUDE.md` and docs in both repos | done |
| 1a | shelf skeleton | done |
| 1b | Downloads core | next |
| 1c | qobuz-dl-go | in progress: steps 1 to 4 of 11 done in that repo |
| 1d | Qobuz module | after 1b and 1c |
| 1e | Apple Music module and shelf-wrapper | after 1b |
| 1f | Navidrome and Telegram | after 1b |
| 1g | Web UI | after 1b to 1f and the design |
| 1h | Deployment and cutover | last |

## Log

### 2026-11-03

- Step 1a: the shelf skeleton. `make check`, `make lint`, `make web-check` and `docker build .` pass, and a manual run checked the image and `make dev`. `config.Load` takes a lookup function, so an empty value can turn a part off. See [decisions.md](decisions.md).

- The phase 1 plan was approved. Its decisions are in [decisions.md](decisions.md).
- Step 0: both repos initialised on `main`, docs written, one commit each.
- Remotes added and pushed: shelf on Forgejo, qobuz-dl-go on GitHub only. See [decisions.md](decisions.md).
- qobuz-dl-go got its requirements, design questions, references and session prompts, so its design session can start in that repo.
- qobuz-dl-go's design session and its steps 1 to 4 are done. Their findings changed shelf's docs: playlists go through `HandleID`, the module's HTTP client has no `Timeout`, the port keeps dicts inside with typed views outside, and there is no `PartialFormatter`. See [decisions.md](decisions.md). sources/qobuz.md now covers the second bundle fetch, `GetURLInfo`'s gaps, the `Result`, and the `DownloadsDB` signature.

## Notes for the next step

- 1b builds on the 1a platform. The As built section of [build/1a.md](build/1a.md) lists what differs from the contract and what 1b has to add: the first platform migration, the jobs system and the hub in `module.Deps`, and event streams that end when serve stops.
- The values the old bot runs with in production are in [sources/qobuz.md](sources/qobuz.md), so the Qobuz settings keep parity.
- 1c ports qobuz-dl 2.5.9. qobuz-dl-go's `docs/port-spec.md` has been checked line by line against the 2.5.9 source; the source still wins where they differ.
- For 1d: `GetURLInfo` reads `/label/<slug>/download-streaming-albums/<id>` as the ID `download` and doesn't match `/playlists/`. Decide in 1d whether `ParseLink` handles these, as it handles `/interpreter/`.
