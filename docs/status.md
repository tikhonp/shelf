# Status

shelf is finished and runs in production on blackberry. [vision.md](vision.md) says what it is for.

- **Downloads.** The web UI at `shelf.music.tikhonnnnn.com` and the bot `@music_shelf_bot` search and download from Qobuz, through qobuz-dl-go `v0.1.1`, and from Apple Music, through amdl and shelf-wrapper. Navidrome scans after downloads. shelf imported the old bot's 169 downloads.
- **Tags.** The Library tab edits every tag in the library, with suggestions, checks and fixes, covers, playback, history with undo, and bulk runs with ReplayGain and AcoustID. [tags.md](tags.md) describes it. `make contract` passes against Navidrome 0.64.2.
- **Home.** The Library tab is the home screen and the first tab. Each album shows its source, from the downloads' records, `shelf.txt` or its source folder, and the filter menu filters by source. [tags.md](tags.md#album-sources) describes it.
- **Delete.** The Library tab deletes an album's release folder and has Navidrome forget its tracks, as [tags.md](tags.md#deleting) describes. `make contract` covers it.

Every check in [manual-checklist.md](manual-checklist.md) passed in production, as its Last run says.

The next deployment includes qobuz-dl-go v0.1.2 and reports missing Qobuz streaming access on failed downloads.

## Remotes

| Repo | Origin | Notes |
| --- | --- | --- |
| shelf | `ssh://git@code.tikhonnnnn.com/tikhon/shelf.git` | Forgejo. CI's image job pushes with the Actions secret `REGISTRY_TOKEN`, which has `write:package`. Its deploy job writes to sh-blackberry with the deploy key in `SH_BLACKBERRY_DEPLOY_KEY`. |
| qobuz-dl-go | `git@github.com:tikhonp/qobuz-dl-go.git` | GitHub only, public. CI runs on GitHub Actions. |

## Production

Production runs `v1.1.3`, which shelf CI deployed through doco-cd on 2026-11-16. Long lists read their next page as their end scrolls into view, in place of Load more: the album list, search results, History, an album's History, Downloads, the Inbox and Memory. An album or track offers only the lowest lossless format that covers its quality, so a CD album no longer offers Max or 24/96, and its Download asks for CD. Both passed their checks in production on `v1.1.2`. Since `v1.1.3`, an album that Qobuz repeats on two pages of a search shows once. For "Bach cello", Qobuz returns 1,000 albums, and 22 of them repeat albums from earlier pages. shelf shows 978. Optional [library agents](agents.md) are off by default and on in production. Production signs in with Shelf accounts. [auth.md](auth.md) describes setup.

shelf-wrapper builds from upstream's `ghcr.io/worldobservationlog/wrapper-lite` image pinned by digest. The index holds 11,461 tracks and 1,008 Navidrome album IDs, forming 1,013 albums because 5 Navidrome albums span two folders.

Since 2026-11-12, shelf-wrapper puts the wrapper in `error` when Apple no longer accepts its Apple ID session, with "Apple no longer accepts the wrapper's sign-in. Sign in again." That day the session had expired while Status said signed in, and every Apple Music download failed on `lite-server /key`. Signing in again fixed it. v1.0.8 brought the check to production. It has not fired there yet.

The [sign-in and agent review redesign](plans/signin-and-agent-review-design.md) and its [second pass](plans/signin-and-agent-review-second-pass.md) run in production.

All five phases of the [tag editor feedback](plans/tag-editor-feedback.md) run in production since `v1.1.0`, and its checks passed there. An All fields row takes several values when Navidrome reads its field as a list, such as Release type, or when the stage holds several, so typing in one input no longer drops the others. Add field with a field's usual key, such as `ARTISTS`, edits the field and shows its suggestions at once. Add field with no value gives an empty staged row. A new key no longer claims Navidrome ignores it when the editor couldn't load the keys Navidrome reads. Artists and Album artists are main rows under Artist and Album Artist, on the desktop and the phone, with MusicBrainz's credits beside them, and taking a credit replaces the list. A Save a file refuses, such as two artists in a WMA file, shows the message under the row it names. Release type is a main row after Compilation with one type per value, and its inputs offer MusicBrainz's types. The filter menu has a Release type group that counts albums under each type, so the owner can list the wrong EPs from Qobuz and fix them one album at a time. Navidrome 0.64.2 reads two types from FLAC, MP3 and M4A. Once a one-value field takes one suggestion, the others fold into one chip, such as "2 other", which opens them. After Save the server marks them as rivals, so they stay folded. A track whose `ARTIST` Navidrome splits into two or more artists, with no `ARTISTS`, gets a "Navidrome's split" suggestion on Artists at 50%. The owner sees the split and corrects a wrong one. The Tagging guide says how to tag a single with guests, and while the Inbox is enabled it says agents can propose tags. The first walk on `v1.1.0` filled the types without reading a file. It counted 899 albums under album, 45 under compilation, 32 under live, 17 under single, 13 under soundtrack, 10 under EP, 2 under mixtape/street, 2 under remix, and 97 with no type.

