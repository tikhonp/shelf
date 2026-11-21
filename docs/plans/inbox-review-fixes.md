# Inbox review fixes

Status: drafted on 2026-11-21. A new session with a clean context builds each phase, in order. A phase is done when its "Done when" list holds, `make check`, `make lint`, `make web-check` and `docker build .` pass, and `docs/agents.md` and `docs/status.md` describe the result in the same commit.

## Where this comes from

A UI and UX review of the Inbox and Proposal Detail screens on 2026-11-21 (store logic, Vue components, styles, and the Go handlers under `internal/modules/agents`). The findings fall into five phases: review safety first, then unsaved input protection, Proposal Detail UI quirks, Inbox list and backend filter accuracy, and optional accessibility/artwork polish.

## Decisions to settle if needed

Each phase names a sensible default so a fresh session can proceed without blocking unless the owner prefers another option:

- **D1 (Phase 2).** Unsaved work on navigation: confirm before discarding edits, Reply text, or a Decline reason (matching the Library editor's staged-edits guard), rather than silently keeping hidden per-proposal drafts in memory.
- **D2 (Phase 3).** Brief summary above the tabs: keep `.ib-why` visible only inside Fields/Files/Talkback *below* the sticky tab bar (or keep the tab bar at a fixed position above all panels) so switching to Brief never shifts the tab bar under the pointer.
- **D3 (Phase 4).** List `stale` marker: remove the dead `"Changed since proposed"` text from list rows unless the server computes real per-proposal staleness cheaply on list queries.

---

## Phase 1. Safe acceptance, rebase, and decline

Fixes the four highest-risk Proposal Detail state bugs so Accept, Save values, and Decline always match what the reviewer actually saw.

### Fixes

1. **Live revision can enable Accept on unreviewed writes.**
   - `setSelection` (`web/src/stores/inbox.ts:377`) only sets `reviewRequired = true` when the preview digest changes under the *same* `[revision, ops]` key (`keyOf`, `inbox.ts:327`). When an agent pushes a new revision while the proposal is open, the key changes, Accept re-enables immediately against the new digest, and identical warning text keeps its prior confirmation.
   - Compare the new preview digest against the last reviewed digest for the current selection across revisions. When the resulting file writes change, clear warning confirmations and require acknowledgment in Files before Accept enables.
2. **Rebasing a stale draft can apply an edit to a different tag.**
   - The server lets a revised pending op change its target under the same op ID (`internal/modules/agents/proposals.go:568`, `:764`). The store keeps drafts keyed only by `opId` (`inbox.ts:458`, `:485`), and `rebase()` (`inbox.ts:551`) only updates `headSeen`. An edit typed for Genre can be saved onto Mood.
   - Store the original target snapshot on each draft (`type`, `field`/`tag_key`, and original `track_id`s). Allow `rebase()` only for drafts whose op target still matches the new head; flag mismatched or removed ops as explicit conflicts that the reviewer must inspect or discard.
3. **Open Decline form can submit a shifted scope or revision.**
   - While the Decline form is open (`web/src/components/InboxBar.vue:88-94`), field rockers stay interactive and live revisions can arrive. On submit, `declineRest` (`inbox.ts:718-723`) recomputes both revision and op IDs (and sends `remaining: true` when no visible field is on). The form also uses a `<div>` instead of `<form>`, so pressing Enter does not submit.
   - Snapshot the revision and exact pending op IDs when opening Decline, display the field names being declined, wrap the inputs in a `<form>` so Enter submits, require reconfirmation if the scope changes while open, and preserve the typed reason if the server returns a conflict.
4. **Live refresh silently drops an in-progress draft.**
   - When another reviewer or agent decides or supersedes a field being edited, `take()` (`inbox.ts:322-323`) removes that draft without explanation.
   - Detect dropped drafts during `take()`, show a persistent notice naming the affected fields, and retain their draft text long enough for the reviewer to copy or dismiss it.

### Start here

- `web/src/stores/inbox.ts` (`take`, `keyOf`, `refreshPreview`, `setSelection`, `drafts`, `revisionOps`, `rebase`, `declineRest`)
- `web/src/components/InboxBar.vue` (Decline form, dirty/behind bar, review status)
- `internal/modules/agents/proposals.go` and `review.go` (revision and decline semantics)
- `web/src/stores/inbox.store.test.ts` and `web/src/components/inbox.test.ts`

### Tests

- A new revision that changes previewed writes forces `reviewRequired = true` and clears warning confirmations.
- A new revision that changes an op's field under the same op ID blocks silent rebase and surfaces a conflict instead of saving to the new field.
- Opening Decline snapshots the revision and op IDs; toggling a switch or receiving a revision while the form is open does not silently decline different fields; pressing Enter submits; a server conflict keeps the typed reason.
- Deciding or superseding an edited op via live refresh surfaces a notice naming the affected field and keeps the draft readable until dismissed.

### Done when

All four fixes and their tests pass, and `docs/agents.md` reflects any updated conflict/review rules.

---

## Phase 2. Unsaved work protection and Talkback reliability

Protects reviewer input against accidental loss during navigation, closing, or concurrent updates. Depends on D1.

### Fixes

1. **Switching proposals, closing, or navigating away silently wipes unsaved work.**
   - Changing `route.params.id` calls `inbox.open()` / `inbox.close()` -> `reset()` (`web/src/pages/InboxPage.vue:69-77`, `web/src/stores/inbox.ts:236-271`), and `:key="id"` remounts `InboxDetail`, clearing value drafts, Talkback reply text, and Decline reasons. Leaving for Library or Memory also clears Reply text. Adding an empty value row in `InboxValues.vue` does not set `dirty` (`inbox.ts:515`) because `revisionOps()` drops empty strings.
   - Track unsaved UI input (value edits including pending empty rows, non-empty Talkback reply, open Decline reason) and guard proposal switches, Close, browser Back, and route changes with a confirmation prompt (or preserve drafts per proposal ID if D1 changes).
2. **Talkback can erase new typing or silently downgrade "Ask for changes" to a comment.**
   - In `web/src/components/InboxDetail.vue:158-161` and `:286-295`, the textarea stays editable during send and `reply.value = ''` clears everything typed while the request was in flight. If the last pending field is decided mid-draft, `hasPending` becomes false, the rocker disappears, and submitting sends a plain comment instead of requesting changes.
   - Disable or snapshot the submitted text so only the sent string is cleared on success. If `asking` was enabled and `hasPending` becomes false, keep the draft and show a clear notice letting the reviewer explicitly confirm sending it as a comment.

### Start here

- `web/src/pages/InboxPage.vue` (route watchers, close handler, navigation guard)
- `web/src/components/InboxDetail.vue` (Talkback form, `send`, `reply`, `asking`)
- `web/src/components/InboxBar.vue` (Decline `reason` state)
- `web/src/stores/inbox.ts` (`reset`, `dirty`, `drafts`)
- `web/src/components/inbox.test.ts` and `web/src/stores/inbox.store.test.ts`

### Tests

- Switching proposals, clicking Close, or navigating to `/library` with unsaved value edits, Reply text, or a Decline reason prompts before discarding (and stays on the proposal if canceled).
- Typing additional text while a Talkback request is in flight does not lose the newly typed text.
- When the last pending op is decided while "Ask for changes" is toggled on, submitting does not silently post a comment without confirmation.

### Done when

Unsaved Proposal Detail input is protected across navigation and live updates, Talkback never silently changes message intent or wipes in-flight typing, and tests cover all paths.

---

## Phase 3. Proposal Detail UI and inspection polish

Fixes the visible layout jumps, inaccurate copy, broken History deep links, stale Library previews, and cramped diff inspection on the Proposal Detail screen. Depends on D2.

### Fixes

1. **Sticky tab bar jumps vertically when switching to/from Brief.**
   - `InboxDetail.vue:190` hides `.ib-why` via `v-if="tab !== 'brief'"` *above* `.ib-tabs`, shifting the tab bar up and down under the pointer when toggling Brief.
   - Move the brief summary card inside the non-Brief tab panels (or keep the header area height invariant above `.ib-tabs`) so `.ib-tabs` never jumps when switching tabs.
2. **Contradictory all-held copy and raw IDs on superseded fields.**
   - When every pending field already matches the files (`eligible.length === 0`, `hidden > 0`), `InboxDetail.vue:218` still says `"Accept marks them accepted too."` even though Accept is disabled (`inbox.ts:569`). Update the copy to match the actual available action when no fields are selectable.
   - `InboxDetail.vue:219` formats `superseded` ops with `name(o.id)`, which only searches `d.current.ops` (`InboxDetail.vue:91`) and falls back to raw op IDs. Resolve superseded op names from prior revisions or server metadata so human field names display.
3. **History links and partial-write inspection in Files.**
   - `InboxAcceptance.vue:17-19` links `"Change N in History"` to the generic History view; neither desktop `LibHistory.vue` nor phone `LibPhoneHistory.vue` selects change `N` or displays change IDs. Pass `change` in the query and scroll/select that change in History.
   - `InboxDetail.vue:126` hides stored previews for partial acceptances. Expose the stored plan for partial writes in Files labeled `"Planned for this acceptance"` so the reviewer can compare what was planned against the outcome.
4. **Library saves leave open proposals stale; failed initial load has no Retry.**
   - `web/src/stores/events.ts:58` receives completed Library change events without refreshing an open Inbox proposal, leaving `"Was…"` values and previews stale until Accept fails. Trigger a detail recheck when a completed Library change touches the open proposal's album.
   - In `InboxDetail.vue:174-175`, add a Retry action for transient detail load failures and a distinct unavailable message for 404.
5. **Long values and lookalike filenames are hard to verify.**
   - In `InboxOp.vue:127-135`, stop ellipsizing track names and `"Was…"` lines inside expanded rows so full values are readable on touch and keyboard.
   - In `InboxDiff.vue:13`, `:56-64`, `:86`, disambiguate duplicate basenames (show parent segment when needed), add an expand toggle for cells exceeding the `9em` scroll cap, and add mobile labels for the file and tag columns.

### Start here

- `web/src/components/InboxDetail.vue`
- `web/src/components/InboxOp.vue`
- `web/src/components/InboxDiff.vue`
- `web/src/components/InboxAcceptance.vue`
- `web/src/pages/LibraryPage.vue`, `web/src/components/LibHistory.vue`, `web/src/components/LibPhoneHistory.vue`
- `web/src/stores/events.ts`, `web/src/stores/inbox.ts`

### Tests

- Switching between Fields and Brief does not shift `.ib-tabs` position.
- All-held proposals show accurate copy instead of claiming Accept will mark them accepted.
- Superseded fields render human-readable field names rather than raw op IDs.
- Clicking `"Change N in History"` opens and highlights change `N`; partial acceptances show their planned diff in Files with the `"Planned for this acceptance"` label.
- A completed Library change event for the open album refreshes the open proposal; a failed detail load shows Retry (and 404 shows an unavailable state).
- Expanded `InboxOp` rows wrap long `"Was…"` values; `InboxDiff` expands long values and disambiguates duplicate basenames.

### Done when

All five Proposal Detail UI/inspection fixes and tests are in place and verified.

---

## Phase 4. Inbox list, tablet navigation, and backend filter accuracy

Fixes Inbox list state bugs, tablet stacked navigation, transient startup hiding, and the backend `kind` filter mismatch. Depends on D3.

### Fixes

1. **Backend `kind` filter matches superseded ops, and list `stale` marker is dead code.**
   - `internal/modules/agents/read.go:253` queries `agents_ops` without excluding `decision = 'superseded'`, so filtering by Covers returns proposals whose cover op was removed in a later revision. Exclude superseded ops from the `kind` predicate so filtering matches the head revision shown in the row.
   - `read.go:292` hardcodes `stale: false`, so `"Changed since proposed"` in `InboxList.vue:91` never appears. Remove the dead UI text (or compute real staleness if D3 changes).
2. **Filter changes show stale rows, SSE refresh discards pagination, and `ScrollMore` sits outside the scroller.**
   - `inbox.ts:167-198` keeps old `proposals` and `next` cursor visible while a new filter loads or after it fails. Clear/invalidate rows and cursor on filter change and add a Retry action on list error.
   - `applyHub` (`inbox.ts:752-758`) reloads only page 1 and drops already-paginated pages. Refresh while preserving loaded pages (or re-fetching up to the loaded count) and scroll position when filters haven't changed.
   - Move `<ScrollMore>` (`InboxList.vue:99`) inside the scrolling `ul.ib-rows` container so IntersectionObserver triggers only when the user actually reaches the end.
3. **Transient startup failure permanently hides the Inbox.**
   - `inbox.ts:134-145` sets `enabled = false` on any error during `detect()`, causing `InboxPage.vue:49-54` to redirect to `/library` and skipping re-detection on later visits.
   - Set `enabled = false` only on a confirmed `404`. Keep an explicit retryable error state for network/5xx failures, and make the disabled-redirect watcher `immediate: true` so deep links behave consistently.
4. **Tablet stacked layout (720–1100px) opens Proposal Detail below the list.**
   - When `.lib-main` stacks vertically (`web/src/styles/library.css:13`), selecting a row in `InboxPage.vue:81-97` renders `InboxDetail` below the list without scrolling or moving focus.
   - On stacked viewports, scroll to and focus the detail heading when a proposal opens, and restore focus to the originating list row on close (and reuse focus restoration on phones).

### Start here

- `internal/modules/agents/read.go` and its tests
- `web/src/stores/inbox.ts` (`detect`, `loadList`, `moreList`, `applyHub`)
- `web/src/components/InboxList.vue`, `web/src/components/ScrollMore.vue`
- `web/src/pages/InboxPage.vue`, `web/src/styles/library.css`
- `web/src/components/Shell.vue`

### Tests

- Backend test: a proposal whose cover op was superseded does not match `?kind=cover`.
- Store/component tests: changing filters clears stale rows/cursor; list errors offer Retry; SSE refresh preserves pagination; `detect()` only disables Inbox on 404 and retries on transient errors.
- Navigation test: opening a proposal in stacked layout focuses the detail heading and closing restores focus to the row.

### Done when

Backend filter test, frontend store/component tests, and `make check` / `make web-check` / `make lint` / `docker build .` all pass.

---

## Phase 5. Accessibility, focus, and artwork polish (Optional)

Lower-priority keyboard, visual, and artwork ergonomics. Can be done after Phases 1–4 or skipped if not needed.

### Fixes

1. **Focus management on editing and Decline controls.**
   - Focus the Decline reason input when opening the form and restore focus to the Decline trigger on Cancel (`InboxBar.vue:43`, `:88-94`).
   - Focus the newly added input after clicking `"Add value"`, and move focus to the adjacent value or Add button after removing a value or leaving out a track (`InboxValues.vue:9-21`, `InboxOp.vue:130`).
2. **Larger, uncropped artwork comparison.**
   - Clicking a 72px cover thumbnail in `InboxOp.vue:139` or `InboxDiff.vue:71-73` opens a larger, uncropped preview (`object-fit: contain`) with an accessible label naming the file and before/after state.
3. **Keyboard controls, help notes, contrast, and evidence labels.**
   - Add roving `tabindex` and arrow-key navigation to the Inbox state rotary (`InboxList.vue:68-74`) and `PickMenu.vue`, restoring focus to the menu trigger on close.
   - Make `LibTip.vue` notes reachable via keyboard focus (not hover only).
   - Add a dark companion stroke to the Day-theme focus ring on selected rows (`web/src/styles/tokens.css:42`, `parts.css:16`) so contrast passes.
   - Format number-only `change` and `event` evidence items as `Change N` and `Event N` in `InboxDetail.vue:154`.

### Start here

- `web/src/components/InboxBar.vue`, `InboxValues.vue`, `InboxOp.vue`, `InboxDiff.vue`, `InboxDetail.vue`, `InboxList.vue`
- `web/src/components/PickMenu.vue`, `LibTip.vue`, `Cover.vue`
- `web/src/styles/tokens.css`, `parts.css`

### Tests

- Focus moves into Decline on open, returns on Cancel, moves to new input on Add value, and stays inside the group on Remove value.
- Clicking a cover diff thumbnail opens the larger uncropped view and Escape closes it.
- Arrow keys navigate the state rotary and `PickMenu`; `Change N` / `Event N` evidence renders with IDs.

### Done when

All polish items and tests pass, and `docs/ui/design/README.md` or `docs/agents.md` is updated if any interaction pattern changed.
