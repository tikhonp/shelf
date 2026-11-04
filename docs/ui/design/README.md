# Design

The screens and styles, exported from the Studio redesign and Library pages of the Claude Design canvas at https://claude.ai/artifact/Hkb6TNxr1Lr9Augzgk1XXn. The app's CSS is the source of record. A small visual change goes straight into the app, and a bigger one goes through the canvas first.

## Files

- `tokens.css` holds the color and font variables for both themes, and the side gutter.
- `components.css` holds the parts outside the Library tab and reads the variables in `tokens.css`.
- `library.css` holds the parts of the Library tab and sits on top of the other two. It is the canvas's `project/library.css`, unchanged.
- `screens/MS-*.html` has one static page per screen. Open one in a browser to see it, and add `?theme=day` to the address for the Day theme.

The two CSS files are the canvas stylesheet `project/retro.css`, cut in two. Each screen is a canvas board rendered once with its sample data. The board's script fills the template, and links between boards point at the exported files.

### Phone screens, 390 by 844

| Screen | Shows | Route |
| --- | --- | --- |
| `MS-phone-start` | Search before the first search: the field, the Sources cap, the type knob and the running download | `/search` |
| `MS-phone-search` | Album results from both sources, one album just queued, and the Queued slot above the tab bar | `/search` |
| `MS-phone-search-tracks`, `-artists`, `-playlists` | The other result types | `/search` |
| `MS-phone-search-day` | Album results in the Day theme | `/search` |
| `MS-phone-album` | An album and its tracks | `/search` |
| `MS-phone-album-format` | The album with the format menu open | `/search` |
| `MS-phone-artist` | An artist with the Download all confirm open | `/search` |
| `MS-phone-playlist` | A playlist and its tracks | `/search` |
| `MS-phone-link` | A pasted Apple Music link | `/search` |
| `MS-phone-first-run` | No source ready yet | `/search` |
| `MS-phone-downloads` | The running download, the queue and the history | `/downloads` |
| `MS-phone-downloads-empty` | Nothing downloading, and the history | `/downloads` |
| `MS-phone-download` | One download, live, with its tracks | `/downloads/:id` |
| `MS-phone-status` | Every part's state, with Apple Music signed out | `/status` |
| `MS-phone-signin`, `-signin-code`, `-signin-done` | Apple Music sign-in: the Apple ID, the code, done | `/status` |

### Desktop screens, 1440 wide

| Screen | Shows | Route |
| --- | --- | --- |
| `MS-desk-search` | Results beside the current download and the queue | `/search` |
| `MS-desk-search-day` | The same in the Day theme, with the source switches open | `/search` |
| `MS-desk-album`, `-artist`, `-playlist` | A panel open beside the results; the artist has the Download all confirm open | `/search` |
| `MS-desk-link` | A pasted Apple Music link in place of the results | `/search` |
| `MS-desk-downloads` | Current download, queue and history, with one download open | `/downloads/:id` |
| `MS-desk-status` | Status with the sign-in dialog at the code step | `/status` |
| `MS-desk-settings` | Status with every part working, and the Notices and Density switches | `/status` |
| `MS-desk-search-compact`, `MS-desk-downloads-compact` | Search and Downloads with compact rows | `/search`, `/downloads/:id` |

### Library on the phone, 390 by 844

| Screen | Shows | Route |
| --- | --- | --- |
| `MS-phone-library` | The album list | `/library` |
| `MS-phone-library-filter` | The list with the filter menu open | `/library` |
| `MS-phone-library-album` | An album at All tracks, with its fields and tracks | `/library/:id` |
| `MS-phone-library-field` | One field's page, with its suggestions | `/library/:id` |
| `MS-phone-library-issues` | An album's issues | `/library/:id` |
| `MS-phone-library-staged` | Staged changes, with the Save bar in its slot | `/library/:id` |
| `MS-phone-library-ask` | A selected track, with the split warning in a slot | `/library/:id` |
| `MS-phone-library-day` | An album in the Day theme, with the player | `/library/:id` |
| `MS-phone-library-note`, `-issue-note` | A field's note and an issue's note, in a slot | `/library/:id` |
| `MS-phone-library-guide` | The Tagging guide | `/library/:id` |

### Library on the desktop, 1440 wide

