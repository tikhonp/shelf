# Design brief: Music Shelf, phase 1

This brief is the input for a Claude Design session. The exported screens, `tokens.css` and a short rules README go to `docs/ui/design/`, and step 1g builds the Vue app from them.

## The product

Music Shelf is a self-hosted web app for one person who grows a personal music library. The library is served to phones and players by Navidrome; Music Shelf decides what goes into it. In phase 1 it finds albums, tracks, artists and playlists on two streaming catalogs, Qobuz and Apple Music, downloads them into the library, and shows every download live. Later phases make a table of the whole library the home screen, with editing, so the tables designed now set the pattern for that one.

- Users: the owner, sometimes a family member. Signed in by the server before the app loads; the app has no login screen.
- Devices: mostly a desktop browser, sometimes a phone. Both need to work well.
- Themes: light and dark, following the system, with a manual switch.
- Language: English.
- Name: Music Shelf in the header and the page title; Shelf where space is short.

## Look and feel

- Dense and calm, table-first, like a well-kept spreadsheet of records. Many rows fit on a screen.
- Album covers supply most of the color. The chrome stays neutral, with one accent color for actions and focus.
- Clear states at a glance: queued, running with progress, done, done with errors, failed, canceled, skipped.
- Keyboard-friendly tables: a visible row cursor, arrow keys, Enter to open, focus rings that show in both themes.
- Text contrast at WCAG AA or better in both themes.
- Fonts must be self-hostable files, because the app's content security policy loads nothing from other origins.

## Shell

- Desktop: a left sidebar with Search, Downloads and Status. Library will join as the first item in a later phase, so leave room above Search.
- Phone: a bottom tab bar with the same items.
- The Downloads item shows the number of running downloads as a small count.
- Toasts in a corner for "Queued", "Finished" and "Failed", each with a link to the download.

## Search

The main screen of phase 1.

- One search field at the top. Typing text searches. Pasting a Qobuz or Apple Music link shows the single item it points to, as a card with cover, title, artist, quality and a Download button.
- A source switch: Qobuz, Apple Music. Each shows a small health dot. A source that is down shows why in place of results, for example "Qobuz token expired" or "Apple Music: not signed in", with a link to Status.
- A type switch: Albums, Tracks, Artists, Playlists. Switching keeps the query.
- Results as a table:
  - Album rows: cover thumbnail, title with version in lighter text, an explicit marker, artist, year, track count, duration, quality badge, label, download status, a Download button with a format menu, and an icon link to the album on the source's site.
  - Track rows: cover, title, artist, album, duration, quality, status, Download.
  - Artist rows: round picture and name. Opening one shows the discography.
  - Playlist rows: cover, name, curator, track count, Download.
- "Load more" at the end of the list.
- A row opens a side panel on desktop or a full-screen sheet on a phone:
  - Album panel: large cover, title, artist, year, label, release date, genre, UPC, quality, the tracklist with number, title, duration, per-track quality and a per-track Download, and a Download album button with the format menu.
  - Artist panel: picture and name, then the discography in groups: Albums, EPs and singles, Compilations, Appears on. Each album has its own Download. "Download all N albums" sits at the top behind a confirmation dialog.
  - Playlist panel: cover, name, curator, description, the track list, Download playlist.
- Download status on rows and panels: nothing, In library, Queued, Downloading with a small progress ring, Failed.
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
- Running downloads: cover, title, artist, source marker, format, a progress bar with "3 of 12 tracks", the current track's name, elapsed time, Cancel.
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
- Quality badge, status pill, and source marker. The marker shows Qobuz and Apple Music as neutral labels with a small color dot, not their logos.
- Progress row and progress ring.
- Split Download button with the format menu.
- Source switch and type switch.
- Side panel and phone sheet.
- Confirmation dialog and the sign-in dialog.
- Toasts.
- Empty, loading and error states.
- Log viewer.

## Screens to deliver

1. The shell with Search, desktop, light and dark.
2. Search with album results, and the same with the album panel open.
3. The artist panel with the discography and the "Download all" confirmation.
4. A pasted link resolved to one album.
5. Search states: empty, loading, no results, source error.
6. Downloads with running, queued and history, and with the download panel open.
7. Status, and the sign-in dialog in both steps.
8. Phone: Search with results, an album sheet, and Downloads.

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

Put into `docs/ui/design/`:

- one HTML file per screen, named like `screens/MS-Search.html`;
- `tokens.css` with the colors for both themes, type scale, spacing, radii and shadows as CSS variables;
- `README.md` with the rules a developer needs: spacing, how tables behave, where the accent color goes, and the state colors.
