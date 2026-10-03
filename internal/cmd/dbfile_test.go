package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"kids-checkin/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// The flag defaults are db.DefaultDBFile, so the compiler already ties them
// together. What it cannot check is that the constant still matches the
// Makefile -- the half that actually drifted, and the half that let a run
// without DB_FILE open a different file than `make db-reset` had written.
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
		assert.NotRegexp(t, `(?i)^(export\s+)?DB_FILE\s*=`, line,
			".env.example line %d must not set DB_FILE; the default already matches the Makefile", i+1)
	}
}

// walkCommands visits every command in the tree, parents and leaves alike.
func walkCommands(c *cli.Command, visit func(*cli.Command)) {
	visit(c)
	for _, sub := range c.Commands {
		walkCommands(sub, visit)
	}
}

// Any command that opens the database must mount db.DBFileFlag(), so that a
// db-file flag can never carry a default that disagrees with the Makefile.
//
// Scope note: this walks the live command tree, so it covers every command
// registered under internal/cmd -- but it cannot see commands that are separate
// main packages (cmd/random-data), and it cannot notice a command that opens
// the database without declaring a db-file flag at all.
// TestDBInitCallSiteDoesNotHardcodePath covers both of those.
func TestEveryDBCommandUsesTheSharedFlag(t *testing.T) {
	// Commands that open the database, by name. This is the command-level
	// backstop: it catches a command losing its flag entirely, which the walk
	// below cannot see because there is no flag left to inspect.
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

// TestDBInitCallSiteDoesNotHardcodePath is the structural guard, and it exists
// because a name-based list cannot be exhaustive: nothing forces the next
// database-opening command to be added to one. The rule is instead a property
// every call site must satisfy -- the path handed to db.InitDB has to be a
// variable the command read from its flag, never a literal.
//
// This is what makes the original drift unrepeatable. Hardcoding a path in a
// command is precisely how kids-checkin.db ended up diverging from the
// Makefile's database/kids-checkin.db, and it is invisible to the walk above:
// a command with no db-file flag, or a command in a separate main package,
// simply has no flag for that test to inspect.
//
// The argument is checked with the AST rather than by matching source text, so
// a path in a comment or a string literal elsewhere in the file cannot satisfy
// or trip the rule.
func TestDBInitCallSiteDoesNotHardcodePath(t *testing.T) {
	found := 0
	eachGoFile(t, func(fset *token.FileSet, path string, file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "InitDB" {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != "db" {
				return true
			}
			found++
			if len(call.Args) == 0 {
				t.Errorf("%s calls db.InitDB() with no argument; pass the --db-file value", fset.Position(call.Pos()))
				return true
			}
			if lit, isLit := call.Args[0].(*ast.BasicLit); isLit && lit.Kind == token.STRING {
				t.Errorf("%s calls db.InitDB(%q); the path must come from db.DBFileFlag() via cmd.String(%q), not a hardcoded literal",
					fset.Position(call.Pos()), lit.Value, "db-file")
			}
			return true
		})
	})
	assert.Positive(t, found, "no db.InitDB call sites found; this guard would silently pass if the call sites moved or were renamed")
}

// TestDbFileFlagIsNeverRedefined closes the gap the other two guards cannot
// reach: a db-file flag declared as a literal anywhere in the tree.
//
// A per-command copy is exactly what internal/db/flag.go exists to prevent, and
// nothing stopped one from being written:
//
//   - cmd/random-data is a separate main package, so it is absent from the
//     command tree TestEveryDBCommandUsesTheSharedFlag walks. It was also where
//     the original hand-rolled precedence lived.
//   - A test helper that mirrors the flags is also absent from that tree. One
//     shipped holding kids-checkin.db, the pre-fix value, with no $DB_FILE
//     source at all.
//   - TestEveryDBCommandUsesTheSharedFlag asserts that a flag's default and env
//     var are right, so a byte-for-byte inline re-spelling satisfied it. The two
//     copies then diverge the moment either is edited.
//
// This test does not need to enumerate commands: db.DBFileFlag() is the only
// place a db-file flag is allowed to be defined, so forbidding the literal
// everywhere else is complete by construction. It covers test files too, since
// that is where the duplicate above lived.
func TestDbFileFlagIsNeverRedefined(t *testing.T) {
	eachGoFile(t, func(fset *token.FileSet, path string, file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			// Match cli.StringFlag and friends, whether written as
			// cli.StringFlag{...} or &cli.StringFlag{...}.
			name := ""
			switch typ := lit.Type.(type) {
			case *ast.SelectorExpr:
				name = typ.Sel.Name
			case *ast.Ident:
				name = typ.Name
			}
			if !strings.HasSuffix(name, "Flag") {
				return true
			}
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok || key.Name != "Name" {
					continue
				}
				value, ok := kv.Value.(*ast.BasicLit)
				if !ok || value.Kind != token.STRING {
					continue
				}
				// BasicLit.Value for a STRING is the raw source text, quotes
				// included. Comparing it to a bare db-file would never match --
				// which is precisely how this guard would end up passing
				// forever without ever firing.
				unquoted, err := strconv.Unquote(value.Value)
				if err != nil || unquoted != "db-file" {
					continue
				}
				t.Errorf("%s declares its own %s{Name: %q}; mount db.DBFileFlag() instead so there is exactly one definition",
					fset.Position(lit.Pos()), name, unquoted)
			}
			return true
		})
	})
}

// eachGoFile parses every .go file in the repository except internal/db -- which
// owns the flag and deliberately takes literal DSNs -- and hands each to visit.
// Test files are included: a duplicated flag declaration is a bug wherever it
// lives.
func eachGoFile(t *testing.T, visit func(fset *token.FileSet, path string, file *ast.File)) {
	t.Helper()

	root := filepath.Join("..", "..")
	skipDir := map[string]bool{
		"node_modules": true,
		".git":         true,
		".worktrees":   true,
		"tmp":          true,
	}
	dbPackage := filepath.Join("internal", "db") + string(filepath.Separator)

	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if rel, relErr := filepath.Rel(root, path); relErr == nil && strings.HasPrefix(rel, dbPackage) {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		visit(fset, path, file)
		return nil
	})
	require.NoError(t, err)
}
