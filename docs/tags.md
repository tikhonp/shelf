# Tags

How shelf reads and writes the tags in audio files, and how it indexes, checks and saves the library. Navidrome is the test of every write. shelf follows its mappings, and a write that reads back right but shows wrong in Navidrome has failed. The code is the library module, `internal/modules/library`, with tag IO in its `tagio` package. Phase 2 builds the editor in steps, and this doc grows with them. [phase-2.md](phase-2.md) has the plan.

## Fields

A field is one of Navidrome's tags, such as `albumartist` or `musicbrainz_trackid`. The table comes from Navidrome's own `resources/mappings.yaml`, copied unchanged into `tagio/mappings.yaml` with a header naming the release. Each field has Navidrome's aliases, a type of `date`, `uuid`, `pair` or none, its split characters, and whether Navidrome reads it per album. Main fields are the ones Navidrome handles itself. Additional fields are the tags it keeps for smart playlists.

- **Keys.** shelf reads a file through TagLib's property map, so a key is TagLib's name for a tag, such as `ALBUMARTIST`. A key feeds a field when its lowercase form is one of the field's aliases. For a `pair` field, such as `lyrics` or `performer`, `LYRICS:ENG` feeds it too.
- **Raw atoms.** Navidrome also reads M4A atoms, ID3 frames and WMA attributes under their raw names. Where TagLib's name and the raw name feed different fields, the format table says so. M4A's `©day` is TagLib's `DATE`, so in M4A `DATE` feeds `recordingdate` and also `releasedate`. `INITIALKEY` feeds `key` in M4A, MP3, WAV, AIFF and WMA, though not in FLAC.
- **New keys.** When a file holds none of a field's aliases, shelf adds the field's key. That is the uppercased name, except `TRACKNUMBER`, `DISCNUMBER`, `DATE` for `recordingdate`, `LABEL`, `ITUNESADVISORY`, `MUSICBRAINZ_TRACKID` for `musicbrainz_recordingid`, and `MUSICBRAINZ_RELEASETRACKID` for `musicbrainz_trackid`. Each one is an alias of its own field. TagLib writes Vorbis comment keys as given, so in FLAC, Ogg and Opus the added key is the one Navidrome reads. The two MusicBrainz names swap. The file's release track ID is Navidrome's track ID, and the file's track ID is the recording.
- **Standard and Extended.** A key that feeds any field is Standard. Every other key, such as `ENSEMBLE` or `UPC`, is Extended, and Navidrome never reads it.

## Formats

| Format | Extensions | Audio check | Limits |
| --- | --- | --- | --- |
| FLAC | `.flac` | SHA-256 of the bytes after the metadata blocks, skipping an ID3v2 tag before `fLaC` and an ID3v1 tag at the end | |
| M4A | `.m4a`, `.m4b` | ffmpeg | Picard's lowercase `----:com.apple.iTunes:originaldate` and `originalyear` atoms are read-only. Pictures have no type or description. |
| MP3 | `.mp3` | ffmpeg | TagLib adds an ID3v1 copy of the tags on every save. |
| Ogg Vorbis | `.ogg`, `.oga` | ffmpeg | |
| Opus | `.opus` | ffmpeg | |
| WAV | `.wav` | ffmpeg | |
| AIFF | `.aiff`, `.aif` | ffmpeg | |
| WMA | `.wma` | ffmpeg | No custom fields and no multi-value writes. Keys TagLib has no WMA attribute for, such as `COMPILATION`, fail the read-back. A file with two pictures fails the read-back, because TagLib reorders them. |

`.mp4` is never audio to shelf. The library's `.mp4` files are amdl's animated artwork, and Navidrome skips them. ffmpeg's check is `ffmpeg -i file:<path> -map 0:a:0 -c copy -f hash -hash sha256 -`, a hash of the first audio stream's packets with no decoding, which no tag touches.

## Reading

`tagio.Read` gives a file's tags, its picture list without the bytes, and its audio properties: codec, length, channels, sample rate, bit depth, bit rate.

