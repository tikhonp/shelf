# Tag editor feedback

Status: drafted on 2026-11-16. All five phases run in production since `v1.1.0`, deployed on 2026-11-16. A new session with a clean context builds each phase, in order. A phase is done when its "Done when" list holds, `make check`, `make lint`, `make web-check` and `docker build .` pass, and `docs/tags.md` and `docs/status.md` describe the result in the same commit.

## Where this comes from

A friend of the owner tagged albums in the editor and gave feedback in a chat on 2026-11-16:

1. He thought artists can't hold several values. Genre and the performer rows have "Add value", and Artist doesn't. `ARTISTS` and `ALBUMARTISTS` show only in All fields, and only on albums where some file already holds them, such as A Love Supreme. He understood ARTIST, the display name, against ARTISTS, the list, only after the owner explained it.
2. He added `ARTISTS` by hand and said it read as Artist, then that it "got added now".
3. When MusicBrainz and a catalog suggest different values and he takes one, the other stays beside the field. Both agreed it clutters.
4. For a guest single, he wanted the release to stay under the main artist in the discography while each track shows all its artists. He found by trial that ARTIST holds the full credit, ARTISTS the list and ALBUMARTIST the main artist alone. Nothing in the editor told him.
5. He typed a single and soundtrack release type and saw one combined "single.soundtrack" category. He also said dozens of albums carry a wrong EP type.

## What the code says

Checked against the code on 2026-11-16, at 2bb02cf.

- `mainFields` (`web/src/lib/tags.ts:25`) has the single `artist` and `albumartist`, and only Genre is multi. `tagRows` (`tags.ts:328`) makes a row for a key no file holds only when the stage or a pending proposal names it.
- MusicBrainz already suggests `albumartists` (`internal/modules/library/anchor.go:348`), each track's `artists` (`anchor.go:395`) and `releasetype` with several values (`anchor.go:360`). The artist lists are invisible, since no row shows them.
- `RELEASETYPE` splits on `,` (`tagio/mappings.yaml:182`), but it isn't in `multiKeys` (`tags.ts:297`). The editor treats it as one text value. "single.soundtrack" isn't something shelf writes; it is likely the client's own grouping of a two-value type. Ask which client showed it before building anything on it.
- The EPs come from Qobuz. qobuz-dl-go writes its `release_type` into `RELEASETYPE` (`qobuz-dl-go/metadata.go:175`, `:473`).
- `artist_split` (`facts.go:138`) fires only when MusicBrainz artist IDs exist and their count differs from Navidrome's split. `guide_albumartists` has no Fix.
- The editor already shows pending proposals: `AlbumProposals.vue` and the tapes beside fields. The friend didn't know agents exist.
- A run asks no service (`docs/tags.md`, Checks and runs), and checks read only files. So a check that compares the type with MusicBrainz is out.

## Decisions

The owner settled these on 2026-11-16. Each phase names the ones it depends on.

- **D1.** The artist lists become main fields under Artist and Album Artist, labelled "Artists" and "Album artists". The notes on Artist and Artists explain the pair. The owner chose yes, without renaming Artist.
- **D2.** Accepting a suggestion on Artists or Album artists replaces the list. On Genre and other multi fields, a one-value suggestion still adds. The owner chose yes.
- **D3.** Release type becomes a main multi field with a list of MusicBrainz types to pick from, and still takes any typed value. The owner chose yes, with the filter by release type.
- **D4.** Rival suggestions fold into one chip after one is taken. The owner chose to keep them folded after Save too, so the server marks them.
- **D5.** The split suggestion covers track artists only. The guest-single case gets a line in the guide, not a rule. The owner chose yes.

---

## Phase 1. Editor bugs

Fixes three defects that also make phases 2 and 3 safe. No decision needed.

### Bugs

