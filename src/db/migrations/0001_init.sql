-- Timestamps are UTC ISO-8601 text (YYYY-MM-DDTHH:MM:SSZ). {{pk}} is the one engine-specific token,
-- expanded in dialect.go.

CREATE TABLE app_users (
    id            {{pk}},
    username      TEXT    NOT NULL UNIQUE CHECK (username <> '' AND username = lower(username)),
    display_name  TEXT    NOT NULL DEFAULT '',
    password_hash TEXT,
    role          TEXT    NOT NULL CHECK (role IN ('admin', 'user')),
    org_code      TEXT,
    access        TEXT    CHECK (access IN ('read', 'write')),
    auth_source   TEXT    NOT NULL CHECK (auth_source IN ('local', 'ad')),
    disabled      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TEXT    NOT NULL,
    updated_at    TEXT    NOT NULL,
    CHECK (role <> 'user' OR (org_code IS NOT NULL AND org_code <> '' AND access IS NOT NULL)),
    CHECK (auth_source <> 'local' OR password_hash IS NOT NULL)
);

CREATE TABLE refresh_tokens (
    id         {{pk}},
    user_id    BIGINT NOT NULL REFERENCES app_users (id),
    token_hash TEXT   NOT NULL UNIQUE,
    expires_at TEXT   NOT NULL,
    revoked_at TEXT,
    created_at TEXT   NOT NULL
);
CREATE INDEX refresh_tokens_user_id ON refresh_tokens (user_id);

CREATE TABLE audit_log (
    id             {{pk}},
    at             TEXT   NOT NULL,
    actor_user_id  BIGINT REFERENCES app_users (id),
    actor_username TEXT   NOT NULL,
    action         TEXT   NOT NULL,
    target         TEXT   NOT NULL,
    previous_value TEXT,
    new_value      TEXT
);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
