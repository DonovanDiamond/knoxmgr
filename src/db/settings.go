package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

type Settings struct{ db *sqlx.DB }

func NewSettings(d *sqlx.DB) *Settings { return &Settings{db: d} }

func (r *Settings) Get(ctx context.Context, key string) (string, error) {
	var v string
	if err := r.db.GetContext(ctx, &v, r.db.Rebind(`SELECT value FROM settings WHERE key = ?`), key); err != nil {
		return "", fmt.Errorf("loading setting %q: %w", key, notFound(err))
	}
	return v, nil
}

// Set stores value and writes the audit entry (with the previous value) in one transaction.
func (r *Settings) Set(ctx context.Context, key, value string, actorUserID *int64, actorUsername string) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("setting %q: %w", key, err)
	}
	defer tx.Rollback()

	var prev *string
	var old string
	switch err := tx.GetContext(ctx, &old, tx.Rebind(`SELECT value FROM settings WHERE key = ?`), key); {
	case err == nil:
		prev = &old
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("setting %q: %w", key, err)
	}

	if _, err := tx.ExecContext(ctx, tx.Rebind(`
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`),
		key, value, now()); err != nil {
		return fmt.Errorf("setting %q: %w", key, err)
	}
	if err := insertAudit(ctx, tx, &AuditEntry{
		ActorUserID: actorUserID, ActorUsername: actorUsername,
		Action: "settings.set", Target: key, PreviousValue: prev, NewValue: &value,
	}); err != nil {
		return err
	}
	return tx.Commit()
}