| Screen | Shows | Route |
| --- | --- | --- |
| `MS-desk-library` | The list, the Editor and the track list at All tracks | `/library/:id` |
| `MS-desk-library-list` | The wide list with no album open | `/library` |
| `MS-desk-library-all` | All fields | `/library/:id` |
| `MS-desk-library-track` | A selected track, playing, with two Genre values | `/library/:id` |
| `MS-desk-library-day` | Staged changes in the Day theme | `/library/:id` |
| `MS-desk-library-long` | Lyrics in the long-form editor | `/library/:id` |
| `MS-desk-library-cover` | The cover dialog | `/library/:id` |
| `MS-desk-library-ask` | The split warning | `/library/:id` |
| `MS-desk-library-note` | A hover note on a field name | `/library/:id` |
| `MS-desk-library-guide`, `-keys` | The Help sheet at Tagging guide and at Keyboard | `/library/:id` |
| `MS-desk-library-history` | History with one Save open | `/library` |
| `MS-desk-library-bulk`, `-run` | The bulk preview and a run in progress | `/library` |

### Other pages

| Page | Shows |
| --- | --- |
| `MS-states` | Search states, source errors, the rate-limit wait, Download all and sign-in results, at phone width |
| `MS-parts` | Every part and the main tokens |
| `MS-library-states`, `-states-day` | Every Library state, at phone width, in Night and Day |
| `MS-library-parts` | The Library parts, the fourth tab, and the changes to Status and Downloads |

## Direction

The look is a recording studio built around a British mixing console: blue-grey modules, ivory push buttons, red and blue knobs, lamps, small displays and a little wood. The look is only visual. UI text uses the glossary words in `CLAUDE.md` and never names the studio, so there is no "tape machine", "rack", "lathe" or "master" on screen.

- The phone comes first for search and download. The desktop uses the same parts with more room.
- Night is the default theme. Day is a switch, and the app keeps the choice in `localStorage`.
- Each element shows one thing. A compact view of a download shows the cover, a display with tracks done and the current track, and one lamp per track. Only the detail view adds a picture, the disc being cut.
- Notifications never cover content. On the phone a notification such as Queued takes its own slot between the window and the tab bar, and the window gets shorter. On the desktop a result or a refusal takes the top of the side column, and Queued shows only in the row and the queue.
- Search covers every source by default and puts the results in one list, with the source named in plain text on each row.

## Root classes

The app's root element carries `rs` and these classes:

| Class | Effect |
| --- | --- |
| `day` | Switches to the Day theme. Without it the page is Night. |
| `ph` | Phone layout: taller title bars, 46px buttons, 56px menu rows. |
| `desk` | The brushed console background. Both layouts use it. |
| `console` | Desktop only. Adds the wooden sides and widens the side gutter from 24px to 38px. |
| `compact` | Desktop only. The Compact density: 40px rows instead of 58px, 36px track rows instead of 44px, and 30px covers instead of 46px. Without it the rows are Comfortable. |

The canvas sets `ph` per board. The app applies the same rules through a media query below 720px of width. The canvas draws only 390px and 1440px frames. 720px holds up in the app, and between 720px and about 950px the grids drop columns and the menubar wraps.

## Layout

On the phone, `.stack` holds the modules between the top edge and the tab bar (`.dock`). The main window takes the free height. A slot module (`.win.slot`), such as Queued or a confirm, takes its own height and the main window shrinks. The tab bar has Search, Downloads, Library and Status, and the current tab is backlit peach.

On the desktop, the menubar holds the brand, the same four tabs, one lamp for each part (Qobuz, Apple Music, Navidrome), the theme switch and a clock. Below it, the results window and a side column share the row and wrap when the window is narrow. The side column shows the current download and the queue, or one panel: album, artist or playlist. A pasted link replaces the results with that item and keeps the current download beside it. The menubar and the content line up on one side gutter.

## Type

| Use | Font | Size |
| --- | --- | --- |
| Body | IBM Plex Sans | 14px, line height 1.45 |
| Secondary lines | IBM Plex Sans | 13px |
| Row titles | IBM Plex Sans, weight 600 | 15px |
| Search field and phone inputs | IBM Plex Sans | 16px |
| Buttons, tabs, headers, labels | IBM Plex Sans Condensed, weight 600 or 700, capitals, letter spacing .06em to .14em | 12px to 16px |
| Displays | Pixelify Sans | 15px lines, 26px to 36px readouts |
| Download log | IBM Plex Mono | 12.5px |

