# Tags

How shelf reads and writes the tags in audio files, and how it indexes, checks and saves the library and deletes from it. Navidrome is the test of every write. shelf follows its mappings, and a write that reads back right but shows wrong in Navidrome has failed. The code is the library module, `internal/modules/library`, with tag IO in its `tagio` package.

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

1. **Check every file.** The path must be absolute, with a known extension. It must be a regular file, not a symlink, and have one link, because a rename would split a hard link. shelf's UID, 10001 in production, must own it and be able to write its folder, and the file's group must be one of shelf's, since the copy keeps it. Otherwise the file is locked. A folder a download is working in, or one inside it, is busy. A revision that differs from the file's is a conflict, and so is an empty one. Any of these stops the write before a file is touched.
2. **Work out the change.** A field edit changes every key of the file that feeds the field, or adds the field's key when none does. Navidrome takes the first alias of a single value and merges the aliases of a multi-value tag, so a stale `ALBUM ARTIST` beside `ALBUMARTIST` would add a second album artist. An edit with no values removes every key that feeds the field. A raw key edit changes that key. An edit that changes nothing drops out, and a file left with nothing to change isn't rewritten.
3. **Refuse what can't be written.** That covers an unknown field, an edit that names both a field and a key or neither, an empty value, a value with a vertical tab or NUL, a key with a tab, vertical tab, NUL or `=`, since go-taglib uses those as separators, two edits of one key, a read-only key, a cover that isn't JPEG or PNG, and a FLAC cover over 16 MiB, which TagLib drops without an error. M4A's two dates share `DATE`, so editing both in one write is refused.
4. **Journal.** The write is recorded with each file's tags before and after, before any file is touched. The journal gets its own copy of the result. It is the change's row in `library_changes`, as [Saving](#saving) describes.
5. **Copy and write.** shelf copies each file to `.shelf-tmp-<16 hex>` in its own folder, creates the copy with mode 0600, and gives it the original's mode and group. The copy hashes a FLAC file's audio on the way. shelf passes only the changed keys to go-taglib and never uses `taglib.Clear`. A new cover replaces the front cover, or is added when there is none. A replaced front cover keeps its type and description, such as "Cover", and an added one gets the description it was given. Other pictures stay.
6. **Verify.** The copy's full tag map must equal the original's with the change applied. Its pictures must match in count, MIME type, bytes, and, outside M4A, type and description. Its audio must hash the same as the original's. Any difference aborts the write, and every original stays untouched.
7. **Re-stat and rename.** Each original must still have its size and mtime, and its folder must not be busy. Then shelf renames the copies over the originals back to back, ignoring cancellation, and fsyncs each folder.

A failure before the renames removes every temp file and changes nothing. A rename that fails after others succeeded ends with `ErrPartial`. Each file is whole, old or new, and the result says which. When a folder fsync fails, every file is already written, and the write returns the error.

At start the library module removes temp files a crash left. It walks the music folder, skips dot folders, and deletes only regular files with the temp name that shelf's UID owns. A folder it can't read is logged and skipped.

**Write probe.** At start, shelf creates and removes a temp file in each top folder of the music folder. `GET /api/library/index` lists the top folders with `writable` and the reason when not. Each walk counts the files shelf can't write, because another UID owns them or their group isn't one of shelf's, as `locked`, per folder and in all. The probe runs once a start, because it moves each folder's mtime.

## Saving

`POST /api/library/albums/{id}/save` sends the edits of one album. Each track names its row ID, the revision the editor loaded, and its edits, each a field or a raw key with its new values. An album-level edit is the same edit on every track.

1. **Check now.** shelf runs the writer's checks against the files as they are, touching none. A conflict, a locked file or a busy folder answers 409 with a `reason` and the track it stopped at. A change a file can't take answers 422. A Save that changes nothing answers 422 too, and nothing is stored.
2. **Confirm what Navidrome does.** shelf works out each file's Navidrome album ID and album artists after the edits. Three outcomes move or lose a star or a rating, and a fourth changes other albums' covers, so the Save answers 409 with `reason: confirm` and a message for each, until the request's `confirm` names all of them:
   - `split`: the album's tracks end with more than one album ID. Tracks the Save leaves alone keep the ID the index read.
   - `merge`: tracks move to an album ID another album already has, in any folder.
   - `album_artist`: the album artists Navidrome shows change. `ALBUMARTISTS` wins over `ALBUMARTIST`, so an edit of the single tag alone renames nobody while the multi-value one is set.
   - `shared_cover`: a cover edit changes a cover file while other albums share the release folder, as [Covers](#covers) says.

   A change of the whole album to a new ID asks nothing, since Navidrome moves its star and rating to the new ID.
3. **Queue.** The change goes into `library_changes` as `queued`, with the edits, and a `library.write` job on the `tags` queue, which runs one write at a time. The answer is 202 with the change. The job's payload holds only the change's ID, because payloads stop at 64 KiB.
4. **Write.** The job runs the writer on the stored edits, so a file that changed since the check is a conflict then. Its journal sets the change `writing` and stores each file's tags before and as planned. When the write ends, each file gets the tags it read back, its new revision, and whether it was replaced. The change ends `done` when every file was replaced, `partial` when some were, and `failed` when none was, with a reason: `conflict`, `locked`, `busy`, `refused`, `verify` or `error`. A failed write fails the change, not the job.
5. **After.** The hub's `change` topic hears the change when it is queued and when it ends. A change that replaced a file asks Navidrome for a scan, labeled `library`, and the index for a walk.

`GET /api/library/changes/{id}` answers the change with each file's changed keys before and after. Its `album_id` is the album the files belong to now, which differs from the one saved once a new Navidrome album ID reaches the index.

**Busy folders.** A source names each folder it writes in through `source.Reporter.Working`, and the downloads module keeps them until `Download` returns. Qobuz names its work folder `[IN PROGRESS] <name>`, and the plain and `[INCOMPLETE]` names it ends with. amdl names its folders only at the end, so Apple Music names its whole folder, `apple-music`. A write checks for busy folders at the start and again before the renames.

**Interrupted writes.** A stop between a change's journal and its end leaves it `writing`. At start, and when the job resumes, shelf reads each of its files: one that holds the planned tags was replaced. The change ends `done`, `partial` or `failed` by that count, with the reason `interrupted`.

## Covers

A cover edit is part of a Save: `"cover": {"upload"}` puts an uploaded image in place of the album's cover, and `"cover": {"delete": true}` removes it. Either needs every track of the album in `tracks`, with its revision, because it may change each file.

- **Upload.** `POST /api/library/covers` takes the image as the raw body, up to 32 MiB, and answers 201 with `{"id", "mime", "width", "height", "size"}`. Only JPEG and PNG that decode are taken. The ID is the SHA-256 of the bytes, and `GET /api/library/covers/{id}` serves it. An upload no change uses lasts at least a day. A Save that names one the retention job removed answers 422, and the editor uploads the image again.
- **Store.** Uploads, the embedded copies made of them, and every cover a change replaces live in `/data/library/covers`, named by that hash. A replaced cover is kept whatever its format, damaged ones too, so an undo can put it back.
- **Embedded.** A track that has a front cover gets the new one. With `every_track`, tracks without one get it too, though Navidrome shows the album's cover for them anyway, so the editor never sets it. The embedded copy stops at 1000 pixels on its longest side: a bigger upload is scaled down in its own format, each pixel the average of the pixels it covers. Other pictures stay, and Delete removes only the front cover.
- **Folder.** The release folder keeps one cover file, `cover.jpg` or `cover.png` after the upload's format, at full size, because Navidrome shows `cover.*` before embedded art. The write replaces the file of that name and then removes every other `cover.*`, so a failure never leaves the folder without a cover. Delete removes every `cover.*`. Other images, such as `folder.jpg`, stay, and Navidrome shows them when no `cover.*` is left.
- **Disc folders.** Navidrome looks for an album's cover in the release folder, then in its disc folders by name, and it serves a disc folder's `cover.*` as that disc's art. A replace leaves those files, because the release folder's cover outranks them for the album. It removes one only when ffmpeg can't decode it, so `bad_cover` clears. Delete removes all of them, or Navidrome would fall back to them.
- **Shared folders.** Navidrome shows a folder's cover for every album in the folder, such as a playlist folder or a folder of singles. A Save that changes a cover file asks `shared_cover` first when the index has another album in the release folder. An edit that changes only embedded art asks nothing. An undo or redo asks nothing either, because it repeats or puts back what the owner confirmed.
- **Checks.** A cover file is replaced through a temp copy and a rename, as audio files are, and needs the same owner, a writable folder and no download in it. A cover file another tool changed since the Save is a conflict. The audio files go first, and the cover files only after all of them were replaced.
- **History.** Each audio file's row holds its front cover's hash before and after, and each cover file has its own row of kind `cover`. Undo and redo put a picture back only while the file holds what the change wrote, as with keys, and report a conflict on the key `cover` otherwise. Each row also holds the front cover's description before and after, so an undo of a Delete brings the cover back with its description. An undo of a replace keeps the description the file still has, which is the old one. A change's `cover` is true when it writes a cover, and `tags` counts keys alone, so a cover edit with no tag edits has `tags: 0`.
- **Save cover alone.** The editor sends a Save with every track, no edits and the cover, so the cover is its own undo step. That write moves the revision of each track with embedded art. Once the change ends, the editor takes each written file's `revision` from `GET /api/library/changes/{id}` for the edits it still holds, and the next Save goes out with those. Reopening the album instead would accept a change another tool made in between.
- **Damaged covers.** An opened album has ffmpeg decode the cover files of its release and disc folders and each different embedded front cover. A picture that makes ffmpeg print an error is the issue `bad_cover`, with the files it came from. The walk reads no picture bytes, so the album list doesn't show it, and a missing ffmpeg judges nothing.
- **Repair.** `POST /api/library/albums/{id}/cover/repair` answers 201 with an upload to stage as the cover: the first of the album's pictures ffmpeg decodes cleanly, else the damaged one decoded as far as it goes and written again as a JPEG. It writes no file. It answers 422 when nothing is damaged or ffmpeg can't repair it.
- **Album cover.** `GET /api/library/albums/{id}/cover` serves what Navidrome shows: the first image the cover priority names, in the release folder and then the disc folders, else the first track's embedded cover.

## History

The history lists the saves and deletes newest first, `GET /api/library/history?before=&limit=`, each with its `undone` and `cover` flags. A delete has no undo or redo. `POST /api/library/changes/{id}/undo` and `/redo` work on a save, in any order, as often as the owner likes.

- **What goes back.** shelf reads each file the save replaced. A key the save changed goes back to its old value only while it holds the value the save wrote. A redo works the other way. A key that already holds the value it would go back to is left as it is. Any other value is a conflict, which stays and is listed on the undo or redo as `{"path", "key"}`.
- **Release IDs.** The MusicBrainz album, release group, album artist, release track and disc IDs go back together or not at all. A conflict on one of them, in any file, leaves all of them in every file and lists them as conflicts, so a file never ends with IDs from two releases.
- **Refusals.** A save that wrote nothing, an undo of an undone save and a redo of one that isn't undone answer 409. So does an undo or redo while another of the same save is still to run. One whose every key changed since answers 409 with `reason: conflict` and its conflicts.
- **The write.** An undo or redo is a change of its own, of kind `undo` or `redo`, with the save as its `target`. It is checked and confirmed as a Save is, because putting tracks back can merge or split albums in Navidrome too, and its body takes the same `confirm`. It writes raw keys through the same `library.write` job. When it replaces any file, the save's `undone` flag follows it.
- **Clear history.** `DELETE /api/library/history` deletes every change that has ended, except a save whose undo or redo is still to run. It answers `{"deleted"}`.
- **Retention.** A `library.retention` job at 05:15 keeps the newest 1,000 saves and deletes. Each save takes its undos and redos with it. The job then removes stored covers no change refers to, except uploads younger than a day.

## Deleting

`POST /api/library/albums/{id}/delete` removes the album's release folder from the disk with everything in it: disc folders, covers, `shelf.txt`, lyrics and any other file. Nothing brings it back. The Delete key sits in the Editor's title bar on the desktop and in the album's title bar on the phone.

1. **Check now.** The folder must sit inside a top folder, so shelf answers 422 for `qobuz` or `rock` itself. shelf's UID must own every file and folder in it, every folder in it and the folder above it must be writable, and no download may work in it. A file of another UID or a folder shelf can't write answers 409 `locked`, a download 409 `busy`, and a folder that is gone 409 `conflict`. Nothing is touched.
2. **Confirm.** Every delete answers 409 with `reason: confirm` until `confirm` names `delete`, whose message gives the file count and the size, and says Navidrome forgets the album with its star, rating, plays and place in playlists. When the folder holds other albums, beside the album or in folders under it, it also asks `shared_folder` and names up to three of them.
3. **Queue.** The delete is a change of kind `delete`, in the history like a Save, with a `library.delete` job on the `tags` queue, so it waits for a write that runs. A second delete of the folder while one waits answers 409.
4. **Delete.** The job checks the folder again and removes it. A delete the checks stop ends `failed` with the reason, a failure part way ends `partial` with what is left, and a stop part way goes on when the job resumes. The start's settling of cut-off writes leaves deletes to their job.
5. **After.** The index walks, and the downloads module forgets the downloaded items in the folder and under it, so a request for one of them downloads it again. The downloads keep their rows.
6. **Navidrome.** Navidrome keeps a track whose file is gone as missing, with its star and plays, until an admin removes it. Its API removes only tracks its scan has marked missing. So the job asks Navidrome for a scan, waits 30 seconds, and lists the missing tracks under the folder with `GET /api/missing?path=`. It removes those with `DELETE /api/missing/?id=`, a hundred at a time, and Navidrome then drops the empty album. Until any show, it looks again every 30 seconds, ten times in all. `path` is a `LIKE` prefix in Navidrome, so shelf removes only tracks whose path starts with the folder. When Navidrome's `Scanner.PurgeMissing` is `always`, its own scans remove them and shelf leaves them.

The change's `deleted` holds `{"files", "bytes", "albums", "forgot", "navidrome", "navidrome_tracks"}`. `navidrome` is `waiting`, then `removed` with the count, `purges`, `none` when no track showed as missing in time, `failed` when Navidrome didn't answer, or `off` without Navidrome. With `none` or `failed`, Navidrome's Missing Files page removes the tracks. History opens a delete to these lines in place of the tags.

## Bulk runs

A run applies chosen fixes, or ReplayGain, to many albums as one undo step. Only the owner starts one, on the desktop. Select in the list's title bar shows a push button on each row, and `X` picks the row under the cursor. The panel opens with the first album picked, and `B` opens or closes it.

**Selection.** A body names albums as `{"albums": [ids]}`, or as `{"all": true, "q", "filter", "folder", "except": [ids]}`. Select all uses the second form, because the list holds only the pages already loaded. The selection is stored in `library_run_albums`, since a job's payload stops at 64 KiB.

**Preview.** `POST /api/library/runs/preview` takes `{"selection", "fixes"}` and answers `{"selected", "fixes": [{"fix", "albums"}], "albums", "files", "bytes", "samples"}`. Each fix counts the selected albums the index says have its issue, and ReplayGain counts every selected album. `albums`, `files` and `bytes` add up the albums the chosen fixes reach. A retag rewrites every file, and the backups copy each one again, so whole albums count. These numbers are a ceiling, because an album may turn out to have nothing to change. `samples` lists `{"album", "key", "before", "after"}`: shelf plans the first five albums for real and shows up to six changes. ReplayGain's values aren't known yet, so its sample says so.

**Fixes.** The server applies the editor's Fixes, in this order, each on what the one before left:

| Fix | What a run does |
| --- | --- |
| `tags_disagree` | Writes the first key's value to every key that feeds the field. |
| `values_differ` | Gives every track the value most tracks hold. A tie leaves the field alone. |
| `no_releasedate` | The same rule as the editor's Fix. A file with no date changes nothing. |
| `various_artists` | Sets the compilation flag on every track. |
| `replaygain` | Writes the album's measured values, as [ReplayGain](#replaygain) says. |

A run asks no service, because nobody reads each album before it writes. So a Fix that would rather take a suggestion uses its rule, and a run doesn't offer `missing_tag`, `two_releases` or `artist_split`, whose Fixes are only suggestions.

**Slices.** `POST /api/library/runs` stores the run and answers 202 with its progress. A `library.run` job on the `tags` queue plans one album from the files as they are, writes it through the same writer as a Save, and defers itself. A Save queued meanwhile goes next, so it waits for one album at most. One run works at a time, and a second answers 409. A selection with nothing to fix answers 422.

**What a run skips.** Each album is checked as a Save is. A conflict, a locked file or a folder a download works in skips the album. So does anything a Save would ask about, a split, a merge or an album artist rename, because Start asks nothing more. An album that left the index since the selection is skipped too. A write that fails verification fails that album, and the run goes on. The run's log says why, and the album keeps its files as they were.

**Progress.** A `run` event carries `{"id", "kind", "fixes", "state", "reason", "error", "cancelling", "total", "done", "skipped", "failed", "files", "now", "now_state", "log", "created_at", "finished_at"}` as each album starts and ends. `log` holds the last 100 albums that ended. `GET /api/library/runs/current` answers `{"run"}`, null when none works, and `GET /api/library/runs/{id}` answers one run, for a page that opens or reconnects.

**Cancel.** `POST /api/library/runs/{id}/cancel` stops a decode at once and lets a running write finish, so a run stops after the album it is writing. What it wrote stays, and the run ends `partial` with reason `cancelled`, or `failed` if it wrote nothing.

**How a run ends.** `done` when no album was skipped or failed, `partial` when some wrote and some didn't, and `failed` when none wrote and some were skipped. Albums with nothing to change count for neither side. A run asks for one walk and one Navidrome scan when it ends, not one per album.

**A stop.** A run outlives a restart. Its job resumes at the next album, and a write the stop cut off is settled from its files first.

**History.** A run is one row, `Run: No RELEASEDATE` or `Run: 2 fixes`, with its albums and files. Its `tags` add up all its albums, its answer has `run`, `{"fixes", "albums", "files", "skipped", "failed"}`, and its detail lists its first 40 files. Each album's write is a change of its own under the run, and retention and Clear history treat the whole run as one save. An album's change can't be undone alone.

**Undo.** Undo and Redo on a run queue a run of the same shape, with the same progress and the same cancel. They work on every album's save that needs it, follow the rules of [History](#history), and keep up to 200 conflicts on the row. They ask nothing, because the owner pressed Undo on the whole run. A cancelled undo leaves the run as not undone, and pressing Undo again finishes the rest.

## ReplayGain

A run's ReplayGain measures each track with one ffmpeg `ebur128` decode, on the `analysis` queue, so hours of decoding hold back no write. A `library.analyze` job works ahead of the writes, one album per slice, and asks for the `library.run` job as each album is ready.

- **Measure.** ffmpeg prints the loudness of every 400 ms block, 10 a second. shelf keeps them as a histogram in 0.1 LU bins, and gates them as ITU BS.1770 says: an absolute gate at -70 LUFS and a relative one 10 LU under the mean.
- **Album gain.** The blocks of all an album's tracks are pooled and gated together. Averaging the tracks' results would give another number.
- **Values.** The gain is -18 LUFS minus the loudness, as `+3.21 dB`, and the peak is the highest sample, as `0.987000`. ffmpeg reports a peak to three places. Each file gets `REPLAYGAIN_TRACK_GAIN`, `_TRACK_PEAK`, `_ALBUM_GAIN` and `_ALBUM_PEAK` in one write, and these replace values already there.
- **Silence.** A silent track has no loudness, so it gets the album's values and no track values. A silent album is skipped.
- **Results.** `library_gains` keeps each measure by the hash of the audio, the same hash a write checks, which a retag doesn't move. So a run cut off or asked for again decodes nothing twice, and a second run on the same audio writes nothing.
- **Failure.** A file ffmpeg can't measure skips its album, because an album gain needs every track.

## Suggestions

A suggestion is a value for a field, shown beside it with its source and a confidence. `GET /api/library/albums/{id}/suggestions` answers an album's suggestions. The editor asks for them after it opens the album, so a slow service never holds the album up. Nothing is written until the owner accepts a suggestion and saves it.

The answer is `{"anchor", "album", "tracks", "covers", "notes", "unavailable", "fingerprinting"}`:

- `album` lists the album's suggestions, and `tracks` lists `{"id", "suggestions"}` per track. Each suggestion is `{"field", "values", "source", "sources", "confidence", "anchor"}`. A fill has `tracks` in place of `values`: one accept gives every track its own value, as a release's track IDs need.
- `covers` lists `{"image", "source", "confidence", "anchor"}`. `GET /api/library/remote/{image}` serves a preview through shelf, so the CSP stays as it is. `POST /api/library/remote/{image}/cover` fetches the full image and stores it as an upload, which a Save names as the cover. shelf fetches only images a suggestion offered, and an offer lasts 30 days.
- `notes` say where Navidrome shows something else. An `.lrc` file beside a track hides its embedded lyrics.
- `unavailable` lists `{"source", "message"}` for each service that didn't answer. The other suggestions still come.
- `fingerprinting` says a job is identifying the album by its audio, as [AcoustID](#the-anchor) says.

A field shows its three best suggestions. A suggestion that every track already holds is left out.

### Sources

| Source | What it suggests | Base confidence |
| --- | --- | --- |
| File name | Track, disc and title from names such as `01 - Title`, `01. Title` and `1-02 Title`, and the artist from `01 Artist - Title` when the track has none. A disc folder such as `CD 2` gives the disc. Only where the track lacks the field. | track 60%, disc 50%, title 45%, artist 35% |
| Folder | Album and album artist from names such as `Artist - Album (1959) [24B-96kHz]`, or the album from the folder and the artist from the folder above it. Only where no track has the field. | album 50%, album artist 40% |
| Other tracks | Where the tracks differ on a field they should share, each value some of them hold. | 40%, plus up to 50% by the share of tracks that hold it |
| `shelf.txt` | The latest album block's title, artist, label, release date and UPC, and each track's ISRC by its file name. | 85% |
| MusicBrainz | Every field of the anchor release: titles, artists with sort names and IDs, dates, label, catalogue number, barcode, country, status, type, script, genre, and the compilation flag on a Various Artists release. A classical album also gets each track's composer and work. | the anchor's |
| Cover Art Archive | The anchor release's front cover. | the anchor's |
| Catalogs | Every source that can search and read albums, by name, such as Qobuz and Apple Music: album, version, album artist, release date, label, UPC, genre, cover, and each track's title with its version, artist and ISRC. | 85% for the album `shelf.txt` names, else 35% plus up to 45% by the search's match |
| LRCLIB | Lyrics, synced before plain. | synced 70%, plain 65% |

File and folder names are cut and cleaned copies of the tags, so they only fill gaps.

**Confidence.** Sources that give the same value combine: 1 − (1 − p1)(1 − p2)…, so agreement raises it. A source counts once per value, with its best p. A match's lengths scale its p, as [The anchor](#the-anchor) says. The answer shows 1% to 99%, since no hint is certain, and names the most confident source first.

**Lyrics.** `GET /api/library/tracks/{id}/suggestions` answers one track's lyrics as `{"suggestions", "unavailable"}`. The editor asks when it opens a track's lyrics. LRCLIB can take seconds a track, and a call for every track on every album opening would hold the album's suggestions up.

**Catalogs.** shelf reaches them through `source.Searcher` and `source.Resolver`, and names no source. A catalog reads the album its own `shelf.txt` block names, else searches for the album artist and the album. A hit must have the album's number of tracks and a title and artist close enough: the share of words they have in common, title 60% and artist 40%, must reach 70%. Its tracks pair with the album's in order, and their lengths pass the anchor's threshold, with a second of slack for whole seconds. A source that can't answer now reports its own reason under `unavailable`.

### The anchor

Every MusicBrainz suggestion comes from one release, the anchor. Accepting fields one at a time could otherwise mix IDs from two releases. The answer's `anchor` is `{"id", "by", "title", "artist", "date", "country", "tracks", "url", "matched", "reason"}`, and shelf looks for it in this order:

1. `staged`: an ID or a MusicBrainz link the owner pasted into the album ID field, sent as `?anchor=`. 97%.
2. `files`: the MusicBrainz album ID most of the tracks hold. 95%.
3. `barcode`: a search for the album's barcode. 90%.
4. `isrc`: a recording search for up to three of the album's ISRCs, with the release most of them share first. 60%, plus up to 30% by the share of codes that agree.
5. `search`: a search for the album and the album artist, of releases MusicBrainz scores at 60 or more. 40%, plus up to 40% by its score.

6. `acoustid`: the releases the album's audio is on, only after the others found nothing and every lookup answered. 50%, plus up to 35% by the share of tracks that agree.

**AcoustID.** It works with `SHELF_ACOUSTID_KEY` set, and the album's suggestions don't wait for it. The first asking answers `"fingerprinting": true` and queues a `library.fingerprint` job on the `analysis` queue. The job runs `fpcalc` on up to four tracks spread over the album, and asks AcoustID for the releases of matches scoring 70% or more. shelf stores the releases with the share of tracks on each, and a `suggestions` event `{"album"}` tells the editor to ask again. The search then reads up to three of them, and a release must pass the threshold below. Finding nothing is stored as a miss for a day. A failure stores nothing, so the next opening tries again. shelf calls AcoustID at most three times a second, and the key travels in the body of a POST.

**Matching.** A release must have the album's number of tracks. The tracks pair by disc and track number, else in order, and each length must be within 10 seconds of the release's, with 3 seconds or less on average. A release that passes is `matched`, and the average difference scales its p from 100% down to 85% at 3 seconds. A release found by search, barcode or ISRC must pass, and shelf reads up to three of each kind, skipping hits whose track count differs. A release named by ID, staged or in the files, stays the anchor even when it fails, since the owner or the files chose it. It then suggests nothing, and `reason` says why, such as "The release has 12 tracks and the album 10."

**Strategies.** shelf searches for a compilation without its album artist, since MusicBrainz credits each track's artists instead. A compilation has the album artist Various Artists or the compilation flag. It searches for a classical album without its album artist too, since MusicBrainz credits the performers. A classical album has "classical" in a genre or its top folder, and shelf reads its release with the works, for composers. An album with the word "live" in its title or folder, or a `live` release type, is searched only among live releases.

**Release fields** name one release: the MusicBrainz IDs, barcode, catalogue number, label, country, status, type, release date, ASIN, script, and disc and track totals. Each shows one value. That is the anchor's while it is matched, and catalogs that agree raise its confidence. Without a matched anchor it is the most confident value. A release field from the anchor carries its ID in `anchor`. The track, recording and artist IDs come only as fills of every track.

**Save.** An accepted release field goes back in the Save with its `anchor`, and typed values carry none. A Save refuses, with 422, edits that name two anchors. It also refuses a track that would end with a MusicBrainz album ID other than the anchor's, because the release's fields would sit beside another release's IDs. Accepting the anchor's album ID with them fixes that.

### Lookups

- **MusicBrainz** answers one request a second from each client. One limiter in shelf holds every call to that, and a bulk caller lets the editor's calls go first. After a 503, shelf asks again 2 and then 4 seconds later. Every call names shelf, its version and `SHELF_PUBLIC_URL` in the User-Agent, as MusicBrainz asks.
- **Cache.** shelf stores answers from MusicBrainz, the Cover Art Archive and LRCLIB in `library_lookups` by request. A found answer serves for 30 days and a "not found" for a day. shelf stores no other answer. The `library.retention` job removes old answers and old offers.
- **Time.** An album's lookups stop after 40 seconds, and shelf lists a service still busy then as not answering.
- **Status.** `GET /api/library/musicbrainz` looks up Various Artists without the cache, waits for the limiter like the editor, and doesn't retry. Its answer serves every caller for 30 seconds, since each page load checks Status. When MusicBrainz is busy, answers with errors or doesn't answer, the Library part on Status turns its lamp amber and says that editing and saving still work.

## The editor

The Library tab holds staged edits in the browser, per album, so opening another album loses none. A reload of the page drops them. Nothing reaches a file until Save.

- **Targets.** An edit names a field or a raw key. All fields edits the field when its key is the one a main row shows, so two rows never stage two edits on one tag. A second key that feeds the same field keeps its own row and edits that key. A field shows an edit staged on its key, so a Fix that stages `RELEASEDATE` shows on Release date, and Revert takes it back.
- **Scope.** At All tracks an edit reaches every track shelf could read. A file it couldn't read is left out and shows a red lamp. A track that already holds the new value stages nothing, because a Save that changes nothing is refused. Retyping the old value drops the edit.
- **Fixes.** The issue names only a kind and its fields, so the editor works out each Fix from the tracks and the suggestions, and an issue with nothing to stage shows no Fix.

| Issue | Its Fix stages |
| --- | --- |
| `no_releasedate` | Per file: a key that already feeds the release date gives `RELEASEDATE` its value. Otherwise `RELEASEDATE` copies `DATE`, and `DATE` takes `ORIGINALDATE` when there is one. A file with no date stages nothing. |
| `values_differ` | Each field's best suggestion, else the value most tracks hold. |
| `tags_disagree` | Each field's best suggestion, else the value the file's first key holds, written as a field so every key that feeds it agrees. |
| `various_artists` | The compilation flag on every track. |
| `missing_tag`, `two_releases`, `artist_split` | Each field's best suggestion. |
| `reissue_year`, `split_album` | No Fix. No rule can tell which the owner wants. |
| `no_cover`, `bad_cover` | No Fix. The issue opens the cover dialog, and `bad_cover` offers Repair there. |

- **After a Save.** A finished Save clears the stage and reads the album again. A Save or an undo that changes the Navidrome album ID gives the album a new row at the next walk. When the walk ends the editor asks the change for its `album_id`, moves there with any staged edits, and the address follows.
- **An undo that asks.** An undo or redo from History asks as a Save does when it would split or merge albums or rename an album artist. The dialog's key says Undo anyway or Redo anyway, and pressing it sends the undo again with the confirm.
- **A running Save.** The event stream has no replay, so a reconnect asks for the change.
- **A release in the album ID field.** A MusicBrainz ID or link typed into `MUSICBRAINZ_ALBUMID` asks for the suggestions again with `?anchor=`. A release that fails to match shows its `reason`.
- **Playback.** The player asks for the file as it is. When the browser refuses it, the player asks again with `?transcode=1`, and shelf streams it through ffmpeg as MP3. That stream has no length, so the position can't move. A second failure says the browser can't play the file.

## Navidrome's view

`tagio.Rules` reads a file the way Navidrome 0.64.2's scanner does, and every check and every album uses that view. The rules start from `mappings.yaml` and apply the owner's Navidrome settings, which the navidrome module reads from `GET /api/config/` with the admin login: the `Tags` changes, `Scanner.ArtistSplitExceptions`, `Scanner.ArtistJoiner`, the old `Scanner.GenreSeparators`, `PID.Album` and `CoverArtPriority`. shelf keeps the last answer, so a start while Navidrome is down uses it, and with none it uses Navidrome's defaults. The rules have an ID, and the index reads a file again when it read the file under other rules.

- **Fields.** Each field takes the values of its aliases in order, split by its separators, with duplicates and empty values dropped. Navidrome cuts a value at the field's length limit, 1024 bytes unless the mapping says otherwise. Dates keep `YYYY-MM-DD` or `YYYY-MM`, else the first year. Navidrome drops a MusicBrainz ID that isn't a UUID, and writes one in braces without them.
- **Artists.** A multi-value `ARTISTS` wins. Otherwise Navidrome splits a single `ARTIST` by the field's separators, or by the mappings' artist separators, skipping the split exceptions. Album artists work the same way, and without any they are the track's artists, or Various Artists on a compilation. The display name joins the tag's values with the joiner.
- **Dates.** With an original date, no release date, and a date no earlier than the original, Navidrome shows the original date and reads the date as the release date. Otherwise it shows the date, then the original date, then the release date.
- **Album ID.** shelf hashes the spec, `musicbrainz_albumid|albumartistid,album,albumversion,releasedate` by default, as Navidrome does. It takes the first alternative with any value and puts the library ID 1 in front. shelf supports `album_legacy` too. The tests hold IDs that a real Navidrome 0.64.2 computed.

What Navidrome 0.64.2 does with shelf's writes, measured on copies of real albums:

- A retag in place keeps a track's ID, star, rating, plays and playlists.
- A new album ID, from a rename or new MusicBrainz IDs, takes the album's star, rating and plays along, and an undo brings the old ID back. A scan between the two halves of a rename ends the same.
- Rewriting every file of an album through temp files keeps its place in Recently Added.
- Navidrome skips dot files, so it never sees a temp copy. TagLib finds the format by content, so a temp name needs no extension.
- `cover.*` beats embedded art, and a track without embedded art shows the album's cover. An `.lrc` file beside a track hides its embedded lyrics.
- The watcher shows a retag about 6 seconds after the write, with no scan request.

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
8. A run's date fix keeps the album's year and star, Navidrome reports the run's ReplayGain on each song, and the run's undo brings the old gain and the album ID back.
9. A delete removes its folder, and Navidrome forgets its tracks and the album, with no missing track left under the folder.

Navidrome keeps the old `songCount` of an album that lost a track in a quick scan, though `getAlbum` lists the right songs. The test counts the songs.

Navidrome serves its placeholder for a few seconds after a scan ends, then the new cover, with no other scan. The test waits for a cover up to 30 seconds. Reading it once failed about one run in three.

The test's container installs Alpine's ffmpeg, the one production has, because a run reads that ffmpeg's output.

## Edge cases and their tests

| Case | Test |
| --- | --- |
| Every format keeps the tags and pictures it wasn't asked to change, multi-value order, its mode and its audio | `TestWriteEveryFormat` |
| A stale `ALBUM ARTIST` and `DISC` change with their field | `TestWriteUpdatesEveryAlias` |
| Removing a field removes every alias; a new `musicbrainz_trackid` is `MUSICBRAINZ_RELEASETRACKID` | `TestWriteAddsAndRemoves` |
| A change to nothing rewrites nothing; a file listed twice is refused | `TestWriteNothingToChange` |
| Conflicts, bad paths, bad edits, bad values, two edits of one key, a cover that isn't an image | `TestWriteRefuses` |
| Another owner, another group, a read-only folder, a busy folder, a hard link, a symlink | `TestWriteLockedAndBusy` |
| WMA: multi-value, custom field, a dropped key, two pictures | `TestWriteWMALimits` |
| M4A's read-only atoms and its shared `DATE` | `TestResolveM4AReadOnly` |
| Covers added, replaced and removed in FLAC and M4A; outside M4A, a replaced cover keeps its description and an added one gets the one asked for; a back cover keeps its own | `TestWriteCovers` |
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
| A delete asks, removes the folder with its disc folder, cover and `shelf.txt`, leaves the folder beside it, has the downloads forget it, waits for Navidrome's scan and has it forget the tracks; History shows it, and it has no undo or redo | `TestDeleteRemovesTheFolder` |
| Other albums in the folder and under it are named and asked for, and a folder whose name starts the same stays; without Navidrome | `TestDeleteAsksForTheAlbumsItTakes` |
| An unknown album, a top folder, a busy folder, a folder shelf can't write and one above it, a second delete, a folder gone before the job | `TestDeleteRefuses` |
| Navidrome that purges by itself, never marks the tracks missing, or doesn't answer | `TestDeleteNavidromeOutcomes` |
| A delete cut off part way is left alone at start and finished by its job | `TestDeleteCutOffGoesOn` |
| The migration that adds deletes keeps every change, file, run and undo, the foreign keys and the ID sequence, and its down migration drops only the deletes | `TestDeleteMigrationKeepsTheHistory` |
| Navidrome's missing tracks under a folder removed, a `LIKE` match beside it kept, nothing missing yet, `PurgeMissing` set to `always`, Navidrome not set up | `TestRemoveMissing` in `navidrome` |
| A deleted folder's downloaded items and those under it forgotten, a folder whose name starts the same kept, the music folder itself refused | `TestForgetADeletedFolder` in `downloads` |
| A delete's History lines | `web/src/lib/history.test.ts` |
| Delete's confirm and its kinds, another change's event, leaving the album, a refusal, a partial delete | `web/src/stores/editor.store.test.ts` |
| Delete's dialog, its progress and the list after it | `web/src/components/library.test.ts` |
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
| A replaced cover keeps its description, such as "Cover"; the undo leaves the tags and pictures hashing as before; an undo of a Delete brings the description back | `TestSaveCoverKeepsDescription` |
| A cover edit without every track, with both or neither, with an unknown upload, with nothing to delete; a cover file changed before the job | `TestSaveCoverRefuses` |
| An album with no embedded art: only the cover file changes, the audio files keep their revisions, undo and redo | `TestSaveCoverOfTheFolderOnly` |
| Edits staged before Save cover alone: a conflict with the old revisions, a Save with the change's revisions, the cover undone on its own, another tool's change still a conflict; `cover` and `tags` in the history | `TestStagedEditsAfterSaveCoverAlone` |
| Disc folders: the cover shown, a replace leaving each disc's art, a busy disc folder, Delete and its undo, `folder.jpg` staying | `TestDiscFolderCovers` |
| A damaged disc cover found, repaired, removed by the replace alone, and put back by an undo | `TestDamagedDiscCover` |
| A shared folder asks for a new cover and a Delete, with and without a cover file there; the undo and an embedded-only edit ask nothing | `TestSaveCoverAsksInASharedFolder` |
| A damaged cover file found by ffmpeg; repair from a good embedded cover, by re-encoding, and nothing to repair | `TestBadCoverAndRepair` |
| An interrupted change's cover files settled by their hash | `TestInterruptedCoverIsSettled` |
| The store keeps covers the history needs and young uploads | `TestCoverStoreSweep` |
| A route's own body limit, and the 1 MiB limit everywhere else | `TestRouteBodyLimit` |
| Navidrome's view after a retag, a rename, an undo, a split, the date fix and a cover; a disc album's cover, disc art and Delete; a cover reaching the album that shares its folder; a deleted album | `TestNavidromeContract`, with `make contract` |
| A download's folders are busy while its source writes, and folders inside them; folders outside the music folder are ignored | `TestBusyWhileTheSourceWrites`, `TestWorkingFolderOutsideTheMusicFolder` |
| Qobuz's work folder and both names it ends with; Apple Music's whole folder | `TestDownloadResult` in `qobuz`, `TestDownloadAlbum` in `applemusic` |
| An MP3 stream for a format the browser refuses; a file ffmpeg can't read | `TestAudioTranscode` |
| The keys Navidrome reads | `TestKeysNavidromeReads` |
| A request the client left is logged as info, and a canceled context with the client still there as an error | `TestAClientThatLeftIsNoError` in `httpx` |
| Mixed values, a staged edit and what it was, a track that holds the value, retyping the old value, Revert, multi-value adds, fills, the Save body, a cover's tracks | `web/src/lib/tags.test.ts` |
| Each Fix: the date pair, a key that feeds the release date, `DATE` alone, a file with no date, differing values, disagreeing tags, the flag, and the issues with no Fix | `web/src/lib/tags.test.ts` |
| All fields: each key once, a row and a main field on one tag, a second key's own row, where a new key goes | `web/src/lib/tags.test.ts` |
| A Save's confirm and Cancel, each refusal keeping the stage, a reload taking new revisions, a finished and a failed write, Save cover alone, following a new album ID, staged edits across albums, a reconnect | `web/src/stores/editor.store.test.ts` |
| The list, its lamp, no match, the first index, a locked tag, a selected track, Revert, a suggestion, a note, a silent service, Fix, each refusal's state, an undo's confirm, custom fields, a deleted tag, keys, keys behind a dialog, a pasted release, a failed anchor, the long editor, the transcode retry, Open in Library | `web/src/components/library.test.ts` |
| The files' release: release fields with one value and the anchor, catalogs that agree, track ID fills, held values left out, covers, the `.lrc` note, stored answers | `TestSuggestionsFromTheFilesRelease` |
| Anchors by barcode, ISRC and search, with a compilation and a live album; a staged link; a files ID that fails the threshold or names no release; a bad anchor | `TestAnchorWays` |
| A classical album searched without its artist, read with works, with composers | `TestClassicalSuggestsComposers` |
| File and folder names, other tracks, `shelf.txt` and the catalog it names | `TestLocalSuggestions`, `TestParseNames` |
| Services that fail, a source's own reason, nothing stored | `TestSuggestionsWhenServicesFail` |
| A deferred job with a follow-up waiting, and the stream carrying a run | `jobs`: `TestDeferTakesInACoalescedFollowUp`; `downloads`: `TestEventsStreamCarriesARun` |
| Lyrics per track, held lyrics, LRCLIB down | `TestLyrics` |
| Cover previews and uploads through shelf, an unknown or broken image, retention | `TestRemoteCovers` |
| Two anchors in a Save; an anchor beside another release's album ID, also on a track left out | `TestSaveRefusesTwoReleases` |
| Agreement, one count per source, release fields held to one value, three per field | `TestMerge` |
| A run's fixes, one undo step, redo, one scan per run, an album of a run refused alone | `TestRunFixesAlbumsAsOneUndoStep` |
| Fixes in order on each other's results; a tie; each date case; M4A's date | `TestRunFixesBuildOnEachOther`, `TestCommonValues`, `TestReleaseDateEdits` |
| Preview counts, samples, each selection form, bad fixes and filters | `TestRunPreview` |
| No fix, nothing to fix, a second run at once, the working run | `TestStartRunRefuses` |
| A busy folder, a split a Save would ask about, nothing to change, an album gone from the index | `TestRunSkipsWhatItCantDecide`, `TestRunWithNothingToChangeEndsWell`, `TestRunSkipsAnAlbumThatLeftTheIndex` |
| A Save between two slices | `TestSaveGoesBetweenSlices` |
| A restart mid-run, a write cut off, a start leaving the head alone | `TestRunSurvivesARestart`, `TestRunSettlesAWriteAStopCutOff`, `TestStartLeavesARunningRunAlone` |
| Cancel of a write, of a waiting analysis, of an undo, and a job that fails | `TestCancelRun`, `TestCancelStopsTheAnalysis`, `TestCancelledUndoFinishesOnTheNextPress`, `TestFailedAnalysisClosesTheRun` |
| An undo that meets a tag changed since | `TestRunUndoLeavesWhatChangedSince` |
| Retention and Clear history keep a run whole | `TestHistoryRetentionKeepsARunWhole` |
| ReplayGain in FLAC and M4A, audio not decoded twice, a file ffmpeg can't read | `TestRunWritesReplayGain`, `TestRunMeasureFailureSkipsTheAlbum` |
| Gates, pooled blocks against one decode, no signal, a stopped decode, tag formats | `loudness`: `TestIntegratedGates`, `TestPoolMatchesOneDecode`, `TestParseKeepsThePeakAndSkipsNoSignal`, `TestAnalyzeStopsWithItsContext`, `TestFormats` |
| AcoustID as the last resort, a miss, a failure, no key, MusicBrainz down | `TestAudioFindsTheAnchorWhenTextFindsNothing`, `TestAudioThatFindsNothingIsKeptAsAMiss`, `TestFailedFingerprintStoresNothing`, `TestNoKeyNoFingerprint`, `TestAudioDoesNothingWhenMusicBrainzFails`, `TestAudioWaitsForTextLookups` |
| The key out of the address, the service's errors, fpcalc failing | `acoustid`: `TestLookupKeepsTheKeyOutOfTheAddress`, `TestLookupReportsTheServiceError`, `TestFingerprint` |
| The client's cache, retries and decoding; links; the limiter's spacing and the editor going first | `TestReleaseAndCache`, `TestShapes`, `TestParseRelease`, `TestLimiterSpacesCalls`, `TestLimiterLetsTheEditorGoFirst` in `musicbrainz`, `TestGet` in `lrclib` |
| The Status probe: no cache, no retry, busy, errors, down, one probe for 30 seconds | `TestPing` in `musicbrainz`, `TestMusicBrainzStatus` |

The fixtures in `tagio/testdata` are half a second of a sine per format, written by `testdata/make.sh`. Tests that need ffmpeg skip where it isn't installed. `musicbrainz/testdata` holds answers in MusicBrainz's and the Cover Art Archive's shapes, written by hand with made-up IDs.
