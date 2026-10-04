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
// every call site must satisfy -- the path handed to db.InitDB has to come from
// the command's --db-file flag. Two ways of not doing that are rejected: naming
// the path as a string literal, and naming it as db.DefaultDBFile (directly or
// through a local alias).
//
// The second is not redundant with the first. Rejecting literals alone left a
// hole that needed no alias and no cleverness: a command that mounted no
// --db-file flag at all and passed db.DefaultDBFile reported nothing here, and
// passed the other four guards too. It would have silently ignored --db-file and
// $DB_FILE, which is the drift this branch exists to end.
//
// This is what makes the original drift unrepeatable. Hardcoding a path in a
// command is precisely how kids-checkin.db ended up diverging from the
// Makefile's database/kids-checkin.db, and it is invisible to the walk above:
// a command with no db-file flag, or a command in a separate main package,
// simply has no flag for that test to inspect.
//
// What it does not cover, stated plainly rather than implied: an argument that
// reaches the path by some third shape -- os.Getenv, a helper's return value,
// string concatenation. Detecting those needs dataflow analysis, which a
// single-file parse is not. The guard covers the two spellings that actually
// caused this bug; it does not prove no others exist.
//
// The argument is checked with the AST rather than by matching source text, so
// a path in a comment or a string literal elsewhere in the file cannot satisfy
// or trip the rule.
//
// The receiver is matched by import, not by the name "db". Keying on the
// identifier meant that a single aliased import -- import store "kids-checkin/internal/db"
// -- moved a call site outside this guard entirely, silently reintroducing
// exactly the hardcoded path the guard exists to prevent.
func TestDBInitCallSiteDoesNotHardcodePath(t *testing.T) {
	found := 0
	eachGoFile(t, skipDBPackage, func(fset *token.FileSet, path string, file *ast.File) {
		receiver := dbImportName(file)
		if receiver == "" {
			return
		}
		// Resolved per file so a local alias of db.DefaultDBFile is caught too.
		decls := sameFileDecls(file)
		isTest := strings.HasSuffix(path, "_test.go")
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
			if !ok || ident.Name != receiver {
				return true
			}
			if !isTest {
				found++
			}
			if len(call.Args) == 0 {
				t.Errorf("%s calls %s.InitDB() with no argument; pass the --db-file value",
					fset.Position(call.Pos()), receiver)
				return true
			}
			// Both ways of naming the path without reading the flag are rejected:
			// a string literal, and db.DefaultDBFile however it is spelled. The
			// second is what made the literal-only rule incomplete -- mounting no
			// flag at all and passing the constant passed every guard here.
			//
			// An argument the parse cannot resolve at all is left alone; see the
			// limitations documented on resolveValue.
			value, resolved := resolveValue(call.Args[0], decls)
			switch {
			case !resolved:
			case value == defaultDBFileRef:
				t.Errorf("%s calls %s.InitDB(%s); the path must come from db.DBFileFlag() via cmd.String(%q), not the default constant -- a command that passes this ignores both --db-file and $%s",
					fset.Position(call.Pos()), receiver, defaultDBFileRef, "db-file", db.EnvDBFile)
			default:
				t.Errorf("%s calls %s.InitDB(%q); the path must come from db.DBFileFlag() via cmd.String(%q), not a hardcoded literal",
					fset.Position(call.Pos()), receiver, value, "db-file")
			}
			return true
		})
	})
	// Test files are excluded from the count on purpose. dbinit_test.go alone
	// makes nine InitDB calls, so counting them would let this assertion stay
	// green after every production call site had moved or been renamed -- which
	// is the exact failure the assertion exists to catch.
	assert.Positive(t, found, "no non-test db.InitDB call sites found; this guard would silently pass if the call sites moved, were aliased away, or were renamed")
}

// dbImportName returns the identifier the file uses to refer to
// kids-checkin/internal/db, honouring an alias, or "" if the file does not
// import it.
func dbImportName(file *ast.File) string {
	const dbImportPath = "kids-checkin/internal/db"
	for _, imp := range file.Imports {
		value, err := strconv.Unquote(imp.Path.Value)
		if err != nil || value != dbImportPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "db"
	}
	return ""
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
//
// The flag name is resolved through same-file constant and variable references
// too, not just string literals. `Name: dbFileFlagName` looks nothing like the
// literal this guard forbids, so matching on BasicLit alone left the most
// plausible way to reintroduce a second definition uncaught.
func TestDbFileFlagIsNeverRedefined(t *testing.T) {
	eachGoFile(t, skipFlagOwner, func(fset *token.FileSet, path string, file *ast.File) {
		decls := sameFileDecls(file)
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
				unquoted, ok := resolveValue(kv.Value, decls)
				if !ok || unquoted != "db-file" {
					continue
				}
				t.Errorf("%s declares its own %s{Name: %q}; mount db.DBFileFlag() instead so there is exactly one definition",
					fset.Position(lit.Pos()), name, unquoted)
			}
			return true
		})
	})
}

// defaultDBFileRef is the denotation resolveValue assigns to any expression that
// reads db.DefaultDBFile, whichever package alias the file imported it under. It
// is deliberately not a plausible string value, so it can never collide with a
// real path.
const defaultDBFileRef = "db.DefaultDBFile"

