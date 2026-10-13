ALTER TABLE general_settings ADD COLUMN check_updates INTEGER NOT NULL DEFAULT 1 CHECK (check_updates IN (0, 1));
