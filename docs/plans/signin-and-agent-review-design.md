# Sign-in and agent review redesign

Status: designed on 2026-11-09. The bugs at the end were fixed the same day, and the rest was built on 2026-11-10. The owner made the choices under Decisions. The Claude Design canvas at https://claude.ai/artifact/LRhUtun1bSeK87kTh6Q7To has the boards, with today's screens beside them. The static exports are in [ui/design/screens](../ui/design/screens), and [ui/design/README.md](../ui/design/README.md) lists them.

The review used a local build with `SHELF_AUTH_PROVIDER=simple`, `SHELF_AGENT_ENABLED=true` and the fake source. A script sent three proposals through the agent API. The [bugs](#bugs-found-on-2026-11-09) at the end can be fixed before the redesign or with it.

## Decisions

| Decision | Why |
| --- | --- |
| Account sign-in shows a switched-off console. The menubar and tabs are dark, a rack unit has the form, and a key switch pictures the passkey. On success the lamps light and the clock starts. | The owner chose the full concept. Today's black page looks like an error box. |
| On the phone, an Inbox cap in the Library title bar opens the Inbox, and the dock keeps four tabs. The desktop menubar keeps its Inbox tab. | A fifth tab squeezes the dock labels, and proposals change library tags. |
| Proposals show in two places. The Inbox is the queue and the full review. The Library editor also shows a pending proposal's values as suggestions. | The owner chose both, so the proposal for an album shows while someone edits that album. |
| Proposal review works like a bulk run. Each field has a rocker, displays count what Accept writes, the table has one row per field, and Accept sits where the Save bar does. | The Library's bulk run already uses this pattern to choose changes. |

## Screens

| Export | Canvas board | Shows |
| --- | --- | --- |
| `MS-desk-account-signin` | `Main.dc.html`, interactive | Desktop sign-in, signed out |
| `MS-phone-account-signin` | `Signin-Phone.dc.html` | Phone sign-in |
| `MS-account-signin-states` | `Signin-States.dc.html` | Checking, wrong password, passkey canceled, signed in, session ended, no accounts |
| `MS-desk-status-account` | `Account.dc.html` | Account, Passkeys and Password units on Status, with a passkey removal waiting for confirmation |
| `MS-desk-inbox` | `Inbox-Desk.dc.html`, interactive | The Inbox list and one proposal under review |
| `MS-phone-inbox` | `Inbox-Phone-List.dc.html` | The Inbox list on the phone |
| `MS-phone-inbox-review` | `Inbox-Phone.dc.html` | One proposal on the phone, with the review slot |
| `MS-desk-status-agents` | `Agents-Status.dc.html` | Agents, one connection, and Connect with a new secret |
| `MS-desk-library-proposal` | `Library-Proposal.dc.html` | The Library editor with an agent's values as suggestions |
| `MS-phone-library-inbox` | `Library-Phone.dc.html` | The phone Library with the Inbox cap |

Two desktop boards on the canvas are interactive. On the sign-in board, `tikhon` with any password signs in, and an empty password fails. On the review board, the rockers, the cable and the Accept count work.

## Account sign-in

The work changes `AccountLogin.vue` and the access check in `App.vue`. The flows in [auth.md](../auth.md) stay as they are.

### Layout

- The page has the desk and console classes. The menubar shows the brand, the five tabs at 35% opacity with `aria-disabled`, three dark lamps, and the clock reading `--:--`.
- A 780px rack unit (`.win.unit`) sits between two blank units with vent slots, centered in the free height.
- The unit's left column has the plate with a lamp, "Sign in", and "to Music Shelf" under it. The key switch picture and the "Sign in with a passkey" key follow.
- The right column has the display, the Username and Password fields, and an amber "Sign in" key. The fields are plain `.input` fields.
- On the phone, the brand, the three lamps and the clock sit above the stack. The unit is a window at the top of the stack, so the keyboard never covers Sign in. Under a divider, the passkey key sits beside a small key switch. The dock shows at 35% opacity and has `aria-hidden`.
- The Day theme works as on other screens.

### Key switch

The key switch is an inline SVG with `aria-hidden`. It draws a chrome bezel and a key bow that points at OFF (-40°) or ON (40°), with a small lamp beside ON. The "Sign in with a passkey" key is the real control. The bow turns to ON when the passkey sign-in succeeds. It turns back to OFF when the sign-in fails or the user cancels it. The turn takes 0.5s and overshoots a little.

### States

The display (`.lcd`) is the status region, so screen readers announce each change. The plate's lamp shows the same state.

| State | Display | Lamp | Other changes |
| --- | --- | --- | --- |
| Checking access on load | CHECKING ACCESS | Yellow, blinking | Replaces the unstyled "Checking account access..." line in `App.vue`. On an error the display reads NO ANSWER, and a Try again key appears. |
| Signed out | SIGNED OUT / PASSWORD OR PASSKEY | Dark | |
| Checking a password | CHECKING / the username | Yellow, blinking | Sign in looks pressed and is disabled. |
| Waiting for a passkey | WAITING FOR / YOUR PASSKEY | Yellow, blinking | |
| Wrong username or password, any 401 | WRONG USERNAME / OR PASSWORD | Red | The password field empties and takes focus. Shelf shows its own words, never the server's sentence. |
| Too many attempts, a 429 | TOO MANY TRIES / TRY AGAIN LATER | Red | The server sends no wait time, so the display shows none. |
| Passkey canceled (`NotAllowedError`) | PASSKEY CANCELED / USE THE PASSWORD | Yellow | The bow turns back to OFF. |
| A browser without passkeys | SIGNED OUT / PASSWORD | Dark | The key switch and its key are hidden, as the key is today. |
| Signed in | HELLO, the username / OPENING THE LIBRARY | Green | The power-up runs. |
| No accounts | NO ACCOUNTS / ADD ONE ON THE SERVER | Yellow | The unit has no fields. A terminal block shows `shelf manage users add <username>`, with a Reload key. |

### Power-up

After a successful sign-in, the bow turns to ON if a passkey signed in. The three menubar lamps light 260ms apart, the clock starts, and the Library tab lights. Then the app mounts. With reduced motion, the app mounts at once.

### Session ended

When a session expires inside the app, a dialog titled "Sign in again" opens over the inert app, as today. Its display reads SESSION ENDED and the number of staged edits the app kept, such as 3 EDITS KEPT. The username stays in its field, with a "Not tikhon?" link under it. Use a passkey and Sign in sit side by side. The same account resumes, and another account reloads the page, as [auth.md](../auth.md) describes.

## Account on Status

`AccountSettings.vue` becomes three rack units below Density, like the other Status parts. Each key is as wide as its label.

- **Account.** The unit has the plate, a display with the username and the number of passkeys, and a "Sign out of Shelf" key.
- **Passkeys.** Each passkey has a row with a key picture, its name, the dates it was added and last used, and Rename and Remove keys. A New passkey name field and an Add passkey key sit under the list.
- **Password.** The Current, New and New again fields share one row. A Change password key follows, with "Then you sign in again with the new one." beside it.

The separate "Confirm password for passkey changes" key goes away. The server answers 403 to adding, renaming or removing a passkey when the last proof is older than five minutes. A confirm note then opens in place. It names the action, for example "Remove iPhone?", and has a Password field, a Use a passkey key, and the action key. After the proof, Shelf sends the request again, once.

## Agent review

### Inbox list

- A grey knob chooses Pending, Reviewed, Withdrawn or All, like the Library's sort knob. It replaces the state buttons, which wrap in a narrow column.
- "Every agent" and "Tags and covers" become caps. Each opens a menu like the Library's filter menu, in place of a browser select.
- Each row shows the cover, the album, the artist and the names of the fields the proposal changes. A third line has the agent and the time. A small display shows the agent's confidence.
- A row shows its state only when the state differs from the filter, such as "changes asked".
- On the phone, the Inbox cap in the Library title bar opens the list, and a back key returns to the Library. The cap and the Library tab carry the amber lamp while proposals wait.

### One proposal

- The title bar names the album and has Open album and close caps. Only the address keeps the proposal number.
- The head has the cover, the proposal's title, and a line with the agent, the time and the revision. The folder follows in mono, and a small display shows the confidence. The sentence "That explains the proposal and never applies it" goes away.
- The agent's explanation is a note. A "Not sure:" line under it gives the uncertainties. Each piece of evidence is tape like a suggestion, with the value and the source. Tape for evidence of kind `link` opens its address. Other kinds are plain tape.
- The fields form a bank with one row per operation. A pending row has a rocker with the field's label and "All 6 tracks" or "Track 03". The value follows in a staged input with "Was ..." under it, then a Per track key. A decided row shows its decision pill in place of the rocker.
- Labels use the editor's words for every field, including extended fields such as Album artists. A label is never an operation ID.
- A patch cable joins each pair of fields that `requires` links. The bank lists a required field above the fields that need it.
- Switching a field off also switches off every field that needs it. Switching a field on also switches on every field it needs. A status line under the bank names the field that moved, for example "Release date goes with Original date, so it switched off too." The server then never refuses a selection for a missing dependency.
- Three displays count the fields that are on, the files, and the tag writes.
- The table under the displays has one row per tag key while every file has the same before and after values. Its Files column then reads "All 6". Show each file expands the table to one row per file.
- Warnings stay notes. Each has a rocker that confirms it.
- Talkback lists the discussion, then has one Reply box, an "Ask for changes" rocker and a Send key. With the rocker on, Send requests changes. With it off, Send posts a comment.
- The bar at the foot sits where the Library's Save bar does. It reads "Accept writes 3 fields to 6 files. History can undo it." and has a "Decline the rest" key and an amber "Accept 3 fields" key. Decline the rest opens a confirm slot with an optional reason.
- While the reviewer has unsaved values, the bar has Discard and an amber Save values key instead, as the "Your values aren't saved" note does today.
- After Accept, the bar shows the outcome pill (Writing, Written, Partly written, Failed), the file count, and a link to the change in History, as the Accepted section does today.
- On the phone, the review bar is a slot above the dock. It shows the field and file counts, a Decline key, and an amber "Accept 2" key. Each field's row shows the rocker with the label and the new value. Pressing the label opens the field for editing.

### Agents on Status

- The Agents unit has a display with the number of connections and of pending proposals, and Open the Inbox and Memory keys.
- Each connection has its own unit with a lamp, its name, and who added it and when. A display shows its last run and its proposals. Its permissions show as tape, with a Revoke key. A revoked connection's unit has a dark lamp and says why it was revoked.
- The Connect unit has the Name field and the permissions as rockers in two columns. An amber Connect key follows, with "No agent can accept, save or delete." beside it. The new secret appears in a note in the same unit, with Copy secret, I stored it, and Copy setup prompt keys.

| Operation | Rocker | Under it |
| --- | --- | --- |
| `library.read` | Read the library | Albums, tags, local suggestions |
| `library.lookup` | Look up | MusicBrainz, sources, LRCLIB |
| `history.read` | History | Changes and who made them |
| `covers.upload` | Upload covers | Images for proposals |
| `proposals.write` | Propose | Create, revise, comment, withdraw |
| `memory.write` | Memory | Notes kept between runs |

### Proposal values in the Library editor

Built on 2026-11-10. Ordinary Saves reconcile through the library port, using retained change records and a durable agents cursor.

- While an album has a pending proposal, the editor shows a "Proposed" header. Under it, an issue-style row has an amber lamp, "Claude at home proposes 3 fields", the field names, and a Review key that opens the proposal in the Inbox. The row replaces today's "1 proposal" link from `AlbumProposals.vue`.
- Each pending field shows the proposed value as suggestion tape, beside the other suggestions. The tape names the agent as its source and shows the agent's confidence as its percentage.
- Pressing the tape stages the value like any suggestion. Save writes it as an ordinary Save.
- When a Save writes the proposed values for every track an operation covers, Shelf marks that operation accepted, with the Save as its library change. Any other value leaves the operation pending, and the Inbox shows it as changed since proposed, as it does today.
- The phone's field page shows the same tape.

The library module reports Saves through `libraryport.SaveHistory`. The existing change event prompts reconciliation. The Suggestion glossary now includes agent proposals.

## Bugs found on 2026-11-09

Each bug can be fixed without the redesign. All 24 were fixed on 2026-11-09. Those fixes built these parts of the redesign:

- Sign-in: plain fields, Day theme, Shelf's own words for 401 and 429, a "Sign in" key, the window at the top on the phone, the switched-off menubar behind it, and the CHECKING ACCESS display. The window stays a window, not the rack unit.
- Account on Status: the three units and the confirm note.
- Inbox: the state knob, the Agent and Kind menus, list rows with cover, field names and confidence, the album in the title bar, evidence as tape, rockers for fields and warnings, the selection that switches dependencies together with a status line, the three displays, the table grouped by tag with Show each file, Talkback with one Reply box, and the review bar at the foot of the window. The page shows a plug mark and a "Goes with" line in place of the drawn cable. On the phone the review bar sticks to the foot of the proposal's window rather than a slot above the dock.
- Phone: four dock tabs, and the Inbox cap with its lamp in the Library title bar.
- Agents on Status: permission rockers and tape. Memory: Reset memory in the title bar.

The list's summaries gained `fields` and `confidence` for the rows, as [library-agent-contracts.md](library-agent-contracts.md) describes.

### Account sign-in

1. The sign-in dialog ignores the Day theme. `web/src/components/AccountLogin.vue:27` sets `class="account-dialog rs"` and never adds `day` from `useTheme()`.
2. The sign-in fields, and the fields in Account on Status, use the display colors (`background: var(--lcd)` in the `.account-form input` rule in `AccountLogin.vue`). The design keeps displays for information and uses plain `.input` fields for typing, as Apple Music sign-in does.
3. A wrong password shows the server's sentence "Incorrect credentials or credentials changed; sign in again" (`internal/platform/auth/http.go:255`) in body color. The app should map 401 and 429 to its own words and show them in the error color.
4. The window title and the main key both say "Sign in to Shelf". The key can say "Sign in".
5. On the phone, the window is centered vertically, so the keyboard covers its keys. It should sit at the top.
6. While Shelf checks access on load, `App.vue:21` shows "Checking account access..." unstyled on an empty page.
7. The backdrop is black at 80% opacity, with no desk or brand behind the window.

### Account on Status

8. Account is a plain window among rack units, and every key spans the full width, about 1,500px on a desktop.
9. The "Confirm password for passkey changes" key sits inside the password form, between Current password and New password, though it belongs to passkeys (`web/src/components/AccountSettings.vue:48`).

### Agent review

10. Album artists shows as `albumartists`. `opLabel` in `web/src/lib/inbox.ts` looks only in `mainFields` from `web/src/lib/tags.ts`, which has no extended fields.
11. "Goes together with originaldate" shows operation IDs (`web/src/components/InboxOp.vue:50`). Discussion lines show them too, for example "proposed originaldate, releasedate, albumartists" (`eventLine` in `web/src/lib/inbox.ts:93`).
12. If the reviewer unticks Original date and leaves Release date ticked, the page shows the server's "operation releasedate requires originaldate" (`InboxDetail.vue:112`). The app should keep the selection consistent, or at least name the fields in its own words.
13. The diff table repeats each field for every file. Three fields on six tracks make 18 rows, and Accept sits under all of them (`InboxDiff.vue`).
14. Each field shows twice, once as an editor in Fields and again in the table.
15. Accept, a decline reason, two Decline keys, "Changes you want" with Request changes, and Comment make one long form with three text boxes.
16. The reviewer picks fields with browser checkboxes, where the Library's bulk run uses rockers.
17. The title bar says "Proposal 1" (`InboxDetail.vue:59`).
18. Evidence prints its kind in mono, such as `link` or `note`, and a link isn't clickable (`InboxDetail.vue:86`).
19. "The agent rates its confidence 86%. That explains the proposal and never applies it." The second sentence adds nothing (`InboxDetail.vue:84`).
20. The list shows a Pending pill on every row while the filter is Pending, and shows no cover or field names (`InboxList.vue`).
21. In the 420px column the state buttons wrap All onto a second line, and Kind and Agent are browser selects.
22. On the phone, Inbox is a fifth tab, and Accept is several screens of scrolling away.

### Agents on Status and Memory

23. Permissions are browser checkboxes, and each connection lists its operations as one mono line of IDs (`AgentsPanel.vue`).
24. Reset memory sits next to New rule with the same weight (`web/src/pages/MemoryPage.vue:80-81`), though it removes rules and context.