A file's revision is its size, its mtime in nanoseconds, and the first 16 hex digits of a SHA-256 over its sorted tags and picture list. A write carries the revision the editor loaded, so a file that moved since is a conflict. That catches Picard and other tools, even one that keeps the mtime and the size.

`shelf manage tags <file>` prints one file this way: format, size, revision and pictures, then its Standard keys with the fields each feeds, then its Extended keys. Long values are cut at 120 characters. It needs no configuration or database and changes nothing.

## Writing

One rule covers every write. `tagio.Writer` runs one write at a time over the files of one album:

1. **Check every file.** The path must be absolute, with a known extension. It must be a regular file, not a symlink, and have one link, because a rename would split a hard link. shelf's UID, 10001 in production, must own it and be able to write its folder, or the file is locked. A folder a download is working in, or one inside it, is busy. A revision that differs from the file's is a conflict, and so is an empty one. Any of these stops the write before a file is touched.
2. **Work out the change.** A field edit changes every key of the file that feeds the field, or adds the field's key when none does. Navidrome takes the first alias of a single value and merges the aliases of a multi-value tag, so a stale `ALBUM ARTIST` beside `ALBUMARTIST` would add a second album artist. An edit with no values removes every key that feeds the field. A raw key edit changes that key. An edit that changes nothing drops out, and a file left with nothing to change isn't rewritten.
3. **Refuse what can't be written.** That covers an unknown field, an edit that names both a field and a key or neither, an empty value, a value with a vertical tab or NUL, a key with a tab, vertical tab, NUL or `=`, since go-taglib uses those as separators, two edits of one key, a read-only key, a cover that isn't JPEG or PNG, and a FLAC cover over 16 MiB, which TagLib drops without an error. M4A's two dates share `DATE`, so editing both in one write is refused.
4. **Journal.** The write is recorded with each file's tags before and after, before any file is touched. The journal gets its own copy of the result. It is the change's row in `library_changes`, as [Saving](#saving) describes.
5. **Copy and write.** shelf copies each file to `.shelf-tmp-<16 hex>` in its own folder, creates the copy with mode 0600, and gives it the original's mode and group. The copy hashes a FLAC file's audio on the way. shelf passes only the changed keys to go-taglib and never uses `taglib.Clear`. A new cover replaces the front cover, or is added when there is none. Other pictures stay.
6. **Verify.** The copy's full tag map must equal the original's with the change applied. Its pictures must match in count, MIME type, bytes, and, outside M4A, type and description. Its audio must hash the same as the original's. Any difference aborts the write, and every original stays untouched.
7. **Re-stat and rename.** Each original must still have its size and mtime, and its folder must not be busy. Then shelf renames the copies over the originals back to back, ignoring cancellation, and fsyncs each folder.

A failure before the renames removes every temp file and changes nothing. A rename that fails after others succeeded ends with `ErrPartial`. Each file is whole, old or new, and the result says which. When a folder fsync fails, every file is already written, and the write returns the error.

At start the library module removes temp files a crash left. It walks the music folder, skips dot folders, and deletes only regular files with the temp name that shelf's UID owns. A folder it can't read is logged and skipped.

## Saving

`POST /api/library/albums/{id}/save` sends the edits of one album. Each track names its row ID, the revision the editor loaded, and its edits, each a field or a raw key with its new values. An album-level edit is the same edit on every track.

1. **Check now.** shelf runs the writer's checks against the files as they are, touching none. A conflict, a locked file or a busy folder answers 409 with a `reason` and the track it stopped at. A change a file can't take answers 422. A Save that changes nothing answers 422 too, and nothing is stored.
2. **Confirm what Navidrome does.** shelf works out each file's Navidrome album ID and album artists after the edits. Three outcomes move or lose a star or a rating, so the Save answers 409 with `reason: confirm` and a message for each, until the request's `confirm` names all of them:
   - `split`: the album's tracks end with more than one album ID. Tracks the Save leaves alone keep the ID the index read.
   - `merge`: tracks move to an album ID another album already has, in any folder.
   - `album_artist`: the album artists Navidrome shows change. `ALBUMARTISTS` wins over `ALBUMARTIST`, so an edit of the single tag alone renames nobody while the multi-value one is set.

   A change of the whole album to a new ID asks nothing, since Navidrome moves its star and rating to the new ID.
3. **Queue.** The change goes into `library_changes` as `queued`, with the edits, and a `library.write` job on the `tags` queue, which runs one write at a time. The answer is 202 with the change. The job's payload holds only the change's ID, because payloads stop at 64 KiB.
4. **Write.** The job runs the writer on the stored edits, so a file that changed since the check is a conflict then. Its journal sets the change `writing` and stores each file's tags before and as planned. When the write ends, each file gets the tags it read back, its new revision, and whether it was replaced. The change ends `done` when every file was replaced, `partial` when some were, and `failed` when none was, with a reason: `conflict`, `locked`, `busy`, `refused`, `verify` or `error`. A failed write fails the change, not the job.
5. **After.** The hub's `change` topic hears the change when it is queued and when it ends. A change that replaced a file asks Navidrome for a scan, labeled `library`, and the index for a walk.

`GET /api/library/changes/{id}` answers the change with each file's changed keys before and after. Its `album_id` is the album the files belong to now, which differs from the one saved once a new Navidrome album ID reaches the index.

**Busy folders.** A source names each folder it writes in through `source.Reporter.Working`, and the downloads module keeps them until `Download` returns. Qobuz names its work folder `[IN PROGRESS] <name>`, and the plain and `[INCOMPLETE]` names it ends with. amdl names its folders only at the end, so Apple Music names its whole folder, `apple-music`. A write checks for busy folders at the start and again before the renames.

**Interrupted writes.** A stop between a change's journal and its end leaves it `writing`. At start, and when the job resumes, shelf reads each of its files: one that holds the planned tags was replaced. The change ends `done`, `partial` or `failed` by that count, with the reason `interrupted`.

## Covers

A cover edit is part of a Save: `"cover": {"upload"}` puts an uploaded image in place of the album's cover, and `"cover": {"delete": true}` removes it. Either needs every track of the album in `tracks`, with its revision, because it may change each file.

- **Upload.** `POST /api/library/covers` takes the image as the raw body, up to 32 MiB, and answers 201 with `{"id", "mime", "width", "height", "size"}`. Only JPEG and PNG that decode are taken. The ID is the SHA-256 of the bytes, and `GET /api/library/covers/{id}` serves it.
- **Store.** Uploads, the embedded copies made of them, and every cover a change replaces live in `/data/library/covers`, named by that hash. A replaced cover is kept whatever its format, damaged ones too, so an undo can put it back.
- **Embedded.** A track that has a front cover gets the new one. With `every_track`, tracks without one get it too, though Navidrome shows the album's cover for them anyway. The embedded copy stops at 1000 pixels on its longest side: a bigger upload is scaled down in its own format, each pixel the average of the pixels it covers. Other pictures stay, and Delete removes only the front cover.
- **Folder.** The release folder keeps one cover file, `cover.jpg` or `cover.png` after the upload's format, at full size, because Navidrome shows `cover.*` before embedded art. The write replaces the file of that name and then removes every other `cover.*`, so a failure never leaves the folder without a cover. Delete removes every `cover.*`. Other images, such as `folder.jpg`, stay, and Navidrome shows them when no `cover.*` is left.
- **Checks.** A cover file is replaced through a temp copy and a rename, as audio files are, and needs the same owner, a writable folder and no download in it. A cover file another tool changed since the Save is a conflict. The audio files go first, and the cover files only after all of them were replaced.
- **History.** Each audio file's row holds its front cover's hash before and after, and each cover file has its own row of kind `cover`. Undo and redo put a picture back only while the file holds what the change wrote, as with keys, and report a conflict on the key `cover` otherwise. A restored embedded cover comes back as a front cover with no description.
- **Damaged covers.** An opened album has ffmpeg decode each of its cover files and each different embedded front cover. A picture that makes ffmpeg print an error is the issue `bad_cover`, with the files it came from. The walk reads no picture bytes, so the album list doesn't show it, and a missing ffmpeg judges nothing.
- **Repair.** `POST /api/library/albums/{id}/cover/repair` answers 201 with an upload to stage as the cover: the first of the album's pictures ffmpeg decodes cleanly, else the damaged one decoded as far as it goes and written again as a JPEG. It writes no file. It answers 422 when nothing is damaged or ffmpeg can't repair it.
- **Album cover.** `GET /api/library/albums/{id}/cover` serves what Navidrome shows: the first folder image the cover priority names, else the first track's embedded cover.

## History

The history lists the saves newest first, `GET /api/library/history?before=&limit=`, each with its `undone` flag. `POST /api/library/changes/{id}/undo` and `/redo` work on a save, in any order, as often as the owner likes.

- **What goes back.** shelf reads each file the save replaced. A key the save changed goes back to its old value only while it holds the value the save wrote. A redo works the other way. A key that already holds the value it would go back to is left as it is. Any other value is a conflict, which stays and is listed on the undo or redo as `{"path", "key"}`.
- **Release IDs.** The MusicBrainz album, release group, album artist, release track and disc IDs go back together or not at all. A conflict on one of them, in any file, leaves all of them in every file and lists them as conflicts, so a file never ends with IDs from two releases.
- **Refusals.** A save that wrote nothing, an undo of an undone save and a redo of one that isn't undone answer 409. So does an undo or redo while another of the same save is still to run. One whose every key changed since answers 409 with `reason: conflict` and its conflicts.
- **The write.** An undo or redo is a change of its own, of kind `undo` or `redo`, with the save as its `target`. It is checked and confirmed as a Save is, because putting tracks back can merge or split albums in Navidrome too, and its body takes the same `confirm`. It writes raw keys through the same `library.write` job. When it replaces any file, the save's `undone` flag follows it.
- **Clear history.** `DELETE /api/library/history` deletes every change that has ended, except a save whose undo or redo is still to run. It answers `{"deleted"}`.
- **Retention.** A `library.retention` job at 05:15 keeps the newest 1,000 saves. Each save takes its undos and redos with it. The job then removes stored covers no change refers to, except uploads younger than a day.

**Write probe.** At start, shelf creates and removes a temp file in each top folder of the music folder. `GET /api/library/index` lists the top folders with `writable` and the reason when not. Each walk counts the files another UID owns, which shelf can't write, as `locked`, per folder and in all. The probe runs once a start, because it moves each folder's mtime.

## Navidrome's view

`tagio.Rules` reads a file the way Navidrome 0.64.2's scanner does, and every check and every album uses that view. The rules start from `mappings.yaml` and apply the owner's Navidrome settings, which the navidrome module reads from `GET /api/config/` with the admin login: the `Tags` changes, `Scanner.ArtistSplitExceptions`, `Scanner.ArtistJoiner`, the old `Scanner.GenreSeparators`, `PID.Album` and `CoverArtPriority`. shelf keeps the last answer, so a start while Navidrome is down uses it, and with none it uses Navidrome's defaults. The rules have an ID, and the index reads a file again when it read the file under other rules.

- **Fields.** Each field takes the values of its aliases in order, split by its separators, with duplicates and empty values dropped. Navidrome cuts a value at the field's length limit, 1024 bytes unless the mapping says otherwise. Dates keep `YYYY-MM-DD` or `YYYY-MM`, else the first year. Navidrome drops a MusicBrainz ID that isn't a UUID, and writes one in braces without them.
- **Artists.** A multi-value `ARTISTS` wins. Otherwise Navidrome splits a single `ARTIST` by the field's separators, or by the mappings' artist separators, skipping the split exceptions. Album artists work the same way, and without any they are the track's artists, or Various Artists on a compilation. The display name joins the tag's values with the joiner.
- **Dates.** With an original date, no release date, and a date no earlier than the original, Navidrome shows the original date and reads the date as the release date. Otherwise it shows the date, then the original date, then the release date.
- **Album ID.** shelf hashes the spec, `musicbrainz_albumid|albumartistid,album,albumversion,releasedate` by default, as Navidrome does. It takes the first alternative with any value and puts the library ID 1 in front. shelf supports `album_legacy` too. The tests hold IDs that a real Navidrome 0.64.2 computed.

## Index

The library module keeps an index of the music folder. A walk runs at start, every 15 minutes, after each finished download, and on `POST /api/library/index`. It never writes a file.

- **What it reads.** Audio files of the formats above, skipping what Navidrome skips: names with exactly one leading dot, its system folders such as `@eaDir` and `lost+found`, and folders whose `.ndignore` holds no pattern. shelf doesn't match `.ndignore` patterns, so it logs such a file and indexes the folder. The walk skips symlinks and lists the image files of each folder for the cover check.
- **What it reads again.** Only a file whose size or mtime changed, or that the index read under other rules or older code. The index keeps a file it can't read as unreadable, and that file joins no album.
- **Albums.** Files group by Navidrome album ID inside a release folder. A release folder is the file's folder, or the one above it when the file's folder is a disc folder such as `CD 2` or `Disc1`. Discs whose tags agree form one album, and discs that disagree form two albums in one folder, which the split check reports. Navidrome merges one album ID across folders, so the index also counts distinct album IDs, and that number is the one to hold against Navidrome's album count.
- **Rows.** An album keeps its row ID while its folder and Navidrome ID stay the same. Every file and album has a row ID, so the API never puts a path in a URL. "Date added" is the earliest file's mtime when the index first saw it, which a retag doesn't move.
- **An opened album.** `GET /api/library/albums/{id}` reads every file of the album again. When a file changed since the walk, the answer says `stale` and the module asks for a walk.

## Checks

Each check is a kind that the filter menu offers. Guide kinds are style only and never light a lamp.

| Kind | An album has it when |
| --- | --- |
| `missing_tag` | A track has no title, artist, album or track number, so Navidrome shows a placeholder or the file name. |
| `split_album` | Two albums of one release folder share an album name. Navidrome shows them apart. |
| `values_differ` | Its tracks hold different values, or a value and none, of a field the album should share: album artist, album artists, album version, compilation, release date, original date, disc total, label, catalogue number, barcode, release type, country and status, and the MusicBrainz album artist ID. |
| `tags_disagree` | Two keys of one track feed one field with different values, such as `ALBUMARTIST` and a stale `ALBUM ARTIST`. |
| `reissue_year` | Navidrome shows a year later than the original date's. |
| `no_releasedate` | A track has no `RELEASEDATE` key. |
| `various_artists` | The album artist is Various Artists and a track lacks the compilation flag. |
| `artist_split` | A track's MusicBrainz artist IDs, or album artist IDs, don't match in number the artists Navidrome splits from its tags. Without IDs there is nothing to judge by. |
| `two_releases` | Its tracks hold two MusicBrainz album or release group IDs, or a split album's halves hold two album IDs. |
| `no_cover` | No image in its folders matches a file pattern of the cover priority, and no track has an embedded picture, or the priority leaves out `embedded`. |
| `guide_albumartists` | A track has no `ALBUMARTISTS`. |
| `guide_replaygain` | A track has no track gain, in ReplayGain or R128 form. |

## Navidrome contract

`make contract` runs shelf's writes against a scratch Navidrome, `deluan/navidrome:latest` as in production, or `SHELF_CONTRACT_IMAGE`. Production can pull a release whose rules differ, and this run shows when they do. The script starts Navidrome and the compiled test in Linux containers that share a Docker volume. On a Mac, Docker Desktop's file sharing showed Navidrome stale mtimes, and its quick scan missed some writes, so the test can't read a folder shared from macOS.

After each step the test runs a quick scan, as production asks for, and holds every album's Navidrome ID, year and songs against shelf's index:

1. At start, shelf's album IDs and years match, and Navidrome skips a `.shelf-tmp-` file.
2. A retag in place keeps a track's ID and star.
3. A whole-album rename gets a new album ID, and the star follows it.
4. Its undo brings the old ID back, with the star.
5. Moving one track to a new album takes the album's star with it, as the Save's warning says.
6. The `RELEASEDATE` fix's pair on an album under the legacy date rule keeps the year shown.
7. A new cover is the one Navidrome serves.

Navidrome keeps the old `songCount` of an album that lost a track in a quick scan, though `getAlbum` lists the right songs. The test counts the songs.

## Edge cases and their tests

| Case | Test |
| --- | --- |
| Every format keeps the tags and pictures it wasn't asked to change, multi-value order, its mode and its audio | `TestWriteEveryFormat` |
| A stale `ALBUM ARTIST` and `DISC` change with their field | `TestWriteUpdatesEveryAlias` |
| Removing a field removes every alias; a new `musicbrainz_trackid` is `MUSICBRAINZ_RELEASETRACKID` | `TestWriteAddsAndRemoves` |
| A change to nothing rewrites nothing; a file listed twice is refused | `TestWriteNothingToChange` |
| Conflicts, bad paths, bad edits, bad values, two edits of one key, a cover that isn't an image | `TestWriteRefuses` |
| Another owner, a read-only folder, a busy folder, a hard link, a symlink | `TestWriteLockedAndBusy` |
| WMA: multi-value, custom field, a dropped key, two pictures | `TestWriteWMALimits` |
| M4A's read-only atoms and its shared `DATE` | `TestResolveM4AReadOnly` |
| Covers added, replaced and removed in FLAC and M4A; a back cover kept | `TestWriteCovers` |
| A failure at the copy, tags, cover, read-back, audio, sync or re-stat stage | `TestWriteFailsAtEachStage`, `TestWriteFailsAtCoverStage` |
| A copy with an extra tag, a lost tag, a lost picture or other audio | `TestWriteCatchesDamage` |
| A file or folder that changes before the renames | `TestWriteRestatSeesAChange` |
| The first rename fails; the second of three fails | `TestWriteRenameFails` |
| The folder fsync fails | `TestWriteDirSyncFails` |
| Cancellation before the renames; a journal that refuses to begin | `TestWriteCanceled`, `TestWriteJournalBeginFails` |
| The returned revision lets the next write run | `TestWriteResultFeedsTheNextWrite` |
| Every key shelf adds feeds its own field; the alias traps | `TestFieldKeyFeedsItsField`, `TestFieldTraps` |
| FLAC audio hash across tags, an ID3v2 prefix and an ID3v1 trailer; broken headers | `TestFLACHashSkipsTags` |
| ffmpeg's hash ignores tags and sees changed samples | `TestFFmpegHash` |
| Empty, garbage and half files read without a panic | `TestReadBadFiles` |
| The revision moves with tags, pictures, and a retag that keeps the mtime | `TestRevisionMoves` |
| The sweep's names, owners, dot folders, symlinks and unreadable folders | `TestSweep`, `TestSweepSkipsUnreadableFolders`, `TestRunSweepsWalksAndHearsDownloads` |
| Album IDs, album artists, dates and years as a real Navidrome 0.64.2 computed them | `TestGoldenNavidrome` |
| Aliases merged, splits, a bad UUID dropped, `3/9` track numbers, a bad day keeping the year | `TestViewMergesAliasesAndSplits` |
| The owner's split exceptions and separators, an ignored tag, genre separators, a folder album spec, cover priority | `TestRulesFollowNavidromeSettings` |
| Disagreeing aliases; lyrics in two languages are no disagreement; M4A's `DATE` against `RELEASEDATE` | `TestDisagreements` |
| Navidrome's settings read with an admin login; a user who isn't an admin; a wrong password kept out of the error | `TestConfigReadsTheTagSettings`, `TestConfigRefusals` |
| Each file check, the legacy date rule that is no reissue, and a split that matches its IDs | `TestFileChecks`, `TestFactsOfAWellTaggedFile` |
| Disc folders merging; discs that disagree splitting; two releases in a folder; a playlist folder that is no split | `TestGroupMergesDiscFolders`, `TestGroupFindsSplits` |
| Shared values differing, Various Artists without the flag, two release groups | `TestAlbumChecks` |
| Cover files, embedded pictures and the owner's cover priority | `TestCovers` |
| Unreadable files left out; the most common quality | `TestGroupSkipsUnreadableAndSummarizes` |
| Dot entries, an ellipsis folder, system folders, an `.ndignore` with only a comment, `.mp4` artwork, a broken file; only changed files read again; a row ID kept; a restart that reads nothing | `TestWalkIndexesTheLibrary` |
| A walk with nothing new writes nothing | `TestWalkWithNothingNewWritesNothing` |
| New Navidrome settings read every file again; Navidrome down keeps the settings last read | `TestNavidromeSettingsChangeTheIndex` |
| List filters, sorts, search, paging and refusals; the filter menu's counts | `TestListFiltersSortsAndSearches` |
| An opened album read again after another tool's retag, and its split seen from the folder's other album | `TestOpenedAlbumIsReadAgain`, `TestOpenedAlbumSeesItsSplit` |
| Audio with its type and ranges; a file swapped for a symlink; an unknown track | `TestAudio` |
| Run sweeps, walks at start, after a download and on request | `TestRunSweepsWalksAndHearsDownloads` |
| A Save writes every track, stores before and after, asks for a scan and a walk, and follows the album's new ID | `TestSaveWritesTheAlbum` |
| Bad bodies, another album's track, a conflict, an unknown field, nothing to change, too many edits, a busy folder, a locked folder | `TestSaveRefuses` |
| Split, merge, album artist renames and their confirmations; an `ALBUMARTIST` edit under `ALBUMARTISTS`; a whole-album change that asks nothing | `TestSaveAsksBeforeNavidromeMovesAStar` |
| A file another tool changes between the check and the job | `TestSaveSeesAChangeBeforeItsJob` |
| Interrupted changes settled as done, partial and failed, at start and by a resumed job, and only once | `TestInterruptedChangeIsSettled` |
| The probe in each top folder, a folder shelf can't write, files another UID owns | `TestProbeAndLockedFiles` |
| Undo and redo in turn restore every key, added and removed ones too, and move the save's flag; the refusals | `TestUndoAndRedo` |
| A key another tool changed since stays as a conflict; a redo with nothing left to redo | `TestUndoLeavesWhatChangedSince` |
| A conflict on one release ID keeps every release ID in every file | `TestUndoKeepsReleaseIDsTogether` |
| An undo that merges tracks back asks first | `TestUndoAsksBeforeNavidromeMovesAStar` |
| History pages, retention, a second undo while one waits, Clear history keeping a save whose undo waits | `TestHistory` |
| Uploads taken and refused; covers by ID; the embedded copy scaled to 1000 in its format, and kept when small | `TestCoverUploads` |
| A cover replaces the front cover only where there is one, writes `cover.jpg` and removes `cover.png`; undo and redo; every track; Delete | `TestSaveCover` |
| A cover edit without every track, with both or neither, with an unknown upload, with nothing to delete; a cover file changed before the job | `TestSaveCoverRefuses` |
| A damaged cover file found by ffmpeg; repair from a good embedded cover, by re-encoding, and nothing to repair | `TestBadCoverAndRepair` |
| An interrupted change's cover files settled by their hash | `TestInterruptedCoverIsSettled` |
| The store keeps covers the history needs and young uploads | `TestCoverStoreSweep` |
| A route's own body limit, and the 1 MiB limit everywhere else | `TestRouteBodyLimit` |
| Navidrome's view after a retag, a rename, an undo, a split, the date fix and a cover | `TestNavidromeContract`, with `make contract` |
| A download's folders are busy while its source writes, and folders inside them; folders outside the music folder are ignored | `TestBusyWhileTheSourceWrites`, `TestWorkingFolderOutsideTheMusicFolder` |
| Qobuz's work folder and both names it ends with; Apple Music's whole folder | `TestDownloadResult` in `qobuz`, `TestDownloadAlbum` in `applemusic` |

The fixtures in `tagio/testdata` are half a second of a sine per format, written by `testdata/make.sh`. Tests that need ffmpeg skip where it isn't installed.
