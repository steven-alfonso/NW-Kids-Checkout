package db

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureDSNParam(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		key  string
		val  string
		want string
	}{
		{"plain path no query", "database/kids-checkin.db", "_foreign_keys", "on", "database/kids-checkin.db?_foreign_keys=on"},
		{"already has query", "file.db?cache=shared", "_foreign_keys", "on", "file.db?cache=shared&_foreign_keys=on"},
		{"already has key exact", "file.db?_foreign_keys=on", "_foreign_keys", "on", "file.db?_foreign_keys=on"},
		{"false positive path substring must still add", "/tmp/my_foreign_keys_backup.db", "_foreign_keys", "on", "/tmp/my_foreign_keys_backup.db?_foreign_keys=on"},
		{"false positive busy_timeout in path", "/tmp/my_busy_timeout.db", "_busy_timeout", "5000", "/tmp/my_busy_timeout.db?_busy_timeout=5000"},
		{"memory shared", "file::memory:?cache=shared", "_busy_timeout", "5000", "file::memory:?cache=shared&_busy_timeout=5000"},
		{"trailing question with no query", "file.db?", "_foreign_keys", "on", "file.db?_foreign_keys=on"},
		{"has different value respects existing", "file.db?_foreign_keys=off", "_foreign_keys", "on", "file.db?_foreign_keys=off"},
		{"idempotent second call", "file.db?cache=shared&_foreign_keys=on", "_foreign_keys", "on", "file.db?cache=shared&_foreign_keys=on"},
		{"multiple existing params", "file.db?cache=shared&mode=memory", "_txlock", "immediate", "file.db?cache=shared&mode=memory&_txlock=immediate"},
		{"key as substring of other key", "file.db?_foreign_keys2=on", "_foreign_keys", "on", "file.db?_foreign_keys2=on&_foreign_keys=on"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ensureDSNParam(tc.dsn, tc.key, tc.val)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEnsureDSNParam_chainedInjection(t *testing.T) {
	// Simulate the InitDB loop. The per-call cases are covered by
	// TestEnsureDSNParam; what matters here is that an already-present key
	// survives the chain while the missing ones are still appended in order.
	dsn := "file.db?_foreign_keys=on"
	for _, kv := range [][2]string{
		{"_foreign_keys", "on"},
		{"_busy_timeout", "5000"},
		{"_txlock", "immediate"},
	} {
		dsn = ensureDSNParam(dsn, kv[0], kv[1])
	}
	assert.Equal(t, "file.db?_foreign_keys=on&_busy_timeout=5000&_txlock=immediate", dsn)
}

func TestInitDB_injectedDSNEnforcesForeignKeys(t *testing.T) {
	// Verify the injected DSN actually results in FK enforcement (end-to-end)
	db, err := InitDB("file::memory:?cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Create parent/child tables to test FK enforcement
	_, err = db.Exec(`CREATE TABLE parent (id INTEGER PRIMARY KEY); CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER, FOREIGN KEY(parent_id) REFERENCES parent(id));`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO child (parent_id) VALUES (999)`)
	require.Error(t, err, "foreign key violation should be enforced via DSN _foreign_keys=on")

	// Also verify busy_timeout is set (non-zero)
	var timeout int
	err = db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout)
	require.NoError(t, err)
	assert.Equal(t, 5000, timeout)
}

func TestInitDB_respectsExistingParams(t *testing.T) {
	// If the caller already supplies _foreign_keys=off, InitDB must not override
	// it (the ensureDSNParam "already present" check), and must still inject the
	// params the caller did not supply.
	db, err := InitDB("file::memory:?cache=shared&_foreign_keys=off")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var fkOn int
	require.NoError(t, db.QueryRow(`PRAGMA foreign_keys`).Scan(&fkOn))
	assert.Equal(t, 0, fkOn, "existing _foreign_keys=off should be respected")

	var timeout int
	require.NoError(t, db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout))
	assert.Equal(t, 5000, timeout, "missing params should still be injected")
}

// TestResolveDSN_leavesFileURIsAlone pins that absolutizing a path never
// rewrites a SQLite URI. mattn/go-sqlite3 treats a leading "file:" as a URI, so
// "file:foo.db" must survive intact; prepending the working directory buries the
// prefix mid-path, the driver stops recognising it, and SQLite creates a file
// literally named "file:foo.db".
func TestResolveDSN_leavesFileURIsAlone(t *testing.T) {
	for _, dsn := range []string{
		"file:foo.db",
		"file:/abs/foo.db",
		"file:./foo.db?cache=shared",
		"file::memory:",
		"file::memory:?cache=shared",
	} {
		t.Run(dsn, func(t *testing.T) {
			assert.Equal(t, dsn, ResolveDSN(dsn))
		})
	}
}

func TestResolveDSN_absolutizesPlainPaths(t *testing.T) {
	got := ResolveDSN("database/kids-checkin.db")
	assert.True(t, filepath.IsAbs(got), "a plain path should be made absolute so the log names the file actually opened: %s", got)
	assert.True(t, strings.HasSuffix(got, filepath.Join("database", "kids-checkin.db")))
}

// A DSN that already carries query params is still a file path, and the log
// line still has to name the file actually opened.
//
// This is not hypothetical: InitDB appends _foreign_keys/_busy_timeout/_txlock
// to such a DSN rather than replacing them, so "--db-file 'data.db?_txlock=deferred'"
// is a supported input. Bailing out on the "?" left those DSNs relative, so the
// one log line meant to make a wrong working directory visible stayed relative
// for exactly the inputs a caller had thought about hardest.
func TestResolveDSN_absolutizesPathsThatCarryQueryParams(t *testing.T) {
	for _, dsn := range []string{
		"data.db?_txlock=deferred",
		"database/kids-checkin.db?_busy_timeout=1000",
		"data.db?",
	} {
		t.Run(dsn, func(t *testing.T) {
			got := ResolveDSN(dsn)
			path, query, _ := strings.Cut(got, "?")
			assert.True(t, filepath.IsAbs(path),
				"the path part must be absolute even when the DSN carries params: %s", got)
			assert.True(t, strings.HasSuffix(path, "data.db") || strings.HasSuffix(path, "kids-checkin.db"),
				"the filename must survive absolutizing: %s", path)

			// The query must round-trip verbatim, including the empty one behind a
			// bare trailing "?": ensureDSNParam treats an empty query specially, so
			// rewriting "data.db?" to "data.db" would change how params get appended.
			_, wantQuery, _ := strings.Cut(dsn, "?")
			assert.Equal(t, wantQuery, query)
		})
	}
}

// Absolutizing a DSN with no path at all would produce the working directory
// followed by a query string, which is not a database. Leave it alone.
func TestResolveDSN_leavesQueryWithoutAPathAlone(t *testing.T) {
	for _, dsn := range []string{"?_busy_timeout=1000"} {
		assert.Equal(t, dsn, ResolveDSN(dsn))
	}
}

// TestResolveDSN_matchesInitDBAndServer pins the two properties callers depend
// on: InitDB logs the same path ResolveDSN reports, and it is idempotent,
// because server.go resolves once and hands the result to both the app database
// and the session store.
func TestResolveDSN_matchesInitDBAndServer(t *testing.T) {
	t.Run("InitDB logs the path ResolveDSN reports", func(t *testing.T) {
		t.Chdir(t.TempDir())

		var logs bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
		t.Cleanup(func() { slog.SetDefault(prev) })

		database, err := InitDB("relative.db")
		require.NoError(t, err)
		t.Cleanup(func() { _ = database.Close() })

		// The file InitDB created must be the file ResolveDSN named. If these
		// two ever disagree, the log line is describing something other than
		// what was opened, which is the whole reason the resolution exists.
		_, err = os.Stat(ResolveDSN("relative.db"))
		require.NoError(t, err, "the path InitDB opened must be the one ResolveDSN reports")
		assert.Contains(t, logs.String(), ResolveDSN("relative.db"))
	})

	t.Run("is idempotent", func(t *testing.T) {
		once := ResolveDSN("database/kids-checkin.db")
		assert.Equal(t, once, ResolveDSN(once), "re-resolving an absolute path must not change it")
	})

	t.Run("leaves non-file DSNs alone", func(t *testing.T) {
		for _, dsn := range []string{"file::memory:", "file:foo.db", "file::memory:?cache=shared"} {
			assert.Equal(t, dsn, ResolveDSN(dsn))
		}
	})

	t.Run("the default names exactly the file the constant does", func(t *testing.T) {
		// server.go resolves the path once and hands the result to both the app
		// database and the session store, so the resolution has to land on the
		// file the constant names -- no query string bolted on, nothing
		// re-pointed. filepath.Abs is the oracle here rather than a second call
		// to ResolveDSN, which would compare a value against itself.
		want, err := filepath.Abs(DefaultDBFile)
		require.NoError(t, err)

		got := ResolveDSN(DefaultDBFile)
		assert.Equal(t, want, got)
		assert.NotContains(t, got, "?", "the default must resolve to a bare file path, not a DSN: %s", got)
	})
}

// TestInitDB_rejectsBlankDSN covers whitespace as well as empty. "   " is not a
// usable DSN, and without this it became a file whose name is three spaces,
// created silently under an authoritative-looking absolute path in the log.
func TestInitDB_rejectsBlankDSN(t *testing.T) {
	for _, dsn := range []string{"", " ", "   ", "\t", "\n"} {
		t.Run(strconv.Quote(dsn), func(t *testing.T) {
			_, err := InitDB(dsn)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "missing database DSN")
		})
	}
}

func TestResolveDSN_memorySubstringIsAFile(t *testing.T) {
	// "backup-:memory:.db" contains the substring but is a real filename and
	// must be absolutized, not passed through as memory.
	for _, dsn := range []string{"backup-:memory:.db", "my-:memory:-backup.db?_txlock=deferred"} {
		got := ResolveDSN(dsn)
		path, _, _ := strings.Cut(got, "?")
		assert.True(t, filepath.IsAbs(path), "substring match must not bypass absolutizing: %s -> %s", dsn, got)
	}
	for _, dsn := range []string{":memory:", ":memory:?cache=shared", "file::memory:", "file::memory:?cache=shared"} {
		assert.Equal(t, dsn, ResolveDSN(dsn), "true memory DSNs must pass through")
	}
}

func TestInitDB_trimsPaddedPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.MkdirAll("database", 0o755))

	database, err := InitDB("  database/kids-checkin.db  ")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	_, err = os.Stat(filepath.Join(dir, "database", "kids-checkin.db"))
	require.NoError(t, err, "padded path must open the trimmed file, not a junk spaced filename")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), " ", "must not create a spaced junk file or dir in %s", dir)
	}
}

func TestInitDB_createsParentDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	database, err := InitDB(filepath.Join("nested", "deep", "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	_, err = os.Stat(filepath.Join(dir, "nested", "deep", "test.db"))
	require.NoError(t, err, "InitDB must mkdir the parent; sqlite creates files, never directories")
}

func TestInitDB_rejectsTildefulPath(t *testing.T) {
	_, err := InitDB("~/data.db")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not expanded")
}
