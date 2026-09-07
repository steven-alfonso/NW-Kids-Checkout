package db

import (
	"database/sql"
	"errors"
	"log/slog"

	_ "github.com/mattn/go-sqlite3"
)

// InitDB initializes the database connection.
func InitDB(dataSourceName string) (*sql.DB, error) {
	if dataSourceName == "" {
		return nil, errors.New("missing database DSN")
	}

	slog.Info("initializing database connection", slog.String("dsn", dataSourceName))

	db, err := sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
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
