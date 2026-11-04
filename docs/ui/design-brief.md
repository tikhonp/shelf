# Design brief: Music Shelf, phase 1

This brief was the input for the Claude Design session of 2026-11-03 and 2026-11-04. Its result is in [design/](design/README.md): the screens, the stylesheets and the rules. Where the two differ the design wins. This brief now matches it. Step 1g builds the Vue app from both.

## The product

Music Shelf is a self-hosted web app for one person who grows a personal music library. The library is served to phones and players by Navidrome; Music Shelf decides what goes into it. In phase 1 it finds albums, tracks, artists and playlists on two streaming catalogs, Qobuz and Apple Music, downloads them into the library, and shows every download live. Later phases make a table of the whole library the home screen, with editing, so the tables designed now set the pattern for that one.

- Users: the owner, sometimes a family member. Signed in by the server before the app loads; the app has no login screen.
- Devices: phone first for searching and downloading. The desktop needs to work well too.
- Themes: Night, which is dark and the default, and Day, which is light. A switch picks one, and the app remembers it.
- Language: English.
- Name: Music Shelf in the header and the page title; Shelf where space is short.

## Look and feel

- The look is a recording studio built around a British mixing console: blue-grey modules, ivory push buttons, knobs, lamps and small displays. The look is only visual, and labels use the words of the glossary in `CLAUDE.md`.
- Usability comes first. Lists stay table-first, with many rows on a screen, because the tables designed now set the pattern for the library table of later phases.
- Album covers supply most of the color in lists. Amber marks the one main action of a screen.
- Clear states at a glance: queued, running with progress, done, done with errors, failed, canceled, skipped.
- Keyboard-friendly tables: a visible row cursor, arrow keys, Enter to open, focus rings that show in both themes.
- Text contrast at WCAG AA or better in both themes.
- Fonts must be self-hostable files, because the app's content security policy loads nothing from other origins.

## Shell

- Desktop: a menubar across the top with Search, Downloads and Status. Library will join as the first item in a later phase.
- Phone: a tab bar at the bottom with the same items.
- A tab shows one amber lamp while there is something to look at: Downloads while downloads run or wait, Status while a part needs attention. Tabs never show a number.
- Notices never cover the content. On the phone a notice such as Queued takes its own place between the window and the tab bar, and the window gets shorter. On the desktop the row's status and the queue show it.

## Search

The main screen of phase 1.

- One search field at the top. Typing text searches. Pasting a Qobuz or Apple Music link shows the single item it points to, as a card with cover, title, artist, quality and a Download button.
- Search covers every source that is on, and the results come in one list with the source named on each row. A Sources switch says what is searched, such as "All sources" or "Qobuz only", with one lamp per source, and opens one on and off switch per source.
- A source that is down shows why, for example "Qobuz token expired" or "Apple Music: not signed in", with a link to Status. The other source's results stay.
- A type switch: Albums, Tracks, Artists, Playlists, drawn as a knob with the four labels beside it. Switching keeps the query.
- Results as a table on the desktop and a list on the phone:
  - Album rows: cover thumbnail, title with version in lighter text, an explicit marker, artist, year, track count, duration, quality badge, label, download status, a Download button with a format menu, and an icon link to the album on the source's site.
  - Track rows: cover, title, artist, album, duration, quality, status, Download.
  - Artist rows: round picture, name and the number of releases. Opening one shows the discography.
  - Playlist rows: cover, name, curator, track count, duration, quality, Download.
- "Load more" at the end of the list.
- A row opens a side panel on the desktop or a full-screen page on the phone:
  - Album panel: large cover, title, artist, year, label, release date, genre, UPC, quality, the tracklist with number, title, duration, per-track quality and a per-track Download, and a Download album button with the format menu.
  - Artist panel: picture and name, then the discography in groups: Albums, EPs and singles, Compilations, Appears on. Each album has its own Download. Download all sits at the top and asks first, inside the panel, with the number of releases, how many are already in the library and skipped, and the format.
  - Playlist panel: cover, name, curator, description, the track list, Download playlist.
- Download status on rows and panels: nothing, In library, Queued, Downloading with its percentage, Failed.
- States to design: before the first search, loading rows, no results, a source error, and rate limited, with a message such as "Qobuz is slowing us down, try again in a minute".

