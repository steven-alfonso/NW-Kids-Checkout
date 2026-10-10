package db_test

import (
	"path/filepath"
	"strings"
	"testing"

	"kids-checkin/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LatestMigrationVersion returns the version of the newest *.up.sqlite
// migration in db/migrations, as the string golang-migrate's schema_migrations
// column expects.
//
// This is deliberately only a version lookup. Migrations are applied by the
// migrate CLI, not by this binary; what is needed here is the stamp that keeps
// the CLI from replaying every migration against a database already built from
// the current schema snapshot.
//
// The expected value is derived here from the files actually on disk rather than
// from a second copy of the version regex. A test that re-declared the regex
// would assert its own literals against itself: it would still pass if the
// implementation's glob stopped matching, or if its regex were deleted outright.
func TestLatestMigrationVersion_matchesNewestFile(t *testing.T) {
	got, err := db.LatestMigrationVersion()
	require.NoError(t, err)
	assert.Regexp(t, `^\d{14}$`, got, "golang-migrate versions are 14-digit timestamps")

	names, err := filepath.Glob(filepath.Join("..", "..", "db", "migrations", "*.up.sqlite"))
	require.NoError(t, err)
	require.NotEmpty(t, names, "no migrations found to compare against")

	var newest string
	for _, name := range names {
		prefix, _, ok := strings.Cut(filepath.Base(name), "_")
		require.True(t, ok, "migration %q has no version prefix", name)
		assert.Regexp(t, `^\d{14}$`, prefix, "migration %q", name)
		if prefix > newest {
			newest = prefix
		}
	}
	assert.Equal(t, newest, got, "must return the newest migration actually on disk")

	// It must be at least as new as the guest family model migration, which is
	// the newest one in the tree at the time of writing.
	assert.GreaterOrEqual(t, got, "20260906164308")
}