1. **A multi-value value on a row that isn't multi.** `rowOf` (`tags.ts:313`) sets `multi` from `multiKeys` or from file values only. Accept MusicBrainz's `[single, soundtrack]` on a `RELEASETYPE` row and two inputs render without "Add value". Typing in either replaces both with one value, because `type()` in `LibTagRow.vue:39` writes one value when the row isn't multi.
   - To fix it, make a row multi also when its staged value holds more than one value, or when any of its fields has a `split` or is multi in the mappings. Expose the mappings' multi fields through `/api/library/keys`, or a sibling list in its answer, rather than copying them into the client.
2. **"Add field" with an empty value does nothing, without saying so.** `add()` in `LibAllFields.vue:28` stages `[]` for a key, and `stageEdit` drops an edit equal to the file's empty value (`tags.ts:99`).
   - To fix it, make an empty value add a staged row with one empty input. The row stays until the owner reverts or saves. A Save sends nothing for it, since `saveBody` drops empty values.
3. **A custom key that feeds a known field targets the key, not the field.** Typing `ARTISTS` in Add field stages `key:ARTISTS` with `fields: []`. The row then shows no suggestions until Save and a reload, and a later suggestion stages the field `artists` beside it, so two edits can sit on one tag.
   - To fix it, when a typed key is a field's usual key, make the row target the field and list it in `fields`, as `tagRows` already does for proposed fields (`tags.ts:343`). `/api/library/keys` needs to answer key-to-field pairs for that.
   - If `/api/library/keys` fails, the row currently lands under "Extended, Navidrome ignores these". Show it under Standard with no claim instead, or show the load error.

### Reproduce first

Before fixing bug 3, write a store test that adds `ARTISTS` with a value on an album without it, saves the body, and checks what the editor shows. That decides whether the friend's "read as Artist" was this bug, the scan delay, or that Navidrome's display name stays ARTIST by design (`navidrome.go:440`). Record the answer in this plan.

Answer, from `ARTISTS added by hand leaves the Artist row as it was` in `editor.store.test.ts` and a run of the same steps before the fix: the editor never showed ARTISTS as Artist. Its Artist row kept the ARTIST value after staging ARTISTS and after the reload that follows a Save. Navidrome's display name also stays ARTIST while ARTISTS is set (`navidrome.go:440`), so "read as Artist" was that, by design. "Got added now" was most likely the scan after Save. Before the fix the hand-added row had no suggestions, and taking MusicBrainz's `artists` staged a second edit beside `key:ARTISTS`, so the Save sent two edits for one tag.

Phase 1 differs from the steps above in two ways. `/api/library/keys` answers only `fields`, since nothing reads the old `keys` list. Add field with no value stages nothing on a tag the tracks hold, because an empty input there would look like a delete that Save doesn't send.

### Start here

- `web/src/lib/tags.ts`: `stageEdit`, `rowOf`, `tagRows`, `accept`.
- `web/src/components/LibAllFields.vue`, `LibTagRow.vue`, `web/src/stores/editor.ts`.
- `internal/modules/library/http.go`: `keys`. `tagio.Fields()` for what the mappings know.
- `web/src/lib/tags.test.ts` and `web/src/components/library.test.ts` for how editor tests are written.
- `docs/tags.md`, The editor.

### Tests

- A row with a staged two-value value on a single-value key is multi, and typing in the second input keeps the first.
- `RELEASETYPE` is multi before any file holds two values.
- Add field with an empty value gives a staged row; Save sends no edit for it.
- Add field `ARTISTS` targets `artists`, shows the field's suggestions at once, and accepting one stages a single edit.
- With the keys unavailable, the row doesn't say Navidrome ignores it.

### Done when

The three fixes and their tests are in, this plan records the reproduction, and `docs/tags.md` describes Add field and multi rows as they now work.

---

## Phase 2. Artists and Album artists rows

Depends on D1 and D2, and on phase 1.

### Behavior