### Quality badges

One vocabulary for both sources:

- Qobuz: `Hi-Res 24/192`, `Hi-Res 24/96`, `CD 16/44.1`, `MP3 320`.
- Apple Music: `Hi-Res Lossless`, `Lossless`, `Dolby Atmos`, `AAC`. An album can carry two badges, such as Lossless and Dolby Atmos.

### Format menus

The Download button's menu:

- Qobuz: Max, which is the default, then 24/96, CD and MP3 320.
- Apple Music: ALAC, which is the default, then Dolby Atmos and AAC.

## Downloads

- Counters at the top: running, queued, failed in the last 24 hours.
- Running downloads: cover, title, artist, source, format, a display with "3 of 12 tracks done", the current track's name and the elapsed time, one lamp per track, and Cancel.
- The queue in order, with Cancel on each row.
- History as a table: finish time, source, title, artist, kind, who asked, result, duration, size, quality obtained, and Retry on failed or partial rows. "Who asked" is Web or the Telegram user's name.
- An artist download shows as one row that expands into its albums.
- Clicking a row opens the download panel: header with cover and result, then each track's status, file path, format and error; the path of the folder's `shelf.txt`; and a live log in a monospace box that follows new lines.
- The Navidrome scan state in a corner: "Last scan 3 minutes ago", "Scanning…", and Scan now.
- Everything updates live without a reload.

## Status

One card per part, each with a clear healthy or not state:

- Qobuz: the account's subscription, such as Studio, whether the token works, and short instructions for replacing an expired token.
- Apple Music: the wrapper state, one of running, signed out, waiting for code or error; the storefront; Restart; a collapsible log tail; and Sign in.
- Navidrome: the address, whether it answers, the last scan.
- Telegram: the bot's name and how many users it answers.

### Sign-in dialog

Two steps:

1. Apple ID and password, with a Sign in button.
2. "Enter the code sent to your Apple devices": six digit boxes, a 60-second countdown, and Confirm.

It ends with "Signed in, storefront RU" on success, or the error with Try again. When the countdown runs out: "The code wasn't entered in time. Try again."

## Components

- Data table with sticky header, row cursor, column for covers, compact and comfortable densities.
- Quality label, status pill and source name. The source name is plain text, with no logo or color mark.
- Track lamps, and the disc being cut in a download's detail view.
- Split Download button with the format menu.
- The Sources switch with one switch per source, the type knob and the format knob.
- Side panel and phone page.
- The Download all confirmation and the sign-in dialog.
- The notice slot.
- Empty, loading and error states.
- Log viewer.

## Screens

The design has every screen this brief asked for, at phone and desktop widths, with their states. [design/README.md](design/README.md) lists them with their routes.

## Not designed yet

The design session left these out, and step 1g decided each one.

- Built: the Compilations and Appears on groups of the artist panel, and on the desktop an artist download as one row that opens into its albums. The phone shows the albums in the download panel.
- Left out, because the API has no such field: a playlist's description.
- Left out, for the owner to decide: compact and comfortable table densities, and notices when a download finishes or fails.

## Sample data

Use made-up names, for example:

| Title | Artist | Year | Source | Quality |
| --- | --- | --- | --- | --- |
| Low Tide Hymns | Northern Static | 2019 | Qobuz | Hi-Res 24/96 |
| Paper Lanterns | The Quiet Arcade | 2023 | Apple Music | Lossless, Dolby Atmos |
| Night Ferry | Mira Holt | 2021 | Qobuz | CD 16/44.1 |
| Glass Orchard (Deluxe Edition) | Vela Choir | 2024 | Apple Music | Hi-Res Lossless |
| Small Hours, Vol. 2 | Various Artists | 2018 | Qobuz | Hi-Res 24/192 |

Downloads: one running at 3 of 12 tracks, two queued, a finished one from Telegram user @tikhon, one failed with "Qobuz returned 401: token expired", one done with errors at 11 of 12 tracks.

## Export

`docs/ui/design/` has `tokens.css` with the color and font variables for both themes, `components.css` with every part, one static page per screen in `screens/`, and a README with the rules. The type scale, spacing, radii and shadows are plain values in `components.css`, and the README lists them.
