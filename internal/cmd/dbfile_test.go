package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"kids-checkin/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The flag defaults are db.DefaultDBFile, so the compiler already ties them
// together. What it cannot check is that the constant still matches the
// Makefile -- the half that actually drifted, and the half that let a run
// without DB_FILE open a different file than `make db-reset` had written.
func TestDefaultDBFileMatchesMakefile(t *testing.T) {
	mk, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	require.NoError(t, err)
	// All assignments are checked: make uses last-wins, so checking only the
	// first would stay green while `make db-reset` writes a second value.
	// Target-specific assignments (`db-reset: VAR = ...`) do not match this
	// regex; TestDefaultDBFileNoTargetSpecificOverride below asserts their
	// absence, since the database path must stay a global.
	m := regexp.MustCompile(`(?m)^\s*(?:export\s+)?KIDS_CHECKIN_DB_FILE\s*[:?+]?=\s*(\S+)`)
	all := m.FindAllSubmatch(mk, -1)
	require.NotEmpty(t, all, "KIDS_CHECKIN_DB_FILE not found in Makefile")
	for _, got := range all {
		if want := string(got[1]); db.DefaultDBFile != want {
			t.Errorf("db.DefaultDBFile = %q but Makefile KIDS_CHECKIN_DB_FILE = %q; make db-reset writes the latter, so the server would open a different file",
				db.DefaultDBFile, want)
		}
	}
}

// TestDefaultDBFileNoTargetSpecificOverride pins that the path stays a
// global. A target-specific assignment (e.g. `db-reset:
// KIDS_CHECKIN_DB_FILE = ...`) would not match the global regex above, so
// the drift it reintroduces would stay green without this guard.
func TestDefaultDBFileNoTargetSpecificOverride(t *testing.T) {
	mk, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	require.NoError(t, err)
	targetSpecific := regexp.MustCompile(`(?m)^[^:#\s][^:]*:\s*(?:export\s+)?KIDS_CHECKIN_DB_FILE\s*[:?+]?=`)
	assert.NotRegexp(t, targetSpecific, string(mk),
		"database path must stay a Makefile global; a target-specific override diverges silently")
}

// dbFileLinePattern matches a line that sets $DB_FILE. Case-sensitive: Unix
// env vars are case-sensitive, so a lowercase db_file= line sets nothing.
// Callers pass a TrimSpace'd line: godotenv trims leading whitespace, so an
// indented line counts too.
var dbFileLinePattern = regexp.MustCompile(`^(export\s+)?DB_FILE\s*=`)

// dbFileLineSetsDBFile reports whether an .env.example line sets $DB_FILE.
// Single definition of the guard so the case-sensitivity test below exercises
// the same pattern the file scan uses, instead of a copy that could drift.
func dbFileLineSetsDBFile(line string) bool {
	return dbFileLinePattern.MatchString(strings.TrimSpace(line))
}

// .env is gitignored, so a DB_FILE line there is an invisible third copy of the
// path that nothing checks. It was the copy that would have kept the old,
// wrong default alive on any machine set up before the fix. .env.example is
// tracked and must not reintroduce it.
//
// The export form is matched too: godotenv strips a leading "export", so
// "export DB_FILE=..." would set the variable just as effectively.
func TestEnvExampleDoesNotPinDBFile(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", ".env.example"))
	require.NoError(t, err)

	for i, line := range strings.Split(string(contents), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		// godotenv trims leading whitespace, so an indented line counts too.
		// Case-sensitive: Unix env vars are case-sensitive, so a lowercase
		// db_file= line would not set $DB_FILE and must not fail this test.
		assert.False(t, dbFileLineSetsDBFile(line),
			".env.example line %d must not set DB_FILE; the default already matches the Makefile", i+1)
	}
}

func TestDBFileLineSetsDBFile(t *testing.T) {
	assert.False(t, dbFileLineSetsDBFile("db_file=/tmp/other.db"))
	assert.False(t, dbFileLineSetsDBFile("  db_file = /tmp/other.db"))
	assert.True(t, dbFileLineSetsDBFile("DB_FILE=/tmp/other.db"))
	assert.True(t, dbFileLineSetsDBFile("export DB_FILE=/tmp/other.db"))
	assert.True(t, dbFileLineSetsDBFile("  DB_FILE = /tmp/other.db"),
		"godotenv trims leading whitespace, so an indented line counts too")
	assert.False(t, dbFileLineSetsDBFile("# DB_FILE=/tmp/other.db"))
}
