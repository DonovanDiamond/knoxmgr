package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type AuditEntry struct {
	ID            int64   `db:"id" json:"id"`
	At            string  `db:"at" json:"at"`
	ActorUserID   *int64  `db:"actor_user_id" json:"actorUserId"`
	ActorUsername string  `db:"actor_username" json:"actorUsername"`
	Action        string  `db:"action" json:"action"`
	Target        string  `db:"target" json:"target"`
	PreviousValue *string `db:"previous_value" json:"previousValue"`
	NewValue      *string `db:"new_value" json:"newValue"`
}

type Audit struct{ db *sqlx.DB }

func NewAudit(d *sqlx.DB) *Audit { return &Audit{db: d} }

// Insert appends e, filling ID and At.
func (r *Audit) Insert(ctx context.Context, e *AuditEntry) error {
	return insertAudit(ctx, r.db, e)
}

// ListRecent returns up to limit entries, newest first.
func (r *Audit) ListRecent(ctx context.Context, limit int) ([]AuditEntry, error) {
	out := []AuditEntry{}
	if err := r.db.SelectContext(ctx, &out, r.db.Rebind(`
		SELECT id, at, actor_user_id, actor_username, action, target, previous_value, new_value
		FROM audit_log ORDER BY id DESC LIMIT ?`), limit); err != nil {
		return nil, fmt.Errorf("listing audit log: %w", err)
	}
	return out, nil
}

// inviteBatch bounds the bind parameters per query (SQLite and Postgres both cap them).
const inviteBatch = 500

// InsertInvites records e and one invites row per user id atomically.
func (r *Audit) InsertInvites(ctx context.Context, e *AuditEntry, userIDs []string) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("recording invites: %w", err)
	}
	defer tx.Rollback()
	if err := insertAudit(ctx, tx, e); err != nil {
		return err
	}
	for _, id := range userIDs {
		if _, err := tx.ExecContext(ctx, tx.Rebind(
			`INSERT INTO invites (user_id, invited_at, invited_by) VALUES (?, ?, ?)`), id, e.At, e.ActorUserID); err != nil {
			return fmt.Errorf("recording invite for %q: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("recording invites: %w", err)
	}
	return nil
}

// LatestInvites maps each of ids that has been invited to the time of its latest invite.
func (r *Audit) LatestInvites(ctx context.Context, ids []string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	for start := 0; start < len(ids); start += inviteBatch {
		batch := ids[start:min(start+inviteBatch, len(ids))]
		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		var rows []struct {
			UserID string `db:"user_id"`
			At     string `db:"at"`
		}
		if err := r.db.SelectContext(ctx, &rows, r.db.Rebind(
			`SELECT user_id, MAX(invited_at) AS at FROM invites WHERE user_id IN (?`+strings.Repeat(",?", len(batch)-1)+`) GROUP BY user_id`), args...); err != nil {
			return nil, fmt.Errorf("listing invites: %w", err)
		}
		for _, row := range rows {
			at, err := time.Parse(timeLayout, row.At)
			if err != nil {
				return nil, fmt.Errorf("parsing invite time %q: %w", row.At, err)
			}
			if at.After(out[row.UserID]) {
				out[row.UserID] = at
			}
		}
	}
	return out, nil
}

// insertAudit works on a connection or a transaction, so callers can audit atomically.
func insertAudit(ctx context.Context, q sqlx.ExtContext, e *AuditEntry) error {
	e.At = now()
	err := q.QueryRowxContext(ctx, q.Rebind(`
		INSERT INTO audit_log (at, actor_user_id, actor_username, action, target, previous_value, new_value)
		VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`),
		e.At, e.ActorUserID, e.ActorUsername, e.Action, e.Target, e.PreviousValue, e.NewValue).Scan(&e.ID)
	if err != nil {
		return fmt.Errorf("writing audit entry %q: %w", e.Action, err)
	}
	return nil
}