- `mainFields` gets `{ field: 'artists', label: 'Artists', multi: true }` after Artist and `{ field: 'albumartists', label: 'Album artists', multi: true }` after Album Artist. They show on every album, on desktop and phone (`LibPhoneField.vue`).
- `tagRows` already skips a field that a main row shows, so All fields stops showing the separate `ARTISTS` and `ALBUMARTISTS` rows. Check that it does.
- Notes in `web/src/lib/notes.ts`:
  - Artist: "The name shown for this track. While Artists is set, Navidrome links the artists from Artists and only shows this."
  - Artists: "One artist per value. Navidrome links each one. Empty: Navidrome splits Artist on / feat. ft. and ;."
  - Album artists: the same for Album Artist.
- The MusicBrainz suggestions for `artists` and `albumartists` now show beside the rows. Proposals for these fields show there too, not in All fields.
- `accept()` (`tags.ts:135`) replaces the list on `artists` and `albumartists` even when the suggestion has one value (D2). Other multi fields keep adding.

### Edge cases

- **Mixed at All tracks.** Track artists usually differ. The row shows Mixed with no "Add value", as Genre does. Selecting a track edits that track.
- **WMA.** A write of more than one value fails, as `formats.go` marks WMA without multi-value. The 422 must name the field on the row, not only in the Save bar.
- **Album artist confirm.** Staging `albumartists` that changes the album artists Navidrome shows gets the existing 409 `album_artist` confirm. A list equal to the current split asks nothing. The server needs no change. Add a save test for the equal case if none exists.
- **Fixes.** `artist_split` and `missing_tag` Fixes still target `artist` and `albumartist`. Leave them.
- **Space.** Two more rows push dates down. If the editor is too long on the phone, hide an empty Album artists row behind All fields on the phone only, and record that here.

### Start here

- `web/src/lib/tags.ts` (`mainFields`, `accept`, `tagRows`), `web/src/lib/notes.ts`.
- `web/src/components/LibField.vue`, `LibPhoneField.vue`, `LibEditor.vue`, `LibPhoneAlbum.vue`.
- `internal/modules/library/save.go` and `save_test.go` (`TestSaveAsksBeforeNavidromeMovesAStar`).
- `docs/tags.md`, The editor and Suggestions. `docs/ui/design/README.md` for the editor screens.

### Tests

- Both rows show on an album no file has the keys on, with MusicBrainz suggestions.
- A one-value suggestion on Artists replaces the list; on Genre it still adds.
- Mixed artists at All tracks show Mixed and no Add value.
- An `albumartists` edit equal to Navidrome's split asks no confirm.

### Done when

The rows are on desktop and phone, the guest-single case (Album Artist = main artist, Artist = full credit, Artists = list) can be done without opening All fields, and the docs and tests match.

### What phase 2 changed from the steps above

- All fields replaces the main rows. It doesn't sit beside them, so `tagRows` no longer skips a main field. A field the stage or a proposal names that no file holds gets an All fields row, main fields too. With the skip, an `ARTISTS` typed into Add field would have left All fields at once. A file's `ARTISTS` key always had its own All fields row, as `ARTIST` has.
- The WMA refusal names the edit. The writer wraps a refused edit in `tagio.EditError`, the 422 carries its `field` or `key`, and the editor shows the message under that row, on the desktop and the phone.
- The phone keeps both rows. It has no All fields to hide an empty Album artists row behind.

---

## Phase 3. Release type

Depends on D3, and on phase 1. The owner thinks the client that showed "single.soundtrack" was Feishin, which likely groups an album's release types into one category. Shelf writes two values, so check what Feishin shows for an album with two types before changing how shelf writes them.

### Behavior

- `mainFields` gets `{ field: 'releasetype', label: 'Release type', multi: true }` after Compilation.
- Each input has a `<datalist>` of MusicBrainz types. Primary: album, single, ep, broadcast, other. Secondary: compilation, soundtrack, spokenword, interview, audiobook, audio drama, live, remix, dj-mix, mixtape/street, demo, field recording. They are lower case, as MusicBrainz and Picard write them. The input also takes values outside the list, since files already hold others.
- The note says "First the primary type, then any secondary types, one per value. Navidrome splits a single value on commas."
- A value with a comma, such as `single, soundtrack`, typed into one input stays as typed. The note explains commas, and the editor doesn't split the value.
- MusicBrainz's `releasetype` suggestion already has several values and replaces the list, like D2.