All four fonts are under the SIL Open Font License. The screens load them from Google Fonts. The app serves its own copies from `web/public/fonts`.

## Scales

- Spacing: 2, 4, 6, 8, 10, 12, 14, 16 and 20px. Windows have 14px of padding. Desktop windows sit 20px apart, phone modules 10px.
- Radii: 3px for buttons, 4px or 5px for displays and menubar tabs, 6px for fields and panels, 7px for windows, 12px for the tab bar. Lamps, knobs and artist pictures are round.
- Light falls from the top left. Top and left edges carry a highlight, and shadows fall down and to the right. A window's shadow is `4px 8px 22px rgba(0, 0, 0, .45)`. A pressed button sinks 2px.
- Touch targets on the phone are 44px or more. A title-bar cap is 30px and has an invisible 44px target around it.

## Color

- Modules are blue-grey (`--win`) with near-black title bars.
- Buttons are ivory caps (`--key`). The one main action of a screen is amber (`.key.sun`). A selected switch, such as the current tab or a chosen segment, is backlit peach (`--lamp`) with brown text.
- Links use `--sun-text`. Focus is a 3px ring, yellow at night and white by day.

### Lamps

A lamp always has words next to it or an `aria-label`, so color never carries the meaning alone. The design has no legend, so this table is the only place that explains the colors.

| Lamp | Class | Meaning |
| --- | --- | --- |
| Green | `led ok` | Works: a source or part is up, a track is done, an item is in the library |
| Yellow | `led warn` | Needs attention: signed out, rate limited |
| Red | `led bad` | Failed or not working |
| Red, blinking | `led rec blink` | The current download, in its module and in the menubar |
| Cyan | `led run` | A download in progress, on a result row |
| Grey | `led` | Queued |
| Dark | `led off` | Off: a source switched off, no rate limit |
| Amber, top right corner of a tab | any `led` inside a tab | Something to look at: Downloads has work running or queued, Status has a part that needs attention, or Library has a bulk run working. A tab never shows a number, and has no lamp when there is nothing to see. |
| Amber, beside a knob position | `led` in `.rot-labels` or `.fmt-list` | The chosen position |
| Yellow, small, no glow | `led iss` | An album or track has an issue. Nearly every album starts with one, so this lamp stays quiet. |
| Amber, small | `led stg` | An edit is staged and waits for Save |

The track lamps of a download (`.meter`) are green when done, amber and blinking for the current track, red when failed, and dark when not started. Result pills (`.pill`) are green for done, yellow for partial, red for failed, cyan for running and grey for queued. A canceled pill has an outline, and a skipped one a dashed outline.

### Quality labels

Quality labels look like masking tape. The words always name the quality, and the color only groups it.

| Tape | Class | Qualities |
| --- | --- | --- |
| Yellow | `stk hi` | Hi-Res 24/96, Hi-Res Lossless |
| Orange | `stk master` | Hi-Res 24/192 |
| Blue | `stk atmos` | Dolby Atmos |
| Grey | `stk lossy` | MP3 320, AAC |
| Plain | `stk` | CD 16/44.1, Lossless, Mixed quality |

## Controls

- Push buttons (`.key`) are 40px high, 32px in the small size and 54px in the large one. On the phone the default is 46px. Legends are capitals.
- The red knob picks the result type and the blue knob picks the format. Pressing a knob turns it to the next position. The labels next to a knob, each with a lamp, are the real radio buttons. A knob has no printed marks around it. Pointer angles are -75, -40, 40 and 75deg for the type, 45, 75, 105 and 135deg for a Qobuz format, and 45, 90 and 135deg for an Apple Music format.
- The Sources switch names what is searched, such as "All sources" or "Qobuz only", and has one lamp per source. It opens one rocker switch per source, and a rocker is on when its top is pressed in. On the phone the switch is a small cap in the corner of the Search title bar.
- On the desktop, an album, a playlist or Download all shows the blue knob with the list of formats. An album also shows the frequency range of the chosen format on a small display. On the phone the caret next to the Download button opens the format menu.
- Search and code fields are plain recessed inputs. Displays only show information and never take input.

