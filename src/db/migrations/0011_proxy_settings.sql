-- Outbound HTTP proxy for Knox and update-check traffic (not AD/LDAP). Singleton row (id = 1), seeded here.
-- password_cipher holds AES-GCM ciphertext (see src/secretbox), never plaintext.

CREATE TABLE proxy_settings (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    url             TEXT NOT NULL DEFAULT '',
    username        TEXT NOT NULL DEFAULT '',
    password_cipher TEXT NOT NULL DEFAULT '',
    bypass          TEXT NOT NULL DEFAULT '',
    updated_at      TEXT NOT NULL
);
INSERT INTO proxy_settings (id, updated_at) VALUES (1, '1970-01-01T00:00:00Z');
