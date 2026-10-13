-- App inventory fixes: track Knox's mandatory flag, add mappings for packages seen in practice.

ALTER TABLE device_apps ADD COLUMN mandatory INTEGER NOT NULL DEFAULT 0 CHECK (mandatory IN (0, 1));

-- Orphaned app caches: the apps list now comes from device_apps.
DELETE FROM knox_device_cache WHERE kind = 'apps';

-- 0009 seeded "com.slack" but the real package id is case-sensitive "com.Slack". ON CONFLICT DO NOTHING keeps any admin mapping.
INSERT INTO app_category_map (package, category_id)
SELECT v.p, c.id FROM app_categories c, (SELECT 'com.google.android.apps.tachyon' AS p UNION ALL SELECT 'com.Slack' AS p) v WHERE c.name = 'Messaging'
ON CONFLICT (package) DO NOTHING;

INSERT INTO app_category_map (package, category_id)
SELECT v.p, c.id FROM app_categories c, (SELECT 'com.azure.authenticator' AS p UNION ALL SELECT 'com.adobe.reader' AS p) v WHERE c.name = 'Productivity'
ON CONFLICT (package) DO NOTHING;

INSERT INTO app_category_map (package, category_id)
SELECT v.p, c.id FROM app_categories c, (SELECT 'com.shazam.android' AS p UNION ALL SELECT 'com.king.candycrushsaga' AS p) v WHERE c.name = 'Entertainment'
ON CONFLICT (package) DO NOTHING;
