// Package db owns the connection, migrations and every SQL statement in the application.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // registers "sqlite"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrDuplicate is returned when a unique value (such as a username) is already taken.
	ErrDuplicate = errors.New("already exists")
	// ErrSourceConflict is returned when an AD login collides with a local account of that name.
	ErrSourceConflict = errors.New("username belongs to an account from another source")
)

// Open connects using driver ("sqlite" or "postgres") and verifies the connection.
// Run Migrate before using any repository.
func Open(driver, dsn string) (*sqlx.DB, error) {
	name, ok := sqlDriverName(driver)
	if !ok {
		return nil, fmt.Errorf("unsupported database driver %q", driver)
	}
	if driver == driverSQLite {
		dsn = sqliteDSN(dsn)
	}
	d, err := sqlx.Open(name, dsn)
	if err != nil {
		return nil, fmt.Errorf("opening %s database: %w", driver, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := d.PingContext(ctx); err != nil {
		d.Close()
		return nil, fmt.Errorf("connecting to %s database: %w", driver, err)
	}
	return d, nil
}

// timeLayout is UTC ISO-8601, identical on both engines and sortable as text.
const timeLayout = "2006-01-02T15:04:05Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func now() string { return formatTime(time.Now()) }

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
