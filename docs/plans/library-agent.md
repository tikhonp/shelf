# Library agent and review inbox

Status: feature draft, 2026-11-07. Built and running in production. [agents.md](../agents.md) describes current behavior. This document records the design discussion. The owner confirmed the product choices below; implementation details remain proposals. Requested review: Claude Opus.

Review status: Claude Opus 5.5 reviewed the draft against the repository on 2026-11-07 and recommended proceeding after revision. This draft incorporates the findings below; the revised draft has not had a second review.

## Confirmed scope

- Bring your own agent. Shelf generates a prompt and connection details; it does not run or schedule the agent in the first version.
- One shared memory for the library, including learned preferences and reusable context. Fresh runs and replacement agent connections can retrieve it; memory is not tied to one account or conversation.
- First-version proposals cover tags and artwork. Other library operations may follow later.
- All library changes require user acceptance in Shelf.
- Any signed-in user can review all proposals. When account sign-in is disabled, anyone with Shelf access can review them.
- The external agent retrieves comments and decisions whenever it next runs. No immediate notifications or hosted runner are required.
- Learned preferences may guide future proposals automatically. Users can inspect, correct, or delete them; applying library changes still requires acceptance.
- The inbox supports editing proposed values and accepting individual fields.

## Idea

An optional agent helps manage the shared music library. It reads the library, proposes changes, explains its choices, and learns from review. Every change to the real library needs a user's acceptance in Shelf. Connecting an agent never gives it permission to approve or apply its own work.

Shelf supplies the connection instructions, library tools, review inbox, feedback, and persistent memory. The agent can be a fresh external session each time. Its useful context lives in Shelf's database rather than depending on one conversation or one model vendor.

Example: the agent notices inconsistent genre tags on ten albums. It reads existing tags and the library's rules, proposes exact replacements with reasons, and returns links to those proposals. The user accepts six, declines two, and asks for more specific genres on two. Only the six accepted changes write files. On its next run, the agent reads those decisions and comments, revises the remaining proposals, and records what it learned.

## Optional feature

- Off by default, independently of optional account sign-in. Proposed operator switch: `SHELF_AGENT_ENABLED=false`.
- With the feature off, Shelf has no agent navigation or usable agent endpoints. Existing library workflows keep working.
- Turning it on permits a user to connect a named agent. Each connection gets its own revocable credential, identity, and allowed operations.
- Disabling the feature blocks agent access and new review actions but preserves proposals, decisions, and memory. An already accepted library job follows the existing job lifecycle; disabling is not an undo or a promise to stop a write already in progress.
- Revocation blocks new calls. The first version uses request/response calls, without agent streams. Re-enabling the feature must not revive revoked credentials.
- Bring your own agent. Shelf generates a starter prompt and connection instructions; it does not host a model, schedule runs, or require an AI subscription itself. Hosting and scheduling can be added later without changing the review contract.

## Connect and resume

The connection screen shows a copyable starter prompt, server address, supported operations, and links to the inbox and memory. Give the secret separately, once; never put it in a URL, proposal, persistent prompt, or log. The prompt explains that library content is data, the agent can only propose writes, and only a Shelf user can accept them.

Use a documented versioned JSON HTTP API under `/api/agent/` first. An MCP adapter can expose the same tools later. Avoid making raw database access or browser automation part of the integration.

Each fresh run calls a bootstrap tool that returns:

- Library identity, protocol version, capabilities, limits, and enabled proposal kinds.
- Explicit user rules and the current summary of learned preferences.
- A list of memory documents with versions, so larger documents can be retrieved on demand.
- Outstanding work derived from proposal state: requested revisions, unanswered comments, and unacknowledged outcomes, with links to the relevant albums and changes.
- Shared learning progress, feedback not yet incorporated into memory, and a saved checkpoint identifying unfinished work.
- User corrections and deletion tombstones so old feedback cannot silently restore removed preferences.

Tools allow paginated library search and album reads, existing tag suggestions and checks, proposal creation and revision, feedback reads, and memory reads and writes. Audio streaming, source credentials, authentication settings, shell access, arbitrary SQL, and direct writes are outside the agent credential's permissions. If listening is desired later, add a separate explicit capability.

