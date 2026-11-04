# UI brief

What each screen does. [design/](design/README.md) has the look, the screens and the rules, and wins where the two differ.

## Basics

- One person uses the app, sometimes a family member. The gateway signs them in, so the app has no login screen.
- Phone first for searching and downloading. The desktop works too.
- Night is the default theme and Day a switch, kept per browser.
- Name: Music Shelf in the header and the page title, Shelf where space is short.
- Lists stay table-first, because they set the pattern for the library table of later phases. Tables have a row cursor, arrow keys, Enter to open, and focus rings in both themes.
- Text contrast is WCAG AA or better in both themes. Fonts are self-hosted, since the CSP loads nothing from other origins.

## Shell

- A menubar on the desktop and a tab bar on the phone, with Search, Downloads, Library and Status. Library comes with phase 2.
- A tab shows one amber lamp while there is something to look at, never a number: Downloads while downloads run or wait, Status while a part needs attention, Library while a bulk run works. Issues never light it, because most albums start with one.
- Notices never cover the content. On the phone a notice such as Queued takes its own slot above the tab bar. On the desktop a result or a refusal takes the top of the side column.

## Search

- One field. Text searches. A pasted link shows the item it points to as a card with a Download button.
- Search covers every source that is on, in one list with the source named on each row. The Sources switch has one switch per source.
- A source that is down says why, such as "Qobuz token expired", with a link to Status. The other sources' results stay.
- The type knob switches between Albums, Tracks, Artists and Playlists and keeps the query.
- Rows: album with cover, title and version, explicit marker, artist, year, track count, duration, quality, label, status, Download with a format menu, and a link to the source's page. Track, artist and playlist rows show what fits their kind. "Load more" ends the list.
- A row opens a side panel on the desktop or a page on the phone. The album panel has the tracklist with a Download per track. The artist panel groups the discography into Albums, EPs and singles, Compilations, and Appears on, with Download all, which asks first with the number of releases, how many are already in the library, and the format. The playlist panel has its tracks.
- Download status: nothing, In library, Queued, Downloading with a percentage, Failed.
- States: before the first search, loading, no results, a source error, and rate limited, "Qobuz is slowing us down, try again in a minute".

Quality labels: Qobuz `Hi-Res 24/192`, `Hi-Res 24/96`, `CD 16/44.1` and `MP3 320`; Apple Music `Hi-Res Lossless`, `Lossless`, `Dolby Atmos` and `AAC`, and an album can carry two. Format menus: Qobuz Max, 24/96, CD and MP3 320; Apple Music ALAC, Dolby Atmos and AAC. The first of each is the default.

## Downloads

- Counters: running, queued, and failed in the last 24 hours.
- A running download shows "3 of 12 tracks done", the current track, the elapsed time, one lamp per track, and Cancel. The queue follows in order.
- History: finish time, source, title, artist, kind, who asked (Web or the Telegram user), result, duration, size and quality, with Retry on failed and partial rows. An artist download is one row that opens into its albums.
- The download panel shows each track's status, path, format and error, the path of `shelf.txt`, and the live log.
- The Navidrome scan state with Scan now.
- Everything updates live.

## Library

Planned for phase 2. A tag editor for everything in the music folder. It changes tags and covers only, and never renames or moves a file. Nothing reaches a file until the owner saves or starts a run. Keep it plain: one tab, one list, one editor, and nothing on screen that the current task doesn't need.

### Album list

- One search box and one filter menu. The filter offers All, Has issues, one entry per kind of issue, No MusicBrainz ID, Guide style, and each top folder such as `qobuz` or `jazz`.
- A row shows the cover, album, artist, year, track count, quality, and one lamp when the album has issues.
- Until the owner runs its fix, nearly every album lacks `RELEASEDATE`, so most rows start with a lamp. The list must stay calm like that.
- Sort by album, artist, year or date added.
- A finished download links to its album here.

### Editor

- The desktop shows the editor beside the list, in panels the owner can resize. The phone opens it as a page.
- The header has the cover, album, artist and quality, and the album's issues.
- The main fields are Title, Artist, Album, Album Artist, Track, Disc, Genre, Release date, Original date, Recording date and Compilation.
- With no track selected, an edit changes every track. A field whose tracks differ shows Mixed. A tag that belongs to a single track, such as Title, Track, ISRC, a track ID, track gain or lyrics, stays read-only until a track is selected. Selecting a track edits that track alone.
- The track list shows number, title, duration, quality, a lamp for issues, and a play button. One small player serves the album.
- All fields is one switch. It shows every tag, each with its own suggestion. Standard holds the tags Navidrome reads, and Extended the ones it ignores. Custom fields are added and deleted here, and both stage like any edit. Adding a key that already has a row goes to that row. A new key Navidrome reads goes under Standard.
- A multi-value tag, such as ARTISTS or Genre, has one input per value and an Add value button. Pressing a suggestion adds it as another value.
- Lyrics and any value over 100 characters open a long-form editor.
- Edits stay staged. A bar with Save and Discard appears only while something is staged, and names how many changes it holds. One Save is one undo step.
- When a save would split an album in Navidrome, Save asks first and says the album's star and rating go with the moved tracks, even after an undo. A save that merges two albums or renames an album artist asks too, and says what happens to their stars and ratings.
- A field shelf can't change in a file, such as the original date of a Picard-tagged M4A, is read-only with a one-line reason.