### Navidrome check

Run `make contract` with an MP3, a FLAC and an M4A holding two release type values. Check that Navidrome 0.64.2 reads two types from two `TXXX:MusicBrainz Album Type` values, two Vorbis comments and two freeform atoms. Record what the API answers for the album. If a format comes back as one value, write that format as one comma-joined value and say so in `docs/tags.md`.

### Filter by release type

So the wrong EPs can be found:

- `GET /api/library/filters` gains `types: [{name, albums}]`, counted from the index. An album counts under each of its types; an album with none counts under `none`.
- `GET /api/library/albums` takes `type=<name>`, which combines with `filter`, `folder` and `source` as those combine today. A type with no albums answers an empty list, and only an empty or malformed `type` answers 400.
- The filter menu (`LibFilterMenu.vue`) gets a "Release type" group with the counts.
- No check and no bulk rule. Rules such as "an EP with seven or more tracks is an album" would be wrong too often. The owner lists the EPs and takes the MusicBrainz chip on each, or asks the agent through the Inbox.

### Start here

- `web/src/lib/tags.ts`, `web/src/lib/notes.ts`, `LibField.vue`, `LibPhoneField.vue`.
- `internal/modules/library/http.go` (`list`, `filters`), `store.go` (`albums`, `counts`), `index.go` for what the index keeps per album.
- `web/src/components/LibFilterMenu.vue`, `web/src/stores/library.ts`.
- `scripts/contract.sh` and `internal/modules/library/contract_test.go`.
- `docs/tags.md`, and `docs/architecture.md` for the API table.

### Tests

- The row is multi, offers the list, keeps an unknown typed value.
- Accepting the MusicBrainz suggestion stages both values.
- Filter counts with zero, one and two types per album; `type=ep` combined with a folder.
- The contract result for each format.

### Done when

Release type can hold several values from the editor, Navidrome reads them as several in every format shelf writes or `docs/tags.md` names the exception, and the filter lists albums by type.

### What phase 3 found and changed from the steps above

- **Feishin.** Feishin's artist page has a grouping selector. With "All release types" it files an album under all its types joined, primary first, so single and soundtrack make one group of their own. With "Primary release types" the album goes under Single. That is the combined category the friend saw. Shelf writes two values, and keeps doing so.
- **Navidrome.** `make contract` against Navidrome 0.64.2 saves `single` and `soundtrack` to a FLAC, an MP3 and an M4A album. `getAlbum` answers `releaseTypes: ["single", "soundtrack"]` for each, so no format needs a comma-joined value.
- **Index.** `library_albums.types` holds each album's types as `,single,soundtrack,`, from migration 00011. The facts already kept the release type, so the walk after the deploy fills the column without reading files again. Types are lower case, so `EP` and `ep` are one entry, and `type=` takes any case.
- **Runs.** A run's Select all also sends `type`, because a run over the EP filter must select only EPs. The bulk panel now loads its preview again when the source or type changes, as it already did for the filter and folder.
- **Suggestions.** On Release type a one-value suggestion replaces the list too, since a suggestion there is a release group's whole type.
- **Menu names.** The filter menu names MusicBrainz's types, such as EP and Spoken word, and No release type. Any other type shows as the files hold it.

---

## Phase 4. Fold rival suggestions

Depends on D4. The client folds, and the server marks rivals so they stay folded after Save.

### Behavior

- Single-value fields only. Multi fields keep every chip, since each one adds or replaces on purpose.
- When a field's staged value equals one of its ordinary suggestions, the other ordinary suggestions fold into one chip, "2 other", with the count. Pressing it shows them until the field changes again.
- Revert, or typing a value no suggestion has, unfolds them. Derive the fold from the stage in `offersFor` (`tags.ts:181`), so there is no extra state to keep in step.
- Proposal tapes never fold.
- A folded chip is a button with an accessible name such as "Show 2 other suggestions for Album".
- Main fields and All fields rows behave the same, and so does the phone.

### After Save

