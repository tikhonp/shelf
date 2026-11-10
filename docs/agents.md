# Library agents

Status: disabled by default, enabled in production. Claude contributed the design, UI, and library/platform work. The final review ran on 2026-11-10.

Shelf's optional agent connection lets an external agent read the library, propose tag and artwork changes, and keep shared memory in Shelf's database. Users review proposals in the Inbox. Agent credentials cannot accept proposals or call ordinary library write endpoints.

## Enable and connect

Set `SHELF_AGENT_ENABLED=true`. The default is `false`. This switch is independent of `SHELF_AUTH_PROVIDER`. An empty or invalid value fails configuration validation.

The module's migrations run even when disabled, so proposals and memory survive switching it off and CLI revocation remains available. Disabled means no agent or inbox endpoints, navigation, or background outcome processing. Already accepted library jobs continue through their existing lifecycle.

With Shelf sign-in enabled, open Status, then Agents, to create a named connection. Choose its permissions and copy the secret once. The secret belongs in the external agent's environment as `SHELF_AGENT_TOKEN`, separately from the generated setup prompt. Shelf stores its hash, not the secret. The browser holds a newly created secret only until it is dismissed or the user leaves the panel.

Without Shelf sign-in, create connections from the CLI:

```sh
shelf manage agents add <name>
shelf manage agents list
shelf manage agents revoke <id>
```

`add` can restrict permissions with `--ops op1,op2`. It prints the token once. Commands require an already migrated database, as account commands do. Both authentication modes allow listing and revocation in the UI. Deleting a connection's creating account revokes that connection on its next request. Password resets do not revoke library connections; revoke them separately when needed.

## Agent access

Set `SHELF_PUBLIC_URL` to the canonical public address so generated prompts and review links use the correct HTTPS origin behind a gateway. Without it, Shelf uses the direct request host and transport.

The JSON API is under `/api/agent/v1/`. Its API description lists the supported endpoints. It requires a bearer credential on every request, regardless of Shelf's account mode. A browser session cookie alone does not authenticate an agent call. Agent bearer credentials are rejected on every other API prefix, including account, review, and ordinary library routes.

Permissions cover library reads, external lookups, history reads, image uploads, proposal writes, and memory writes. A valid connection can read shared memory and proposal feedback. It cannot retrieve source credentials, run shell commands, query arbitrary SQL, download audio, or write music files directly. Metadata reads can expose tags, lyrics, file names, history, and author names. Grant a connection only the data and operations its external agent needs.

Shelf bounds request rates, page sizes, body sizes, storage, pending proposals, and daily usage. External lookups share an agent-wide concurrency gate and per-connection budgets. Agent album reads skip ffmpeg artwork checks and agent suggestions do not start fingerprint jobs.

## Production gateway

Production has no gateway basic auth. Shelf's accounts guard every ordinary API and the Inbox, and Shelf authenticates `/api/agent/` with its scoped token, so the gateway needs no rule for agents. Shelf rejects ambiguous API paths before routing, including dot segments, empty segments, and unusual escaping.

A deployment behind gateway basic auth must exempt only `/api/agent/` and check the raw request URI as well as its normalized location match. Never give an agent the gateway password: when Shelf account sign-in is off, it also opens the ordinary write API. An installation with publicly open ordinary APIs can't tell a human from an agent using them, so account-free deployments need the gateway or another access restriction.

Before relying on agents in a deployment, verify all of these:

- A valid agent token can bootstrap a run.
- A cookie without an agent token cannot bootstrap an agent run.
- An agent token cannot Save, accept, undo, or delete through ordinary endpoints.
- A traversal-shaped request such as `/api/agent/v1/../../library/albums/1/save`, literal or encoded, cannot reach an ordinary endpoint without a session.
- Disabling the feature or revoking the connection refuses subsequent agent requests.

## Review proposals

The Inbox appears only when the feature is enabled. Any signed-in user can review every connection's proposals. With account sign-in disabled, anyone with ordinary Shelf access can review them, and the author is Anonymous.

A proposal contains structured operations for one album, an explanation, evidence, uncertainties, and a before/after preview calculated by Shelf. Its link remains useful after the album is regrouped or removed because Shelf stores a diff snapshot.

Reviewers can edit proposed values, save those edits as a new proposal revision, switch individual fields on or off with rockers, and accept the fields that are on. Each proposed value has a line under it with what the files hold now, such as "Was empty". A drawn patch cable joins the fields that go together. The review bar sits at the foot of the proposal's window on the desktop and in a slot above the dock on the phone. On the phone, each field's row shows its new value, and pressing the field's name opens it for editing. Fields left off remain pending, and Decline the rest declines them, or every pending field when none is on. One Reply box sends a comment, or a request for changes with its Ask for changes rocker on. Both go into a durable feedback feed for the agent's next run. None of those actions writes music files. Acceptance does.

Some operations depend on others. The Inbox keeps a selection whole. Switching a field on switches on the fields it requires, and switching it off switches off the fields that require it. A line under the fields names what moved. Shelf still refuses a subset that breaks those dependencies or the existing library rules. An unselected accepted dependency counts only while its write is queued, writing, or done and has not been undone. Once history has trimmed its change, its last recorded outcome decides. Failed, partial, and undone dependencies must be selected again, except fully written operations accepted through an ordinary Save; only failed writes with nothing written can be retried. It never adds unselected edits silently. Artwork is one logical operation, including its folder and embedded-art effects. Existing album split, merge, artist rename, and shared-folder artwork confirmations still apply.

The Inbox ignores late responses for another proposal or selection, so they cannot replace the current drafts or preview. A new revision updates the selection to follow its requirements.

