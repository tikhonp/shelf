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

- A menubar on the desktop and a tab bar on the phone, with Search, Downloads and Status.
- A tab shows one amber lamp while there is something to look at, never a number: Downloads while downloads run or wait, Status while a part needs attention.
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

## Status

One module per part:

- Qobuz: the subscription, whether the token works, and how to replace an expired token.
- Apple Music: the wrapper state, the storefront, Restart, a collapsible log tail, and Sign in while the wrapper isn't running.
- Navidrome: the address, whether it answers, the last scan.
- Telegram: the bot's name and how many users it answers.
- Notices when a download ends, Off, Failures or All, and on the desktop the table density.

The sign-in dialog has two steps: Apple ID and password, then six digit boxes with a 60-second countdown. It ends with "Signed in, storefront RU", the error with Try again, or "The code wasn't entered in time. Try again."
