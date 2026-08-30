package db

import (
	"database/sql"
	"errors"
	"log/slog"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// InitDB initializes the database connection.
func InitDB(dataSourceName string) (*sql.DB, error) {
	if dataSourceName == "" {
		return nil, errors.New("missing database DSN")
	}

	slog.Info("initializing database connection", slog.String("dsn", dataSourceName))

	dsn := dataSourceName
	if !strings.Contains(dsn, "_foreign_keys") {
		if strings.Contains(dsn, "?") {
			dsn += "&_foreign_keys=on"
		} else {
			dsn += "?_foreign_keys=on"
		}
	}
	if !strings.Contains(dsn, "_busy_timeout") {
		if strings.Contains(dsn, "?") {
			dsn += "&_busy_timeout=5000"
		} else {
			dsn += "?_busy_timeout=5000"
		}
	}
	if !strings.Contains(dsn, "_txlock") {
		if strings.Contains(dsn, "?") {
			dsn += "&_txlock=immediate"
		} else {
			dsn += "?_txlock=immediate"
		}
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	// DSN params _foreign_keys, _busy_timeout, _txlock are the load-bearing
	// per-connection settings (they apply to every pooled connection). The
	// Exec below only affects one connection and must not be relied on for
	// per-connection correctness — keep DSN params as the source of truth.
	_, err = db.Exec(`
  		PRAGMA synchronous = NORMAL;
  		PRAGMA temp_store = MEMORY;`)
	if err != nil {
		return nil, err
	}

	err = db.Ping()
	if err != nil {
		return nil, err
	}

	slog.Info("database connection established")
	return db, nil
}
