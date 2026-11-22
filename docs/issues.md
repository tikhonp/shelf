# Known issues and limitations

Open bugs, upstream limits, and dependency constraints.

## Open limitations

- **Patterned `.ndignore` files are not applied.** Shelf skips folders with an empty `.ndignore`, as Navidrome does. A folder whose `.ndignore` contains patterns is logged and still indexed.
- **Transcoded audio cannot seek.** `GET /api/library/tracks/{id}/audio?transcode=1` streams 256 kbps MP3 without a known length or range support, so the player cannot seek.
- **Partial acceptance writes cannot be retried.** Retrying a failed acceptance only works when zero files were written. A conflict after approval leaves the operation accepted and unwritten until the user retries or the agent sends a follow-up.

## Format limitations

- **M4A lowercase Picard atoms are read-only.** `go-taglib` cannot change or remove `----:com.apple.iTunes:originaldate` or `originalyear`, and nothing reads the uppercase atoms it adds instead.
- **WMA limits.** WMA refuses custom fields, multi-value writes, keys TagLib has no WMA attribute for such as `COMPILATION`, and files with two pictures, because TagLib reorders them on save and fails read-back.

## Source and Navidrome limitations

- **Apple Music videos and stations.** Music video and station links are not supported. When an album or playlist includes music videos, `amdl` still saves them as `.mp4` files, which Shelf ignores.
- **Apple Music `wrapper-lite`.** Runs on `amd64` only, requires a privileged container, enforces a 60-second 2FA code window, handles `/key` and `/m3u8` one request at a time, and queries hardcoded DNS servers `223.5.5.5` and `223.6.6.6`.
- **Navidrome quick-scan quirks.** Navidrome keeps an album's old `songCount` after a quick scan removes a track, although `getAlbum` returns the real track list. It also serves a placeholder cover for a few seconds after a scan before serving a new cover.

## Dependency constraints

- **TypeScript pinned at `6.0.3`.** `vue-tsc` `3.3.14` cannot start with TypeScript 7, which dropped `lib/tsc`. Move when TypeScript 7.1 and `@vue/content-mapper` 4 are both stable, then put `vueCompilerOptions` under `contentMappers[].options` in `tsconfig` and declare global components in `GlobalComponents`.
- **`web/package.json` overrides.** Keep the `overrides` for `glob >= 14.0.4` under `js-beautify` and `minimatch >= 11.0.5` under `editorconfig` until `js-beautify` depends on fixed versions.