With account authentication enabled, a signed-in user creates and revokes connections; the connection remembers its creator. Account deletion revokes its connections. Password resets leave library connections valid; users revoke them separately. Agent credentials are not browser sessions and must not authenticate to ordinary `/api/library`, downloads, approval, or account routes. Reject agent bearer credentials on those routes even in account-free mode rather than silently treating them as anonymous requests.

With account authentication disabled, propose an operator CLI to issue and revoke connections. The existing open web UI remains the human review surface. This mode cannot prove that a reviewer is a particular person; recommend simple authentication when that distinction matters. Do not claim the approval boundary can resist an agent separately given ordinary web credentials or unrestricted local access.

Any signed-in user can review and accept any connection's proposals. Without account sign-in, anyone with ordinary Shelf access can review and accept; record the existing Anonymous author. These access rules do not grant approval permission to an agent credential.

### Production access

Production currently uses gateway basic auth with Shelf account authentication disabled. Giving the agent that shared gateway password would also let it reach ordinary write endpoints. The proposed deployment therefore exempts only `/api/agent/` from gateway basic auth, forwards the bearer credential to Shelf, and leaves the UI, review endpoints, and all ordinary APIs behind their existing protection. Shelf enforces the scoped token on every agent endpoint regardless of account mode. The exemption must not rewrite or proxy arbitrary paths into ordinary APIs.

Draft that narrow gateway change for the owner to commit in sh-main during implementation; this feature draft changes no infrastructure. Never include the shared gateway password in agent setup. Deployment verification must prove an agent can read/propose with only its token while that token cannot reach Save, acceptance, undo, or delete. For account-free deployments, ordinary APIs must remain protected by the gateway or an equivalent access boundary; publicly open ordinary APIs cannot provide agent-only restrictions.

## Proposals

Use **proposal** for the agent's reviewable unit. Shelf already uses **suggestion** for an individual tag value from MusicBrainz, a source, or another lookup. A proposal can cite those suggestions as evidence.

A proposal contains:

- A stable ID and review URL, creating connection, run ID, creation time, and revision number.
- Kind, affected albums/tracks, exact intended operations, and the base state they were prepared against.
- A readable title, reason, evidence and uncertainties. Confidence is an explanation aid, never permission to apply.
- A before/after preview calculated by Shelf from the operations, plus warnings, affected files and expected side effects.
- Discussion, decisions, and links to applied library changes or download records.

The agent's explanation is not the authoritative diff. Shelf validates the payload and generates the diff from current library data. Store structured allowlisted operations, not commands, scripts, filesystem paths chosen by the agent, or arbitrary patches.

The architecture can support any operation Shelf deliberately exposes. It cannot safely support an arbitrary action merely because the agent can describe it. New kinds need validation, a preview, approval semantics, and an executor. Existing decisions still apply: Shelf never renames or moves music files. Supporting those actions would need a separate decision.

First delivery: tag edits and artwork replacement/removal, using the existing library writer. Downloads, ReplayGain, bulk fixes, and release-folder deletion are outside the first version. Deletion is irreversible in Shelf today and must say so if added later; do not present every future proposal as undoable.

Artwork enters through an agent-scoped byte upload using the library's JPEG/PNG decode checks and 32 MiB upload limit, or through an existing suggestion offer that Shelf resolves and materializes. Shelf never fetches an arbitrary URL supplied by the agent. Use a library port to pin the resulting cover hash to a proposal revision before an upload or offer expires. Retention honors pins for pending reviews, accepted work, and retained diff snapshots, releasing them when those holders expire. The agents module never reads cover tables directly. Existing format limits, all-track revision requirements, and shared-folder warnings still apply; do not set `every_track` automatically.

A multi-album group is for review convenience. Each album has an exact prepared change and its own result. Explicitly distinguish independent proposals from a set that must be reviewed together. First version should avoid dependent multi-album plans that require all-or-nothing execution; the filesystem and current writer do not provide that guarantee across the library.

## Review inbox

Add an Inbox entry only when the feature is enabled. Show pending proposals first, with filters for album, kind, agent, and state. Keep reviewed proposals accessible with their outcomes. Album pages link to their pending proposals. Every proposal has a stable deep link that the agent can return to the user.

The detail view shows the authoritative before/after diff, agent explanation and sources, warnings, discussion, and current state. The user can:

- **Accept** the displayed revision. Shelf validates it and enqueues the existing library operation.
- **Edit proposed values** and inspect the updated diff before accepting.
- **Accept selected fields**, leaving other fields for later review.
- **Decline** selected fields or **Decline remaining**, optionally with a reason. Nothing in the library changes.
- **Request changes**, with a comment. The proposal waits for a new agent revision.
- **Comment** without deciding. A comment alone does not accept or decline anything.

Users can edit proposed values and accept individual fields in the first version. Store user edits as attributed immutable revisions, preserving the original agent values for feedback. Editing or selecting fields alone writes no library files. Acceptance binds to the exact revision, selected operations, and final values displayed.

Reuse the existing editor's field controls and Save validation for user edits and selected fields, carrying the proposal reference into the staged edits. Reuse the same server preparation path for inbox acceptance; do not create a second set of tag rules. Shelf validates the selected subset and recalculates its preview and warnings before acceptance. Aliases and MusicBrainz identifiers must remain consistent; M4A date combinations that share `DATE` remain refused under the existing rules. Explain invalid subsets rather than silently adding operations. Artwork is one logical operation covering its folder and embedded-art effects.

Keep unselected fields pending rather than treating them as declined. Track decisions and execution results per selected operation, with a proposal-level summary that can show partially reviewed or partially applied work. After any write, revalidate the remaining fields against the new file state. An unchanged reviewed diff remains eligible for acceptance; changed effects require a refreshed preview. Preserve every accepted subset as a separate execution record; accepting another subset must not repeat previously accepted operations.

Store per-operation decisions: `pending`, `accepted` with its durable execution reference, `declined`, or `superseded`. Keep `changes_requested` and `withdrawn` as proposal-level flags. Derive partial-review summaries from those decisions. Read queued, applying, applied, partial, failed, and undone outcomes from the linked library change through the port; do not maintain a second execution state machine. Compute staleness when preparing review. Preserve an outcome snapshot before history expires. Accepted means approved, not successfully written.

An agent can revise pending operations, but it cannot overwrite a revision the user is reading. Every edit creates an immutable revision. Revision writes include the expected parent revision and fail if another user or agent advanced the head. Review actions include the displayed revision and fail on a mismatch. A declined proposal stays declined; a follow-up links to it and explains what changed. The server fingerprints operations by target files, field, and canonical proposed values to suppress duplicates of pending or declined work. Reconsidering a declined operation requires an explicit follow-up reference and rationale, rather than a fresh unlinked copy.

## Applying an accepted proposal

1. Resolve the reviewer through the existing human access path. Agent credentials cannot call this path.
2. Bind acceptance to the exact revision, selected operations, and final user-edited values shown. Preserve both the proposing agent and the accepting user's identity; the library change's author is the accepting user.
3. Re-read affected files and recompute the exact selected diff: targets, before/after values, artwork hashes, affected file/folder scope, album membership, dependencies, side effects, warnings, and confirmation set. If it matches the preview the user accepted, bind the Save to the newly read file revisions. An unrelated field write need not make an unchanged operation stale. If any reviewed effect changed, show a refreshed diff and require renewed acceptance. The existing full-file revision check still protects the interval from acceptance to job execution; conflicts there do not authorize automatic retries against new revisions.
4. Store the immutable decision first with a unique idempotency key derived from proposal, revision, and selected operations/final values. The library port atomically gets or creates the Save and its queued job by that key, returning an existing change on retry. Use a new unique nullable column on library changes so ordinary Saves need no key. Reconcile decisions missing a change reference after a crash through the same port/key. Serialize overlapping selections so concurrent reviewers cannot approve one pending operation twice. Keep idempotency mappings while the corresponding decision can still be retried, even after ordinary history retention.
5. Run through the existing library queues, locks, verified temp-file writes, and execution-time conflict checks. Never implement an alternate tag writer inside the agent module.
6. Publish the outcome to the inbox and durable feedback feed, linking existing history and undo where supported. A queued change can still become stale or fail; acceptance is not a guarantee of application.

Existing confirmations for album splits, merges, artist renames, and shared-folder artwork must remain part of the reviewed approval. A warning that changes after approval requires renewed review. External effects such as Navidrome stars and ratings cannot always be restored by tag undo.