After Save the held value drops out on the server (`suggest.go:419`), so the client can't tell a rival from a new offer. D4 keeps the rivals folded, so the server marks them. When the files hold a value one of a field's candidates offers, the server marks the field's other ordinary suggestions `rival: true`. When the files hold a value no source offers, it marks nothing, and every suggestion shows. The client folds marked suggestions the same way it folds them before Save. Describe the mark in `docs/tags.md`, Suggestions, and in the suggestions answer in `docs/architecture.md`.

### Start here

- `web/src/lib/tags.ts` (`offersFor`, `suggestionsFor`), `LibField.vue`, `LibTagRow.vue`, `LibPhoneField.vue`, `LibSug.vue`.
- `internal/modules/library/suggest.go` (`holds`, where it drops the held value) and `suggest_test.go`.
- `docs/tags.md`, Suggestions.

### Tests

- Taking one of two suggestions folds the other; Revert unfolds it.
- Typing a different value unfolds.
- A proposal tape stays visible next to a fold.
- A multi field never folds.
- In `suggest_test.go`: files that hold one source's value mark the other source's suggestion as a rival; files that hold a value no source offers mark nothing; a multi field marks nothing.
- After a Save and reload, marked rivals show folded.

### Done when

A taken suggestion leaves one chip of rivals, before Save and after it, and the docs describe the mark.

### What phase 4 changed from the steps above

- **Fills.** A fill, such as the track IDs, never folds. Like a proposal tape, it gives each track its own value, so no staged value equals it.
- **Mark.** The server marks a track's own suggestions from that track's files, as it marks the album's from every track's.
- **Keys.** A suggestion's number key reaches only the tapes on show, not the folded ones.
- **All fields.** An All fields row now picks its tapes as a main field does, so every pending proposal tape shows. Before, a row showed its first three tapes, proposals included.

---

## Phase 5. Split suggestion and guidance

Depends on D5, and on phase 2.

### Split suggestion

- A local source in `internal/modules/library/local.go`: for a track with `ARTIST`, no `ARTISTS`, and `View.Artists()` returning two or more, suggest `artists` with those values. Source name "Navidrome's split", confidence 50%.
- It shows what Navidrome already does, so staging it changes no artist in Navidrome and asks no confirm. Its point is that the owner sees the split and corrects a wrong one, such as a band name with an ampersand the split exceptions miss.
- No album artist half (D5).
- Tests in `TestLocalSuggestions`: two artists give a suggestion, and one artist, ARTISTS already set or a split exception give none.

### Guidance

- `LibGuide.vue`, "Several artists", one more line: "For a single with guests, Album Artist is the main artist alone, so the release stays under them. Artist holds the full credit and Artists the list."
- `LibGuide.vue` or `LibHelp.vue`, shown only while the Inbox is enabled: "An agent can research tags, such as performers, and propose them. Proposals show beside fields and in the Inbox."

### Start here

- `internal/modules/library/local.go`, `suggest.go`, `suggest_test.go`, `tagio` for `View.Artists()`.
- `web/src/components/LibGuide.vue`, `LibHelp.vue`, `web/src/stores/inbox.ts` or wherever the Inbox's enabled state lives.
- `docs/tags.md`, the Sources table.

### Done when

The suggestion shows on split artists, the Sources table lists it, and the two guide lines are in.

### What phase 5 changed from the steps above

- **Test.** `TestSplitSuggestions` holds the cases, not `TestLocalSuggestions`, because a split exception needs Navidrome's settings and an album of its own. The test rig's walk now hands its rules to the module, as `Run` does, so the suggestions read an opened album under the exceptions.
- **Agents line.** The Tagging guide gets an Agents part in its last column, shown only while the Inbox is enabled.

---

## Not doing

- A check or run that compares the release type with MusicBrainz. Checks read files only and runs ask no service.
- Changing what qobuz-dl-go writes for the release type. It is Qobuz's data; the editor and filter fix it per album.
- Promoting more tags to main fields. All fields and Add field cover any tag; promote one when someone asks for it.
