package db

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
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

// TestInitDB_logsAbsolutePath pins that the log names the file actually opened.
// The default is relative, so a wrong working directory is otherwise invisible
// until query time.
func TestInitDB_logsAbsolutePath(t *testing.T) {
	t.Chdir(t.TempDir())

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	database, err := InitDB("relative.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	want, err := filepath.Abs("relative.db")
	require.NoError(t, err)
	_, err = os.Stat(want)
	require.NoError(t, err, "InitDB must open the absolute path it logs")
	assert.Contains(t, logs.String(), want)
	assert.NotContains(t, logs.String(), "dsn=relative.db",
		"logging only the relative path hides which file was actually opened")
}

// TestInitDB_rejectsBlankDSN covers whitespace as well as empty. "   " is not a
// usable DSN.
func TestInitDB_rejectsBlankDSN(t *testing.T) {
	for _, dsn := range []string{"", " ", "   ", "\t", "\n"} {
		t.Run(strconv.Quote(dsn), func(t *testing.T) {
			_, err := InitDB(dsn)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "missing database DSN")
		})
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
