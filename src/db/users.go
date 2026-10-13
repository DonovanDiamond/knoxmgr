package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

type User struct {
	ID           int64   `db:"id" json:"id"`
	Username     string  `db:"username" json:"username"`
	DisplayName  string  `db:"display_name" json:"displayName"`
	PasswordHash *string `db:"password_hash" json:"-"`
	Role         string  `db:"role" json:"role"`
	OrgCode      *string `db:"org_code" json:"orgCode"`
	Access       *string `db:"access" json:"access"`
	AuthSource   string  `db:"auth_source" json:"authSource"`
	Disabled     bool    `db:"disabled" json:"disabled"`
	CreatedAt    string  `db:"created_at" json:"createdAt"`
	UpdatedAt    string  `db:"updated_at" json:"updatedAt"`
}

const userCols = `id, username, display_name, password_hash, role, org_code, access, auth_source, disabled, created_at, updated_at`

type Users struct{ db *sqlx.DB }

func NewUsers(d *sqlx.DB) *Users { return &Users{db: d} }

// Create inserts u and fills ID and the timestamps. Usernames are stored lower-case.
func (r *Users) Create(ctx context.Context, u *User) error {
	u.Username = strings.ToLower(u.Username)
	u.CreatedAt = now()
	u.UpdatedAt = u.CreatedAt
	err := r.db.QueryRowxContext(ctx, r.db.Rebind(`
		INSERT INTO app_users (username, display_name, password_hash, role, org_code, access, auth_source, disabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`),
		u.Username, u.DisplayName, u.PasswordHash, u.Role, u.OrgCode, u.Access, u.AuthSource, u.Disabled, u.CreatedAt, u.UpdatedAt,
	).Scan(&u.ID)
	if isUniqueViolation(err) {
		return fmt.Errorf("creating user %q: %w", u.Username, ErrDuplicate)
	}
	if err != nil {
		return fmt.Errorf("creating user %q: %w", u.Username, err)
	}
	return nil
}

func (r *Users) GetByID(ctx context.Context, id int64) (*User, error) {
	var u User
	if err := r.db.GetContext(ctx, &u, r.db.Rebind(`SELECT `+userCols+` FROM app_users WHERE id = ?`), id); err != nil {
		return nil, fmt.Errorf("loading user %d: %w", id, notFound(err))
	}
	return &u, nil
}

func (r *Users) GetByUsername(ctx context.Context, username string) (*User, error) {
	var u User
	username = strings.ToLower(username)
	if err := r.db.GetContext(ctx, &u, r.db.Rebind(`SELECT `+userCols+` FROM app_users WHERE username = ?`), username); err != nil {
		return nil, fmt.Errorf("loading user %q: %w", username, notFound(err))
	}
	return &u, nil
}

// Update writes the mutable fields of u (not username, auth source or created_at).
func (r *Users) Update(ctx context.Context, u *User) error {
	u.UpdatedAt = now()
	res, err := r.db.ExecContext(ctx, r.db.Rebind(`
		UPDATE app_users SET display_name = ?, password_hash = ?, role = ?, org_code = ?, access = ?, disabled = ?, updated_at = ?
		WHERE id = ?`),
		u.DisplayName, u.PasswordHash, u.Role, u.OrgCode, u.Access, u.Disabled, u.UpdatedAt, u.ID)
	if err != nil {
		return fmt.Errorf("updating user %d: %w", u.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("updating user %d: %w", u.ID, ErrNotFound)
	}
	return nil
}

// UpsertADUser creates or refreshes the account for a directory login: display name, role, org and
// access always follow AD; the disabled flag is left alone. It never takes over a local account.
func (r *Users) UpsertADUser(ctx context.Context, u *User) (*User, error) {
	u.Username = strings.ToLower(u.Username)
	t := now()
	var out User
	err := r.db.GetContext(ctx, &out, r.db.Rebind(`
		INSERT INTO app_users (username, display_name, role, org_code, access, auth_source, disabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'ad', FALSE, ?, ?)
		ON CONFLICT (username) DO UPDATE SET
			display_name = excluded.display_name, role = excluded.role, org_code = excluded.org_code,
			access = excluded.access, updated_at = excluded.updated_at
		WHERE app_users.auth_source = 'ad'
		RETURNING `+userCols),
		u.Username, u.DisplayName, u.Role, u.OrgCode, u.Access, t, t)
	if err != nil {
		if notFound(err) == ErrNotFound { // conflict row was local, so the WHERE filtered it out
			return nil, fmt.Errorf("upserting AD user %q: %w", u.Username, ErrSourceConflict)
		}
		return nil, fmt.Errorf("upserting AD user %q: %w", u.Username, err)
	}
	return &out, nil
}

func (r *Users) CountAdmins(ctx context.Context) (int, error) {
	var n int
	if err := r.db.GetContext(ctx, &n, `SELECT COUNT(*) FROM app_users WHERE role = 'admin'`); err != nil {
		return 0, fmt.Errorf("counting admins: %w", err)
	}
	return n, nil
}

// List returns every app account ordered by username.
func (r *Users) List(ctx context.Context) ([]User, error) {
	out := []User{}
	if err := r.db.SelectContext(ctx, &out, `SELECT `+userCols+` FROM app_users ORDER BY username`); err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	return out, nil
}

// Delete removes an account and its refresh tokens. Audit rows are kept (they carry the username)
// but lose their link to the deleted account.
func (r *Users) Delete(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("deleting user %d: %w", id, err)
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM refresh_tokens WHERE user_id = ?`,
		`UPDATE audit_log SET actor_user_id = NULL WHERE actor_user_id = ?`,
		`UPDATE invites SET invited_by = NULL WHERE invited_by = ?`,
	} {
		if _, err := tx.ExecContext(ctx, tx.Rebind(q), id); err != nil {
			return fmt.Errorf("deleting user %d: %w", id, err)
		}
	}
	res, err := tx.ExecContext(ctx, tx.Rebind(`DELETE FROM app_users WHERE id = ?`), id)
	if err != nil {
		return fmt.Errorf("deleting user %d: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("deleting user %d: %w", id, ErrNotFound)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("deleting user %d: %w", id, err)
	}
	return nil
}
