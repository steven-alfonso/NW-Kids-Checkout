//go:build dev

package dbinit

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureCounts is what makes applyFixture's "everything happens in one
// transaction, the read-back count included" true. If it could not fail, the
// count would not need to be inside the transaction at all, and moving it back
// out would reintroduce reporting an error for a database that was already
// seeded -- leaving it built and making the retry demand --force.
//
// So this pins the two halves of that contract: it counts a transaction's own
// uncommitted rows, and it fails when a table it counts is missing.
func TestFixtureCounts(t *testing.T) {
	// seeded builds a transaction holding the four tables the counts read, with
	// rows in three of them. It returns the tx and a rollback.
	seeded := func(t *testing.T) (*sql.Tx, func()) {
		t.Helper()
		database, err := sql.Open("sqlite3", "file::memory:?cache=shared")
		require.NoError(t, err)
		t.Cleanup(func() { _ = database.Close() })

		tx, err := database.BeginTx(t.Context(), nil)
		require.NoError(t, err)

		for _, ddl := range []string{
			`CREATE TABLE location_groups (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE events (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE locations (id INTEGER PRIMARY KEY)`,
			`CREATE TABLE event_check_windows (id INTEGER PRIMARY KEY)`,
		} {
			_, err := tx.ExecContext(t.Context(), ddl)
			require.NoError(t, err)
		}
		for i := range 3 {
			_, err := tx.ExecContext(t.Context(), `INSERT INTO locations (id) VALUES (?)`, i)
			require.NoError(t, err)
		}
		_, err = tx.ExecContext(t.Context(), `INSERT INTO events (id) VALUES (1)`)
		require.NoError(t, err)

		return tx, func() { _ = tx.Rollback() }
	}

	t.Run("counts a transaction's own uncommitted rows", func(t *testing.T) {
		tx, rollback := seeded(t)
		defer rollback()

		counts, err := fixtureCounts(t.Context(), tx)
		require.NoError(t, err)
		assert.Equal(t, map[string]int{
			"location_groups":     0,
			"events":              1,
			"locations":           3,
			"event_check_windows": 0,
		}, counts)
	})

	// A transaction reads its own uncommitted writes, which is the property that
	// lets the count move inside the transaction without changing the numbers.
	t.Run("sees rows the transaction has not committed", func(t *testing.T) {
		tx, rollback := seeded(t)
		defer rollback()

		var committed int
		require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM locations`).Scan(&committed))
		assert.Equal(t, 3, committed, "precondition: the rows are visible to the tx")

		counts, err := fixtureCounts(t.Context(), tx)
		require.NoError(t, err)
		assert.Equal(t, 3, counts["locations"])
	})

	t.Run("fails when a counted table is missing", func(t *testing.T) {
		tx, rollback := seeded(t)
		defer rollback()

		_, err := tx.ExecContext(t.Context(), `DROP TABLE event_check_windows`)
		require.NoError(t, err)

		_, err = fixtureCounts(t.Context(), tx)
		require.Error(t, err, "a count that cannot be taken must surface as an error, or the rollback it guards is unreachable")
		assert.Contains(t, err.Error(), "event_check_windows")
	})
}
