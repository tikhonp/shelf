# Telegram

The bot is the phone entry point: send a link or a few words, and watch the download in one message. It uses go-telegram/bot v1.27.0 as vk2tg does, with long polling. `bot.WithHTTPClient` carries the proxy transport when `SHELF_TELEGRAM_USE_PROXY` is on.

## Who it answers

Only user IDs in `SHELF_TELEGRAM_USERS`. Anyone else gets "This bot is private." and nothing is queued.

## Links

A message with links queues one download per distinct link. Each source's `ParseLink` decides which links are its own.

For each download the bot sends a status message: the cover as a photo, and a caption with the title, the artist, the source and the quality. It edits that message while the download moves:

| State | Caption line | Buttons |
| --- | --- | --- |
| queued | `Queued, 2 ahead` | Cancel, Open in Shelf |
| running | `Downloading, 3 of 12 tracks` | Cancel, Open in Shelf |
| done | `Done: 12 tracks, FLAC 24-bit 96 kHz, 1.2 GB` | Open in Shelf |
| partial | `Done with errors: 11 of 12 tracks` | Retry, Open in Shelf |
| failed | `Failed: <error>` | Retry, Open in Shelf |
| canceled | `Canceled` | Retry |
| skipped | `Already downloaded on 1 Oct 2026` | Download again, Open in Shelf |

Open in Shelf is a URL button to `SHELF_PUBLIC_URL/downloads/<id>`. Without `SHELF_PUBLIC_URL` it is left out.

Links the bot can't use get a plain answer:

- a link no source recognises: "This link is not from Qobuz or Apple Music."
- a link from a source that is switched off: "Apple Music is not set up in Shelf."
- an Apple Music video or station: "Music videos and stations are not supported yet."

## Artists and labels

An artist or label link asks before queuing anything: "Download 34 albums by Example Artist?" with Download all, Open in Shelf and Cancel. Download all queues the artist download, which expands into one download per album, and the message turns into a summary that counts done albums.

## Search by text

A message without links searches albums on every enabled source, up to five per source. The answer is one message with a button per album: `Low Tide Hymns, Northern Static, 2019, Qobuz Hi-Res 24/96`. A tap queues that album and sends its status message. Nothing found gives `Nothing found for "<text>".`

## Commands

- `/start` and `/help` explain the three uses: links, search by text, and `/queue`.
- `/queue` lists running and queued downloads with their progress, one line each.

## Buttons

Callback data stays under Telegram's 64-byte limit:

| Data | Action |
| --- | --- |
| `c:<download id>` | Cancel |
| `r:<download id>` | Retry |
| `f:<download id>` | Download again with `force` |
| `q:<source>:<kind>:<id>` | Queue an item from search results |
| `a:<source>:<kind>:<id>` | Confirm an artist or label download |
| `x` | Dismiss a confirmation |

A button checks the user ID again before it acts.

## Sending and editing

- Progress edits happen at most every 5 seconds per message, plus at every state change.
- All sends and edits go through vk2tg's per-chat send queue, from `internal/tg/queue.go`: about one message every 3 seconds per chat with a burst of 3, 30 per second overall, and a wait after a 429 for its `retry_after`.
- The last edit belongs to the download job's `notify` step, so it happens once, even across a restart. Edits before it are best effort.
- "Message is not modified" answers are ignored.

## Texts

Plain English without decorative emoji, following the unslop rules. The texts live in one file in the module so they can be reviewed together.
