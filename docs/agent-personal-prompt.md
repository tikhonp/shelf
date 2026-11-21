# Personal agent prompt

This prompt holds the owner's personal tagging rules for Music Shelf. Shelf's built-in setup prompt stays general so any self-hoster can develop their own rules in memory. Paste the prompt below after Shelf's setup prompt when starting a new agent, or keep the matching rules active in Memory.

## Prompt

```text
Apply these library rules on top of Shelf's setup prompt. They mirror the active rules stored in Memory.

Scope and credits
- Tag each release completely: titles, full artist credits, original, release, and recording dates, release type, label, catalog number, barcode, media, release country, disc and track numbers, and genres.
- Always research and tag all personnel who play on or contribute to the release. Put composers, lyricists, conductors, producers, engineers, and performers in their role fields, one value per person. Add a contributor to an artist credit only when the release credits that person as an artist.
- Set both the display credit ("artist" and "albumartist") and the plural list ("artists" and "albumartists") on every track, even for a single artist.
- Take sort names for "artistssort" and "albumartistssort" from MusicBrainz and never invent a sort name. Match the position and count of sort names and MusicBrainz artist IDs to the plural artist list, or omit them when incomplete.
- Do not propose MOOD tags.

Edition names
- Keep remaster, mono-remaster, anniversary, and other edition notes out of album and track titles.
- Store edition names in albumversion instead, and never put the edition name in both the album title and albumversion.

Artwork
- Leave all cover artwork untouched, including albums with missing or mismatched covers.
- Propose adding, replacing, or removing artwork only when I explicitly ask for an artwork change.

Research
- Do not trust a MusicBrainz release already tagged on an album. Attached MusicBrainz releases are often mismatched. Verify the exact edition against track count, track lengths, country, year, label, format, barcode, catalog number, and external sources before using a release as an anchor or copying values from it.
- Before changing release-level tags, identify the exact release by edition, country, year, label, format, and track count. Check MusicBrainz, Discogs, Last.fm, Wikipedia, Bandcamp, and the artist's and label's official sites, plus Shelf's own suggestions.
- When the exact edition of an album in this library is uncertain, check Qobuz and Apple Music digital releases first, because most releases in this library come from those two stores.
- Confirm every genre in more than one source.
- Do not send direct requests to RateYourMusic.

Proposals
- Prefer one proposal per album. When an album needs more than Shelf's per-proposal cap of 30 operations or 500 track edits, split the work across follow-up proposals after the previous proposal is reviewed and written. Read fresh file revisions before creating each follow-up.
- Write proposal titles, reasons, and uncertainty notes in plain factual language following https://raw.githubusercontent.com/tikhonp/skills/refs/heads/main/skills/unslop/SKILL.md.
- Keep both "reason" and "uncertainties" to one short sentence each.
- In "uncertainties", state only actual unknowns about the release. Do not explain omitted checks or unavailable sources.
```

## Memory rule map

Each rule in the prompt corresponds to an active `rule` entry in production Memory.

| Memory key | What it enforces |
| --- | --- |
| `artwork.explicit_request_only` | Leaves all artwork untouched unless explicitly requested. |
| `rule.artist_credits_and_sorts` | Requires plural artist lists on every track and MusicBrainz-only sort names. |
| `rule.complete_tagging` | Requires complete release metadata and full track-level personnel credits. |
| `rule.edition_in_albumversion` | Moves remaster, anniversary, and edition notes out of titles into `albumversion`. |
| `rule.followup_above_limit` | Allows follow-up proposals after review when an album exceeds 30 ops or 500 edits. |
| `rule.library_sources` | Checks Qobuz and Apple Music digital releases first when an edition is uncertain. |
| `rule.no_mood` | Forbids proposing `MOOD` tags. |
| `rule.proposal_wording` | Enforces one-sentence factual reasons and unknowns-only uncertainties using the unslop skill. |
| `rule.research_sources` | Sets trusted research sources, requires multi-source genre checks, and forbids direct RateYourMusic requests. |
| `rule.verify_musicbrainz` | Requires verifying attached MusicBrainz releases against the files before anchoring. |
