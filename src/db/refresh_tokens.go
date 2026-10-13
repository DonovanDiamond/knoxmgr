package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

type RefreshToken struct {
	ID        int64   `db:"id"`
	UserID    int64   `db:"user_id"`
	TokenHash string  `db:"token_hash"`
	ExpiresAt string  `db:"expires_at"`
	RevokedAt *string `db:"revoked_at"`
	CreatedAt string  `db:"created_at"`
}

// RefreshTokens stores only hashes of tokens; the raw value never reaches the database.
type RefreshTokens struct{ db *sqlx.DB }

func NewRefreshTokens(d *sqlx.DB) *RefreshTokens { return &RefreshTokens{db: d} }

func (r *RefreshTokens) Insert(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx, r.db.Rebind(
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at, created_at) VALUES (?, ?, ?, ?)`),
		userID, tokenHash, formatTime(expiresAt), now())
	if err != nil {
		return fmt.Errorf("storing refresh token for user %d: %w", userID, err)
	}
	return nil
}

// GetValid returns the token with this hash if it is neither revoked nor expired at at.
func (r *RefreshTokens) GetValid(ctx context.Context, tokenHash string, at time.Time) (*RefreshToken, error) {
	var t RefreshToken
	err := r.db.GetContext(ctx, &t, r.db.Rebind(`
		SELECT id, user_id, token_hash, expires_at, revoked_at, created_at FROM refresh_tokens
		WHERE token_hash = ? AND revoked_at IS NULL AND expires_at > ?`), tokenHash, formatTime(at))
	if err != nil {
		return nil, fmt.Errorf("loading refresh token: %w", notFound(err))
	}
	return &t, nil
}

// GetByHash returns the token with this hash whether or not it is revoked or expired, so a
// replayed (already rotated) token can be recognised.
func (r *RefreshTokens) GetByHash(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	var t RefreshToken
	err := r.db.GetContext(ctx, &t, r.db.Rebind(`
		SELECT id, user_id, token_hash, expires_at, revoked_at, created_at FROM refresh_tokens
		WHERE token_hash = ?`), tokenHash)
	if err != nil {
		return nil, fmt.Errorf("loading refresh token: %w", notFound(err))
	}
	return &t, nil
}

// Revoke revokes one token. It returns ErrNotFound if it was already revoked, so of two
// concurrent uses of the same token only one wins.
func (r *RefreshTokens) Revoke(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, r.db.Rebind(
		`UPDATE refresh_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`), now(), id)
	if err != nil {
		return fmt.Errorf("revoking refresh token %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("revoking refresh token %d: %w", id, ErrNotFound)
	}
	return nil
}

func (r *RefreshTokens) RevokeAllForUser(ctx context.Context, userID int64) error {
	if _, err := r.db.ExecContext(ctx, r.db.Rebind(
		`UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`), now(), userID); err != nil {
		return fmt.Errorf("revoking refresh tokens of user %d: %w", userID, err)
	}
	return nil
}
