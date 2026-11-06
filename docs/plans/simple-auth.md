# Optional authentication and change authors

Status: implemented locally, 2026-11-06. Claude Opus 5.5 and Codex agreed on the design after two reviews. [auth.md](../auth.md) describes the implementation. Production cutover and real-device passkey checks remain pending.

## Agreed behavior

- `SHELF_AUTH_PROVIDER=none` is the default. Shelf keeps its current access behavior and has no account controls.
- `SHELF_AUTH_PROVIDER=simple` requires an account for the web app and its API. Empty and unknown values fail configuration validation.
- The CLI creates accounts. There is no public registration, email recovery, role system, or external identity provider.
- Every account can use all shared library, download, history, and source functions. Users can manage only their own credentials in the app.
- Accounts start with a username and password. Users can add several named passkeys. Either a password or a passkey signs them in. A password remains available for recovery.
- Usernames are case-sensitive. `Alice` and `alice` are different accounts. Accounts have no display name or email address.
- Users can change their password, add or remove passkeys, and sign out.
- Sessions last 30 days and survive server restarts. Password changes, CLI password resets, CLI passkey resets, and account deletion revoke that user's sessions.
- Production uses one public HTTPS address. App authentication replaces nginx basic auth when enabled.
- The Telegram allowlist stays independent of web accounts for now.
- With authentication enabled, existing records gain an author. The UI shows that author.
- Add authors to existing history and action logs only. Do not add an activity log or start recording actions that have no record today.

## Scope assumptions

The first version uses browser cookie authentication and has no API tokens. No scripts that need HTTP API access have been identified. The Telegram bot keeps its own allowlist and does not need a web session.

## Existing code and records

`internal/platform/platform.go` builds the Echo app and mounts every module under `/api`. It also serves the embedded Vue app. `internal/platform/httpx` already checks cross-origin writes, limits bodies, and sends security headers. Authentication must preserve those checks.

`cmd/shelf/main.go` builds modules for both `serve` and `manage`. Account commands must work without a running server and must not need music-service credentials or network calls.

`library_changes` already records saves, undo, redo, deletes, and the parent records for bulk runs. It has no author. `downloads` has `origin` and `origin_ref`, including Telegram identity where available. Jobs have a `created_by` string, which is not enough for a durable author record by itself.

`web/src/main.ts` mounts the app immediately. `App.vue` always renders the application shell. Add an authentication check before protected stores fetch data or open event streams.

## Configuration and access

Keep the first version to two settings for authentication: the new `SHELF_AUTH_PROVIDER` and the existing `SHELF_PUBLIC_URL`.

In simple mode, require `SHELF_PUBLIC_URL` to identify the canonical HTTPS origin. Derive the WebAuthn relying party ID from its hostname. Do not derive either value from request headers. Reject invalid URLs, user information, query strings, and fragments. Tests can use an explicit HTTPS test origin. Extra LAN addresses and plain HTTP development are outside the agreed deployment.

Mount authentication routes separately from protected module routes. Permit unauthenticated access only to the UI files, `/healthz`, the authentication status endpoint, and password and passkey sign-in endpoints. Protect all other API routes, including audio, covers, logs, event streams, and `/api/version`. API refusals return JSON with status 401. The Vue app handles navigation to sign-in.

In none mode, the authentication status endpoint reports that accounts are disabled. Credential endpoints are unavailable. Ignore session cookies and do not infer a user from nginx headers. Starting serve in none mode clears saved sessions after migrations. Never clear sessions from platform.Open, AfterMigrate, or manage commands. Pending authentication challenges disappear when the server restarts. Keep accounts, passkeys, and recorded authors so a later switch back does not erase them.

Simple mode with zero users must remain closed. Show a setup message directing the operator to the CLI. There is no first-visitor setup flow and no fallback to none.

## Accounts and CLI

Store and look up usernames with case-sensitive equality. Apply the same rules to CLI commands and web sign-in. Do not lowercase or case-fold usernames, or use a case-insensitive unique index. Use the username wherever the UI or a passkey needs an account name. A passkey's user-chosen name labels the credential, not the account.

