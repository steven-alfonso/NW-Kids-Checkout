package db

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// migrationsGlob matches the up-migrations in db/migrations.
const migrationsGlob = "migrations/*.up.sqlite"

// migrationVersion matches the golang-migrate version prefix of a migration
// filename, e.g. the 20260823160408 in 20260823160408_add_checkins.up.sqlite.
var migrationVersion = regexp.MustCompile(`^(\d+)_`)

// LatestMigrationVersion returns the version of the newest up-migration in
// db/migrations, formatted the way the schema_migrations.version column
// expects.
//
// This is only a version lookup, not a migration runner: migrations are applied
// by the migrate CLI. What callers need is the stamp that keeps that CLI from
// replaying every migration against a database already built from the current
// schema snapshot.
func LatestMigrationVersion() (string, error) {
	dir := filepath.Join(dbDir(), "migrations")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("migrations directory not found at %s: %w", dir, err)
	}
	names, err := filepath.Glob(filepath.Join(dbDir(), migrationsGlob))
	if err != nil {
		return "", fmt.Errorf("list migrations: %w", err)
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no migrations found in %s", dir)
	}

	// Take the max over all files, comparing by (length, lexicographic) so a
	// short non-timestamp name like "9_hotfix" cannot beat a 14-digit
	// timestamp: lexicographic "9" > "2" would otherwise win. All real
	// migrations are 14 digits, where this coincides with numeric order.
	var newest string
	for _, name := range names {
		match := migrationVersion.FindStringSubmatch(filepath.Base(name))
		if match == nil {
			continue
		}
		if longerOrGreater(match[1], newest) {
			newest = match[1]
		}
	}
	if newest == "" {
		return "", fmt.Errorf("no versioned migrations in %s", dir)
	}
	return newest, nil
}

// longerOrGreater reports whether a sorts after b by (length, lexicographic).
// Migration versions are zero-padded timestamps, so longer means newer across
// widths and lexicographic decides within a width.
func longerOrGreater(a, b string) bool {
	if len(a) != len(b) {
		return len(a) > len(b)
	}
	return a > b
}
