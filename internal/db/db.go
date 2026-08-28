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

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
  		PRAGMA foreign_keys = ON;
  		PRAGMA synchronous = NORMAL;
  		PRAGMA temp_store = MEMORY;
  		PRAGMA busy_timeout = 5000;`)
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
