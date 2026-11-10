# Sign-in and agent review, second pass

Status: designed on 2026-11-10. By the end of that day the app had all of it. The owner reviewed the screens of the [first redesign](signin-and-agent-review-design.md) over 15 rounds of notes, then Fable 5.2 reviewed them, and the owner accepted its fixes. The static exports are in [ui/design/screens](../ui/design/screens), and [ui/design/README.md](../ui/design/README.md) lists them. The review board is a private artifact, https://claude.ai/artifact/3QghuteHwNFLqehu4RNE3d.

The Library screens were not reviewed. The work must not change the Library tab, apart from the Memory key in the Inbox title bars and the plate rule under [Shared styles](#shared-styles).

## Screens

| Export | Shows | Route |
| --- | --- | --- |
| `MS-desk-account-signin` | Desktop sign-in: only the unit, the Passkey slide and the key switch | any, signed out |
| `MS-phone-account-signin` | Phone sign-in, centered, with the slide in its own row | any, signed out |
| `MS-desk-account-setup` | Create the first account | any, no accounts |
| `MS-phone-account-setup` | Create the first account on the phone | any, no accounts |
| `MS-account-signin-states` | Eleven states, among them the session-ended dialog and the phone with the keyboard open | any, signed out |
| `MS-desk-status-account` | Account with the sign-out key switch, Passkeys, Password | `/status` |
| `MS-phone-status-account` | The same units in one column | `/status` |
| `MS-desk-status-agents` | Agents, connections, a revoked one, and Connect with a new prompt | `/status` |
| `MS-phone-status-agents` | The same units in one column | `/status` |
| `MS-desk-inbox` | The Inbox with the divider and a proposal under review. With `#note`, a help note is open | `/inbox/:id` |
| `MS-phone-inbox` | The Inbox list on the phone | `/inbox` |
| `MS-phone-inbox-review` | A proposal on the phone. With `#note`, the `?` cap's note is open | `/inbox/:id` |

The screens have small review-only scripts in `slide-demo.js`, `tips.js` and inline: the slide and key switch, the notes, the divider, the jack, the sign-out key and the Send label respond. The app's components replace them.

## Account sign-in

`AccountDesk.vue`, `AccountLogin.vue`, `KeySwitch.vue`, `web/src/lib/account.ts`.

Built on 2026-11-10. The slide is `PasskeySlide.vue`. The desktop key switch is 150px, as `MS-desk-account-signin` draws it. 120px is the size in the narrow units of the states board and on Status, and below 150px the legends grow so OFF and ON stay readable.

- Sign-in shows only its unit. The desktop has no menubar and the phone has no brand row, title bar or dock. The power-up lights three lamps inside the unit, then the app opens with its menubar.
- The unit has no plate, because the display repeats it. The display's first line is a dim MUSIC SHELF (`.lcd-l.dim`), then the state. The unit keeps a heading for screen readers.
- Sign in is the only key. A horizontal Passkey slide (`slide.css`) replaces the "Sign in with a passkey" key. Sliding it to ON, by a press or a 24px drag, asks for a passkey. It springs back to OFF when the passkey is canceled or fails, and stays at ON after a passkey sign-in.
- On the desktop the key switch sits at the top of the left column, at 120px, with no lamp. Pressing it does what the slide does, and its bow turns with the slide. The phone has no key switch, and the slide has its own row under the form.
- On the phone the unit is centered vertically. While the keyboard is open it moves to the top, so the keyboard never covers Sign in. That needs `visualViewport`, because the layout viewport doesn't shrink on iOS.
- Failures turn the display red (`.lcd.err`) and are alerts: wrong username or password, too many tries, no answer, can't sign in. A canceled passkey turns it amber (`.lcd.warn`). The other states keep the teal display. The display keeps the words in `account.ts`.
- A browser without passkeys shows no slide and no key switch, and the display says SIGNED OUT / PASSWORD.
- The Sign in again dialog keeps its title, since a dialog needs a name. Its display reads SESSION ENDED and the edits kept. The username stays filled in with a "Not tikhon?" link under it, the slide follows Sign in, and the paragraph goes.

### The first account

With no accounts, the display reads NO ACCOUNTS YET / CREATE THE FIRST ONE, and the unit has Username, Password and Repeat password fields and a Create account key. The new account signs in at once. The display turns red with PASSWORDS DIFFER or 8 CHARACTERS OR MORE, or USERNAME TAKEN when someone else created the first account a moment earlier.

Built on 2026-11-10. `POST /api/auth/setup` creates an account only while none exists, in the same transaction as the count, so two requests can't both succeed. See [the risk below](#found-on-2026-11-10).

The owner accepted that risk. `SHELF_AUTH_SETUP_UI=false` turns the screen off, and the UI then names `shelf manage users add` as before.

- The UI checks the length and the repeat before it sends anything. The display reads PASSWORD TOO SHORT / 8 CHARACTERS OR MORE, or PASSWORDS DIFFER / TYPE THEM AGAIN.
- When the server answers that an account exists, the display reads USERNAME TAKEN / SIGN IN INSTEAD, and the unit turns to sign-in with the username kept.
- The new account powers up with the three lamps, as a sign-in does. The unit has no Passkey slide, because the account has no passkey yet.

## Account on Status

`AccountSettings.vue`, `PartCard.vue`.

Built on 2026-11-10. Rename and Add passkey use the same confirm as Remove, beside the passkey or under the form. Rename opens the name in place with Cancel and Save, since the screens draw the name as text. A sign-out that fails turns the key back to ON.

- Sign out is the key switch from sign-in at 120px, turned to ON, with "Sign out" beside it. Pressing it turns the key to OFF and signs out, so sign-in and sign-out use the same key.
- With staged Library edits (`editor.kept`), the key stops halfway and asks "Sign out with 3 staged edits?" with Keep editing and Sign out.
- Removing a passkey asks with a Password field, Cancel, "Confirm with a passkey", and Remove. Remove stays disabled until the password field has text.
- "Then you sign in again with the new one." becomes the note of Change password.
- On the phone the three units stack in one column, with the same key switch. Rename, Remove and the password keys are 46px tall.

## Agents on Status

`AgentsPanel.vue`, `PatchJack.vue`. The target is the layout in the screens, a rack unit per connection, not today's single card.

Built on 2026-11-10. The connection list gained who revoked a connection, its last run, its proposals with fields still pending, and its active rules, for the displays. A connection revoked before then names only how it was revoked. The Agents unit lost its paragraph, since Open the Inbox and Memory name where to go. A copy that fails says so beside Copy prompt and selects the text.

- The Agents unit has a display with the connections and the pending proposals. Open the Inbox is a square lamp that glows amber while proposals wait (`.msgbtn[data-lit]`). Under the pointer only the lens brightens. Memory is a floppy disk half out of its drive that slides in under the pointer (`.fdbtn`). The Inbox title bars use the same Memory key.
- Each connection has a unit with a lamp, its name, who added it and when, a display with its last run and proposals, and its permissions as tape. A long name wraps in the plate.
- Revoke is the connection's red patch cable in a jack (`.jackbtn`). Pressing it pulls the plug half out and asks "Revoke Claude at home? Its token stops working at once." with Cancel and Revoke. Revoke pulls it out, the lamp goes dark, and the display reads REVOKED JUST NOW. A revoked connection keeps a dim unit that names who revoked it.
- Connect has the Name field and the permissions as rockers in two columns, one column on the phone. "No agent can accept, save or delete." becomes the note of Connect.
- After Connect, the prompt note shows "Prompt for <name>" with the secret inside, and the secret alone for `SHELF_AGENT_TOKEN`. The form above it locks. Copy prompt is the only amber key. Done replaces "I stored it", and asks "Nothing copied yet, and Shelf shows it only once." with Close anyway when the user copied nothing.
- The Setup prompt section, the copy without a secret, goes. [agents.md](../agents.md) mentions it and changes in the same commit.

## Inbox

`InboxList.vue`, `InboxDetail.vue`, `InboxBar.vue`, `InboxOp.vue`, `LibSplit.vue`, `LibTip.vue`.

Built on 2026-11-10. The note texts are in `web/src/lib/notes.ts`. Only the first evidence tape has a note, so the same note doesn't repeat on every tape. The list's state names have no notes, because the plan doesn't list them.

- The desktop Inbox has the Library's divider between the list and the proposal. It drags, and the left and right arrows move it. The list stays between 300 and 640px. Below 1100px the panels stack and the divider goes.
- Help notes work as in the Library editor and never show at rest. On the desktop a note opens 350ms after the pointer reaches a name, and names with a note gain a dotted underline. Notes sit on Memory, the confidence, Open album, the revision, Not sure, the evidence, the field names, the cable, Per track, Tag writes, Talkback, Ask for changes, Decline the rest and Accept. A key with a note keeps its pointer cursor and gets no underline. On the phone a `?` cap in the title bar opens the page's note in a slot above the review slot.
- Linked evidence shows an arrow after its source. Plain evidence has none.
- The bar shows only counts, "3 fields · 6 files", on the desktop too. The sentence becomes the note of Accept.
- Send's label follows the Ask for changes rocker: "Send comment" or "Ask for changes". The rocker sits right beside Send, at the right end of the row, because it sets what Send does.
- On the phone the review keeps Not sure, What Accept writes, and Talkback with Reply, Ask for changes and Send. The decline key names its count, such as "Decline 1".

## Shared styles

These go into the app's stylesheets. Only the first touches pages outside this work. They went into `web/src/styles` on 2026-11-10. The plate rule is in `shell.css`, beside the other plate rules. The app leaves out the screens' `[hidden]` rule and uses `v-if` instead.

- `components.css`: the line under a plate's title is indented 21px to start where the title text does, past the lamp. The rule is `.plate h2:has(> .led) + .sub`, so Density and Notices, which have no lamp, stay as they are. Every Status unit with a lamp shifts.
- `slide.css`: the slide, `.lcd.err`, `.lcd.warn` and `.lcd-l.dim`, with Day colors. The Day dim line has a higher opacity so it passes contrast.
- `gear.css`: the floppy, the Inbox lamp, the jack, `.gear-name`, the sign-out key switch, and a dimmed `.rocker[aria-disabled]`.
- `tips.css`: the Inbox notes and the phone note slot, on top of the Library's `.tipbox`.

## Found on 2026-11-10

These are bugs in the running app and questions to settle before building.

1. **Sign out drops staged edits without asking.** `logout` in `web/src/stores/auth.ts` reloads the page, and the edits `editor.kept` counts are lost. The design asks first. Fixed on 2026-11-10, and the sign-out key switch asks the same way.
2. **Creating the first account from the UI is open to anyone.** On a fresh install, whoever reaches the page first gets the account. Production has accounts, so only a new install is open to this. On 2026-11-10 the owner accepted the risk, with no setup code, and asked for an environment option that turns off creating the first account in the UI. Built as `SHELF_AUTH_SETUP_UI` on 2026-11-10.
3. **Helper text shows at rest.** "Accept writes 3 fields to 6 files. History can undo it." (`InboxBar.vue:30`), "No agent can accept, save or delete." (`AgentsPanel.vue:110`) and "Then you sign in again with the new one." (`AccountSettings.vue:116`). Each becomes a note. Fixed on 2026-11-10.
4. **The small key switch's legends are unreadable.** `AccountDesk.vue:138` draws it 96px wide, and OFF and ON come out under 8px. The design draws it at 120px. Fixed on 2026-11-10, and below 150px the legends grow.
5. **The phone's decline key says only "Decline"** (`InboxBar.vue:67`), which reads as declining everything beside "Accept 2". Fixed on 2026-11-10.
6. **Send does two things under one label** (`InboxDetail.vue:186`). It posts a comment or asks for changes, after the rocker. Fixed on 2026-11-10.
7. **"I stored it" drops a secret that shows only once**, and doesn't ask when the user copied nothing (`AgentsPanel.vue:73`). Connect and Copy prompt are both amber while the prompt shows. Fixed on 2026-11-10.
8. **The sign-in title bar repeats the Sign in key** on the phone (`AccountDesk.vue:111`), and no sign-in screen names Music Shelf.
9. **Render the help slot and confirm rows with `v-if`, not `hidden`.** `.win` and `.key` set `display`, which beats the `hidden` attribute, so a hidden slot still shows. The screens needed `.rs [hidden] { display: none !important; }` for this.
10. **Removing the Setup prompt section** leaves no way in the UI to see the prompt again for an existing connection. The CLI still prints it with `SHELF_PUBLIC_URL` set. A connection that lost its prompt is revoked and connected again. Done on 2026-11-10.
11. **The phone sign-in moves from the top to the center** with this design. The first redesign put it at the top for the keyboard. The new layout has to move it up while the keyboard is open, and S4 draws that state.
12. **`docs/ui/design/README.md` said the redesign wasn't built**, after it was. This export fixes it.

The app already does some of what the review asked of the screens: whole Inbox rows are links, the confidence display has an `aria-label`, Decline the rest hides while every field is on, and the phone row separates the rocker from the field.
