package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate applies, in order, every embedded migration not yet recorded in schema_migrations.
// Migrations are append-only: fix mistakes with a new file, never by editing a shipped one.
func Migrate(ctx context.Context, d *sqlx.DB) error {
	if _, err := d.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}
	var applied []int
	if err := d.SelectContext(ctx, &applied, `SELECT version FROM schema_migrations`); err != nil {
		return fmt.Errorf("reading schema_migrations: %w", err)
	}
	done := make(map[int]bool, len(applied))
	for _, v := range applied {
		done[v] = true
	}

	files, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	pk := strings.NewReplacer("{{pk}}", primaryKey(d.DriverName()))

	for _, f := range files {
		name := strings.TrimSuffix(strings.TrimPrefix(f, "migrations/"), ".sql")
		version, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %q: filename must start with a number: %w", f, err)
		}
		if done[version] {
			continue
		}
		body, err := migrationFS.ReadFile(f)
		if err != nil {
			return err
		}
		if err := applyMigration(ctx, d, version, name, pk.Replace(string(body))); err != nil {
			return fmt.Errorf("applying migration %s: %w", name, err)
		}
	}
	return nil
}

// dataMigrations run in the same transaction as their version's SQL, for steps plain SQL cannot
// do portably (e.g. splitting strings).
var dataMigrations = map[int]func(context.Context, *sqlx.Tx) error{
	2: backfillInvites,
}

// backfillInvites derives one invites row per id from the comma-joined targets of past invite audit entries.
func backfillInvites(ctx context.Context, tx *sqlx.Tx) error {
	var rows []struct {
		At     string `db:"at"`
		Actor  *int64 `db:"actor_user_id"`
		Target string `db:"target"`
	}
	if err := tx.SelectContext(ctx, &rows, tx.Rebind(
		`SELECT at, actor_user_id, target FROM audit_log WHERE action = ? ORDER BY id`), "knox.user.invite"); err != nil {
		return err
	}
	for _, row := range rows {
		for _, id := range strings.Split(row.Target, ",") {
			if id == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, tx.Rebind(
				`INSERT INTO invites (user_id, invited_at, invited_by) VALUES (?, ?, ?)`), id, row.At, row.Actor); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyMigration(ctx context.Context, d *sqlx.DB, version int, name, body string) error {
	tx, err := d.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return err
	}
	if fn := dataMigrations[version]; fn != nil {
		if err := fn(ctx, tx); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, tx.Rebind(
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`), version, name, now()); err != nil {
		return err
	}
	return tx.Commit()
}
