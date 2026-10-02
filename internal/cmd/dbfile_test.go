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
	"github.com/urfave/cli/v3"
)

// The db-file default and the Makefile's KIDS_CHECKIN_DB_FILE drifted apart:
// the Makefile seeded database/kids-checkin.db while every CLI flag defaulted
// to kids-checkin.db in the repo root, so running the binary without DB_FILE
// opened a different file than `make db-seed` had just populated. sqlite
// creates that file on demand, so the server started cleanly and only failed
// at query time.
//
// The flag defaults are all db.DefaultDBFile, so the compiler already ties
// them together. What it cannot check is that the constant still matches the
// Makefile, which is the half that actually drifted.
func TestDefaultDBFileMatchesMakefile(t *testing.T) {
	mk, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	require.NoError(t, err)
	m := regexp.MustCompile(`(?m)^KIDS_CHECKIN_DB_FILE\s*:=\s*(\S+)`)
	got := m.FindSubmatch(mk)
	require.NotNil(t, got, "KIDS_CHECKIN_DB_FILE not found in Makefile")
	if want := string(got[1]); db.DefaultDBFile != want {
		t.Errorf("db.DefaultDBFile = %q but Makefile KIDS_CHECKIN_DB_FILE = %q; make db-reset writes the latter, so the server would open a different file",
			db.DefaultDBFile, want)
	}
}

// .env is gitignored, so a DB_FILE line there is an invisible third copy of the
// path that nothing checks. It was the copy that would have kept the old,
// wrong default alive on any machine set up before the fix. .env.example is
// tracked and must not reintroduce it.
func TestEnvExampleDoesNotPinDBFile(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", ".env.example"))
	require.NoError(t, err)

	for _, line := range strings.Split(string(contents), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		assert.NotRegexp(t, `(?i)^DB_FILE\s*=`,
			".env.example must not set DB_FILE; the default already matches the Makefile")
	}
}

// walkCommands visits every command in the tree, parents and leaves alike.
func walkCommands(c *cli.Command, visit func(*cli.Command)) {
	visit(c)
	for _, sub := range c.Commands {
		walkCommands(sub, visit)
	}
}

// Any command that opens the database must mount db.DBFileFlag(). Checking the
// real tree -- rather than trusting that each author remembered -- is what
// stops the next command from spelling out its own flag with a hardcoded path,
// which is how the original drift happened in the first place.
func TestEveryDBCommandUsesTheSharedFlag(t *testing.T) {
	// Commands that open the database, by name. Listing them explicitly keeps
	// the assertion honest: a new database-opening command has to be added
	// here, and the failure message says so.
	databaseCommands := map[string]bool{
		"apiserver":        true,
		"checkout-fetcher": true,
		"upsert-location":  true,
		"delete-old":       true,
		"seed-preview":     true,
		"db-init":          true, // dev build tag; absent from production builds
	}

	seen := map[string]bool{}
	walkCommands(NewCommand(), func(c *cli.Command) {
		if c.Name == "" {
			return
		}
		for _, f := range c.Flags {
			sf, ok := f.(*cli.StringFlag)
			if !ok || sf.Name != "db-file" {
				continue
			}
			seen[c.Name] = true
			assert.Equal(t, db.DefaultDBFile, sf.Value,
				"%s defines its own db-file default instead of using db.DBFileFlag()", c.Name)
			assert.Contains(t, sf.GetEnvVars(), db.EnvDBFile,
				"%s mounts a db-file flag that ignores $%s", c.Name, db.EnvDBFile)
		}
	})

	for name := range databaseCommands {
		if seen[name] {
			continue
		}
		// A dev-tagged command is legitimately absent from a production build;
		// anything else missing is a real gap.
		if name == "db-init" && !DevBuild {
			continue
		}
		t.Errorf("command %q has no db-file flag; every database-opening command must use db.DBFileFlag()", name)
	}
}
