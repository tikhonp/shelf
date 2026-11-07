-- +goose Up
-- Connections of external agents. The token is shown once; only its hash stays.
CREATE TABLE agents_connections (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
    token_hash    TEXT NOT NULL UNIQUE,
    operations    TEXT NOT NULL,                     -- JSON array
    account_id    INTEGER,                           -- creator in simple mode; platform table, no FK
    created_by_id TEXT NOT NULL,                     -- user:<id> | cli | anonymous
    created_by    TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    last_used_at  TEXT,
    revoked_at    TEXT,
    revoked_by    TEXT,
    revoke_reason TEXT NOT NULL DEFAULT ''           -- '' | user | cli | account_deleted
) STRICT;
CREATE UNIQUE INDEX agents_connections_name ON agents_connections (name) WHERE revoked_at IS NULL;

CREATE TABLE agents_runs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    connection_id INTEGER NOT NULL REFERENCES agents_connections (id),
    client        TEXT NOT NULL DEFAULT '' CHECK (length(client) <= 200),
    started_at    TEXT NOT NULL,
    last_seen_at  TEXT NOT NULL
) STRICT;

CREATE TABLE agents_proposals (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    connection_id        INTEGER NOT NULL REFERENCES agents_connections (id),
    run_id               INTEGER NOT NULL REFERENCES agents_runs (id),
    album_id             INTEGER NOT NULL,           -- at creation; library table, no FK
    folder               TEXT NOT NULL,
    title                TEXT NOT NULL,              -- album title and artist at creation
    artist               TEXT NOT NULL,
    group_key            TEXT NOT NULL DEFAULT '',   -- review grouping only, never all-or-nothing
    follow_up_of         INTEGER REFERENCES agents_proposals (id),
    head                 INTEGER NOT NULL,           -- head revision number
    pending              INTEGER NOT NULL,           -- pending ops at head, kept in step
    changes_requested_at TEXT,                       -- set by a user, cleared by an agent revision
    withdrawn_at         TEXT,
    created_at           TEXT NOT NULL,
    updated_at           TEXT NOT NULL
) STRICT;
CREATE INDEX agents_proposals_open ON agents_proposals (pending, updated_at);
CREATE INDEX agents_proposals_connection ON agents_proposals (connection_id, pending);

-- Immutable. Revision 1 is the agent's; later ones are the agent's or a user's.
CREATE TABLE agents_revisions (
    proposal_id   INTEGER NOT NULL REFERENCES agents_proposals (id) ON DELETE CASCADE,
    revision      INTEGER NOT NULL,
    author_kind   TEXT NOT NULL CHECK (author_kind IN ('agent', 'user')),
    connection_id INTEGER,
    run_id        INTEGER,
    author_id     TEXT NOT NULL,                     -- agent:<id> | user:<id> | anonymous
    author        TEXT NOT NULL,
    title         TEXT NOT NULL CHECK (length(title) <= 200),
    reason        TEXT NOT NULL CHECK (length(reason) <= 4000),
    uncertainties TEXT NOT NULL DEFAULT '' CHECK (length(uncertainties) <= 2000),
    confidence    INTEGER CHECK (confidence BETWEEN 0 AND 100),
    evidence      TEXT NOT NULL DEFAULT '[]',        -- JSON
    base          TEXT NOT NULL,                     -- JSON: [{track_id, path, revision}]
    ops           TEXT NOT NULL,                     -- JSON: every op of this revision
    preview       TEXT NOT NULL,                     -- JSON: recorded preview of all pending ops
    op_digests    TEXT NOT NULL,                     -- JSON: {op_id: digest of that op alone}
    created_at    TEXT NOT NULL,
    PRIMARY KEY (proposal_id, revision)
) STRICT;

CREATE TABLE agents_acceptances (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    proposal_id INTEGER NOT NULL REFERENCES agents_proposals (id) ON DELETE CASCADE,
    revision    INTEGER NOT NULL,
    op_ids      TEXT NOT NULL,                       -- JSON, sorted
    idem_key    TEXT NOT NULL UNIQUE,                -- the client's request_id; the payload is compared on replay
    attempt     INTEGER NOT NULL DEFAULT 1,
    digest      TEXT NOT NULL,
    preview     TEXT NOT NULL,                       -- JSON: the preview the user accepted
    confirm     TEXT NOT NULL DEFAULT '[]',
    change_id   INTEGER NOT NULL,                    -- library table, no FK
    author_id   TEXT NOT NULL,
    author      TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    outcome     TEXT NOT NULL DEFAULT '{}',          -- JSON: last ChangeStatus seen, plus expired
    final       INTEGER NOT NULL DEFAULT 0 CHECK (final IN (0, 1))
) STRICT;
CREATE INDEX agents_acceptances_open ON agents_acceptances (final) WHERE final = 0;
CREATE INDEX agents_acceptances_proposal ON agents_acceptances (proposal_id);

