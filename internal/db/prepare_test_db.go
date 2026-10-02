package db

import (
	"database/sql"
)

// inMemoryDSN is the DSN for the shared in-memory database tests run against.
// It carries no parameters of its own: InitDB appends the same
// _foreign_keys/_busy_timeout/_txlock set production gets, so the test
// database cannot drift from the real one on connection settings.
const inMemoryDSN = "file::memory:?cache=shared"

type Cleanup func()

func PrepareTestDB() (*sql.DB, Cleanup, error) {
	tempDB, err := InitDB(inMemoryDSN)
	if err != nil {
		return nil, nil, err
	}

	schema, err := StructureSQL()
	if err != nil {
		_ = tempDB.Close()
		return nil, nil, err
	}

	if _, err := tempDB.Exec(schema); err != nil {
		_ = tempDB.Close()
		return nil, nil, err
	}

	return tempDB, func() { _ = tempDB.Close() }, nil
}