Proposed commands follow the existing `manage` entry point:

```text
shelf manage users add <username>
shelf manage users list
shelf manage users delete <username>
shelf manage users set-password <username>
shelf manage users reset-passkeys <username>
```

`add` and `set-password` prompt for a password twice with terminal echo disabled. Never accept a password as a positional argument or print it. `list` shows account identifiers and creation times, never password hashes or credential material.

Commands work in either provider mode so the operator can create an account before enabling simple mode. Account deletion removes its credentials and sessions but preserves authorship in old records. Deleting the last account leaves simple mode closed until the operator adds another account.

Passwords contain 15 to 128 Unicode code points of valid UTF-8, with spaces allowed and no character-class rules. Do not trim or normalize a password. Store an Argon2id hash with a random salt and encoded parameters. Start with 64 MiB of memory, three iterations, and one lane. Measure verification on the production hardware before settling those values. Bound concurrent password checks so repeated requests cannot exhaust memory.

## Sessions and credential changes

Store accounts, sessions, and passkey credentials in the existing SQLite database. Keep five-minute WebAuthn ceremonies in a bounded in-memory map and consume them under a mutex. A restart cancels pending ceremonies. Use new STRICT migrations and UTC timestamps. Give each account a stable internal ID and an opaque WebAuthn user handle independent of its username.

Issue a random 32-byte session token after successful sign-in. Store only its hash. Put the token in a host-only `__Host-shelf_session` cookie with Secure, HttpOnly, SameSite=Lax, and Path=/ attributes. Do not store authentication tokens in browser storage. Use a fixed 30-day expiry rather than extending it on every request.

Validate the session and account on every protected request. Logout revokes the current session and clears its cookie. Password changes and operator resets revoke all sessions in the same transaction as the credential change. Close existing event streams and check authorization during long-lived responses so revocation does not leave an open stream reading indefinitely. Bytes already delivered cannot be recalled.

Credential changes need recent proof of a password or passkey, within five minutes. Require the current password to replace it in the web UI. A successful password change returns the user to sign-in. Adding, renaming, or removing a passkey can use recent password or passkey proof. Removing a passkey revokes all sessions, including the current one, and returns the user to sign-in.

Use generic sign-in errors for an unknown username and a wrong password. Perform a dummy password check for unknown users. Rate-limit sign-in and reauthentication by exact username, including unknown names, with bounded storage and a global concurrency bound. Do not add proxy configuration or trust forwarded client IP headers. There is no permanent account lockout.

Apply the existing cross-origin protection to sign-in, sign-out, and passkey endpoints too. Authentication responses use `Cache-Control: no-store`. Preserve existing private cache headers for covers and other protected resources. API 401 responses never send WWW-Authenticate. Logs and history must never contain passwords, cookie tokens, WebAuthn challenges, source secrets, or Apple ID codes.

## Passkeys

Use `github.com/go-webauthn/webauthn` for protocol verification. Pin a supported release during implementation and review its integration requirements. Do not implement signature verification directly.

The sign-in page has a separate "Sign in with a passkey" button. Use discoverable credentials so this flow does not require a username. Require user verification during registration and sign-in. Request no attestation and support both synced passkeys and security keys that meet these requirements.

Bind each registration challenge to the signed-in account, its session, and the operation. Bind each sign-in challenge to a short-lived browser ceremony cookie. Store the initiating account and credential generation for account-bound ceremonies. Challenges expire after five minutes and can complete only once, including under concurrent requests. Validate the configured origin, relying party ID, challenge, credential ownership, and user verification through the library. Persist the credential data the library needs, including counters and backup flags. Handle synced credentials according to the library's counter rules.

The account page lists passkey names, creation times, and last-use times. Users can add, rename, and remove only their own passkeys. Canceling the browser prompt leaves the password form available. Changing the public hostname can require users to register new passkeys, so deployment docs must explain that before rollout.

