-- Local mirror of the Knox tenant (Knox ids are natural keys). Rows are replaced wholesale by
-- each sync and patched by write-through; synced_at is when the row was last written.

CREATE TABLE knox_orgs (
    org_code    TEXT PRIMARY KEY,
    org_name    TEXT NOT NULL DEFAULT '',
    parent_code TEXT NOT NULL DEFAULT '',
    synced_at   TEXT NOT NULL
);
CREATE INDEX knox_orgs_parent_code ON knox_orgs (parent_code);

CREATE TABLE knox_users (
    user_id      TEXT PRIMARY KEY,
    user_name    TEXT NOT NULL DEFAULT '',
    email        TEXT NOT NULL DEFAULT '',
    first_name   TEXT NOT NULL DEFAULT '',
    last_name    TEXT NOT NULL DEFAULT '',
    mobile_phone TEXT NOT NULL DEFAULT '',
    contact      TEXT NOT NULL DEFAULT '',
    emp_no       TEXT NOT NULL DEFAULT '',
    org_code     TEXT NOT NULL DEFAULT '',
    org_name     TEXT NOT NULL DEFAULT '',
    enabled      BOOLEAN NOT NULL DEFAULT FALSE,
    user_type    TEXT NOT NULL DEFAULT '',
    device_count INTEGER NOT NULL DEFAULT 0,
    synced_at    TEXT NOT NULL
);
CREATE INDEX knox_users_org_code ON knox_users (org_code);

CREATE TABLE knox_groups (
    group_id   TEXT PRIMARY KEY,
    group_name TEXT NOT NULL DEFAULT '',
    group_type TEXT NOT NULL DEFAULT '',
    user_count INTEGER NOT NULL DEFAULT 0,
    synced_at  TEXT NOT NULL
);

CREATE TABLE knox_group_members (
    group_id  TEXT NOT NULL,
    user_id   TEXT NOT NULL,
    synced_at TEXT NOT NULL,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX knox_group_members_user_id ON knox_group_members (user_id);

CREATE TABLE knox_devices (
    device_id           TEXT PRIMARY KEY,
    device_model        TEXT NOT NULL DEFAULT '',
    device_model_kind   TEXT NOT NULL DEFAULT '',
    device_status       TEXT NOT NULL DEFAULT '',
    device_version_name TEXT NOT NULL DEFAULT '',
    platform            TEXT NOT NULL DEFAULT '',
    user_id             TEXT NOT NULL DEFAULT '',
    user_name           TEXT NOT NULL DEFAULT '',
    org_code            TEXT NOT NULL DEFAULT '',
    last_connection     TEXT NOT NULL DEFAULT '',
    synced_at           TEXT NOT NULL
);
CREATE INDEX knox_devices_user_id ON knox_devices (user_id);

CREATE TABLE sync_runs (
    id          {{pk}},
    "trigger"   TEXT NOT NULL,
    started_at  TEXT NOT NULL,
    finished_at TEXT,
    status      TEXT NOT NULL CHECK (status IN ('running', 'ok', 'failed')),
    error       TEXT NOT NULL DEFAULT '',
    orgs        INTEGER NOT NULL DEFAULT 0,
    users       INTEGER NOT NULL DEFAULT 0,
    groups      INTEGER NOT NULL DEFAULT 0,
    memberships INTEGER NOT NULL DEFAULT 0,
    devices     INTEGER NOT NULL DEFAULT 0
);
