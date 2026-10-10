package db

import (
	"database/sql"
	"fmt"
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
// test helpers in other packages, so it has to be importable from them. The cost
// is that it, and the dbDir resolution behind StructureSQL, are linked into
// production binaries.
//
// Linking them is not the same as running them. dbDir() resolves fresh on each
// call and does no work until something actually calls StructureSQL, and
// nothing in production does. The snapshot itself stays on disk.
func PrepareTestDB() (*sql.DB, Cleanup, error) {
	tempDB, err := InitDB(inMemoryDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("open in-memory test database: %w", err)
	}

	schema, err := StructureSQL()
	if err != nil {
		_ = tempDB.Close()
		return nil, nil, fmt.Errorf("prepare test database: %w", err)
	}

	if _, err := tempDB.Exec(schema); err != nil {
		_ = tempDB.Close()
		return nil, nil, fmt.Errorf("apply schema to test database: %w", err)
	}

	return tempDB, func() { _ = tempDB.Close() }, nil
}
