# Known issues and limitations

Open bugs, performance issues, upstream limits, and dependency constraints collected from the docs.

## Open bugs and issues

- **Concurrent undo requests on one album.** Sending two undo requests within a second for one album returns HTTP 202 for both. Only the first writes. The writer rejects the later ones because the files changed under them, and the API client gets no upfront error. Undoing one change at a time in History works.
- **Repeated file reads in agent proposal previews.** Creating or viewing a proposal runs `Plan` up to 31 times, and each call reads the album's files. Large albums may need a batched `Plan` that reads files once.
- **Patterned `.ndignore` files are not applied.** Shelf skips folders with an empty `.ndignore`, as Navidrome does. A folder whose `.ndignore` contains patterns is logged and still indexed.
- **Apple Music expired-session check is unverified in production.** `shelf-wrapper` watches for `credentialHandler:` lines in service mode, added in `v1.0.8`. Those lines appear only during a download, and the check has not fired in production yet.

## Format and playback limitations

- **M4A lowercase Picard atoms are read-only.** `go-taglib` cannot change or remove `----:com.apple.iTunes:originaldate` or `originalyear`, and nothing reads the uppercase atoms it adds instead.
- **WMA limits.** WMA refuses custom fields, multi-value writes, keys TagLib has no WMA attribute for such as `COMPILATION`, and files with two pictures, because TagLib reorders them on save and fails read-back.
- **MP3 differences.** TagLib adds an ID3v1 copy on every save. Repeated Qobuz MP3 downloads also differ byte for byte because `bogem/id3v2` stores frames in a map. Audio and tags match.
- **Transcoded audio cannot seek.** `GET /api/library/tracks/{id}/audio?transcode=1` streams 256 kbps MP3 without a known length or range support, so the player cannot seek.

## Source and Navidrome limitations

- **Apple Music videos and stations.** Music video and station links are not supported. When an album or playlist includes music videos, `amdl` still saves them as `.mp4` files, which Shelf ignores.
- **Apple Music `wrapper-lite`.** Runs on `amd64` only, requires a privileged container, enforces a 60-second 2FA code window, handles `/key` and `/m3u8` one request at a time, and queries hardcoded DNS servers `223.5.5.5` and `223.6.6.6`.
- **Navidrome quick-scan quirks.** Navidrome keeps an album's old `songCount` after a quick scan removes a track, although `getAlbum` returns the real track list. It also serves a placeholder cover for a few seconds after a scan before serving a new cover.

## Agent limitations

- **Learning cursor cannot prove comprehension.** Shelf checks that an agent's learning cursor moves forward within valid IDs, not that the agent processed every event below it.
- **Partial acceptance writes cannot be retried.** Retrying a failed acceptance only works when zero files were written. A conflict after approval leaves the operation accepted and unwritten until the user retries or the agent sends a follow-up.
- **Unpinned cover offers expire.** Unreferenced cover uploads expire after 24 hours, and remote cover offers expire after 30 days unless pinned by a proposal.

## Dependency constraints

- **TypeScript pinned at `6.0.3`.** `vue-tsc` `3.3.14` cannot start with TypeScript 7, which dropped `lib/tsc`. Move when TypeScript 7.1 and `@vue/content-mapper` 4 are both stable, then put `vueCompilerOptions` under `contentMappers[].options` in `tsconfig` and declare global components in `GlobalComponents`.
- **`web/package.json` overrides.** Keep the `overrides` for `glob >= 14.0.4` under `js-beautify` and `minimatch >= 11.0.5` under `editorconfig` until `js-beautify` depends on fixed versions.
