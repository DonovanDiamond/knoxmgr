CREATE TABLE invites (
    id         {{pk}},
    user_id    TEXT NOT NULL,
    invited_at TEXT NOT NULL,
    invited_by BIGINT REFERENCES app_users(id)
);

CREATE INDEX invites_user_id_invited_at ON invites (user_id, invited_at);
