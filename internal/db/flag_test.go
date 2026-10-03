package db_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"testing"

	"kids-checkin/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// resolveDBFile runs a throwaway command carrying only the real db-file flag
// and reports what the command resolved to. Driving the actual flag -- rather
// than a helper that models the precedence -- is the point: this is the same
// object every subcommand mounts, so a change to DBFileFlag that broke
// precedence would break production, not just this test.
//
// The leading "prog" is required: cli.Command.Run consumes args[0] as the
// program name, so without it the first real argument is silently dropped and
// --db-file never reaches the parser.
func resolveDBFile(t *testing.T, args []string) string {
	t.Helper()
	var got string
	cmd := &cli.Command{
		Flags:  []cli.Flag{db.DBFileFlag()},
		Action: func(_ context.Context, c *cli.Command) error { got = c.String("db-file"); return nil },
	}
	require.NoError(t, cmd.Run(t.Context(), append([]string{"prog"}, args...)))
	return got
}

func TestDBFileFlag_precedence(t *testing.T) {
	tests := []struct {
		name string
		env  *string
		args []string
		want string
	}{
		{"neither flag nor env falls back to the default", nil, nil, db.DefaultDBFile},
		{"env supplies the path when no flag is given", ptr("/tmp/from-env.db"), nil, "/tmp/from-env.db"},
		{"flag wins over env", ptr("/tmp/from-env.db"), []string{"--db-file", "/tmp/from-flag.db"}, "/tmp/from-flag.db"},

		// These two pin the behaviour internal/db/db.go documents in its
		// missing-DSN error: a blank DB_FILE does NOT fall back to the default,
		// and an explicitly empty --db-file does not fall back either. Without
		// them a future urfave/cli release that started trimming empty env
		// values would silently change documented behaviour with a green suite.
		{"blank env does not fall back to the default", ptr(""), nil, ""},
		{"explicit empty flag does not fall back to env", ptr("/tmp/from-env.db"), []string{"--db-file", ""}, ""},
		{"whitespace env is preserved for InitDB to reject", ptr("   "), nil, "   "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// t.Setenv registers a cleanup restoring the original value, so
			// unsetting afterwards is safe and leaves no env leakage.
			t.Setenv(db.EnvDBFile, "")
			if tc.env == nil {
				require.NoError(t, os.Unsetenv(db.EnvDBFile))
			} else {
				require.NoError(t, os.Setenv(db.EnvDBFile, *tc.env))
			}
			assert.Equal(t, tc.want, resolveDBFile(t, tc.args))
		})
	}
}

func ptr(s string) *string { return &s }

// The default is a relative path, so what it resolves to depends on the working
// directory. That is exactly the failure the db-file/Makefile drift caused:
// sqlite creates a missing file on demand, so the server started cleanly against
// the wrong database and only failed later at query time. Logging the absolute
// path makes a wrong working directory visible in the first log line.
func TestInitDB_logsAbsolutePath(t *testing.T) {
	t.Chdir(t.TempDir())

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	database, err := db.InitDB("relative.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	assert.Contains(t, logs.String(), "relative.db")
	assert.NotContains(t, logs.String(), "dsn=relative.db",
		"logging only the relative path hides which file was actually opened")
}

// structure.sql is read from disk rather than //go:embed'd: nothing in
// production uses it, only tests and the dev-only init command do.
func TestStructureSQL_readsFromDisk(t *testing.T) {
	schema, err := db.StructureSQL()
	require.NoError(t, err)

	assert.Contains(t, schema, "CREATE TABLE checkins")
	assert.Contains(t, schema, "CREATE TABLE events")
	// fiber_storage belongs to the session store, which creates its own table
	// on first use. It is not part of the reviewed schema snapshot.
	assert.NotContains(t, schema, "fiber_storage")
}

// The test database is built through the same code path as production, so it
// cannot silently diverge on connection settings -- the hand-written DSN
// literal it used to carry was a second copy of these params.
func TestPrepareTestDB_matchesInitDBConnectionSettings(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err)
	t.Cleanup(cleanup)

	var fkOn int
	require.NoError(t, testDB.QueryRow(`PRAGMA foreign_keys`).Scan(&fkOn))
	assert.Equal(t, 1, fkOn, "test DB must enforce foreign keys like production")

	var timeout int
	require.NoError(t, testDB.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout))
	assert.Equal(t, 5000, timeout, "test DB must use the production busy_timeout")
}
