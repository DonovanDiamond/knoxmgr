-- Application inventory: per-device app lists pulled from Knox, plus admin-editable categories.
-- Booleans are 0/1 integers like the other settings columns.

CREATE TABLE device_apps (
    device_id              TEXT    NOT NULL,
    package                TEXT    NOT NULL,
    name                   TEXT    NOT NULL DEFAULT '',
    version                TEXT    NOT NULL DEFAULT '',
    system                 INTEGER NOT NULL DEFAULT 0 CHECK (system IN (0, 1)),
    managed                INTEGER NOT NULL DEFAULT 0 CHECK (managed IN (0, 1)),
    installed_at           TEXT,
    knox_id                TEXT    NOT NULL DEFAULT '',
    uninstall_requested_at TEXT,
    synced_at              TEXT    NOT NULL,
    PRIMARY KEY (device_id, package)
);
CREATE INDEX device_apps_package ON device_apps (package);

CREATE TABLE app_categories (
    id   {{pk}},
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE app_category_map (
    package     TEXT    PRIMARY KEY,
    category_id INTEGER NOT NULL REFERENCES app_categories (id) ON DELETE CASCADE
);
CREATE INDEX app_category_map_category_id ON app_category_map (category_id);

CREATE TABLE app_inventory (
    id                     INTEGER PRIMARY KEY CHECK (id = 1),
    last_pull_at           TEXT,
    last_report_request_at TEXT
);
INSERT INTO app_inventory (id) VALUES (1);

ALTER TABLE general_settings ADD COLUMN app_inventory_hours INTEGER NOT NULL DEFAULT 6 CHECK (app_inventory_hours BETWEEN 1 AND 168);
ALTER TABLE general_settings ADD COLUMN app_report_daily INTEGER NOT NULL DEFAULT 1 CHECK (app_report_daily IN (0, 1));

-- Seed: well-known package ids, editable in the UI.
INSERT INTO app_categories (name) VALUES ('Social media');
INSERT INTO app_categories (name) VALUES ('Messaging');
INSERT INTO app_categories (name) VALUES ('Entertainment');
INSERT INTO app_categories (name) VALUES ('Productivity');
INSERT INTO app_categories (name) VALUES ('Browsers');
INSERT INTO app_categories (name) VALUES ('Google');
INSERT INTO app_categories (name) VALUES ('Samsung');
INSERT INTO app_categories (name) VALUES ('Device management');
INSERT INTO app_categories (name) VALUES ('Other');

INSERT INTO app_category_map (package, category_id)
SELECT v.p, c.id FROM app_categories c, (SELECT 'com.facebook.katana' AS p UNION ALL SELECT 'com.facebook.lite' AS p UNION ALL SELECT 'com.instagram.android' AS p UNION ALL SELECT 'com.zhiliaoapp.musically' AS p UNION ALL SELECT 'com.ss.android.ugc.trill' AS p UNION ALL SELECT 'com.twitter.android' AS p UNION ALL SELECT 'com.linkedin.android' AS p UNION ALL SELECT 'com.snapchat.android' AS p UNION ALL SELECT 'com.pinterest' AS p UNION ALL SELECT 'com.reddit.frontpage' AS p) v WHERE c.name = 'Social media';

INSERT INTO app_category_map (package, category_id)
SELECT v.p, c.id FROM app_categories c, (SELECT 'com.whatsapp' AS p UNION ALL SELECT 'com.whatsapp.w4b' AS p UNION ALL SELECT 'org.telegram.messenger' AS p UNION ALL SELECT 'org.thoughtcrime.securesms' AS p UNION ALL SELECT 'com.facebook.orca' AS p UNION ALL SELECT 'com.viber.voip' AS p UNION ALL SELECT 'com.discord' AS p UNION ALL SELECT 'com.skype.raider' AS p UNION ALL SELECT 'com.slack' AS p) v WHERE c.name = 'Messaging';

INSERT INTO app_category_map (package, category_id)
SELECT v.p, c.id FROM app_categories c, (SELECT 'com.google.android.youtube' AS p UNION ALL SELECT 'com.netflix.mediaclient' AS p UNION ALL SELECT 'com.spotify.music' AS p UNION ALL SELECT 'com.amazon.avod.thirdpartyclient' AS p UNION ALL SELECT 'com.disney.disneyplus' AS p UNION ALL SELECT 'tv.twitch.android.app' AS p UNION ALL SELECT 'com.google.android.apps.youtube.music' AS p) v WHERE c.name = 'Entertainment';

INSERT INTO app_category_map (package, category_id)
SELECT v.p, c.id FROM app_categories c, (SELECT 'com.microsoft.office.outlook' AS p UNION ALL SELECT 'com.microsoft.teams' AS p UNION ALL SELECT 'us.zoom.videomeetings' AS p UNION ALL SELECT 'com.microsoft.office.word' AS p UNION ALL SELECT 'com.microsoft.office.excel' AS p UNION ALL SELECT 'com.microsoft.office.powerpoint' AS p UNION ALL SELECT 'com.microsoft.office.officehubrow' AS p UNION ALL SELECT 'com.microsoft.skydrive' AS p UNION ALL SELECT 'com.microsoft.office.onenote' AS p UNION ALL SELECT 'com.microsoft.todos' AS p UNION ALL SELECT 'com.dropbox.android' AS p) v WHERE c.name = 'Productivity';

INSERT INTO app_category_map (package, category_id)
SELECT v.p, c.id FROM app_categories c, (SELECT 'com.android.chrome' AS p UNION ALL SELECT 'org.mozilla.firefox' AS p UNION ALL SELECT 'com.sec.android.app.sbrowser' AS p UNION ALL SELECT 'com.microsoft.emmx' AS p UNION ALL SELECT 'com.brave.browser' AS p UNION ALL SELECT 'com.opera.browser' AS p) v WHERE c.name = 'Browsers';

INSERT INTO app_category_map (package, category_id)
SELECT v.p, c.id FROM app_categories c, (SELECT 'com.google.android.gm' AS p UNION ALL SELECT 'com.google.android.apps.maps' AS p UNION ALL SELECT 'com.google.android.apps.docs' AS p UNION ALL SELECT 'com.google.android.apps.docs.editors.docs' AS p UNION ALL SELECT 'com.google.android.apps.docs.editors.sheets' AS p UNION ALL SELECT 'com.google.android.apps.photos' AS p UNION ALL SELECT 'com.google.android.calendar' AS p UNION ALL SELECT 'com.google.android.apps.meetings' AS p UNION ALL SELECT 'com.google.android.googlequicksearchbox' AS p UNION ALL SELECT 'com.android.vending' AS p) v WHERE c.name = 'Google';

INSERT INTO app_category_map (package, category_id)
SELECT v.p, c.id FROM app_categories c, (SELECT 'com.samsung.android.messaging' AS p UNION ALL SELECT 'com.samsung.android.dialer' AS p UNION ALL SELECT 'com.samsung.android.app.notes' AS p UNION ALL SELECT 'com.samsung.android.calendar' AS p UNION ALL SELECT 'com.sec.android.gallery3d' AS p UNION ALL SELECT 'com.samsung.android.email.provider' AS p) v WHERE c.name = 'Samsung';

INSERT INTO app_category_map (package, category_id)
SELECT v.p, c.id FROM app_categories c, (SELECT 'com.samsung.android.knox.containercore' AS p UNION ALL SELECT 'com.samsung.android.knox.kpu' AS p UNION ALL SELECT 'com.samsung.android.kmxcore' AS p UNION ALL SELECT 'com.samsung.android.knox.attestation' AS p UNION ALL SELECT 'com.sec.enterprise.knox.cloudmdm.smdms' AS p UNION ALL SELECT 'com.samsung.android.knox.kmagent' AS p UNION ALL SELECT 'com.samsung.klmsagent' AS p UNION ALL SELECT 'com.samsung.android.knox.zt.framework' AS p) v WHERE c.name = 'Device management';