## Lists

- Desktop results are a grid with a row cursor. The up and down arrows move it, Enter opens the row's panel, Esc closes the panel, and `/` jumps to the search field. Clicking a title also opens the panel.
- The columns follow the type. Albums show cover, album, tracks, length, quality, status, download and link. Track rows leave out the count. Artists show a round picture and the number of releases, and an open button takes the place of download. Playlists show tracks, length and quality. While a panel is open, the grid drops tracks, length and link (`.g-res-n`).
- Download on a row uses the default format, and the caret next to it picks another one.
- A phone row has the cover, two or three lines and one 46px button. The button queues the item in the default format and then shows its state.
- The Density switch on the desktop's Status page sets how tall the rows are in search results, track lists and History. A preview of two History rows under the switch shows the choice. Compact keeps each row's second line. The phone has no switch, because a shorter row there would lose its quality labels.
- Search shows the first page from each source in one list. The window title counts what is shown, such as "14 of 31 albums", and Load more fetches the next page.

## Library

The canvas boards are `project/Lib-*.dc.html`, styled by `project/retro.css` and `project/library.css`. `Lib-Phone` holds the whole phone flow and `Lib-Desk` the whole desktop flow. The other boards import one of the two with a prop, and the exported screens are their first states.

- A suggestion is a strip of masking tape (`.sug`) beside its field, or under it in a narrow panel. One press stages its value.
- The grey knob sorts the album list by Album, Artist, Year or Added. It uses the type knob's pointer angles.
- The album list shows every column in a wide panel. Below 640px of panel width, the year moves into the row's second line and the track count sits under the quality label.
- A display above the fields says which tracks an edit reaches, `ALL 11 TRACKS` or `TRACK 03 ONLY`. The track list starts with an All 11 tracks row, which is the way back from a selected track.
- A staged field has an amber edge, a line with its old value, and a Revert button. Revert drops that one edit, so a Fix can be reviewed field by field.
- A tag that belongs to a single track is read-only at All tracks, with a dashed edge and a lock, and accepts typing once a track is selected. That covers Title, Track, ISRC, the track IDs, track gain and lyrics. On the phone the Title and Track rows don't open, and the other tags aren't shown.
- A per-track ID keeps its suggestion at All tracks, because one press fills every track with its own ID.
- A multi-value tag has one input per value, a remove button on each when there are two or more, and a plain Add value button (`.lf-add`). A single-value field looks the same as before. Pressing a suggestion on a multi-value tag adds it as another value.
- Delete in the cover dialog stages, like Apply to album. Save cover alone is the one action that writes at once.
- An added custom field shows the same staged look as other edits. A deleted one stays in its row, struck through, until Save.
- All fields is a small rocker in the Editor's title bar. On the phone, the same rocker pattern sets Compilation, and All fields doesn't exist.
- The Save bar sits at the foot of the Editor on the desktop. On the phone it takes a slot above the tab bar, and so does the player. The desktop player sits at the foot of the track list. Both players appear only while a track plays.
- Select and History are caps in the Library title bar, and the bulk run and History open beside the album list. The phone has neither.
- Dividers (`.lib-split`) resize the panels by dragging or with the left and right arrows. Below 1100px the panels stack and the dividers go.
- The filter menu shows how many albums each entry holds.
- A phone confirm, such as the split warning, is a slot. A desktop confirm is a dialog.

### Help