### Suggestions

- A suggestion sits beside its field, with where it came from and a confidence percentage, such as "Kind of Blue, MusicBrainz, 96%". The source is plain text, as on search rows.
- One tap puts the value in the field as a staged edit. Nothing else changes. There is no album card and no Apply step.
- A field shows its best few suggestions. A field with none shows nothing, not a placeholder.
- Release fields, such as the MusicBrainz IDs, barcode and label, show one value, because shelf holds an album to one release. Pasting a MusicBrainz ID or link into the album ID field moves every release field to that release.
- A cover suggestion is a thumbnail. Accepting one stages a new cover.
- Suggestions load after the editor opens, so a field may fill in a moment later.
- A short note appears where Navidrome will show something else, such as "Navidrome shows the .lrc file, not these lyrics" or "Navidrome will show 2011 for this album".

### Covers

- Preview, Upload, Save cover alone, Apply to album and Delete.
- Save cover alone writes the cover at once as its own undo step, and staged tag edits keep waiting. Apply to album and Delete stage for the next Save.
- A corrupt cover shows as an issue with Repair.
- A cover edit changes the embedded cover and the folder's `cover.*`, and the dialog says so in one line.

### Issues

- The album's issues are a short list with a Fix on each. A Fix stages edits for review, and nothing is written until Save.
- Issues: a missing required tag, a split album, values that differ across tracks, tags that disagree, Navidrome showing a reissue year, no `RELEASEDATE`, Various Artists without the compilation flag, a wrong artist split, IDs from two releases, no cover.
- Guide style, such as `ALBUMARTISTS` missing or no ReplayGain, stays under its filter and never lights a lamp.

### History, desktop only

- A timeline of Saves, newest first: when, which album or run, and how many tags changed. A row opens to before and after values.
- Undo and Redo on any row, in any order. A field that changed since then is left alone and listed as a conflict.
- Clear history, which asks first.

### Bulk, desktop only

- The owner selects albums in the list and picks fixes, or ReplayGain.
- The preview shows sample changes, the number of albums and files, the GB touched, and what the nightly backups will copy again. Start asks nothing more.
- A run shows its progress and can be cancelled. A whole run is one undo step.

### Keyboard, desktop only

Every action has a key: list and field movement, Enter to edit, Esc to cancel, accept a suggestion, play, Save. `?` opens a sheet with all of them.

### Help

Helper text never adds to a screen at rest. On the desktop a note opens under the pointer, and on the phone a small `?` opens it. A Tagging guide, based on Navidrome's tagging guidelines, sits behind the `?` in the menubar and behind each note on the phone.

### Phone

The phone shows the list, an album, its issues and its main fields with suggestions. It can Save. Custom fields, bulk runs, History and the keyboard sheet are desktop only. Screens on the phone leave those out, with no disabled buttons in their place.

### States

Loading an album, no albums match, saving, saved, a conflict because another tool changed a file, a locked file owned by another user, an album busy with a download, a failed save with the files left as they were, suggestions unavailable because a service doesn't answer, and a first index still running.

### Screens to design

- Phone: album list, album, field edit with suggestions, album issues.
- Desktop: list with editor, All fields, a selected track, long-form editor, cover dialog, history, bulk preview and progress, keyboard sheet.
- Both: the states above, in Night and Day.

## Status

One module per part:

- Qobuz: the subscription, whether the token works, and how to replace an expired token.
- Apple Music: the wrapper state, the storefront, Restart, a collapsible log tail, and Sign in while the wrapper isn't running.
- Navidrome: the address, whether it answers, the last scan.
- Telegram: the bot's name and how many users it answers.
- Library, with phase 2: whether the index is running, the albums and tracks it holds, and whether MusicBrainz answers.
- Notices when a download ends, Off, Failures or All, and on the desktop the table density.

The sign-in dialog has two steps: Apple ID and password, then six digit boxes with a 60-second countdown. It ends with "Signed in, storefront RU", the error with Try again, or "The code wasn't entered in time. Try again."
