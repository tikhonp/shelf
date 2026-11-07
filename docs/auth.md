# Accounts and authors

Shelf has optional account authentication. Set `SHELF_AUTH_PROVIDER=simple` and set `SHELF_PUBLIC_URL` to the public HTTPS origin, such as `https://shelf.music.tikhonnnnn.com`. The default provider is `none`. An empty or unknown provider stops startup.

Every account has access to the same library, downloads, source accounts, history, and actions. Users can manage only their own passwords and passkeys. The CLI manages accounts. There are no roles, registration pages, email addresses, or display names.

## Account commands

Run migrations before creating the first account:

```sh
shelf manage migrate up
shelf manage users add Alice
shelf manage users list
shelf manage users set-password Alice
shelf manage users reset-passkeys Alice
shelf manage users delete Alice
```

`add` and `set-password` prompt twice with terminal echo disabled. Passwords do not go in command arguments. In Docker, use `docker exec -it shelf shelf manage users add Alice`. `make manage ARGS="users add Alice"` also provides a terminal. A command without a terminal fails instead of reading a visible password.

Usernames contain 1 to 32 ASCII letters, digits, dots, underscores, or hyphens. They are case-sensitive. `Alice` and `alice` are separate accounts. Creating an existing exact username fails without changing it.

Passwords contain 8 to 128 Unicode code points of valid UTF-8. Spaces count. There are no requirements for uppercase letters, digits, or symbols. Shelf hashes the exact password bytes without trimming or normalization. The database holds a salted Argon2id hash with 64 MiB of memory, three iterations, and one lane. At most two HTTP password checks run at once. Other checks wait up to ten seconds for a slot and stop waiting if the request ends. An overloaded queue returns 429. A username gets five attempts, then one more every twelve seconds. Limiter storage holds at most 1,024 names, including unknown names. Password limiter keys include a user prefix. Passkey login has a separate budget of ten requests at once, then one more per second. There is no permanent lockout.

Account commands work with either provider and do not revoke sessions merely by opening the database or running migrations. Password resets, passkey resets, and account deletion revoke that account's sessions. A passkey reset removes all its passkeys. Deleting the last account leaves simple mode closed until the operator creates another account.

## Web access

The web app checks `GET /api/auth` before mounting its application shell or opening event streams. This endpoint returns `provider`, `account` with `id` and `username` or null, and `setup_needed`. A server without accounts shows the CLI setup instruction.

"Sign in to Shelf" accepts a username and password, or a passkey. The Account panel on Status lets a user change their password, manage their passkeys, or sign out. Password changes require the current password. Passkey changes require proof from the last five minutes. A fresh login counts as proof. Users can confirm their password or sign in again with a passkey in the Account panel.

A passkey has a name, creation time, and last-use time. Adding and renaming a passkey keep the session. Password changes and passkey removal end all sessions, including the current one, and show the login dialog.

An expired session closes event streams and stops playback. The login dialog makes the mounted app inert and keeps staged edits in memory. Signing back in as the same account reconnects events and refreshes shared data while preserving staged edits, without resending any action. Signing in as a different account or signing out reloads the page. Passwords and session tokens never go in browser storage.

## Sessions and requests

Sessions expire 30 days after sign-in, without sliding renewal. They survive server restarts. Shelf stores only a hash of each random session token and sends the token in a host-only `__Host-shelf_session` cookie with Secure, HttpOnly, SameSite=Lax, and Path=/ attributes.

Every protected request checks the session and account with an indexed query, then checks again after registering the request to close the revocation race. Neither check writes to the database. Password changes, operator resets, and passkey removal increment a credential generation and delete sessions in one transaction. An old password check or pending passkey response cannot finish after that generation changes.

The server registers open authenticated requests. Web revocations cancel them directly. Every 30 seconds it also checks active sessions in the database, so a reset from a separate CLI process closes existing streams too. Canceled response writers refuse further writes, including media writes, and expose `Unwrap` for streaming flushes. The checker runs only with `serve`.

The UI files, `/healthz`, `GET /api/auth`, and password and passkey login endpoints are public. All other API routes require a session, including audio, covers, logs, and events. API refusals return JSON with status 401 and no `WWW-Authenticate` header. Existing cross-origin checks and body limits cover account endpoints. Auth responses use `no-store`. Protected media keeps its existing cache policy.

Ordinary API routes do not accept API tokens. Scripts calling a protected ordinary HTTP API need a valid session. Optional [agent connections](agents.md) use scoped bearer tokens only under `/api/agent/v1/`. Those routes authenticate through the agents module in either account mode and ignore browser sessions. A bearer credential on any other API prefix is rejected, even in provider none. The Telegram bot keeps its separate allowlist and calls Shelf services without a web login.

## Passkeys

Shelf uses go-webauthn v0.18.2. Passkeys are discoverable credentials and require user verification. Shelf asks for no attestation. Both synced passkeys and compatible security keys can work.

The relying party ID comes from the hostname in `SHELF_PUBLIC_URL`, and the configured HTTPS origin is the accepted origin. Request headers cannot change either. The public URL must have no credentials, query, fragment, or path other than `/`. A new hostname requires users to add new passkeys for that hostname. Local HTTP development uses provider none.

Pending passkey ceremonies live in memory for five minutes, with a limit of 256. Each browser cookie identifies one ceremony. Registration checks recent proof when it starts and binds the ceremony to the current account and session. The user can finish within the ceremony lifetime even if that proof expires while the browser prompt is open. Finishing consumes a ceremony once under a mutex. Restarting the server cancels unfinished ceremonies. Credential generation checks reject ceremonies invalidated by a reset. The protocol library verifies origin, relying party ID, challenge, user handle, signature, and user verification.

## Provider none

Provider none keeps access open, with no login screen or Account panel. It ignores session cookies and identity headers. Account credential endpoints are unavailable.

Starting `serve` in none mode deletes saved sessions and logs the number removed. Accounts, passkeys, and history remain. CLI commands and migrations never perform this purge. Old sessions cannot regain access after a later switch back to simple mode.

## Authors in existing records

Downloads and library changes store `author_id` and `author`. The ID is `user:<id>`, `telegram:<id>`, `cli`, `system`, or `anonymous`. The author is the name at the time of the request. A web account's ID never gets reused. Deleting an account does not change its existing records.

New downloads record the web account, Telegram user, or CLI. Child downloads copy the parent's author. Library saves, undo, redo, deletes, and bulk-run heads record the requesting account. Run children copy the head's author. Bob undoing Alice's save creates an undo by Bob and preserves Alice on the save.

Older records without an author show Unknown. Old Telegram downloads use the identity already stored in `origin_ref` when available. New web records in none mode use Anonymous. Disabling authentication keeps old authors visible.

The UI shows authors beside existing history rows and download details. Existing HTTP request logs include the author's ID and name. Jobs keep their `created_by` and `cancel_requested_by` fields, with `user:<id>` for an authenticated web user. Download retries and cancellations use those existing job fields and leave the creator unchanged. Run cancellation keeps its existing flag, analysis-job cancellation, and resume-job attribution. Coalesced scans gain no author history.

This feature adds no activity log, changes no retention rule, and creates no new history for actions that had none.