Accepted selections are immutable. The agent may propose a follow-up or revise unaccepted operations but cannot change queued work. Concurrent accepts and request retries execute at most once. Other pending operations are revalidated after a write and become stale only when their reviewed effects change. Cancellation and retry must distinguish "not started" from "possibly partly written"; do not replay an uncertain outcome as a new approval. Undoing an accepted selection supplies feedback but does not reopen it or authorize reapplication.

## Feedback and learning

Keep durable, ordered feedback in SQLite rather than relying on the in-process event hub. Include accepted and declined decisions, selected fields, original and user-edited values, requests for changes, comments, revisions, execution results, and linked undo/redo outcomes. Use event IDs and a cursor for replay across restarts and disconnected agent runs. The first version uses retrieval when the external agent next runs; immediate notification is outside scope.

User actions are available to the agent on its next run. Automatic correction while the agent is disconnected would require a runner, scheduler, or webhook consumer; the initial external-agent model does not promise that. Keep a library-wide learning watermark, independent of connection identity. An atomic commit checks expected memory versions and the expected old watermark, writes the memory updates and checkpoint, then advances the watermark in the same transaction. A competing run must reload rather than overwrite or double-process those events. Never advance past unprocessed feedback. Proposal work remains discoverable from current state even after its feedback has been incorporated into memory.

Learning from ordinary manual tagging remains in scope: the owner explicitly asked the agent to learn tagging and management behavior. Expose read-only existing library history, including author and undo state, through a port rather than adding general UI tracking or another manual-edit activity log. The agent can inspect retained history on fresh runs and checkpoint what it incorporated. History is bounded at 1,000 saves and can be cleared, so expose coverage gaps instead of promising a complete behavioral record. Proposal decisions and comments have their own durable feed. A manual edit or acceptance is evidence, not necessarily a lasting preference. An undo may mean a bad result or a changed mind; preserve context instead of declaring it a universal rule.

Proposed memory layers:

| Layer | Contents | Who controls it |
| --- | --- | --- |
| Explicit rules | "Use specific genres; preserve original release dates." Scope and exceptions included. | User edits; agent may propose a rule for confirmation. |
| Learned preferences | Inferences with confidence, scope, supporting event IDs, and counterexamples. | Agent writes; user can inspect, correct, or delete. |
| Working context | Album notes, research, unresolved questions, summaries, checkpoints. | Agent writes; user can inspect or clear. |

All layers live in Shelf's database and can be read by a fresh connected run. Memory edits do not change tags or files and can be stored without accepting a library proposal. The agent must not promote an inference into an explicit user rule on its own. Rules guide future proposals; even an explicit rule never grants automatic application.

Learned preferences can guide subsequent proposals immediately, without a separate confirmation step. Users can inspect their evidence, correct them, or delete them. Preserve user corrections and deletions so a later run cannot silently restore an overridden preference from old feedback; reconsideration needs new evidence and must respect explicit rules.

Use one shared library memory space for explicit rules, learned preferences, and reusable working context. Any authorized agent connection can retrieve this context, including a replacement connection starting a fresh run. Record the connection and run that wrote each document for attribution, without making that document private to them. Learning progress and resumable work are library-wide. Connection-local pagination positions are only transport conveniences and never determine whether shared feedback was learned. There are no personal preference spaces in the first version.

Memory documents have stable IDs, type, scope, body, version, author, timestamps, and evidence links. Use expected-version writes to prevent concurrent runs losing updates. Support pagination, summaries, quotas, export, deletion, and a clear/reset action. Persisted memory is not unlimited conversation history. Keep compact useful context, not hidden chain-of-thought, credentials, or raw audio.

The user can see why a preference exists and mark it wrong. User corrections override learned preferences. Agent memory remains untrusted input: it cannot expand credential scope, disable review, or modify the application's system instructions. Comments and music metadata also cannot do that.

## Fit with Shelf

Add an `agents` module owning connections, proposals and immutable revisions, discussion/decisions, feedback events, run checkpoints, and memory documents. It uses small ports to library, downloads, and authentication. It never queries another module's tables. Use new STRICT goose migrations and UTC timestamps.

