# Library agents

Shelf's optional agent module lets external agents read the library, propose tag and artwork edits, and store shared memory in SQLite. Users review proposals in the Inbox. Agent tokens cannot accept proposals or call ordinary write routes.

## Enable and connect

Set `SHELF_AGENT_ENABLED=true` (default `false`). This setting is independent of `SHELF_AUTH_PROVIDER`. Empty or invalid values fail startup.

Migrations run even while disabled, so proposals and memory survive toggling the feature off and CLI revocation remains available. While disabled, agent and Inbox routes, navigation, and background outcome processing stay off. Already accepted library jobs finish normally.

With Shelf sign-in enabled, open Status, then Agents, to create a named connection. New connections get every permission by default; Choose opens rockers to restrict them. Shelf shows the setup prompt with the embedded secret once, alongside the raw token for `SHELF_AGENT_TOKEN`. While the prompt is open, the Connect form stays locked. If nothing was copied, Done asks for confirmation before closing. Shelf stores only the token hash and never shows the prompt again.

To replace a lost token without losing the connection's history, use New secret on Status or `shelf manage agents secret <id>`. The old token stops working at once. Each connection card on Status shows its permissions, last run, pending proposals, active rules, and a Revoke jack. Revoked connections stay visible in a dimmed state and keep their proposals in the Inbox.

Without Shelf sign-in, manage connections from the CLI (requires a migrated database):

```sh
shelf manage agents add <name> [--ops op1,op2]
shelf manage agents list
shelf manage agents revoke <id>
shelf manage agents secret <id>
```

Deleting the account that created a connection revokes that connection on its next request. Password resets do not revoke connections.

## Agent access

Set `SHELF_PUBLIC_URL` so generated prompts and review links use the public HTTPS origin.

The built-in setup prompt stays general for any self-hoster. It instructs the agent to follow Navidrome's tagging guides, active Memory rules, and user feedback, and to learn conventions from `GET /history`.

The machine API lives under `/api/agent/v1/`. Its root endpoint documents all routes, allowed Navidrome fields, and request bodies. Every request requires a bearer token (`shelf_agent_` followed by 43 characters, 55 total). Invalid or missing headers return 401 explaining what failed (missing header, wrong scheme, missing prefix, invalid character, or wrong total length) without echoing the token. Browser cookies do not authenticate agent routes, and agent bearer tokens are rejected on every other API prefix.

Permissions cover library reads, external lookups, history reads, image uploads, proposal writes, and memory writes. Valid connections can read shared memory and proposal feedback, but cannot read source credentials, run commands, query SQL, download audio, or write music files directly. Shelf bounds request rates, page sizes, body sizes, storage, pending proposals, external lookup concurrency, and daily usage. Agent album reads skip ffmpeg cover checks, and agent suggestions do not start fingerprint jobs.

## Gateway setup

When you use Shelf accounts (`SHELF_AUTH_PROVIDER=simple`) without reverse-proxy basic auth, your proxy needs no agent rule. Shelf rejects ambiguous API paths (dot segments, empty segments, unusual escaping) before routing.

If you run behind gateway basic auth, exempt only `/api/agent/` and validate the raw request URI as well as the normalized path. Never give an agent the gateway password, because it also opens ordinary write routes when account sign-in is off.

## Reviewing proposals

The Inbox appears when `SHELF_AGENT_ENABLED=true`. `detect()` hides it only on a confirmed 404 and shows a retryable error on transient failures. Any signed-in user (or Anonymous when auth is off) can review every connection's proposals.

Filtering by kind matches operations in the head revision. Changing filters clears stale rows and cursors at once, SSE refreshes preserve loaded pages, and stacked tablet or phone layouts focus the detail heading on open and restore focus to the list row on close.

Each proposal targets one album and stores structured operations, an explanation, evidence, uncertainties, and a server-calculated before/after preview snapshot.

- **Fields.** Opens by default, grouped under sticky headers with group rockers. Pressing a field opens its inline editor with full wrapped values. Save values creates a new revision. Clicking cover thumbnails opens an uncropped preview.
- **Brief.** Holds the agent's full explanation and evidence (`Change N`, `Event N`). The summary card sits below the sticky tab bar on other tabs so switching tabs never shifts the bar.
- **Files.** Shows the physical tag and artwork diff for the current selection, disambiguates duplicate basenames, and expands long cells. Completed acceptances show their stored snapshot (partial acceptances label their stored plan as "Planned for this acceptance").
- **Talkback.** Lists comments, change requests, and acceptance outcomes, with deep links to History (`Change N in History`). The Ask for changes switch controls whether Reply posts a comment or requests revisions.

