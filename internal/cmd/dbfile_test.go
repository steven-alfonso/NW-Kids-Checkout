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
	m := regexp.MustCompile(`(?m)^\s*KIDS_CHECKIN_DB_FILE\s*[:?+]?=\s*(\S+)`)
	all := m.FindAllSubmatch(mk, -1)
	require.NotEmpty(t, all, "KIDS_CHECKIN_DB_FILE not found in Makefile")
	for _, got := range all {
		if want := string(got[1]); db.DefaultDBFile != want {
			t.Errorf("db.DefaultDBFile = %q but Makefile KIDS_CHECKIN_DB_FILE = %q; make db-reset writes the latter, so the server would open a different file",
				db.DefaultDBFile, want)
		}
	}
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
		assert.NotRegexp(t, `(?i)^(export\s+)?DB_FILE\s*=`, strings.TrimSpace(line),
			".env.example line %d must not set DB_FILE; the default already matches the Makefile", i+1)
	}
}
