package db

import (
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
			assert.Equal(t, dsn, resolveDSN(dsn))
		})
	}
}

func TestResolveDSN_absolutizesPlainPaths(t *testing.T) {
	got := resolveDSN("database/kids-checkin.db")
	assert.True(t, filepath.IsAbs(got), "a plain path should be made absolute so the log names the file actually opened: %s", got)
	assert.True(t, strings.HasSuffix(got, filepath.Join("database", "kids-checkin.db")))
}

// TestResolvePath covers the exported entry point the Fiber session store relies
// on. It has to agree with what InitDB opens, and it has to be idempotent,
// because server.go resolves once and hands the result to both connections.
func TestResolvePath(t *testing.T) {
	t.Run("agrees with InitDB", func(t *testing.T) {
		assert.Equal(t, resolveDSN(DefaultDBFile), ResolvePath(DefaultDBFile))
	})

	t.Run("is idempotent", func(t *testing.T) {
		once := ResolvePath("database/kids-checkin.db")
		assert.Equal(t, once, ResolvePath(once), "re-resolving an absolute path must not change it")
	})

	t.Run("leaves non-file DSNs alone", func(t *testing.T) {
		for _, dsn := range []string{"file::memory:", "file:foo.db", "file::memory:?cache=shared"} {
			assert.Equal(t, dsn, ResolvePath(dsn))
		}
	})

	t.Run("the default resolves to one stable path", func(t *testing.T) {
		// Two resolutions of the same default must name the same file, which is
		// the property server.go depends on to open one database rather than two.
		assert.Equal(t, ResolvePath(DefaultDBFile), ResolvePath(DefaultDBFile))
		assert.True(t, filepath.IsAbs(ResolvePath(DefaultDBFile)))
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
