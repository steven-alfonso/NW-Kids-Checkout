package db

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// migrationsDir is the repository's db/migrations directory, resolved from the
// working directory. It is only used by the dev-only db-init command and its
// tests, never in production.
func migrationsDir() string {
	if wd, err := os.Getwd(); err == nil {
		dir := wd
		for {
			candidate := filepath.Join(dir, "db", "migrations")
			if st, err := os.Stat(candidate); err == nil && st.IsDir() {
				return candidate
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return filepath.Join("db", "migrations")
}

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
	names, err := filepath.Glob(filepath.Join(migrationsDir(), "*.up.sqlite"))
	if err != nil {
		return "", fmt.Errorf("list migrations: %w", err)
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no migrations found in %s", migrationsDir())
	}

	// Glob sorts, and the fixed-width version prefix sorts the same way the
	// filenames do -- the same assumption `make db-migrate` makes with
	// `ls | sort | tail -1` -- so the last entry is the newest migration.
	newest := names[len(names)-1]
	match := migrationVersion.FindStringSubmatch(filepath.Base(newest))
	if match == nil {
		return "", fmt.Errorf("migration %q has no version prefix", filepath.Base(newest))
	}
	return match[1], nil
}
