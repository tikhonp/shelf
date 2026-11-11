# Status

shelf is finished and runs in production on blackberry. [vision.md](vision.md) says what it is for.

- **Downloads.** The web UI at `shelf.music.tikhonnnnn.com` and the bot `@music_shelf_bot` search and download from Qobuz, through qobuz-dl-go `v0.1.0`, and from Apple Music, through amdl and shelf-wrapper. Navidrome scans after downloads. shelf imported the old bot's 169 downloads.
- **Tags.** The Library tab edits every tag in the library, with suggestions, checks and fixes, covers, playback, history with undo, and bulk runs with ReplayGain and AcoustID. [tags.md](tags.md) describes it. `make contract` passes against Navidrome 0.64.2.
- **Home.** The Library tab is the home screen and the first tab. Each album shows its source, from the downloads' records, `shelf.txt` or its source folder, and the filter menu filters by source. [tags.md](tags.md#album-sources) describes it.
- **Delete.** The Library tab deletes an album's release folder and has Navidrome forget its tracks, as [tags.md](tags.md#deleting) describes. `make contract` covers it.

Every check in [manual-checklist.md](manual-checklist.md) passed in production, the last on 2026-11-10.

## Remotes

| Repo | Origin | Notes |
| --- | --- | --- |
| shelf | `ssh://git@code.tikhonnnnn.com/tikhon/shelf.git` | Forgejo. CI's image job pushes with the Actions secret `REGISTRY_TOKEN`, which has `write:package`. |
| qobuz-dl-go | `git@github.com:tikhonp/qobuz-dl-go.git` | GitHub only, public. CI runs on GitHub Actions. |

## Production

Production runs `main-24dc4ce`, which the owner deployed on 2026-11-11. Optional [library agents](agents.md) are off by default and on in production. Production signs in with Shelf accounts. [auth.md](auth.md) describes setup.

shelf-wrapper builds from upstream's `ghcr.io/worldobservationlog/wrapper-lite` image pinned by digest. The index holds 11,461 tracks and 1,008 Navidrome album IDs, forming 1,013 albums because 5 Navidrome albums span two folders.

The [sign-in and agent review redesign](plans/signin-and-agent-review-design.md) and its [second pass](plans/signin-and-agent-review-second-pass.md) run in production.

An agent's 401 for a malformed token says whether the prefix is missing, a character is not allowed, or the length is wrong, and it gives the length for the whole token. The old message compared the 43 characters after the prefix with the whole token's length.

An agent connection can get a new secret on Status or with `shelf manage agents secret <id>`, and Connect hides its permission rockers until Choose. Neither is deployed.

On a phone, a proposal no longer scrolls sideways when a field's state quotes a long value or an evidence source is long. Inbox rows have the Library's phone sizes and no longer overlap. On an album with a proposal, the stuck scope LCD no longer has the Proposed header behind it, and rows no longer show through the gap above it. An agent's unit on Status has no empty space under Revoke. Not deployed.

## Operational notes

- Source modules log in and start goroutines in `Run`, never `Init`, and register with `Switch` when settings are missing.
- A wrapper signed in to the owner's Apple ID needs its own data folder and `--device-info`.
- Unset `SHELF_QOBUZ_TOKEN` for a local run with only the fake source, or shelf logs in to the owner's Qobuz account.
- Music files belong to UID 10001. Delete test downloads from inside the shelf container.
- Third, Portishead and Wish You Were Here 50 came from a checklist run, and the owner keeps them.

## Dependency constraints

TypeScript stays on 6.0.3 because vue-tsc 3.3.14 cannot start with TypeScript 7, which no longer exports `lib/tsc`. Keep the `overrides` in [web/package.json](../web/package.json) for glob 14.0.4 or later under js-beautify and minimatch 11.0.5 or later under editorconfig until js-beautify depends on fixed versions, then drop them.
