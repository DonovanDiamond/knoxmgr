-- Admin-editable settings. Each singleton table holds exactly one row (id = 1), seeded here.
-- *_cipher columns hold AES-GCM ciphertext (see src/secretbox), never plaintext.

CREATE TABLE knox_settings (
    id                  INTEGER PRIMARY KEY CHECK (id = 1),
    region              TEXT    NOT NULL DEFAULT '',
    base_url            TEXT    NOT NULL DEFAULT '',
    client_id           TEXT    NOT NULL DEFAULT '',
    tenant_id           TEXT    NOT NULL DEFAULT '',
    client_secret_cipher TEXT    NOT NULL DEFAULT '',
    allow_insecure_http BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at          TEXT    NOT NULL
);
INSERT INTO knox_settings (id, updated_at) VALUES (1, '1970-01-01T00:00:00Z');

CREATE TABLE ad_settings (
    id                   INTEGER PRIMARY KEY CHECK (id = 1),
    enabled              BOOLEAN NOT NULL DEFAULT FALSE,
    url                  TEXT    NOT NULL DEFAULT '',
    base_dn              TEXT    NOT NULL DEFAULT '',
    upn_suffix           TEXT    NOT NULL DEFAULT '',
    bind_user            TEXT    NOT NULL DEFAULT '',
    bind_password_cipher TEXT    NOT NULL DEFAULT '',
    updated_at           TEXT    NOT NULL
);
INSERT INTO ad_settings (id, updated_at) VALUES (1, '1970-01-01T00:00:00Z');

CREATE TABLE ad_group_mappings (
    id        {{pk}},
    position  INTEGER NOT NULL,
    group_dn  TEXT    NOT NULL,
    role      TEXT    NOT NULL CHECK (role IN ('admin', 'user')),
    org_code  TEXT    NOT NULL DEFAULT '',
    access    TEXT    NOT NULL DEFAULT '' CHECK (access IN ('', 'read', 'write'))
);

CREATE TABLE general_settings (
    id                    INTEGER PRIMARY KEY CHECK (id = 1),
    installation_name     TEXT    NOT NULL DEFAULT 'knoxmgr',
    sync_interval_minutes INTEGER NOT NULL DEFAULT 15 CHECK (sync_interval_minutes BETWEEN 1 AND 1440),
    updated_at            TEXT    NOT NULL
);
INSERT INTO general_settings (id, updated_at) VALUES (1, '1970-01-01T00:00:00Z');