-- One row per op ID a proposal has ever had.
CREATE TABLE agents_ops (
    proposal_id   INTEGER NOT NULL REFERENCES agents_proposals (id) ON DELETE CASCADE,
    op_id         TEXT NOT NULL,
    type          TEXT NOT NULL CHECK (type IN ('tag', 'cover')),
    fingerprint   TEXT NOT NULL,                     -- of the head values
    decision      TEXT NOT NULL CHECK (decision IN ('pending', 'accepted', 'declined', 'superseded')),
    decided_rev   INTEGER,
    acceptance_id INTEGER REFERENCES agents_acceptances (id),
    decided_at    TEXT,
    decided_by_id TEXT,
    decided_by    TEXT,
    PRIMARY KEY (proposal_id, op_id)
) STRICT;
CREATE INDEX agents_ops_fingerprint ON agents_ops (fingerprint) WHERE decision IN ('pending', 'declined');

CREATE TABLE agents_targets (
    proposal_id INTEGER NOT NULL REFERENCES agents_proposals (id) ON DELETE CASCADE,
    path        TEXT NOT NULL,
    PRIMARY KEY (proposal_id, path)
) STRICT;
CREATE INDEX agents_targets_path ON agents_targets (path);

-- The durable feed. Never updated.
CREATE TABLE agents_events (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    at            TEXT NOT NULL,
    kind          TEXT NOT NULL,
    proposal_id   INTEGER REFERENCES agents_proposals (id) ON DELETE CASCADE,
    revision      INTEGER,
    acceptance_id INTEGER,
    memory_id     INTEGER,
    actor_kind    TEXT NOT NULL CHECK (actor_kind IN ('user', 'agent', 'system')),
    actor_id      TEXT NOT NULL,
    actor         TEXT NOT NULL,
    connection_id INTEGER,
    run_id        INTEGER,
    body          TEXT NOT NULL DEFAULT '' CHECK (length(body) <= 4000),
    data          TEXT NOT NULL DEFAULT '{}'
) STRICT;
CREATE INDEX agents_events_proposal ON agents_events (proposal_id, id);

CREATE TABLE agents_memory (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    key              TEXT NOT NULL UNIQUE CHECK (length(key) BETWEEN 1 AND 120), -- slug; tombstones keep it
    layer            TEXT NOT NULL CHECK (layer IN ('rule', 'preference', 'context')),
    status           TEXT NOT NULL CHECK (status IN ('active', 'proposed', 'deleted')),
    title            TEXT NOT NULL CHECK (length(title) <= 200),
    scope            TEXT NOT NULL DEFAULT '' CHECK (length(scope) <= 500),
    body             TEXT NOT NULL CHECK (length(CAST(body AS BLOB)) <= 65536),
    confidence       INTEGER CHECK (confidence BETWEEN 0 AND 100),
    evidence         TEXT NOT NULL DEFAULT '[]',     -- JSON: [{event}|{change}|{proposal}|{note}]
    version          INTEGER NOT NULL,
    author_kind      TEXT NOT NULL CHECK (author_kind IN ('user', 'agent')),
    author_id        TEXT NOT NULL,
    author           TEXT NOT NULL,
    connection_id    INTEGER,
    run_id           INTEGER,
    corrected_event  INTEGER,                        -- last user correction or deletion
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
) STRICT;
CREATE INDEX agents_memory_layer ON agents_memory (layer, status);

-- One row, library-wide.
CREATE TABLE agents_learning (
    id             INTEGER PRIMARY KEY CHECK (id = 1),
    version        INTEGER NOT NULL,
    events_cursor  INTEGER NOT NULL,
    history_cursor INTEGER NOT NULL,
    checkpoint     TEXT NOT NULL DEFAULT '{}' CHECK (length(CAST(checkpoint AS BLOB)) <= 65536),
    updated_at     TEXT NOT NULL,
    connection_id  INTEGER,
    run_id         INTEGER
) STRICT;
INSERT INTO agents_learning (id, version, events_cursor, history_cursor, updated_at)
VALUES (1, 0, 0, 0, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

CREATE TABLE agents_usage (
    connection_id INTEGER NOT NULL,
    day           TEXT NOT NULL,                     -- UTC date
    kind          TEXT NOT NULL,                     -- lookup | offer | upload | upload_bytes | proposal | revision | memory
    n             INTEGER NOT NULL,
    PRIMARY KEY (connection_id, day, kind)
) STRICT;

CREATE TABLE agents_state (key TEXT PRIMARY KEY, value INTEGER NOT NULL) STRICT;
INSERT INTO agents_state(key,value) VALUES('history_high',0);

-- +goose Down
DROP TABLE agents_state;
DROP TABLE agents_usage;
DROP TABLE agents_learning;
DROP TABLE agents_memory;
DROP TABLE agents_events;
DROP TABLE agents_targets;
DROP TABLE agents_ops;
DROP TABLE agents_acceptances;
DROP TABLE agents_revisions;
DROP TABLE agents_proposals;
DROP TABLE agents_runs;
DROP TABLE agents_connections;
