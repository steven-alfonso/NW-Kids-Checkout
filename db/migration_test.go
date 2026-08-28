package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func applyMigrationsUpTo(t *testing.T, db *sql.DB, exclude string) {
	t.Helper()
	migrations, err := filepath.Glob("migrations/*.up.sqlite")
	require.NoError(t, err)
	sort.Strings(migrations)
	for _, m := range migrations {
		if m == exclude {
			break
		}
		sqlBytes, readErr := os.ReadFile(m)
		require.NoError(t, readErr)
		_, execErr := db.Exec(string(sqlBytes))
		require.NoError(t, execErr, "failed to apply migration %s", m)
	}
}

func TestMigration_GuestFamilyModel_RoundTrip(t *testing.T) {
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	require.NoError(t, err)
	defer db.Close()

	targetMigration := "migrations/20260825030013_add_guest_family_model.up.sqlite"
	applyMigrationsUpTo(t, db, targetMigration)

	_, err = db.Exec(`INSERT INTO manual_checkins (first_name, last_name) VALUES ('Alice', 'Smith')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO manual_checkins (first_name, last_name) VALUES ('Bob', 'Jones')`)
	require.NoError(t, err)

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM manual_checkins").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "should have 2 rows before migration")

	upSQL, err := os.ReadFile(targetMigration)
	require.NoError(t, err)
	_, err = db.Exec(string(upSQL))
	require.NoError(t, err, "guest family model up migration failed")

	err = db.QueryRow("SELECT COUNT(*) FROM manual_checkins").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "data should survive up migration")

	var childIDCol string
	err = db.QueryRow("SELECT name FROM pragma_table_info('manual_checkins') WHERE name = 'child_id'").Scan(&childIDCol)
	require.NoError(t, err, "child_id column should exist after up migration")
	assert.Equal(t, "child_id", childIDCol)

	_, err = db.Exec("SELECT 1 FROM parents LIMIT 0")
	require.NoError(t, err, "parents table should exist")
	_, err = db.Exec("SELECT 1 FROM children LIMIT 0")
	require.NoError(t, err, "children table should exist")
	_, err = db.Exec("SELECT 1 FROM guest_submissions LIMIT 0")
	require.NoError(t, err, "guest_submissions table should exist")

	downSQL, err := os.ReadFile("migrations/20260825030013_add_guest_family_model.down.sqlite")
	require.NoError(t, err)
	_, err = db.Exec(string(downSQL))
	require.NoError(t, err, "guest family model down migration failed")

	err = db.QueryRow("SELECT COUNT(*) FROM manual_checkins").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "data should survive down migration")

	err = db.QueryRow("SELECT name FROM pragma_table_info('manual_checkins') WHERE name = 'child_id'").Scan(&childIDCol)
	assert.Error(t, err, "child_id column should not exist after down migration")

	_, err = db.Exec("SELECT 1 FROM parents LIMIT 0")
	assert.Error(t, err, "parents table should not exist after down migration")
	_, err = db.Exec("SELECT 1 FROM children LIMIT 0")
	assert.Error(t, err, "children table should not exist after down migration")
	_, err = db.Exec("SELECT 1 FROM guest_submissions LIMIT 0")
	assert.Error(t, err, "guest_submissions table should not exist after down migration")
}

func TestMigration_GuestFamilyModel_NoRedundantAlterTable(t *testing.T) {
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	require.NoError(t, err)
	defer db.Close()

	targetMigration := "migrations/20260825030013_add_guest_family_model.up.sqlite"
	applyMigrationsUpTo(t, db, targetMigration)

	_, err = db.Exec(`INSERT INTO manual_checkins (first_name, last_name) VALUES ('Alice', 'Smith')`)
	require.NoError(t, err)

	upSQL, err := os.ReadFile(targetMigration)
	require.NoError(t, err)
	upSQLStr := string(upSQL)

	assert.False(t, strings.Contains(upSQLStr, "ALTER TABLE manual_checkins ADD COLUMN"),
		"migration should not contain a redundant ALTER TABLE ADD COLUMN for child_id")

	_, err = db.Exec(upSQLStr)
	require.NoError(t, err, "migration should apply cleanly without the dead ALTER TABLE")

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM manual_checkins").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "data should survive migration")

	var childIDCol string
	err = db.QueryRow("SELECT name FROM pragma_table_info('manual_checkins') WHERE name = 'child_id'").Scan(&childIDCol)
	require.NoError(t, err, "child_id column should exist after migration")
	assert.Equal(t, "child_id", childIDCol)
}

func TestMigration_GuestFamilyModel_BlankNameBackfill(t *testing.T) {
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	require.NoError(t, err)
	defer db.Close()

	targetMigration := "migrations/20260825030013_add_guest_family_model.up.sqlite"
	applyMigrationsUpTo(t, db, targetMigration)

	// Insert legacy blank-name rows that would have been allowed by main's
	// CreateManualCheckin but violate the new CHECK constraint.
	_, err = db.Exec(`INSERT INTO manual_checkins (first_name, last_name) VALUES ('', '')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO manual_checkins (first_name, last_name) VALUES ('', 'Doe')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO manual_checkins (first_name, last_name) VALUES ('John', '')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO manual_checkins (first_name, last_name) VALUES ('Alice', 'Smith')`)
	require.NoError(t, err)

	upSQL, err := os.ReadFile(targetMigration)
	require.NoError(t, err)
	_, err = db.Exec(string(upSQL))
	require.NoError(t, err, "up migration should succeed even with legacy blank-name rows")

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM manual_checkins").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 4, count, "all rows should survive migration")

	rows, err := db.Query(`SELECT first_name, last_name FROM manual_checkins ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()

	type pair struct{ first, last string }
	var got []pair
	for rows.Next() {
		var f, l string
		require.NoError(t, rows.Scan(&f, &l))
		got = append(got, pair{f, l})
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 4)

	// Blank rows should be backfilled to Unknown/Guest.
	assert.Equal(t, "Unknown", got[0].first)
	assert.Equal(t, "Guest", got[0].last)
	assert.Equal(t, "Unknown", got[1].first)
	assert.Equal(t, "Guest", got[1].last)
	assert.Equal(t, "Unknown", got[2].first)
	assert.Equal(t, "Guest", got[2].last)
	// Normal row should be untouched.
	assert.Equal(t, "Alice", got[3].first)
	assert.Equal(t, "Smith", got[3].last)

	// Verify CHECK constraint is enforced after migration.
	_, err = db.Exec(`INSERT INTO manual_checkins (first_name, last_name) VALUES ('', 'Doe')`)
	assert.Error(t, err, "blank names should be rejected after migration")
}
