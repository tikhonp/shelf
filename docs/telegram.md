# Telegram

The bot is the phone entry point: send a link or a few words, and watch the download in one message. It uses go-telegram/bot v1.27.0 as vk2tg does, with long polling. `bot.WithHTTPClient` carries the proxy transport when `SHELF_TELEGRAM_USE_PROXY` is on; with it off, the bot ignores the `HTTP_PROXY` variables too.

The bot connects when serve starts, never in `manage`. When Telegram can't be reached, the bot tries again after 5 seconds, then after twice as long each time, up to 5 minutes. A token Telegram refuses turns the bot off until the next start, and the log says so.

## Who it answers

Only user IDs in `SHELF_TELEGRAM_USERS`. Anyone else gets "This bot is private." and nothing is queued. A button checks the user ID again, so a tap from anyone else gets the same answer and does nothing.

## Links

A message with links queues one download per distinct item. The bot reads the links Telegram marked, with or without `https://`, and any other `http` or `https` link in the text or a photo's caption. Each source's `ParseLink` decides which links are its own.

For each download the bot sends a status message: the cover as a photo, and a caption with the title, the artist, the source and the quality:

```
Low Tide Hymns
Northern Static
Qobuz, Hi-Res 24/96

Downloading, 3 of 12 tracks
```

The bot looks the item up first, for the cover and the caption, and gives up after 10 seconds; the caption then shows the link until the download knows its title. When there is no cover, or Telegram can't fetch it, the status message is plain text. `origin_ref` records that, so later edits change the text and not a caption.

The bot sends the status message before it queues the download, so the download carries the message's ID from the start and the `notify` step can always reach it. The first edit, right after the queuing, adds the buttons, which need the download's ID.

The bot edits the message while the download moves:

| State | Caption line | Buttons |
| --- | --- | --- |
| queued | `Queued, 2 ahead`, or `Queued` when nothing waits before it | Cancel, Open in Shelf |
| running | `Downloading, 3 of 12 tracks`, or `Downloading` before the count is known | Cancel, Open in Shelf |
| done | `Done: 12 tracks, FLAC 24-bit 96 kHz, 1.2 GB` | Open in Shelf |
| partial | `Done with errors: 11 of 12 tracks` | Retry, Open in Shelf |
| failed | `Failed: <error>` | Retry, Open in Shelf |
| canceled | `Canceled` | Retry |
| skipped | `Already downloaded on 1 Oct 2026` | Fetch missing, Open in Shelf |

"Ahead" counts the albums, playlists and tracks of the same source that run or wait before this download. The date of a skipped download is when the item went into the library, in `SHELF_TIMEZONE`. Open in Shelf is a URL button to `SHELF_PUBLIC_URL/downloads/<id>`. Without `SHELF_PUBLIC_URL` it is left out.

Links the bot can't use get a plain answer:

- a link no source recognises: "This link is not from Qobuz or Apple Music." The names are those of the sources whose links shelf knows.
- a link from a source that is switched off: "Apple Music is not set up in Shelf."
- a link a source knows but can't download, such as an Apple Music video or station: "Music videos and stations are not supported yet." A source says so through `source.LinkRefuser`.

When queuing fails, the status message turns into "Could not queue this download: <why>". The downloads module's own errors fill in why. For any other error the message says "Shelf's log says why", and the log has the error.

## Artists and labels

An artist or label link asks before queuing anything: "Download 34 albums by Example Artist?", or "Download 34 albums from Example Label?", with Download all, Open in Shelf and Cancel. Open in Shelf goes to `SHELF_PUBLIC_URL/search?q=<the link>`, since nothing is downloaded yet. The count is each album of every section once, as the download expands it.

Download all queues the artist download, which expands into one download per album, and the confirmation turns into its status message:

| State | Caption line |
| --- | --- |
| running | `Downloading, 12 of 34 albums` |
| done | `Done: 34 albums, 12 GB`, or `Done: nothing to download` |
| partial | `Done with errors: 30 of 34 albums` |

The other states read as for an album. An album counts as done when it ended done or skipped.

Cancel turns the confirmation into "Nothing was queued." An artist without albums gets "Example Artist has no albums to download.", one the source doesn't have gets "Qobuz has no such artist.", and a lookup that fails gets "Could not read this link on Qobuz. Try again later."

## Search by text

A message without links searches albums on every enabled source, up to five per source, in parallel. Each search gets 10 seconds; a source that fails or answers later is left out. The answer is `Albums for "<text>":` with a button per album, grouped by source in the order the sources were registered: `Low Tide Hymns, Northern Static, 2019, Qobuz Hi-Res 24/96`. A tap queues that album and sends its status message; the results stay for another tap.

Nothing found gives `Nothing found for "<text>".` When no source answered, the bot says "Search is not working right now. Try again in a minute.", and when no source can search, "No source in Shelf can search right now."

## Commands

- `/start` and `/help` explain the three uses: links, search by text, and `/queue`. An unknown command and a message without text get the same help.
- `/queue` lists running downloads, then queued ones, oldest first, one line each: `Low Tide Hymns, Northern Static: Downloading, 3 of 12 tracks`. A download that doesn't know its title yet shows its link. With nothing there it answers "Nothing is downloading or waiting."

The bot sets the command menu to `/queue` and `/help` when it connects.

## Buttons

Callback data stays under Telegram's 64-byte limit:

| Data | Action | Answer |
| --- | --- | --- |
| `c:<download id>` | Cancel | "Canceling", or "This download has already ended." |
| `r:<download id>` | Retry; the message follows the download again | "Queued again", or "Only a failed, partial or canceled download can be retried." |
| `f:<download id>` | Fetch what is missing, with `force`, into the same message | "Queued" |
| `q:<source>:<kind>:<id>` | Queue an item from search results | "Queued" |
| `a:<source>:<kind>:<id>` | Confirm an artist or label download | "Queued" |
| `x` | Dismiss a confirmation | none |

A source that was switched off since the message was sent answers "Qobuz is not set up in Shelf." A second tap on Download all or Fetch missing queues nothing more. An item whose ID would not fit in 64 bytes gets no button, and the log says so.

## Sending and editing

- Progress edits happen at most every 5 seconds per message, plus at once at every state change. The editor keeps one edit per message in the send queue, and an edit reads the download's state only when its turn comes, so a message never goes back to an older state.
- All sends and edits go through vk2tg's per-chat send queue, from `internal/tg/queue.go`: about one message every 3 seconds per chat with a burst of 3, 30 per second overall, and a wait after a 429 for its `retry_after`.
- The last edit belongs to the download job's `notify` step, so it happens once, even across a restart. Edits before it are best effort, and the editor never shows a state that has ended. The last edit gives up after a minute, since the source's next download waits for the `notify` step.
- "Message is not modified" answers are ignored.
- After a restart the bot learns of running downloads from Telegram through their events and `origin_ref`, and edits their messages again.

## Status page

`GET /api/telegram` answers `{"enabled", "connected", "username", "users"}` for the Status page. `connected` is false until Telegram took the token, and for two minutes after a long poll failed.

## Texts

Plain English without decorative emoji, following the unslop rules. The texts live in `internal/modules/telegram/texts.go` so they can be reviewed together.
