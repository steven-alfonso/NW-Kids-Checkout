package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFindDBDir covers the walk-up that resolves db/ when the compile-time path
// is unusable. The case that matters most is -trimpath: there runtime.Caller
// reports a module-relative path like kids-checkin/internal/db/schema.go, which
// resolves against the working directory instead of the source tree, so this
// walk is the only thing standing between a trimmed build and 17 failing
// packages.
func TestFindDBDir(t *testing.T) {
	// makeRepo lays out <root>/db/structure.sql and returns root.
	makeRepo := func(t *testing.T, snapshot bool) string {
		t.Helper()
		root := t.TempDir()
		if snapshot {
			require.NoError(t, os.MkdirAll(filepath.Join(root, "db", "migrations"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, "db", "structure.sql"), []byte("CREATE TABLE t (id INTEGER);"), 0o644))
		} else {
			require.NoError(t, os.MkdirAll(filepath.Join(root, "db"), 0o755))
		}
		return root
	}

	t.Run("snapshot sits directly under start", func(t *testing.T) {
		root := makeRepo(t, true)
		got, ok := findDBDir(root)
		require.True(t, ok)
		assert.Equal(t, filepath.Join(root, "db"), got)
	})

	t.Run("walks up from a nested package directory", func(t *testing.T) {
		root := makeRepo(t, true)
		nested := filepath.Join(root, "internal", "repo", "checkin")
		require.NoError(t, os.MkdirAll(nested, 0o755))

		got, ok := findDBDir(nested)
		require.True(t, ok)
		assert.Equal(t, filepath.Join(root, "db"), got)
	})

	t.Run("a db directory without the snapshot is not the target", func(t *testing.T) {
		// Guards against matching on the directory name alone: a stray
		// db/migrations/ must not stop the walk before the real one.
		root := makeRepo(t, true)
		decoy := filepath.Join(root, "internal", "db")
		require.NoError(t, os.MkdirAll(decoy, 0o755))

		got, ok := findDBDir(decoy)
		require.True(t, ok)
		assert.Equal(t, filepath.Join(root, "db"), got, "should skip the decoy and find the real db/")
	})

	t.Run("reports not found when there is no snapshot", func(t *testing.T) {
		root := makeRepo(t, false)
		_, ok := findDBDir(root)
		assert.False(t, ok)
	})
}

// TestFindDBDir_fromThisPackage pins the fallback against the real repository
// rather than a fixture. Whatever working directory the package's tests happen to
// run in, resolution must reach the checked-in db/ directory.
func TestFindDBDir_fromThisPackage(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)

	dir, ok := findDBDir(wd)
	require.True(t, ok, "db/ not found by walking up from %s", wd)

	_, err = os.Stat(filepath.Join(dir, "structure.sql"))
	require.NoError(t, err)
}

// TestStructureSQL_readsRealSnapshot is the end-to-end guard: a resolution that
// silently returns the wrong thing must fail here rather than surface as an
// empty schema deep inside a test database.
func TestStructureSQL_readsRealSnapshot(t *testing.T) {
	schema, err := StructureSQL()
	require.NoError(t, err)
	assert.Contains(t, schema, "CREATE TABLE", "snapshot should contain table definitions")
	assert.True(t, filepath.IsAbs(dbDir()), "dbDir must be absolute so errors name a concrete path, got %q", dbDir())
	assert.Contains(t, schema, "CREATE TABLE checkins", "snapshot should contain the checkins table")
}
