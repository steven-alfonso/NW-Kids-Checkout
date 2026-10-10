//go:build dev

package dbinit_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"kids-checkin/internal/cmd/dbinit"
	"kids-checkin/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// run executes the command the way the binary would, so the flag wiring and the
// dev gate are both exercised. The leading "prog" is consumed by Run as the
// program name.
//
// An env value of "" means "unset", not "empty": these tests run under
// `godotenv`, which loads .env and sets ENVIRONMENT=dev, so leaving the
// variable alone would not test what it looks like it tests.
func run(t *testing.T, env map[string]string, args ...string) error {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
		if v == "" {
			require.NoError(t, os.Unsetenv(k))
		}
	}
	cmd := &cli.Command{
		Flags:  dbinit.Flags(),
		Action: dbinit.Run,
	}
	return cmd.Run(context.Background(), append([]string{"prog"}, args...))
}

// devEnv is the environment `make db-init` runs under.
var devEnv = map[string]string{"ENVIRONMENT": "dev"}

func initDBAt(t *testing.T, path string, args ...string) {
	t.Helper()
	require.NoError(t, run(t, devEnv, append([]string{"--db-file", path}, args...)...))
}

// The command ships fake kids' check-in data. It must not run in production,
// and the build tag is only half of that: a dev build deployed by mistake still
// has to refuse.
func TestRun_refusesOutsideDev(t *testing.T) {
	for _, env := range []map[string]string{
		{"ENVIRONMENT": "production"},
		{"ENVIRONMENT": ""}, // unset
	} {
		t.Run("ENVIRONMENT="+env["ENVIRONMENT"], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "prod.db")
			err := run(t, env, "--db-file", path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "development")
			assert.NoFileExists(t, path, "the gate must fire before the database is created")
		})
	}
}

func TestRun_createsSchemaAndTopsology(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dev.db")
	initDBAt(t, path)

	database, err := db.InitDB(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	counts := map[string]int{}
	for _, table := range []string{"location_groups", "events", "locations", "event_check_windows"} {
		var n int
		require.NoError(t, database.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&n))
		counts[table] = n
	}
	assert.Equal(t, map[string]int{
		"location_groups": 3, "events": 2, "locations": 33, "event_check_windows": 2,
	}, counts, "the fixture is the real Planning Center topology, ported verbatim")

	// Real Planning Center ids are the whole reason this fixture is checked in.
	var pcid string
	require.NoError(t, database.QueryRow(
		`SELECT planning_center_id FROM events WHERE name = 'Weekend Experience'`).Scan(&pcid))
	assert.Equal(t, "151353", pcid)

	// The room hierarchy is what makes the parent/child location tree and the
	// check-window fetcher exercisable.
	var parent string
	require.NoError(t, database.QueryRow(
		`SELECT planning_center_parent_id FROM locations WHERE planning_center_id = '295919'`).Scan(&parent))
	assert.Equal(t, "295939", parent)

	// Per-visit data is random-data's job, not this command's.
	for _, table := range []string{"children", "parents", "checkins", "guest_submissions", "manual_checkins"} {
		var n int
		require.NoError(t, database.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&n))
		assert.Zero(t, n, "%s should stay empty; use `make random-data` for it", table)
	}
}

// The snapshot in db/structure.sql deliberately omits schema_migrations, and
// `make db-migrate` drives the real migrate CLI. Without a stamp the CLI would
// try to replay every migration on top of an already-current schema.
//
// The expected version is read off disk here rather than taken from
// db.LatestMigrationVersion(). That function is what the command calls, so asking
// it what it ought to return checks nothing: flipping its glob to return the
// OLDEST migration left this test green while `migrate up` on the resulting
// database failed with "index idx_location_groups_name already exists" and left
// schema_migrations dirty, which blocks every later migration. Deriving the
// expectation from the migration files themselves is what makes this a real
// check.
func TestRun_stampsSchemaMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dev.db")
	initDBAt(t, path)

	database, err := db.InitDB(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	var version uint64
	require.NoError(t, database.QueryRow(`SELECT version FROM schema_migrations`).Scan(&version))

	var dirty bool
	require.NoError(t, database.QueryRow(`SELECT dirty FROM schema_migrations`).Scan(&dirty),
		"a dirty stamp blocks every subsequent `migrate up`")
	assert.False(t, dirty, "db-init must leave schema_migrations clean")

	want := newestMigrationVersionOnDisk(t)
	assert.Equal(t, want, version, "the stamp must match the newest migration in db/migrations")
}

// newestMigrationVersionOnDisk reads the version prefix of the newest up-migration
// directly from db/migrations, independently of db.LatestMigrationVersion.
//
// The prefix is everything before the first underscore, which is the
// golang-migrate convention: 20260823160408_add_checkins.up.sqlite.
func newestMigrationVersionOnDisk(t *testing.T) uint64 {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(repoRoot(t), "db", "migrations", "*.up.sqlite"))
	require.NoError(t, err)
	require.NotEmpty(t, matches, "no up-migrations found in db/migrations")

	// Glob sorts, and the version prefix sorts the same way the filenames do,
	// so the last entry is the newest.
	base := filepath.Base(matches[len(matches)-1])
	prefix, _, ok := strings.Cut(base, "_")
	require.True(t, ok, "migration %q has no version prefix", base)
	require.Regexp(t, `^\d{14}$`, prefix, "golang-migrate versions are 14-digit timestamps: %q", prefix)

	version, err := strconv.ParseUint(prefix, 10, 64)
	require.NoError(t, err)
	return version
}

// repoRoot walks up from the package directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	for dir := wd; ; {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return wd
		}
		dir = parent
	}
}

// Re-running against a live database would silently duplicate or overwrite
// development data, so it takes an explicit --force, matching checkins
// seed-preview.
func TestRun_refusesToClobberWithoutForce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dev.db")
	initDBAt(t, path)

	err := run(t, devEnv, "--db-file", path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force")

	// With --force it succeeds and the topology is still intact.
	initDBAt(t, path, "--force")
	database, err := db.InitDB(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	var n int
	require.NoError(t, database.QueryRow(`SELECT COUNT(*) FROM locations`).Scan(&n))
	assert.Equal(t, 33, n)
}

// database/ is gitignored, so it is absent on a fresh clone. sqlite3 creates a
// missing file but never a missing directory, so without the MkdirAll in InitDB the
// documented first-run path fails outright.
func TestRun_createsTheDatabaseDirectory(t *testing.T) {
	// A nested path whose parent does not exist, which is the fresh-clone case.
	path := filepath.Join(t.TempDir(), "database", "kids-checkin.db")
	require.NoDirExists(t, filepath.Dir(path))

	initDBAt(t, path)

	assert.FileExists(t, path, "db-init should have created the database directory and the file")
}
