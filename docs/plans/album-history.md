# Album History

Status: designed on 2026-11-12 and not built. The canvas is a private artifact, https://claude.ai/artifact/QQxnFV1qCfFVoKjvqnULqw. Its static exports are in [ui/design/screens](../ui/design/screens), and [ui/design/README.md](../ui/design/README.md) lists them.

History today is one list for the whole library, and the phone has none. With an album open, the owner can't see who changed it, what changed, or when. Album History shows one album's changes, on the desktop and on the phone. The global History stays as it is.

## Screens

| Export | Shows | Route |
| --- | --- | --- |
| `MS-desk-library-history-cap` | The Editor with the History cap between All fields and Delete | `/library/:id` |
| `MS-desk-library-album-history` | Album History with one Save open and the exact time in a hover note | `/library/:id` |
| `MS-desk-library-album-history-compact` | The same with Compact rows | `/library/:id` |
| `MS-desk-library-album-history-day` | The same in Day | `/library/:id` |
| `MS-phone-library-history-cap` | The phone album page with the History cap | `/library/:id` |
| `MS-phone-library-history` | The album's History on the phone | `/library/:id` |
| `MS-phone-library-history-open` | One Save open, with Undo in the slot above the tab bar | `/library/:id` |
| `MS-phone-library-history-run` | A run open, with Undo run in the slot | `/library/:id` |
| `MS-library-history-states` | No Saves, one Save, Load more, still writing, undo with conflicts, before authors, from a proposal, Partial | `/library/:id` |

## Decisions

The owner settled these on 2026-11-12.

- On the desktop, a History cap sits in the Editor's title bar between All fields and Delete. Album History takes the space of the Editor and the Tracks window, as the global History does, and the album list stays on the left. The window is titled History with the album's name beside it. The cap stays lit, and pressing it again brings the Editor back.
- On the phone, the History cap sits in the album's title bar before Delete. History is its own page with a back button. Rows carry no keys. Opening a change shows its lines and puts Undo, Redo or Undo run in the Save bar's slot above the tab bar.
- A row's title names the kind of change: "Save", "Save: cover" when only the cover changed, or the run's title, such as "Run: ReplayGain". The global History keeps album names.
- The second line leads with the author in bold and the main text color: "By Tikhon", "By Telegram @name", "By CLI", "By System", "By Anonymous" or "By Unknown".
- The time is relative, such as "2 hours ago", from `ago()` in `lib/format.ts`. On the desktop a hover note gives the full date and time. The When column is 104px.
- The pills use the app's words: Saved, Saving, Partial, Failed, Undone and Deleted. A queued change says Saving. There is no Queued pill.
- The Tags column shows the count, "Cover" when only the cover changed, or "4 · Cover" when both changed.
- A run's row says "Run on 20 albums, undone together", and its key says Undo run. A run opened from an album shows only that album's lines.
- A Save from a proposal shows "From a reviewed proposal" with a link to the Inbox, as the global History does.
- An undo or redo with conflicts shows the fields that stayed, in the global History's words.
- The last row, under a Download header, is the download that brought the release: "Downloaded from Qobuz", its author, time, format, a Done pill and an Open link.
- Row height follows the Density setting on the desktop. The phone has no density.
- Helper text never sits beside a control. Explanations go in hover notes on the desktop and the `?` cap on the phone.

## API

`GET /api/library/history` has no album filter today.

- Add `album=<id>` to `GET /api/library/history`. It returns the album's Saves and deletes, plus every run with a child change for that album. Today `saves()` reads only rows with `parent IS NULL`, so a run's album needs a join on its children. Paging with `before` and `limit` stays the same.
- Add `album=<id>` to `GET /api/library/changes/:id`, so a run returns only that album's files.
- Add `folder=<path>` to `GET /api/downloads` in the downloads module. It returns the downloads that wrote into that release folder, with source, format, author, time and ID. The UI joins the two answers, because a module never reads another module's tables.
- Every row field the screens show is already in `LibraryChange`.

## Steps

Each step is its own commit, with `make check`, `make lint`, `make web-check` and `docker build .` before it.

1. API: the three filters above, with Go tests for no changes, a run, Load more, an undone Save with conflicts, a change with no author, and a folder with no download. Update the library and downloads API docs.
2. Desktop: the History cap in `LibEditor.vue`, and an album version of `LibHistory.vue` that replaces the Editor and Tracks windows. Vitest covers the cap, the titles, the hover note, Undo run and the download row.
3. Phone: the History cap in `LibPhoneAlbum.vue`, the History page, the stacked Before and After lines, and the Undo slot. The Inbox's "Change 84 is in History on a desktop" becomes a link to the album's History.
4. Docs: update `docs/ui/design/README.md`, which says the phone has no History, `docs/decisions.md` and `docs/status.md`, and set this plan's status to built.