The library port should prepare exact operations and previews, validate, apply with a durable idempotency key, manage cover pins, expose retained history, and expose results. Identify targets through stable file references resolved by the port, with library-relative path snapshots and file revisions; never rely on album IDs alone, since retagging can change them. Do not accept arbitrary paths from the agent. Resolve current album membership before review. Old proposal links must still open their recorded diff after albums are regrouped or deleted.

Agent reads should not start ffmpeg artwork checks by default; expose expensive analysis explicitly. Keep external suggestions opt-in per request, use the existing shared service limiters, and give background agent lookups lower priority than interactive editing. Bound each connection's catalog lookup and analysis budgets so a library-wide pass cannot monopolize the editor or source accounts.

Agent tokens are random, stored as hashes, shown once, and checked for connection revocation and permitted operations on every call. Bound page sizes, body sizes, memory storage, pending proposals, and request rates. Library read permissions expose metadata and potentially lyrics to the external agent; setup should explain the granted data scope. Do not return source secrets or full host paths.

Retain pending proposals and active memory until explicitly removed. Decide a bounded retention/export policy for reviewed proposals and feedback. Existing history trims to 1,000 saves and jobs expire; preserve enough proposal diff, outcome, and learning evidence that a removed history row does not break review or misrepresent what the agent learned. Removed artwork blobs need equivalent retention handling. Backups contain memory and may retain deleted data until they expire.

## Delivery steps

1. Finalize the technical API and data contracts using the confirmed connection, access, review, and memory choices above.
2. Add the disabled-by-default module, scoped connections, bootstrap/read tools, versioned memory, and durable feedback.
3. Add exact tag/artwork proposals, server-generated previews, inbox discussion, user edits, field selection, and decisions.
4. Connect acceptance to existing writes, authorship, confirmations, conflict checks, history and undo. Verify retries, crashes, concurrent reviewers, stale files, and revocation.
5. Generate the external-agent starter prompt and demonstrate a fresh run resuming from stored memory and responding to review.
6. Add further operation kinds and optional automation of agent runs after the review contract works.

## Acceptance examples

- With the feature off, no agent can connect and the UI has no agent entry.
- An agent can read permitted metadata, persist context, and create proposals, but cannot change a library byte or accept a proposal with its credential.
- A user accepts one exact diff; only those operations run and history identifies both proposer and reviewer.
- A user edits a proposed genre and accepts only that field. Only the reviewed genre value is written, other fields remain pending, and the next agent run sees the correction.
- An invalid subset of linked fields is refused with an explanation; the accepted payload never gains undisplayed operations.
- Decline, comment, and request changes write no library files and are replayable after a restart.
- Changed proposal revisions or changed selected effects require renewed review and acceptance.
- An unrelated field change may advance file revisions without changing the selected diff. Acceptance succeeds only after Shelf verifies the selected effects and warnings still match; a changed selected value or dependency requires renewed review.
- A crash or repeated accept does not duplicate a write; a failed write is shown as failed, not applied.
- After clearing the agent's conversation, a fresh run retrieves rules, context, outstanding proposals, and unprocessed feedback without the user repeating them.
- A user's correction to a learned preference survives later runs; no memory rule can bypass review.

## Remaining implementation choices

The product questions above are answered. Proposed technical defaults are JSON HTTP first, a narrow agent-only gateway exemption while basic auth remains, no token revocation on password reset, per-operation decisions, and reuse of existing Save preparation. Retention limits and schema details remain for implementation. The gateway exemption is a deployment proposal, not an applied infrastructure change.

## Opus review and disposition

Opus found four high-priority design gaps. The revisions above address them with a separately protected agent API, operation-level diff validation for partial acceptance, pinned validated artwork, and library-wide atomic learning progress. The review also prompted reuse of the editor/Save path, per-field decline, simpler derived state, durable idempotency, stable file targets, lookup budgets, duplicate suppression, and revision concurrency checks.

One recommendation was adjusted: Opus suggested dropping manual-tag learning as unrequested. The original request explicitly includes learning from tagging and library management, so the feature stays in scope using existing history through a read-only port, without new UI tracking.

During implementation, update auth.md to distinguish scoped agent credentials from ordinary API access, decisions.md for server-stored proposals and diff-based review validation, deployment.md for the gateway boundary, data-model.md for new records, and the glossary for Proposal and Inbox. Current behavior docs must continue to say these capabilities are not implemented until they exist.