## Change authors

Use a small shared author value with an identifier and a name captured at the time of the action. The identifier encodes the kind and stable identity. A null identifier means the author is unknown. The name is the username for a web account, the Telegram user's name when available, or CLI, System, Anonymous, or Unknown. Keep the Telegram user ID even if no name is available. Modules store this value in their own records and do not query account tables. Preserve the captured name after account deletion. Reusing a username must not merge two people's history.

Resolve web actors from the validated session, never from a request body or an author header. Telegram actors come from the bot's accepted update. CLI and scheduled work supply their own actors. In none mode, new web actions store author_id anonymous and author Anonymous. Old rows have NULL author_id and read as Unknown. Keep older attribution visible after disabling authentication.

Capture the author on the durable download or library change before enqueueing work. Store author_id and author columns, using user:<id>, telegram:<id>, cli, or system as identifiers. Jobs keep their existing created_by strings with user:<id> replacing web for authenticated requests. Child downloads and bulk-run children inherit the initiating actor. Automatic continuation and retries after restart keep the existing row and its author. Coalesced follow-up scans have no single author and gain no new history. Independent scheduled maintenance uses System where it already records an action.

An explicit cancel or retry updates only existing jobs.created_by or jobs.cancel_requested_by attribution. Run cancellation keeps its existing flag and records the requester through the existing analysis-job cancellation and resume-job attribution. Undo and redo each have a library_changes row with the requesting author. Preserve the original request's author. For example, Alice's save remains Alice's save after Bob requests an undo, and the undo records Bob. A download's creator must not change when someone else cancels it. Do not create a separate history just to record these actors.

Add author data to library history and detail responses, downloads, bulk runs, and their event payloads. Show "By <name>" beside the existing time or origin in history and details, on phones too. Show authors as text, without links to an account directory. Attribution does not restrict who can cancel or undo shared work.

Existing records default to Unknown. Use existing Telegram origin_ref information when reading old downloads, without a backfill migration. Do not guess the author of old web records from the library owner or gateway account.

Keep the existing history coverage, retention, and clearing behavior. Album deletion already has a history record and gains an author there. Actions without an existing history or action log do not gain one in this feature.

## Implementation steps

1. Inventory existing history records and action logs. Map their HTTP handlers, CLI commands, bot actions, and queued work to the actor that initiated them.
2. Add configuration validation, account migrations, password handling, and CLI account commands. Keep network access out of initialization.
3. Add sessions, explicit public-route handling, request authentication, and revocation for long-lived responses. Put shared identity types and authentication services under the platform, with small interfaces for consumers.
4. Add the sign-in page, initial authentication check, account page, and sign-out. Pause protected requests until the check finishes. On a later 401, close event streams, stop playback, and show an inert authentication overlay over the mounted app to keep staged edits. The same account can resume without automatic resubmission. A different account causes a full navigation. Sign-out also causes a full navigation. EventSource and audio errors check GET /api/auth because those APIs do not expose the HTTP status.
5. Add passkey enrollment, sign-in, and management, with bounded, single-use in-memory challenges.
6. Add author migrations and pass actors through web, CLI, bot, and queued work. Update existing history, action logs, responses, and UI text without adding a new activity log.
7. Run backend and frontend checks, test real passkeys on the public HTTPS deployment, and update current-behavior docs when the feature ships.

## Verification

- Confirm none mode preserves existing routes and behavior, and invalid provider values fail startup.
- Confirm simple mode with no users stays closed. Verify the CLI works without the server and without remote source access.
- Verify that `Alice` and `alice` can coexist and that sign-in and every account command select the exact username. Confirm duplicate exact usernames fail.
- Verify password handling, generic sign-in errors, throttling, session expiry, restart persistence, logout, account deletion, and concurrent credential resets.
- Check unauthenticated requests against all module routes, media, logs, and event streams. Test cross-origin writes, cookie flags, spoofed identity headers, and untrusted forwarding headers.
- Test passkey challenge expiry and replay, concurrent finishes, wrong origins, wrong relying party IDs, unknown credentials, absent user verification, and enrollment into another account.
- Verify author persistence through job restarts, child work, account deletion, and switching providers. Test separate creators and actors for cancel, retry, undo, and redo.
- Confirm authors use existing history and action logs, with no new activity history or changes to retention and clearing.
- Verify UI sign-in routing, password-manager fields, passkey cancellation, account controls, 401 handling, phone layouts, and author text.
- Test upgrade from the current database with all history intact. Review migration rollback constraints before deployment.
- Run `make check`, `make lint`, `make web-check`, and `docker build .` for the implementation. Extend the manual checklist for actual passkeys and the gateway cutover.

