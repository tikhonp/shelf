# Status

shelf is finished and runs in production on blackberry. [vision.md](vision.md) says what it is for.

- **Downloads.** The web UI at `shelf.music.tikhonnnnn.com` and the bot `@music_shelf_bot` search and download from Qobuz, through qobuz-dl-go `v0.1.1`, and from Apple Music, through amdl and shelf-wrapper. Navidrome scans after downloads. shelf imported the old bot's 169 downloads.
- **Tags.** The Library tab edits every tag in the library, with suggestions, checks and fixes, covers, playback, history with undo, and bulk runs with ReplayGain and AcoustID. [tags.md](tags.md) describes it. `make contract` passes against Navidrome 0.64.2.
- **Home.** The Library tab is the home screen and the first tab. Each album shows its source, from the downloads' records, `shelf.txt` or its source folder, and the filter menu filters by source. [tags.md](tags.md#album-sources) describes it.
- **Delete.** The Library tab deletes an album's release folder and has Navidrome forget its tracks, as [tags.md](tags.md#deleting) describes. `make contract` covers it.

Every check in [manual-checklist.md](manual-checklist.md) passed in production, except the run row in Album History check 1, as its Last run says.

## Remotes

| Repo | Origin | Notes |
| --- | --- | --- |
| shelf | `ssh://git@code.tikhonnnnn.com/tikhon/shelf.git` | Forgejo. CI's image job pushes with the Actions secret `REGISTRY_TOKEN`, which has `write:package`. Its deploy job writes to sh-blackberry with the deploy key in `SH_BLACKBERRY_DEPLOY_KEY`. |
| qobuz-dl-go | `git@github.com:tikhonp/qobuz-dl-go.git` | GitHub only, public. CI runs on GitHub Actions. |

## Production

Production runs `v1.0.5`, which shelf CI deployed through doco-cd on 2026-11-12. Optional [library agents](agents.md) are off by default and on in production. Production signs in with Shelf accounts. [auth.md](auth.md) describes setup.

shelf-wrapper builds from upstream's `ghcr.io/worldobservationlog/wrapper-lite` image pinned by digest. The index holds 11,461 tracks and 1,008 Navidrome album IDs, forming 1,013 albums because 5 Navidrome albums span two folders.

The [sign-in and agent review redesign](plans/signin-and-agent-review-design.md) and its [second pass](plans/signin-and-agent-review-second-pass.md) run in production.

[Album History](plans/album-history.md) runs in production. A History cap on an album opens its Saves, deletes and runs, and the download that brought it, on the desktop and on the phone. On the desktop, a Save's row counts the tracks it wrote. On the phone the Inbox links an accepted change to its album's History. [tags.md](tags.md#the-editor) describes it. Since `v1.0.5`, hovering a change's time on the desktop opens a note with its full date. Before, the row's button covered the time, so the pointer never reached it.

Tags disagree no longer flags a year beside a full date in that year, such as Picard's `ORIGINALYEAR=1997` beside `ORIGINALDATE=1997-07-01`. Before `v1.0.4` the check flagged 660 of the 1,020 albums in production, every one for this reason. The first walk after the deploy read all 11,514 files again, and now no album has the issue. The Fix changes only the tracks whose keys disagree.

`v1.0.4` has two phone fixes on the album page. The `?` key now lines up with History and Delete. Before, it sat 2.5px lower. The panel-coloured mask above the stuck LCD now covers only the 8px gap above it. Before, it covered the bottom 14px of the issue row.

An agent's 401 for a malformed token says whether the prefix is missing, a character is not allowed, or the length is wrong, and it gives the length for the whole token. The old message compared the 43 characters after the prefix with the whole token's length.

An agent connection can get a new secret on Status or with `shelf manage agents secret <id>`, and Connect hides its permission rockers until Choose.

On a phone, a proposal no longer scrolls sideways when a field's state quotes a long value or an evidence source is long. Inbox rows have the Library's phone sizes and no longer overlap. On an album with a proposal, the stuck scope LCD no longer has the Proposed header behind it, and rows no longer show through the gap above it. An agent's unit on Status has no empty space under Revoke.

On a phone, Memory's title bar says Reset instead of Reset memory and its status keys fill one row, so neither runs off the screen. A proposal field puts a wide tag key or a Changed since proposed pill under its label. Before, they squeezed the label to one letter per line. The Inbox state labels keep their full width, so Withdrawn no longer runs under the knob. A proposal's Ask for changes switch is at the left of the reply form, and Send comment has its own row under it.

On the desktop, the Inbox now fits the window like the Library. The list and the proposal scroll inside their windows, so the menubar stays at the top and the divider's grip stays in the middle of the window. Before, the whole page scrolled. The Send key keeps one width, so the Ask for changes switch no longer moves left when you turn it on.

A rocker switch, such as Compilation, now has I and O on its paddle and lights peach when on, so on and off no longer look alike. A flip fades from one state to the other over 160 ms.

The 2026-11-11 dependency update brings qobuz-dl-go v0.1.1 and `golang.org/x/crypto` v0.59.0. qobuz-dl-go requires `modernc.org/libc` v1.81.0, so shelf has it too. `npm update` moved Vite to 8.3.8 and refreshed the lockfile, and `make contract` runs on alpine 3.24. Go 1.27.4, the base images, golangci-lint, govulncheck, air, goose, amdl, the wrapper-lite digest and the CI actions were already the latest.

Status ends with a Shelf part that shows the version the running server was built with.

On `main` but in no release yet, `shelf manage users add` and `set-password` take `--password-stdin`. A script can then make an account with `docker exec -i` and a password file, without `expect` driving a terminal. [auth.md](auth.md#account-commands) describes it.

A pushed `v*` tag now deploys through sh-blackberry and doco-cd, as [deployment.md](deployment.md#deploys) describes.

## Operational notes

- Source modules log in and start goroutines in `Run`, never `Init`, and register with `Switch` when settings are missing.
- A wrapper signed in to the owner's Apple ID needs its own data folder and `--device-info`.
- Unset `SHELF_QOBUZ_TOKEN` for a local run with only the fake source, or shelf logs in to the owner's Qobuz account.
- Music files belong to UID 10001. Delete test downloads from inside the shelf container.
- Third, Portishead and Wish You Were Here 50 came from a checklist run, and the owner keeps them.

## Dependency constraints

`github.com/mfridman/interpolate` stays on v0.0.2 because goose v3.30.0 does not build with v0.2.0, which renamed its API.

TypeScript stays on 6.0.3 because vue-tsc 3.3.14 cannot start with TypeScript 7, which no longer exports `lib/tsc`. Keep the `overrides` in [web/package.json](../web/package.json) for glob 14.0.4 or later under js-beautify and minimatch 11.0.5 or later under editorconfig until js-beautify depends on fixed versions, then drop them.
