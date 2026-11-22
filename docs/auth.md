# Accounts and authors

Shelf supports optional account authentication. Set `SHELF_AUTH_PROVIDER=simple` and set `SHELF_PUBLIC_URL` to the public HTTPS origin, such as `https://shelf.music.tikhonnnnn.com`. The default provider is `none`. An empty or unknown value stops startup.

Every account shares access to the library, downloads, source accounts, and history. Users manage only their own passwords and passkeys. There are no roles, email addresses, or display names.

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

`add` and `set-password` prompt twice with terminal echo disabled. In Docker, use `docker exec -it shelf shelf manage users add Alice` or `make manage ARGS="users add Alice"`. Without a TTY, pass `--password-stdin` to read one password line from standard input (`docker exec -i shelf shelf manage users add Alice --password-stdin < password.txt`). Shelf strips one trailing `\n` or `\r\n` and rejects empty input, multi-line input, or input over 4 KiB.

Usernames are 1 to 32 ASCII letters, digits, dots, underscores, or hyphens, and are case-sensitive (`Alice` and `alice` are distinct). Passwords are 8 to 128 valid UTF-8 code points, hashed as exact bytes with salted Argon2id (64 MiB, 3 iterations, 1 lane). At most two HTTP password checks run concurrently (waiting up to 10 seconds before returning 429). Login throttling allows 5 attempts per username (tracking up to 1,024 names) then 1 every 12 seconds; passkey login allows 10 bursts then 1 per second. There is no permanent lockout.

Account commands work in either provider mode and do not revoke sessions by running migrations. Password resets, passkey resets, and account deletion revoke that account's sessions. Deleting the last account re-enables first-account setup when `SHELF_AUTH_SETUP_UI=true`.

## Web access and first-account setup

Before mounting the app shell or opening event streams, the UI calls `GET /api/auth`, returning `provider`, `account` (`id`, `username`, or null), `setup_needed`, and `setup_ui`.

When no accounts exist and `SHELF_AUTH_SETUP_UI=true` (the default), the UI shows the first-account form (`POST /api/auth/setup`). Anyone reaching a fresh install first can create the account; creation and session issuance share one write transaction so concurrent requests cannot both succeed. Set `SHELF_AUTH_SETUP_UI=false` to disable the endpoint (404) and require `shelf manage users add`.

While signed out, sign-in shows only its rack unit with a status display, username/password fields, a Sign in key, and a Passkey slide (plus a key switch on desktop). On phones, the unit centers vertically and scrolls above the keyboard when focused.

Status adds three units: Account (sign-out key switch, which asks before dropping staged Library edits), Passkeys (add, rename, remove), and Password. Password changes require the current password. Passkey changes require proof within the last 5 minutes (prompting inline for password or passkey confirmation if older). Password changes and passkey removal end all sessions and open "Sign in again".

When a session expires, Shelf closes event streams, stops playback, and opens "Sign in again" over the inert app while keeping staged edits in memory. Signing back in as the same account reconnects and refreshes data without resending actions; signing in as a different account reloads the page.

## Sessions and requests

Sessions last 30 days without sliding renewal and survive restarts. Shelf stores only the token hash and sets a host-only `__Host-shelf_session` cookie (`Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`).

Every protected request validates the session and account via an indexed query, then checks again after registering the request to prevent revocation races. Password changes, operator resets, and passkey removal increment `credential_gen` and delete sessions atomically. Open authenticated requests are canceled directly on web revocation, and a 30-second background check (in `serve` only) closes streams after CLI revocations.

Public routes are UI assets, `/healthz`, `GET /api/auth`, password/passkey login, and `POST /api/auth/setup`. All other routes, including audio, covers, logs, and events, return JSON 401 without `WWW-Authenticate`. Auth responses send `Cache-Control: no-store`.

Ordinary API routes do not accept bearer tokens. Optional [agent connections](agents.md) use scoped bearer tokens only under `/api/agent/v1/` and ignore browser sessions; bearer tokens on any other API prefix are rejected even in `none` mode. The Telegram bot uses its own allowlist.

## Passkeys

Shelf uses `go-webauthn` with discoverable credentials, user verification required, and no attestation. The relying party ID and accepted origin come strictly from `SHELF_PUBLIC_URL` (no credentials, query, fragment, or extra path). Changing the hostname requires registering new passkeys.

Pending ceremonies live in memory for 5 minutes (limit 256), bound to a browser ceremony cookie and consumed once under a mutex. Server restarts cancel unfinished ceremonies.

## Provider none

`none` mode keeps access open with no login screen or account units, ignoring session cookies and identity headers. Starting `serve` in `none` mode purges saved sessions (logging the count) while keeping accounts, passkeys, and history intact.

## Authors in existing records

Downloads and library changes store `author_id` (`user:<id>`, `telegram:<id>`, `cli`, `system`, or `anonymous`) and `author` (captured at request time). Child downloads and bulk-run children inherit the parent's author. Undo and redo record the requesting user while preserving the original save's author. Deleting an account keeps its historical names. Older records without an author show Unknown (or the stored Telegram identity from `origin_ref`); new web records in `none` mode show Anonymous.

Request logs include the author. Jobs keep `created_by` and `cancel_requested_by`. No separate activity log is created.