## Rollout and documentation

Ship with none as the default. Back up the database, deploy and migrate in none mode, create an account with the CLI, set the canonical HTTPS URL and simple provider, and verify sign-in through the gateway before removing nginx basic auth. Verify that logged-out media and event requests receive 401. Register and test a passkey before considering the cutover complete.

Update the Account and Author glossary entries in `CLAUDE.md`, distinguish account sign-in from Apple ID sign-in, and update `.env.example`, the CLI help, architecture, data model, deployment, decisions, tags and history docs, manual checklist, and UI design notes in the implementation. Replace the current gateway-only decision only when the feature ships. Draft gateway repository changes for the owner to commit.

## Technical references

- [OWASP password storage guidance](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html) supports salted Argon2id password storage. The proposed production parameters still need measurement.
- [OWASP session management guidance](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html) describes cookie protections, expiry, and session invalidation.
- [WebAuthn specification](https://www.w3.org/TR/webauthn-3/) defines relying party and origin checks, challenges, discoverable credentials, and user verification.
- [go-webauthn](https://github.com/go-webauthn/webauthn) supplies the Go protocol implementation proposed here.

## Design review agreement

Claude Opus 5.5 reviewed this plan on 2026-11-06. Both reviewers agreed to implementation after the corrections below. The implementation uses these additional rules:

- Accounts have an AUTOINCREMENT ID and credential_gen. Sessions and account-bound ceremonies capture the generation. Password and passkey finishes check it again inside the write transaction. Resets, password changes, and passkey removal increment it and delete sessions atomically. This prevents an old password check or pending passkey ceremony from creating access after a reset.
- Authenticated requests register a cancel function and a response writer that rejects writes after revocation and exposes Unwrap for ResponseController. A server ticker checks active sessions in the database at most every 30 seconds for requests already running, including revocations made by the CLI. The ticker starts in Serve, never during initialization. Web revocations also cancel matching requests directly.
- Account commands require existing migrations and report how to run migrate up if tables are missing. Inject the CLI password reader for tests. Noninteractive terminals fail with a clear instruction to use a TTY.
- Usernames match `^[A-Za-z0-9._-]{1,32}$` with case-sensitive equality. Passwords retain their exact bytes without normalization, as proposed.
- Session lookup uses an indexed join, without updating the row on each request. A second check after request registration closes the revocation race. Sign-in removes expired sessions. Recent proof lives on the session and starts at sign-in.
- GET /api/auth returns provider, account with id and username or null, and setup_needed. Use account/auth prefixes in code and "Sign in to Shelf" in text. Do not reuse Apple Music SignIn.vue.
- Keep users reset-passkeys and Unknown for old records because the owner accepted them. Keep passkey rename within the account page. These do not need a separate history.
- Keep the agreed public HTTPS deployment. Tests use HTTPS origins. Local HTTP development continues to use none mode.

## Implementation review follow-up

Claude Opus 5.5 reviewed the final implementation and found no confirmed authentication bypass. The follow-up fixes make password checks wait up to ten seconds for one of two Argon2 slots, separate passkey and username budgets, refresh data after the same account returns, share Telegram author fallback between API responses and events, reload the document when switching accounts, and check registration proof at the start of the ceremony. Regression tests cover these cases and exercise authentication through App.HTTP. Real-device and production checks remain in the manual checklist.
