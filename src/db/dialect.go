package db

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// This file is the ONE place where SQLite and Postgres differ. Everything else is portable SQL
// (placeholders are rewritten by sqlx Rebind).

const (
	driverSQLite   = "sqlite"
	driverPostgres = "postgres"
)

// sqlDriverName maps the config driver name to the database/sql driver name.
func sqlDriverName(driver string) (string, bool) {
	switch driver {
	case driverSQLite:
		return "sqlite", true
	case driverPostgres:
		return "pgx", true
	}
	return "", false
}

// sqliteDSN turns on foreign keys (per connection), WAL and a busy timeout for every connection
// the pool opens.
func sqliteDSN(dsn string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
}

// primaryKey is the autoincrementing integer primary key column definition.
func primaryKey(sqlDriver string) string {
	if sqlDriver == "pgx" {
		return "BIGSERIAL PRIMARY KEY"
	}
	return "INTEGER PRIMARY KEY AUTOINCREMENT"
}

// isUniqueViolation reports a UNIQUE constraint failure on either engine.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
