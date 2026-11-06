-- +goose Up
CREATE TABLE accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE COLLATE BINARY,
    password_hash TEXT NOT NULL,
    user_handle BLOB NOT NULL UNIQUE,
    credential_gen INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL
) STRICT;
CREATE TABLE account_sessions (
    token_hash TEXT PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    credential_gen INTEGER NOT NULL,
    verified_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
) STRICT;
CREATE INDEX account_sessions_account ON account_sessions(account_id);
CREATE TABLE account_passkeys (
    id TEXT PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    credential TEXT NOT NULL,
    created_at TEXT NOT NULL,
    last_used_at TEXT
) STRICT;
CREATE INDEX account_passkeys_account ON account_passkeys(account_id);

-- +goose Down
DROP TABLE account_passkeys;
DROP TABLE account_sessions;
DROP TABLE accounts;
