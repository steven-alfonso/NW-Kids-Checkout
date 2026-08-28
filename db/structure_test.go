package db

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStructureSQLMatchesMigrations(t *testing.T) {
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	require.NoError(t, err)
	defer db.Close()

	applyMigrationsUpTo(t, db, "")

	rows, err := db.QueryContext(t.Context(), `SELECT sql || ';' FROM sqlite_master WHERE type IN ('table','index') AND name NOT IN ('schema_migrations','sqlite_sequence','version_unique') AND sql IS NOT NULL`)
	require.NoError(t, err)
	defer rows.Close()

	var stmts []string
	for rows.Next() {
		var stmt string
		require.NoError(t, rows.Scan(&stmt))
		stmts = append(stmts, stmt)
	}
	require.NoError(t, rows.Err())

	want, err := os.ReadFile("structure.sql")
	require.NoError(t, err)

	assert.Equal(t, strings.Join(stmts, "\n")+"\n", string(want),
		"db/structure.sql is stale; regenerate with `make db-migrate`")
}
