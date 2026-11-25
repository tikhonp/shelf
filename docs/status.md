# Status

Shelf runs in production on `blackberry` at `shelf.music.tikhonnnnn.com` with the bot `@music_shelf_bot`.

## What runs in production

- **Downloads.** Web UI and Telegram bot search and download from Qobuz (`qobuz-dl-go` `v0.1.4`) and Apple Music (`amdl` and `shelf-wrapper`, built from `ghcr.io/worldobservationlog/wrapper-lite` pinned by digest). Navidrome scans after downloads finish. The old bot's 169 downloads are imported. Downloads are opt-in behind `SHELF_DOWNLOADS_ENABLED=true`.
- **Library and tags.** The Library tab is the home screen. It edits tags and covers, plays tracks, shows album sources and release types, runs checks and bulk ReplayGain fixes, and keeps undoable history per album and globally. `make contract` passes against Navidrome `0.64.2`.
- **Accounts.** Production signs in with Shelf accounts (`SHELF_AUTH_PROVIDER=simple`) and passkeys, with gateway basic auth removed.
- **Agents.** Optional library agents (`SHELF_AGENT_ENABLED=true`) run in production. The built-in setup prompt stays general, while the owner's personal rules live in Memory and [agent-personal-prompt.md](agent-personal-prompt.md).
- **Deploys.** Pushing a `v*` tag builds both images and deploys through `sh-blackberry` and `doco-cd` ([deployment.md](deployment.md#deploys)).
- **Last recorded deploy.** `v1.2.8` on 2026-11-23. Releases `v1.2.4` through `v1.2.8` added Apple Music partial-run reconciliation, Inbox review hardening, the generalized agent setup prompt, batched proposal planning, concurrent-undo protection, dependency updates, and inline styles in `style-src`.

Every check in [manual-checklist.md](manual-checklist.md) passed in production. Known unfixed bugs and dependency constraints live in [issues.md](issues.md).

## Remotes

| Repo | Origin | Notes |
| --- | --- | --- |
| `shelf` | `ssh://git@code.tikhonnnnn.com/tikhon/shelf.git` | Forgejo. CI pushes images with `REGISTRY_TOKEN` (`write:package`) and updates `sh-blackberry` with `SH_BLACKBERRY_DEPLOY_KEY`. |
| `qobuz-dl-go` | `git@github.com:tikhonp/qobuz-dl-go.git` | Public on GitHub with GitHub Actions CI. |

## Operational notes

- Source modules log in and start goroutines in `Run`, never `Init`, and register with `Switch` when settings are missing.
- A wrapper signed in to the owner's Apple ID needs its own data folder and `--device-info`.
- Unset `SHELF_QOBUZ_TOKEN` for local runs with only the fake source, or Shelf logs in to the owner's Qobuz account.
- Music files belong to UID `10001`. Delete test downloads from inside the `shelf` container.
- *Third* (Portishead) and *Wish You Were Here 50* came from a checklist run and are kept in the library.
