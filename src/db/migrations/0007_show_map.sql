ALTER TABLE general_settings ADD COLUMN show_map INTEGER NOT NULL DEFAULT 1 CHECK (show_map IN (0, 1));