Help never shows on a screen at rest, and the design adds no helper text beside a control. The text follows [Navidrome's tagging guidelines](https://www.navidrome.org/docs/usage/library/tagging/) and the measurements in `docs/decisions.md`.

- On the desktop, a note (`.tipbox`) opens 350ms after the pointer reaches a name, and closes when it leaves. Names with a note gain a dotted underline under the pointer. That covers field and tag names, the display that says which tracks an edit reaches, issue names, Standard and Extended, filter entries, suggestions, the Save bar's count, Undo and Redo, and the fixes of a bulk run. A field with nothing worth explaining has no note.
- The `N` key shows the same note for the field the cursor is on. In the app, the note is also the field's `aria-describedby`.
- The menubar's `?` opens the Help sheet at the Tagging guide, and the `?` key opens it at Keyboard.
- The phone has no hover. A small `?` cap (`.wbtn.q`) sits in the title bar of the album and field pages and on each issue. It opens the note in a slot above the tab bar, and the slot links to the Tagging guide page. The album list stays as it is.

## Accessibility

- Every text color in `tokens.css` passes WCAG AA on every module, panel and field color. The lowest pair at night is 4.5:1, secondary text on the lighter top of a window. The lowest by day is 4.6:1, secondary text on panel blue. Button text on ivory is 11.7:1, backlit tab text 6.6:1, and display text 12.4:1 at night and 8.3:1 by day.
- Lamps and icons carry text for screen readers. The type and format pickers are radio groups, the source rockers are switches, the results are a grid, and notifications are status regions.
- With reduced motion, nothing blinks, spins or slides.

## Where the app differs from the screens

- The screens outside the Library still show three tabs. The Library screens show all four, with 12px legends so Downloads clears its key.
- `MS-desk-library-note` shows its note at a fixed place. The canvas and the app place a note under the name it explains.
- The strict CSP forbids inline styles, and the screens use them for layout. The app moves them into scoped stylesheets and sets changing values, such as the knob angle `--a`, the progress `--p` and the size `--s`, as CSS variables from script.
- The app switches to the phone layout with a media query at 720px, not with the `ph` class.
- The app serves its own font files.
- The covers are made-up CSS art (the `a-*` classes). The app shows the real artwork.
- Names, numbers and dates are sample data.
- Each screen shows its board's first state. On the canvas the boards are interactive: knobs turn, rows open panels, Download all asks and confirms, and the Sources switch opens.
- The phone's theme switch is on Status, because the phone has no menubar.
- Notices when a download ends are new. They use the Queued slot with a green, yellow or red lamp, and a Notices switch on Status sets which ones show.
- The Density preview can't be clicked in the app, and its table says Title and Web, the words of the real History.
- Compact rows also cover the lists in the app's download panel.
- Error text on a module is a pale red at night and a dark red by day, not `--red`, which is about 2:1 there. Lamps and pills keep `--red`.
- Status has no Rate limits module, and the Qobuz card has no last request time, because the API has neither.
- A running track says Downloading, without a percentage, because the API has no per-track progress.
- Between 720 and about 950 pixels the results and the history drop columns, and the menubar wraps.
- A tag struck through for deletion uses `.lib-in.struck`, not the canvas's `.gone`, which `components.css` already uses to hide things.
- The rotary labels, the rocker and the menu moved from one component's scoped style into `parts.css`, because the Library uses them too.
- Below 1100 pixels each stacked panel stops at the window's height and scrolls by itself, so the Save bar stays in view.
- A lossy file shows its codec, such as MP3, because the index has no bitrate. A lossless one that is neither CD nor Hi-Res says Lossless.
- The list has no version beside the album's name, because the list API has none.
- The bulk preview's rockers all start off, where the screen shows one on, so a run does only what the owner chose. A rocker whose fix reaches no selected album is disabled.
- A fix in the bank has its issue's name, such as Various Artists without the compilation flag.
- After a run ends, the panel says how it went in place of the note about Saves, and the title bar has a close button in place of the lamp.
- The meter shows one lamp per album up to 40, and lamps share albums past that.
- The cover dialog shows no size for the current cover, because the API has none.
- On the phone the cover dialog is the desktop's dialog in a narrower window. The canvas has no phone board for it.
- Status's Library part keeps the index's two lines and gives MusicBrainz a third, with its answer time when it answers. The note under it while MusicBrainz is down says "Suggestions from MusicBrainz" where the canvas says "Its suggestions".
- Delete is new, with no canvas board. It is a text cap in the Editor's title bar on the desktop and in the album's title bar on the phone. It opens the confirm dialog, or the phone's slot, with Delete as its key. The title is "Delete this album?", or "Delete these albums?" when other albums share the folder. While the delete runs, the Save bar's place shows DELETING and the file count. A refusal uses the Not saved panel, titled Not deleted. In History a delete reads "Deleted: Blue", has a Deleted pill and no Undo, and opens to the folder, the files and size, the other albums, the downloads and Navidrome.
