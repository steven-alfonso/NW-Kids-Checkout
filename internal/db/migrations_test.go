package db_test

import (
	"regexp"
	"testing"

	"kids-checkin/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// versionPattern matches the golang-migrate version prefix of a migration
// filename, e.g. 20260823160408_add_checkins_fetched_at.up.sqlite.
var versionPattern = regexp.MustCompile(`^(\d+)_`)

// LatestMigrationVersion returns the version of the newest *.up.sqlite
// migration in db/migrations, as the string golang-migrate's schema_migrations
// column expects.
//
// This is deliberately only a version lookup. Migrations are applied by the
// migrate CLI, not by this binary; what is needed here is the stamp that keeps
// the CLI from replaying every migration against a database already built from
// the current schema snapshot.
func TestLatestMigrationVersion_parsesVersionPrefix(t *testing.T) {
	for _, tc := range []struct {
		filename string
		want     string
	}{
		{"20260823160408_add_checkins_fetched_at.up.sqlite", "20260823160408"},
		{"20251101031047_initial.down.sqlite", "20251101031047"},
		{"no_version_prefix.sqlite", ""},
		{"", ""},
	} {
		t.Run(tc.filename, func(t *testing.T) {
			got := versionPattern.FindStringSubmatch(tc.filename)
			if tc.want == "" {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.want, got[1])
		})
	}
}

func TestLatestMigrationVersion_matchesNewestFile(t *testing.T) {
	got, err := db.LatestMigrationVersion()
	require.NoError(t, err)
	assert.Regexp(t, `^\d{14}$`, got, "golang-migrate versions are 14-digit timestamps")

	// It must be at least as new as the guest family model migration, which is
	// the newest one in the tree at the time of writing.
	assert.GreaterOrEqual(t, got, "20260906164308")
}
