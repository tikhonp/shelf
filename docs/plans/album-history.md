# Album History

Status: built on main on 2026-11-12 in f1d607a, 3f6bf76 and 2b4616e, not deployed. Production still runs `v1.0.1`. [tags.md](../tags.md#the-editor) describes the result. The canvas is a private artifact, https://claude.ai/artifact/QQxnFV1qCfFVoKjvqnULqw. Its static exports are in [ui/design/screens](../ui/design/screens), and [ui/design/README.md](../ui/design/README.md) lists them.

Before this plan, History was one list for the whole library, and the phone had none. With an album open, the owner couldn't see who changed it, what changed, or when. Album History shows one album's changes, on the desktop and on the phone. The global History stays as it is.

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

`GET /api/library/history` had no album filter before this plan.

- Add `album=<id>` to `GET /api/library/history`. It returns the album's Saves and deletes, plus every run with a child change for that album. `saves()` read only rows with `parent IS NULL`, so a run's album needed a join on its children. Paging with `before` and `limit` stays the same.
- Add `album=<id>` to `GET /api/library/changes/:id`, so a run returns only that album's files.
- Add `folder=<path>` to `GET /api/downloads` in the downloads module. It returns the downloads that wrote into that release folder, with source, format, author, time and ID. The UI joins the two answers, because a module never reads another module's tables.
- Every row field the screens show is already in `LibraryChange`.

## Start here

A new session reads CLAUDE.md, this plan, and the screens it builds, then these files for each step.

1. API
   - `internal/modules/library/http.go`. `GET /library/history` goes to `m.history`.
   - `internal/modules/library/undo.go`. `saves()` is the query that the album filter changes.
   - `internal/modules/library/runs_test.go` and `undo_test.go`, for how runs and undo are tested.
   - `internal/modules/downloads/http.go`. `GET /downloads` goes to `m.list`. Also read `downloads.go` and `http_test.go`.
   - `docs/architecture.md`, where the API table lists `/api/library/history`, `/api/library/changes/{id}` and `/api/downloads`.
   - `docs/tags.md#history`, for the undo rules.
2. Desktop
   - `web/src/components/LibEditor.vue` and `LibHistory.vue`. The global History in `LibHistory.vue` already has the pills, the run note, the proposal link and the conflicts text.
   - `web/src/components/library.test.ts`.
   - Screens: `MS-desk-library-history-cap`, `MS-desk-library-album-history` and its `-compact` and `-day` versions, and the global `MS-desk-library-history` for comparison.
3. Phone
   - `web/src/components/LibPhoneAlbum.vue`, whose title bar gets the History cap.
   - `web/src/pages/LibraryPage.vue`. The phone opens the Tagging guide with `?view=guide`, and History can open the same way. The `.win slot tall` box there holds `LibSaveBar` and will hold the Undo key.
   - `web/src/components/InboxDetail.vue`, line 176, "Change … is in History on a desktop".
   - `web/src/components/inbox.test.ts`.
   - Screens: `MS-phone-library-history-cap`, `MS-phone-library-history`, `-open` and `-run`, and `MS-library-history-states`.
4. Docs
   - `docs/ui/design/README.md`, `docs/decisions.md`, `docs/status.md` and `docs/README.md`.

Open a screen in a browser to see it. Add `?theme=day` to the address for Day. The screens are drawings, and the app's real data and words win where they differ. The boards they come from are in [ui/design/boards/album-history](../ui/design/boards/album-history), and the design README says how to export them again.

## Steps

Each step is its own commit, with `make check`, `make lint`, `make web-check` and `docker build .` before it.

1. API: the three filters above, with Go tests for no changes, a run, Load more, an undone Save with conflicts, a change with no author, and a folder with no download. Update the library and downloads API docs.
2. Desktop: the History cap in `LibEditor.vue`, and an album version of `LibHistory.vue` that replaces the Editor and Tracks windows. Vitest covers the cap, the titles, the hover note, Undo run and the download row.
3. Phone: the History cap in `LibPhoneAlbum.vue`, the History page, the stacked Before and After lines, and the Undo slot. The Inbox's "Change 84 is in History on a desktop" becomes a link to the album's History.
4. Docs: update `docs/ui/design/README.md`, which says the phone has no History, `docs/decisions.md` and `docs/status.md`, and set this plan's status to built.
