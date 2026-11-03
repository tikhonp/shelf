# Status

## Now

Phase 1. Step 0 finished on 2026-11-03: `CLAUDE.md` and the docs in shelf and qobuz-dl-go, pushed to their remotes.

Waiting on the user: a Claude Design session with [ui/design-brief.md](ui/design-brief.md). Step 1g needs its exported screens.

Next: step 1a in shelf and step 1c in qobuz-dl-go. Neither depends on the other.

## Remotes

| Repo | Origin | Notes |
| --- | --- | --- |
| shelf | `ssh://git@code.tikhonnnnn.com/tikhon/shelf.git` | Forgejo. The image job in 1a needs the Actions secret `REGISTRY_TOKEN` with `write:package`. |
| qobuz-dl-go | `git@github.com:tikhonp/qobuz-dl-go.git` | GitHub only, public. CI runs on GitHub Actions. |

## Steps

| Step | What | State |
| --- | --- | --- |
| 0 | `CLAUDE.md` and docs in both repos | done |
| 1a | shelf skeleton | next |
| 1b | Downloads core | after 1a |
| 1c | qobuz-dl-go | next, in its own repo |
| 1d | Qobuz module | after 1b and 1c |
| 1e | Apple Music module and shelf-wrapper | after 1b |
| 1f | Navidrome and Telegram | after 1b |
| 1g | Web UI | after 1b to 1f and the design |
| 1h | Deployment and cutover | last |

## Log

### 2026-11-03

- The phase 1 plan was approved. Its decisions are in [decisions.md](decisions.md).
- Step 0: both repos initialised on `main`, docs written, one commit each.
- Remotes added and pushed: shelf on Forgejo, qobuz-dl-go on GitHub only. See [decisions.md](decisions.md).

## Notes for the next step

- 1a copies proxier's platform packages. The files to start from are listed in [build/1a.md](build/1a.md).
- The values the old bot runs with in production are in [sources/qobuz.md](sources/qobuz.md), so the Qobuz settings keep parity.
- 1c ports qobuz-dl 2.5.9. The behavior notes in qobuz-dl-go's `docs/port-spec.md` were drawn from 2.5.5 plus the 2.5.6 to 2.5.9 changelog, so the 2.5.9 source wins where they differ.
