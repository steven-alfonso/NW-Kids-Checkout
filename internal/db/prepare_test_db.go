package db

import (
	"database/sql"
	"fmt"

	dbschema "kids-checkin/db"
)

// inMemoryDSN is the DSN for the shared in-memory database tests run against.
// It carries no parameters of its own: InitDB appends the same
// _foreign_keys/_busy_timeout/_txlock set production gets, so the test
// database cannot drift from the real one on connection settings.
const inMemoryDSN = "file::memory:?cache=shared"

type Cleanup func()

// PrepareTestDB opens the shared in-memory test database and applies the schema
// snapshot to it.
//
// This file is deliberately not a _test.go file: PrepareTestDB is called from
// test helpers in other packages, so it has to be importable from them.
func PrepareTestDB() (*sql.DB, Cleanup, error) {
	tempDB, err := InitDB(inMemoryDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("open in-memory test database: %w", err)
	}

	if _, err := tempDB.Exec(dbschema.Schema); err != nil {
		_ = tempDB.Close()
		return nil, nil, fmt.Errorf("apply schema to test database: %w", err)
	}

	return tempDB, func() { _ = tempDB.Close() }, nil
}
