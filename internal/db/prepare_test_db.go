package db

import (
	"database/sql"

	"kids-checkin/db"
)

type Cleanup func()

func PrepareTestDB() (*sql.DB, Cleanup, error) {
	tempDB, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	if err != nil {
		return nil, nil, err
	}

	// Apply the schema to the in-memory database
	_, err = tempDB.Exec(db.Schema)
	if err != nil {
		return nil, nil, err
	}

	return tempDB, func() { _ = tempDB.Close() }, nil
}
