# Tags

How shelf reads and writes the tags in audio files. Navidrome is the test of every write. shelf follows its mappings, and a write that reads back right but shows wrong in Navidrome has failed. The code is `internal/modules/library/tagio`. Phase 2 builds the editor in steps, and this doc grows with them. [phase-2.md](phase-2.md) has the plan.

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

1. **Check every file.** The path must be absolute, with a known extension. It must be a regular file, not a symlink, and have one link, because a rename would split a hard link. shelf's UID, 10001 in production, must own it and be able to write its folder, or the file is locked. A folder a download is working in is busy. A revision that differs from the file's is a conflict, and so is an empty one. Any of these stops the write before a file is touched.
2. **Work out the change.** A field edit changes every key of the file that feeds the field, or adds the field's key when none does. Navidrome takes the first alias of a single value and merges the aliases of a multi-value tag, so a stale `ALBUM ARTIST` beside `ALBUMARTIST` would add a second album artist. An edit with no values removes every key that feeds the field. A raw key edit changes that key. An edit that changes nothing drops out, and a file left with nothing to change isn't rewritten.
3. **Refuse what can't be written.** That covers an unknown field, an edit that names both a field and a key or neither, an empty value, a value with a vertical tab or NUL, a key with a tab, vertical tab, NUL or `=`, since go-taglib uses those as separators, two edits of one key, a read-only key, a cover that isn't JPEG or PNG, and a FLAC cover over 16 MiB, which TagLib drops without an error. M4A's two dates share `DATE`, so editing both in one write is refused.
4. **Journal.** The write is recorded with each file's tags before and after, before any file is touched. The journal gets its own copy of the result. Until step 3 gives it a table, the journal is a port with no implementation.
5. **Copy and write.** shelf copies each file to `.shelf-tmp-<16 hex>` in its own folder, creates the copy with mode 0600, and gives it the original's mode and group. The copy hashes a FLAC file's audio on the way. shelf passes only the changed keys to go-taglib and never uses `taglib.Clear`. A new cover replaces the front cover, or is added when there is none. Other pictures stay.
6. **Verify.** The copy's full tag map must equal the original's with the change applied. Its pictures must match in count, MIME type, bytes, and, outside M4A, type and description. Its audio must hash the same as the original's. Any difference aborts the write, and every original stays untouched.
7. **Re-stat and rename.** Each original must still have its size and mtime, and its folder must not be busy. Then shelf renames the copies over the originals back to back, ignoring cancellation, and fsyncs each folder.

A failure before the renames removes every temp file and changes nothing. A rename that fails after others succeeded ends with `ErrPartial`. Each file is whole, old or new, and the result says which. When a folder fsync fails, every file is already written, and the write returns the error.

At start the library module removes temp files a crash left. It walks the music folder, skips dot folders, and deletes only regular files with the temp name that shelf's UID owns. A folder it can't read is logged and skipped.

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
| The sweep's names, owners, dot folders, symlinks and unreadable folders | `TestSweep`, `TestSweepSkipsUnreadableFolders`, `TestRunSweepsTempFiles` |

The fixtures in `tagio/testdata` are half a second of a sine per format, written by `testdata/make.sh`. Tests that need ffmpeg skip where it isn't installed.
