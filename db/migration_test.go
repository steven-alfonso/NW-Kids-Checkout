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
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared&_foreign_keys=on")
	require.NoError(t, err)
	defer db.Close()

	targetMigration := "migrations/20260906164308_add_guest_family_model.up.sqlite"
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

	var fkOn int
	err = db.QueryRow("PRAGMA foreign_keys").Scan(&fkOn)
	require.NoError(t, err)
	assert.Equal(t, 1, fkOn, "foreign_keys should be enabled via DSN")

	// Verify indexes created after up and CHECK enforced.
	_, err = db.Exec(`INSERT INTO manual_checkins (first_name, last_name) VALUES ('', 'Doe')`)
	assert.Error(t, err, "blank names should be rejected after up migration")
	var upIdxCount int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_manual_checked_out_at'").Scan(&upIdxCount)
	require.NoError(t, err)
	assert.Equal(t, 1, upIdxCount, "idx_manual_checked_out_at should exist after up")
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_manual_checkins_child_id'").Scan(&upIdxCount)
	require.NoError(t, err)
	assert.Equal(t, 1, upIdxCount, "idx_manual_checkins_child_id should exist after up")
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_guest_submissions_created_at'").Scan(&upIdxCount)
	require.NoError(t, err)
	assert.Equal(t, 1, upIdxCount, "idx_guest_submissions_created_at should exist after up")
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_guest_submissions_parent_id'").Scan(&upIdxCount)
	require.NoError(t, err)
	assert.Equal(t, 1, upIdxCount, "idx_guest_submissions_parent_id should exist after up")
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_children_parent_id'").Scan(&upIdxCount)
	require.NoError(t, err)
	assert.Equal(t, 1, upIdxCount, "idx_children_parent_id should exist after up")

	downSQL, err := os.ReadFile("migrations/20260906164308_add_guest_family_model.down.sqlite")
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

	// Verify CHECK constraint removed after down: blank-name inserts should succeed.
	_, err = db.Exec(`INSERT INTO manual_checkins (first_name, last_name) VALUES ('', '')`)
	require.NoError(t, err, "CHECK should be removed after down; blank names should be allowed")
	_, err = db.Exec(`INSERT INTO manual_checkins (first_name, last_name) VALUES ('   ', '   ')`)
	require.NoError(t, err, "whitespace-only names should be allowed after down")

	// Verify index recreation/drop via sqlite_master.
	var idxCount int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_manual_checked_out_at'").Scan(&idxCount)
	require.NoError(t, err)
	assert.Equal(t, 1, idxCount, "idx_manual_checked_out_at should be recreated after down")

	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_manual_checkins_child_id'").Scan(&idxCount)
	require.NoError(t, err)
	assert.Equal(t, 0, idxCount, "idx_manual_checkins_child_id should not exist after down")

	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_guest_submissions_created_at'").Scan(&idxCount)
	require.NoError(t, err)
	assert.Equal(t, 0, idxCount, "idx_guest_submissions_created_at should not exist after down")

	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_guest_submissions_parent_id'").Scan(&idxCount)
	require.NoError(t, err)
	assert.Equal(t, 0, idxCount, "idx_guest_submissions_parent_id should not exist after down")

	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_children_parent_id'").Scan(&idxCount)
	require.NoError(t, err)
	assert.Equal(t, 0, idxCount, "idx_children_parent_id should not exist after down")
}

func TestMigration_GuestFamilyModel_NoRedundantAlterTable(t *testing.T) {
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared&_foreign_keys=on")
	require.NoError(t, err)
	defer db.Close()

	targetMigration := "migrations/20260906164308_add_guest_family_model.up.sqlite"
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
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared&_foreign_keys=on")
	require.NoError(t, err)
	defer db.Close()

	targetMigration := "migrations/20260906164308_add_guest_family_model.up.sqlite"
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
	_, err = db.Exec(`INSERT INTO manual_checkins (first_name, last_name) VALUES ('   ', 'Doe')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO manual_checkins (first_name, last_name) VALUES ('John', '   ')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO manual_checkins (first_name, last_name) VALUES ('   ', '   ')`)
	require.NoError(t, err)

	upSQL, err := os.ReadFile(targetMigration)
	require.NoError(t, err)
	_, err = db.Exec(string(upSQL))
	require.NoError(t, err, "up migration should succeed even with legacy blank-name rows")

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM manual_checkins").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 7, count, "all rows should survive migration")

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
	require.Len(t, got, 7)

	// Blank rows should be backfilled per-column; surviving names preserved.
	assert.Equal(t, "Unknown", got[0].first)
	assert.Equal(t, "Guest", got[0].last)
	assert.Equal(t, "Unknown", got[1].first)
	assert.Equal(t, "Doe", got[1].last)
	assert.Equal(t, "John", got[2].first)
	assert.Equal(t, "Guest", got[2].last)
	// Normal row should be untouched.
	assert.Equal(t, "Alice", got[3].first)
	assert.Equal(t, "Smith", got[3].last)
	// Whitespace-only names should be backfilled per-column as well.
	assert.Equal(t, "Unknown", got[4].first)
	assert.Equal(t, "Doe", got[4].last)
	assert.Equal(t, "John", got[5].first)
	assert.Equal(t, "Guest", got[5].last)
	assert.Equal(t, "Unknown", got[6].first)
	assert.Equal(t, "Guest", got[6].last)

	// Verify CHECK constraint is enforced after migration.
	_, err = db.Exec(`INSERT INTO manual_checkins (first_name, last_name) VALUES ('', 'Doe')`)
	assert.Error(t, err, "blank names should be rejected after migration")
	_, err = db.Exec(`INSERT INTO manual_checkins (first_name, last_name) VALUES ('   ', 'Doe')`)
	assert.Error(t, err, "whitespace-only names should be rejected after migration")
}
