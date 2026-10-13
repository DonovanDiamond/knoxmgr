-- Last live Knox answer per device and kind (detail, apps, location), served when Knox is
-- unreachable. payload is JSON.

CREATE TABLE knox_device_cache (
    device_id  TEXT NOT NULL,
    kind       TEXT NOT NULL,
    payload    TEXT NOT NULL,
    fetched_at TEXT NOT NULL,
    PRIMARY KEY (device_id, kind)
);
