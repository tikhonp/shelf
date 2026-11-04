Answers written by hand in the shape of MusicBrainz's `ws/2` JSON and the Cover Art Archive's, with made-up IDs. They hold only the keys shelf reads, plus a few it ignores, so a decode that trips on an unknown key shows.

- `release-blue.json`: a two-track release whose tracks are half a second long, as the tag fixtures are.
- `release-three.json`: the same album with a third track, which fails the threshold.
- `release-classical.json`: a two-track release with works and their composers.
- `search-*.json`, `isrc.json`, `caa.json`: release searches, a recording search by ISRC, and a cover listing.

On 2026-11-04 the client read real answers for a search, a release with works, a recording search by ISRC and a cover listing, and their shapes match these.