Before accepting, Shelf recomputes the exact selected diff. If its effects or warnings changed, the user must review the new preview and accept again. An unrelated tag write can advance a file revision while leaving that selected diff unchanged. In that case Shelf can bind acceptance to the newly read revision. The normal full-file revision checks still protect the queued write.

Acceptance, per-field decisions, the library change, and the write job commit together. Stable request IDs make a repeated acceptance request return its existing result. The library writer remains responsible for file verification, conflicts, and undo. Accepted means approved, not successfully written: the Inbox shows queued, successful, partial, or failed outcomes. Undo supplies feedback and never automatically reopens or reapplies a proposal.

## Proposal values in the Library

The editor shows pending proposals under Proposed. Each row names the agent and fields and links to its Inbox review. Pending tags also appear as suggestion tape on desktop and phone. A field no file holds yet, such as Album artists, gets its own row in All fields for its tape. Each tape shows the connection name and the agent's confidence. An omitted confidence says "Not rated". Shelf never combines it with other sources' confidence.

Pressing a proposal tape stages its exact values on its target tracks. The Save runs the usual checks and writes. Shelf then accepts each tag operation whose values that Save wrote on every target file. Other values, missing files and unwritten files leave it pending. Artwork still needs Inbox acceptance. Track IDs and recorded paths must both match.

The agents runner reads completed Saves through the library port at startup, after each change event, and every 5 minutes. A durable cursor and transactional decisions make replay safe after a missed event or restart. The library's history gives it the written keys, final tags, paths and author. Saves before the proposal's current revision cannot accept it.

Before Clear history or retention deletes changes, the library has the agents module catch up through the last change below the first unfinished one. The agents module stores its decisions and write outcomes, then returns how far it read. The library deletes nothing past that point. A Save that finishes during the catch-up keeps its row for the next read. The catch-up runs outside the delete transaction and gives up after 10 seconds. If it fails, Clear answers an error and retention tries again on its next run. With agents disabled, Clear and retention skip it.

A Save acceptance records the Save's author and change ID in the existing acceptance and feedback records. The Inbox shows that Save's outcome and later undo. If every file of an operation was written in a partial Save, that operation satisfies dependencies until undo, even though the Inbox still shows the Save as partial.

## Artwork

Agents can upload JPEG or PNG image bytes, up to 32 MiB, or materialize a cover offer Shelf already supplied. Shelf never fetches an arbitrary URL from an agent. Images must decode fully and have at most 40 million pixels, with neither side above 16,384 pixels. Agent uploads share the external-work gate so requests cannot decode many large images at once. Existing library file-format limits still apply.

Proposal references pin their cover hashes so a waiting review cannot lose its image to normal upload cleanup. Retained snapshots and queued work also need their images. Retention releases the pins when their holders expire. Artwork bytes stay in Shelf's existing cover store; proposal and pin records live in SQLite.

## Shared memory and fresh runs

Memory has three layers:

| Layer | Purpose | Control |
| --- | --- | --- |
| Rules | Explicit user instructions and exceptions. | Users create, edit, or confirm an agent-proposed rule. |
| Preferences | What agents infer from review and tagging behavior. | Agents can use them for future proposals immediately; users can inspect, correct, or delete them. |
| Context | Notes, research, unresolved work, and compact summaries. | Agents persist them so fresh runs can resume. |

All authorized agent connections share the library memory. A replacement connection can retrieve it. Its usefulness does not depend on an old conversation, account, or model. A memory write changes no tags or files and needs no library acceptance. No memory rule grants permission to skip review.

Each memory write names its expected version (0 for a new key). Versions come from a shared increasing counter and never repeat after deletion or reset. Learning commits update memory and the library-wide learning checkpoint together. Concurrent runs cannot silently overwrite each other's memory or advance the learning checkpoint twice. Shelf can validate cursor bounds; it cannot prove that an external agent understood every event it acknowledged.

User corrections override inferred preferences. Tombstones preserve deleted preferences so a later run cannot recreate them from old feedback. Reconsideration needs new evidence. Deleting context clears its body, scope, and evidence to release the shared memory quota; preference tombstones keep their content. Agents cannot change active user rules on their own. The Memory page supports inspection, corrections, confirmation of proposed rules, deletion, export, and reset.

The agent can inspect existing library history to learn from manual tagging, including authors and undo state. History is bounded and can be cleared; the API reports coverage gaps. It returns completed changes in ID order and waits behind unfinished changes, so an agent cannot advance past a pending Save and miss its eventual result. The trim marker also waits behind unfinished changes, even after Clear history. A proposal's change stays in the history for 10 minutes after it, or its undo or redo, ends, so a Clear can't erase an outcome before the Inbox records it. Each history file reports whether its planned diff was written, including partial Saves. Proposal decisions and comments have their own durable feed. Shelf records no general UI tracking.

At the start of a fresh run, the agent bootstraps its connection, reads shared rules and context, retrieves unprocessed feedback, and checks outstanding proposal state. It then proposes revisions or new changes and saves useful context. The setup prompt asks it to return links to every proposal it creates. Shelf does not run or schedule the agent or notify it to wake up in this version.

## Storage and limits

SQLite stores connections, runs, immutable proposal revisions, per-operation decisions, acceptances, durable events, memory, learning progress, and usage counters. Cover bytes use the existing cover store. Database backups include the agent records; old backups can retain data after its deletion in the app.

Pending proposals and active memory survive restarts and feature disablement. Reviewed proposals, feedback, and artwork snapshots have retention rules independent of the library's 1,000-save history. Retention removes memory feedback events older than 365 days once the learning cursor has passed them. Removed evidence cannot justify reconsideration; stored corrections still protect memory. The feature plan and implementation contract describe the design and the checks required before release.