### Selection, conflicts, and safety

- **Held fields.** Pending fields whose values the files already hold are hidden from counts. Accept marks them accepted when their dependencies are selected; if every pending field is already held, the UI prompts you to decline them instead.
- **Dependencies.** Switching a field on or off switches dependent fields with it and explains what moved. Shelf rejects subsets that break dependencies or library rules. Artwork is one logical operation covering folder and embedded art.
- **Live updates.** When a new revision changes the previewed writes, Accept waits until you acknowledge the updated diff in Files and reconfirm warnings. Unsaved value drafts keep their original target and block silent rebasing onto a retargeted operation. Dropped drafts stay visible to copy or dismiss.
- **Decline.** Opening Decline snapshots the revision and pending fields, submits on Enter, requires reconfirmation if scope shifts while open, and keeps your typed reason on conflict.
- **Unsaved work.** Leaving a proposal with unsaved edits, reply text, or a decline reason asks before discarding. Talkback snapshots submitted text so typing added in flight stays in the box. If the last pending field is decided while Ask for changes is on, the draft stays put instead of posting as a plain comment.
- **Atomic acceptance.** Acceptance recomputes the exact selected diff (allowing newer file revisions when every reviewed effect is identical) and commits decisions, the library change, and the write job in one SQLite transaction. Stable request IDs make retries idempotent. Accepted means approved, not written; Talkback records queued, written, partial, or failed outcomes. Only failed writes with zero files written can be retried.

## Proposal values in the Library

Pending proposals appear in the Library editor under Proposed and as suggestion tapes beside matching fields (desktop and phone), showing the connection name and agent confidence ("Not rated" if omitted, never combined with other sources). Pressing a tape stages its values for an ordinary Save.

When an ordinary Save writes the exact proposed values on every target file, the agents runner marks those tag operations accepted and links the Save. The runner reconciles completed Saves through `libraryport.SaveHistory` at startup, on `change` events, and every 5 minutes using a durable cursor. Artwork still requires Inbox acceptance.

Before Clear history or retention deletes changes, the library calls the agents module (outside the delete transaction, 10-second timeout) to catch up below the first unfinished change, and deletes only through the returned cursor. On failure or timeout, nothing is deleted.

## Artwork

Agents upload JPEG or PNG bytes (up to 32 MiB, at most 40 million pixels, neither side above 16,384 pixels) or materialize an existing Shelf cover offer. Shelf never fetches arbitrary agent URLs. Uploads share the external-work gate. Proposals pin their cover hashes in `library_cover_pins` until expired by retention.

## Shared memory and fresh runs

All connections share one library memory store with three layers:

| Layer | Purpose | Control |
| --- | --- | --- |
| Rules | Explicit user instructions and exceptions. | Users create, edit, or confirm agent-proposed rules. |
| Preferences | Inferred from review and tagging history. | Agents use immediately; users inspect, correct, or delete. |
| Context | Album notes, research, and resumable summaries. | Agents write and clear as needed. |

Every memory write checks an expected version (`0` for new keys) from a monotonic counter. Learning commits update memory and the library-wide learning checkpoint atomically. User corrections override inferred preferences, and deleted preferences leave tombstones so old feedback cannot recreate them without new evidence. Memory writes change no audio files and never bypass review.

Agents can read retained library history (`GET /history`) to learn from manual tagging. History paging waits behind unfinished changes and reports coverage gaps. Proposal changes stay in history for at least 10 minutes after finishing (or after undo/redo) so the Inbox always records outcomes.

At the start of a fresh run, an agent bootstraps via `POST /runs`, which returns its connection's change requests, unanswered comments, and write outcomes above the learning cursor. Shelf does not host, schedule, or wake agents; the setup prompt instructs the agent to loop between albums and poll every 10 minutes when idle.

## Storage and retention

SQLite stores connections, runs, immutable revisions, decisions, acceptances, durable events, memory, learning checkpoints, and daily usage counters. Cover bytes live in the existing cover store. Pending proposals and active memory survive restarts and feature disablement. Retention removes memory feedback events older than 365 days once the learning cursor has passed them.