[Album History](plans/album-history.md) runs in production. A History cap on an album opens its Saves, deletes and runs, and the download that brought it, on the desktop and on the phone. On the desktop, a Save's row counts the tracks it wrote. On the phone the Inbox links an accepted change to its album's History. [tags.md](tags.md#the-editor) describes it. Since `v1.0.5`, hovering a change's time on the desktop opens a note with its full date. Before, the row's button covered the time, so the pointer never reached it.

Tags disagree no longer flags a year beside a full date in that year, such as Picard's `ORIGINALYEAR=1997` beside `ORIGINALDATE=1997-07-01`. Before `v1.0.4` the check flagged 660 of the 1,020 albums in production, every one for this reason. The first walk after the deploy read all 11,514 files again, and now no album has the issue. The Fix changes only the tracks whose keys disagree.

On a phone 400px wide or less, the album's title bar now drops its stripes, so its `?` key fits. On the vivo's 360px screen the bar needed 374px and had 338, so it cut off `?`. The Tag editor feedback checks found it on `v1.1.0`, and `v1.1.1` fixed it. On the vivo the bar now fits History, Delete and `?`.

`v1.0.4` has two phone fixes on the album page. The `?` key now lines up with History and Delete. Before, it sat 2.5px lower. The panel-coloured mask above the stuck LCD now covers only the 8px gap above it. Before, it covered the bottom 14px of the issue row.

An agent's 401 for a malformed token says whether the prefix is missing, a character is not allowed, or the length is wrong, and it gives the length for the whole token. The old message compared the 43 characters after the prefix with the whole token's length.

An agent connection can get a new secret on Status or with `shelf manage agents secret <id>`, and Connect hides its permission rockers until Choose.

On a phone, a proposal no longer scrolls sideways when a field's state quotes a long value or an evidence source is long. Inbox rows have the Library's phone sizes and no longer overlap. On an album with a proposal, the stuck scope LCD no longer has the Proposed header behind it, and rows no longer show through the gap above it. An agent's unit on Status has no empty space under Revoke.

On a phone, Memory's title bar says Reset instead of Reset memory and its status keys fill one row, so neither runs off the screen. A proposal field puts a wide tag key or a Changed since proposed pill under its label. Before, they squeezed the label to one letter per line. The Inbox state labels keep their full width, so Withdrawn no longer runs under the knob. A proposal's Ask for changes switch is at the left of the reply form, and Send comment has its own row under it.

The Inbox hides a proposal field whose values the files already hold. It stays on, so Accept marks it accepted with the other fields, and no count includes it. Before, such a field said "Changed since proposed", because a field that writes nothing has no digest to compare. Santana's Caravanserai proposal had five of them among its 22 fields. Shelf now refuses an agent's new or changed field that writes nothing, with code `unchanged`.

On the desktop, the Inbox now fits the window like the Library. The list and the proposal scroll inside their windows, so the menubar stays at the top and the divider's grip stays in the middle of the window. Before, the whole page scrolled. The Send key keeps one width, so the Ask for changes switch no longer moves left when you turn it on.

A rocker switch, such as Compilation, now has I and O on its paddle and lights peach when on, so on and off no longer look alike. A flip fades from one state to the other over 160 ms.

The 2026-11-11 dependency update brings qobuz-dl-go v0.1.1 and `golang.org/x/crypto` v0.59.0. qobuz-dl-go requires `modernc.org/libc` v1.81.0, so shelf has it too. `npm update` moved Vite to 8.3.8 and refreshed the lockfile, and `make contract` runs on alpine 3.24. Go 1.27.4, the base images, golangci-lint, govulncheck, air, goose, amdl, the wrapper-lite digest and the CI actions were already the latest.

Status ends with a Shelf part that shows the version the running server was built with.

Since `v1.0.6`, a run's row in an album's History counts only that album's tags. Before, it counted the whole run's, so *A Passion Play*'s 2 files showed 24 tags from a 2-album run. Now they show 8.

Since `v1.0.6`, `shelf manage users add` and `set-password` take `--password-stdin`. A script can then make an account with `docker exec -i` and a password file, without `expect` driving a terminal. [auth.md](auth.md#account-commands) describes it.

Since 2026-11-12, shelf uses goose v3.30.1 and `github.com/mfridman/interpolate` v0.2.0. interpolate had stayed on v0.0.2, because v0.2.0 renamed its API and goose v3.30.0 did not build with it.

A pushed `v*` tag now deploys through sh-blackberry and doco-cd, as [deployment.md](deployment.md#deploys) describes.

## In development

Proposal Details opens on grouped Fields with inline editors. Brief holds the agent's explanation and evidence, Files shows the current preview and completed acceptance snapshots, and Talkback keeps review outcomes, including earlier failed attempts. The review bar stays visible while the proposal scrolls on desktop and phone.

## Operational notes

- Source modules log in and start goroutines in `Run`, never `Init`, and register with `Switch` when settings are missing.
- A wrapper signed in to the owner's Apple ID needs its own data folder and `--device-info`.
- Unset `SHELF_QOBUZ_TOKEN` for a local run with only the fake source, or shelf logs in to the owner's Qobuz account.
- Music files belong to UID 10001. Delete test downloads from inside the shelf container.
- Third, Portishead and Wish You Were Here 50 came from a checklist run, and the owner keeps them.

## Dependency constraints

TypeScript stays on 6.0.3 because vue-tsc 3.3.14 cannot start with TypeScript 7, which no longer exports `lib/tsc`. vue-tsc will not get TypeScript 7 support. Vue Language Tools v4, in [draft PR #6170](https://github.com/vuejs/language-tools/pull/6170), replaces it with `@vue/content-mapper` and `tsc --runExternalCode`, and needs TypeScript 7.1. Move when TypeScript 7.1 and `@vue/content-mapper` 4 are both stable. Moving means putting `vueCompilerOptions` under `contentMappers[].options` in the tsconfig, and declaring global components in `GlobalComponents`, because v4 always reports unknown ones.

Keep the `overrides` in [web/package.json](../web/package.json) for glob 14.0.4 or later under js-beautify and minimatch 11.0.5 or later under editorconfig until js-beautify depends on fixed versions, then drop them.