// resolveValue resolves an expression to the thing it denotes: a string literal's
// value, or defaultDBFileRef for a read of the db.DefaultDBFile constant, either
// directly or through an identifier bound to one by a const or var declaration in
// the same file. ok is false for anything else, including an identifier declared
// in another file, which a single-file parse cannot follow.
//
// The constant is resolved because naming it is the one way to reach the default
// path without a string literal. A command that mounted no --db-file flag and
// passed db.DefaultDBFile -- directly or via a local alias -- ignored both
// --db-file and $DB_FILE, and a literal-only rule reported nothing.
//
// What this cannot see, and no single-file parse can: an expression that computes
// the path some other way, such as os.Getenv("DB_FILE") or a value returned by a
// helper. Those remain unchecked. The guard raises the cost of reintroducing the
// drift; it does not prove the absence of it.
//
// BasicLit.Value for a STRING is the raw source text, quotes included.
// Comparing it to a bare db-file would never match -- which is precisely how
// this guard would end up passing forever without ever firing.
func resolveValue(expr ast.Expr, decls map[string]string) (string, bool) {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return "", false
		}
		unquoted, err := strconv.Unquote(value.Value)
		if err != nil {
			return "", false
		}
		return unquoted, true
	case *ast.Ident:
		resolved, ok := decls[value.Name]
		return resolved, ok
	case *ast.ParenExpr:
		return resolveValue(value.X, decls)
	case *ast.SelectorExpr:
		if value.Sel.Name == "DefaultDBFile" {
			return defaultDBFileRef, true
		}
		return "", false
	default:
		return "", false
	}
}

// sameFileDecls maps the name of every string constant, every read of
// db.DefaultDBFile, and every variable declared anywhere in the file to its
// denotation: package-level, in a grouped block, or local to a function body.
//
// All three scopes matter. Package-level alone missed `func f() { const n = "db-file" }`,
// which is exactly the shape a person writes to dodge a grep for the literal.
// Grouped blocks (`const ( ... )`) are handled by the ValueSpec loop, and
// multi-name specs (`const a, b = "db-file", "x"`) are matched up positionally.
//
// Short variable declarations (`alias := db.DefaultDBFile`) are collected too.
// They are not ValueSpecs, so the const/var loop cannot see them, and `:=` is how
// such an alias actually gets written -- handling only const and var left the
// obvious spelling of the alias undetected.
//
// Cross-file and non-literal initialisers are skipped: a single-file parse cannot
// follow them, and guessing would be worse than not matching.
func sameFileDecls(file *ast.File) map[string]string {
	decls := make(map[string]string)
	collect := func(gen *ast.GenDecl) {
		if gen.Tok != token.CONST && gen.Tok != token.VAR {
			return
		}
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// A spec may declare several names against several values; pair them
			// positionally. iota-style implicit repeats have fewer values than
			// names and are skipped, since guessing an iota value would be worse
			// than not resolving the name at all.
			for i, name := range valueSpec.Names {
				if i >= len(valueSpec.Values) {
					continue
				}
				if resolved, ok := resolveValue(valueSpec.Values[i], nil); ok {
					decls[name.Name] = resolved
				}
			}
		}
	}

	for _, decl := range file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok {
			collect(gen)
		}
	}
	// Function bodies hold their own const and var declarations, and their own
	// `:=` declarations, which are assignments rather than declarations the
	// ValueSpec loop can see.
	ast.Inspect(file, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.GenDecl:
			collect(decl)
		case *ast.AssignStmt:
			// Only `:=` introduces a name. A later `x = y` rebinds nothing and
			// must not overwrite what the declaration established.
			if decl.Tok != token.DEFINE {
				break
			}
			for i, lhs := range decl.Lhs {
				if i >= len(decl.Rhs) {
					break
				}
				name, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				if resolved, ok := resolveValue(decl.Rhs[i], nil); ok {
					decls[name.Name] = resolved
				}
			}
		}
		return true
	})
	return decls
}

// eachGoFile parses every .go file in the repository, skipping the paths skip
// reports true for, and hands the rest to visit. Test files are included: a
// duplicated flag declaration or a hardcoded InitDB path is a bug wherever it
// lives.
//
// The two guards pass different skip sets, and the difference is deliberate:
// internal/db/flag.go is the one file allowed to declare the flag, whereas the
// rest of internal/db passes literal DSNs to InitDB on purpose -- that is the
// package's own subject matter.
func eachGoFile(t *testing.T, skip func(rel string) bool, visit func(fset *token.FileSet, path string, file *ast.File)) {
	t.Helper()

	root := filepath.Join("..", "..")
	skipDir := map[string]bool{
		"node_modules": true,
		".git":         true,
		".worktrees":   true,
		"tmp":          true,
	}

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
		rel, relErr := filepath.Rel(root, path)
		if relErr == nil && skip(rel) {
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

// skipDBPackage reports whether rel is inside internal/db, which owns the flag
// and passes literal DSNs by design.
func skipDBPackage(rel string) bool {
	return strings.HasPrefix(rel, filepath.Join("internal", "db")+string(filepath.Separator))
}

// skipFlagOwner reports whether rel is internal/db/flag.go, the single file
// permitted to declare a db-file flag.
//
// This exempts one file rather than the whole package. Skipping the directory
// meant a second db-file flag added anywhere else in internal/db was invisible.
func skipFlagOwner(rel string) bool {
	return rel == filepath.Join("internal", "db", "flag.go")
}
