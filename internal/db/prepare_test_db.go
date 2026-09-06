package db

import (
	"database/sql"

	"kids-checkin/db"
)

type Cleanup func()

func PrepareTestDB() (*sql.DB, Cleanup, error) {
	tempDB, err := sql.Open("sqlite3", "file::memory:?cache=shared&_foreign_keys=on&_busy_timeout=5000&_txlock=immediate")
	if err != nil {
		return nil, nil, err
	}

	// DSN params _foreign_keys, _busy_timeout, _txlock are the load-bearing
	// per-connection settings; no one-shot PRAGMA Exec needed here.

	// Apply the schema to the in-memory database
	_, err = tempDB.Exec(db.Schema)
	if err != nil {
		return nil, nil, err
	}

	return tempDB, func() { _ = tempDB.Close() }, nil
}
