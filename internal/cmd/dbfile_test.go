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
	// Find all assignments; make uses last-wins, so a duplicate second
	// assignment is what db-reset actually writes. Checking only the first
	// would stay green while make writes the second.
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
		// godotenv trims leading whitespace, so an indented DB_FILE line
		// would still set the variable. Match after trimming.
		assert.NotRegexp(t, `(?i)^(export\s+)?DB_FILE\s*=`, strings.TrimSpace(line),
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
// TestDBInitCallSiteDoesNotHardcodePath covers both of those, and its
// expectedFiles exact set forces a new InitDB file to be registered here too:
// add the command name to databaseCommands below when adding a call site.
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
// What it does not cover, stated plainly rather than implied: an identifier
// declared in another file, or a path built by a helper that takes no literal
// at the call site. A CallExpr argument that is not cmd.String("db-file") or
// db.ResolveDSN(...) is now rejected, which catches os.Getenv and direct
// helper calls, but `db.InitDB(pathVar)` where pathVar came from a helper in
// another statement still needs dataflow analysis, which a single-file parse is
// not. The guard covers the spellings that actually caused this bug plus the
// direct-call shapes; it does not prove no others exist.
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
	foundFiles := map[string]bool{}
	global := packageLevelDecls(t)
	root := filepath.Join("..", "..")
	eachGoFile(t, skipDBInitGuard, func(fset *token.FileSet, path string, file *ast.File) {
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		receiver, isDot := dbImportName(file)
		if receiver == "" && !isDot {
			return
		}
		// Resolved per file so a local alias of db.DefaultDBFile is caught too.
		// Falls back to same-package globals so a const moved to a sibling
		// file does not silently disable the guard.
		decls := sameFileDecls(file)
		for k, v := range globalForFile(global, path) {
			if _, ok := decls[k]; !ok {
				decls[k] = v
			}
		}
		if isDot {
			// A dot-imported DefaultDBFile reads as a bare Ident.
			decls["DefaultDBFile"] = defaultDBFileRef
		}
		allowed := allowedPathVars(file, decls)
		isTest := strings.HasSuffix(path, "_test.go")
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			// Dot-imported InitDB("..."): Fun is *ast.Ident, not SelectorExpr.
			if isDot {
				if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "InitDB" {
					if !isTest {
						found++
						foundFiles[path] = true
					}
					checkInitDBArg(t, fset, call, ".", decls, allowed, isTest, rel)
					return true
				}
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
				foundFiles[rel] = true
			}
			if len(call.Args) == 0 {
				t.Errorf("%s calls %s.InitDB() with no argument; pass the --db-file value",
					fset.Position(call.Pos()), receiver)
				return true
			}
			checkInitDBArg(t, fset, call, receiver, decls, allowed, isTest, rel)
			return true
		})
		// Direct sql.Open / sqlite storage opens bypass InitDB entirely.
		checkDirectSQLOpens(t, fset, path, file, isTest)
	})
	// Test files are excluded from the count on purpose. dbinit_test.go alone
	// makes nine InitDB calls, so counting them would let this assertion stay
	// green after every production call site had moved or been renamed -- which
	// is the exact failure the assertion exists to catch.
	assert.Positive(t, found, "no non-test db.InitDB call sites found; this guard would silently pass if the call sites moved, were aliased away, or were renamed")
	// Liveness is an exact set, not a lower bound: every expected production
	// call site must be present. A new command must add its file here, so a
	// deleted or renamed call site fails loudly instead of hiding in a >=.
	expectedFiles := map[string]bool{
		filepath.Join("internal", "controllers", "server.go"):           true,
		filepath.Join("internal", "cmd", "checkins", "checkins.go"):     true,
		filepath.Join("internal", "cmd", "checkins", "seed_preview.go"): true,
		filepath.Join("internal", "cmd", "location", "cmd.go"):          true,
		filepath.Join("internal", "cmd", "checkoutsfetcher", "cmd.go"):  true,
		filepath.Join("internal", "cmd", "dbinit", "dbinit.go"):         true,
		filepath.Join("cmd", "random-data", "main.go"):                  true,
	}
	for f := range expectedFiles {
		assert.True(t, foundFiles[f], "expected InitDB call site %s missing; update the guard if call sites moved", f)
	}
	for f := range foundFiles {
		assert.True(t, expectedFiles[f], "unexpected InitDB call site %s; update expectedFiles (and databaseCommands above if it is a new command) if legitimate", f)
	}
}

// checkInitDBArg rejects the two spellings that caused the drift (a string
// literal and db.DefaultDBFile however spelled), a direct CallExpr that is not
// flag-derived, and -- for non-test files -- an identifier that is not
// established as flag-derived in the same file. Test files may use temp paths,
// so unresolved identifiers stay allowed there; production call sites must trace
// to cmd.String("db-file") or ResolveDSN thereof. Cross-package helpers remain
// a documented gap (see the Test doc comment).
func checkInitDBArg(t *testing.T, fset *token.FileSet, call *ast.CallExpr, receiver string, decls map[string]string, allowed map[string]bool, isTest bool, rel string) {
	t.Helper()
	if len(call.Args) == 0 {
		return
	}
	arg := call.Args[0]
	// Unwrap parens before classifying: (os.Getenv(...)) must not bypass the
	// CallExpr check.
	for {
		if paren, ok := arg.(*ast.ParenExpr); ok {
			arg = paren.X
			continue
		}
		break
	}
	if _, ok := arg.(*ast.CallExpr); ok {
		if !isAllowedPathCall(arg.(*ast.CallExpr), allowed, decls) {
			t.Errorf("%s calls %s.InitDB with a computed path; pass cmd.String(\"db-file\") (or db.ResolveDSN of it) so --db-file and $DB_FILE are honored",
				fset.Position(call.Pos()), receiver)
			return
		}
		return
	}
	// A computed path hidden inside concatenation: "kids-"+helper(),
	// dir+"/kids-checkin.db", ""+os.Getenv(...). Any non-allowed call inside
	// fails the site even when a literal half resolves.
	if bin, ok := arg.(*ast.BinaryExpr); ok {
		if containsDisallowedCall(bin, allowed, decls) {
			t.Errorf("%s calls %s.InitDB with a computed path; pass cmd.String(\"db-file\") (or db.ResolveDSN of it) so --db-file and $DB_FILE are honored",
				fset.Position(call.Pos()), receiver)
			return
		}
	}
	// Both ways of naming the path without reading the flag are rejected:
	// a string literal, and db.DefaultDBFile however it is spelled. The
	// second is what made the literal-only rule incomplete -- mounting no
	// flag at all and passing the constant passed every guard here.
	value, resolved := resolveValue(arg, decls)
	switch {
	case !resolved:
		// Production identifiers must be flag-derived; test temp paths are
		// exempt. Func params are pre-seeded as allowed, so this fires only
		// for genuinely untracked paths (helper returns, env reads via var,
		// wrong-flag strings).
		if !isTest {
			if ident, ok := arg.(*ast.Ident); ok && !allowed[ident.Name] {
				t.Errorf("%s calls %s.InitDB(%s) with a path that does not trace to cmd.String(\"db-file\"); pass the --db-file value so --db-file and $DB_FILE are honored",
					fset.Position(call.Pos()), receiver, ident.Name)
			}
		}
	case value == defaultDBFileRef:
		t.Errorf("%s calls %s.InitDB(%s); the path must come from db.DBFileFlag() via cmd.String(%q), not the default constant -- a command that passes this ignores both --db-file and $%s",
			fset.Position(call.Pos()), receiver, defaultDBFileRef, "db-file", db.EnvDBFile)
	default:
		// prepare_test_db.go's single in-memory DSN is by design (test DB
		// shares production connection settings). Anything else there fails.
		if rel == filepath.Join("internal", "db", "prepare_test_db.go") || rel == filepath.Join("..", "..", "internal", "db", "prepare_test_db.go") {
			if strings.Contains(value, "memory") {
				return
			}
		}
		t.Errorf("%s calls %s.InitDB(%q); the path must come from db.DBFileFlag() via cmd.String(%q), not a hardcoded literal",
			fset.Position(call.Pos()), receiver, value, "db-file")
	}
}

// isAllowedPathCall reports whether a CallExpr argument is a legitimate
// flag-derived path: cmd.String("db-file"), or <db>.ResolveDSN wrapping a
// flag-derived path. Anything else computing a path (os.Getenv,
// legacyPath(), ResolveDSN("literal"), ResolveDSN(DefaultDBFile), ...) is
// rejected at the call site.
//
// ResolveDSN alone proves nothing: wrapping a hardcoded path in it still
// ignores --db-file, so the inner argument must itself be allowed.
func isAllowedPathCall(call *ast.CallExpr, allowed map[string]bool, decls map[string]string) bool {
	fun, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if fun.Sel.Name == "String" && len(call.Args) == 1 {
		if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil && s == "db-file" {
				return true
			}
		}
		return false
	}
	if fun.Sel.Name == "ResolveDSN" && len(call.Args) == 1 {
		return isAllowedPathArg(call.Args[0], allowed, decls)
	}
	return false
}

// isAllowedPathArg reports whether an InitDB/ResolveDSN argument is
// flag-derived: a direct allowed call, a paren thereof, or an identifier
// previously established as flag-derived in the same file.
func isAllowedPathArg(expr ast.Expr, allowed map[string]bool, decls map[string]string) bool {
	switch v := expr.(type) {
	case *ast.CallExpr:
		return isAllowedPathCall(v, allowed, decls)
	case *ast.ParenExpr:
		return isAllowedPathArg(v.X, allowed, decls)
	case *ast.Ident:
		return allowed[v.Name]
	}
	return false
}

// containsDisallowedCall reports whether expr contains a CallExpr that is not
// flag-derived. Catches ""+os.Getenv(...), "kids-"+helper(), (os.Getenv(...))
// and other wrappers that hide a computed path inside a larger expression.
func containsDisallowedCall(expr ast.Expr, allowed map[string]bool, decls map[string]string) bool {
	var found bool
	ast.Inspect(expr, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !isAllowedPathCall(call, allowed, decls) {
			found = true
			return false
		}
		return true
	})
	return found
}

// allowedPathVars returns the set of identifiers in file established as
// flag-derived: initialized from cmd.String("db-file") or ResolveDSN thereof
// (through parens and chains), plus all function parameters (callers like
// server.go receive the flag value as a param). Fixed-point so b:=a follows
// a:=cmd.String(...). Both := and = are tracked; a later = rebinds.
func allowedPathVars(file *ast.File, decls map[string]string) map[string]bool {
	allowed := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		if fn.Type.Params == nil {
			return true
		}
		for _, field := range fn.Type.Params.List {
			for _, name := range field.Names {
				allowed[name.Name] = true
			}
		}
		return true
	})
	type assign struct {
		lhs []ast.Expr
		rhs []ast.Expr
	}
	var assigns []assign
	ast.Inspect(file, func(n ast.Node) bool {
		if decl, ok := n.(*ast.AssignStmt); ok {
			assigns = append(assigns, assign{lhs: decl.Lhs, rhs: decl.Rhs})
		}
		return true
	})
	for range len(assigns) + 5 {
		changed := false
		for _, a := range assigns {
			for i, lhs := range a.lhs {
				if i >= len(a.rhs) {
					break
				}
				name, ok := lhs.(*ast.Ident)
				if !ok || name.Name == "_" {
					continue
				}
				if allowed[name.Name] {
					continue
				}
				if isAllowedPathArg(a.rhs[i], allowed, decls) {
					allowed[name.Name] = true
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	return allowed
}

// packageLevelDecls collects every package-level const/var string denotation
// keyed by directory, so a constant moved to a sibling file in the same
// package still resolves.
func packageLevelDecls(t *testing.T) map[string]map[string]string {
	t.Helper()
	out := map[string]map[string]string{}
	root := filepath.Join("..", "..")
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil
		}
		dir := filepath.Dir(path)
		if out[dir] == nil {
			out[dir] = map[string]string{}
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if len(vs.Values) == 1 && len(vs.Names) > 1 {
					if r, ok := resolveValue(vs.Values[0], out[dir]); ok {
						for _, name := range vs.Names {
							out[dir][name.Name] = r
						}
					}
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if r, ok := resolveValue(vs.Values[i], out[dir]); ok {
						out[dir][name.Name] = r
					}
				}
			}
		}
		return nil
	})
	return out
}

func globalForFile(global map[string]map[string]string, path string) map[string]string {
	dir := filepath.Dir(path)
	for gdir, m := range global {
		if dir == gdir {
			return m
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		adir := filepath.Dir(abs)
		for gdir, m := range global {
			if gabs, err := filepath.Abs(gdir); err == nil && adir == gabs {
				return m
			}
		}
	}
	return nil
}

// checkDirectSQLOpens forbids bypassing InitDB entirely with sql.Open or a
// sqlite storage open carrying a .db literal. Production code must go through
// InitDB so DSN params and path resolution stay single-sourced.
func checkDirectSQLOpens(t *testing.T, fset *token.FileSet, path string, file *ast.File, isTest bool) {
	t.Helper()
	if isTest {
		return
	}
	if strings.HasPrefix(path, filepath.Join("..", "..", "internal", "db")) {
		return
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "Open" && len(call.Args) == 2 {
			if lit, ok := call.Args[1].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil && strings.Contains(s, ".db") {
					t.Errorf("%s opens a database with sql.Open literal %q; go through db.InitDB so DSN params stay single-sourced",
						fset.Position(call.Pos()), s)
				}
			}
		}
		if sel.Sel.Name == "New" && len(call.Args) == 1 {
			if comp, ok := call.Args[0].(*ast.CompositeLit); ok {
				for _, elt := range comp.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok || key.Name != "Database" {
						continue
					}
					if lit, ok := kv.Value.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if s, err := strconv.Unquote(lit.Value); err == nil && strings.Contains(s, ".db") {
							t.Errorf("%s opens a database with storage literal %q; resolve once via db.ResolveDSN and share the spelling",
								fset.Position(call.Pos()), s)
						}
					}
				}
			}
		}
		return true
	})
}

// dbImportName returns the identifier the file uses to refer to
// kids-checkin/internal/db, honouring an alias, plus whether the import is a
// dot-import. A dot-import (`import . "kids-checkin/internal/db"`) makes
// InitDB callable bare, so the guard must match *ast.Ident as well as
// *ast.SelectorExpr for those files.
func dbImportName(file *ast.File) (string, bool) {
	const dbImportPath = "kids-checkin/internal/db"
	for _, imp := range file.Imports {
		value, err := strconv.Unquote(imp.Path.Value)
		if err != nil || value != dbImportPath {
			continue
		}
		if imp.Name != nil {
			if imp.Name.Name == "." {
				return "", true
			}
			if imp.Name.Name == "_" {
				continue
			}
			return imp.Name.Name, false
		}
		return "db", false
	}
	return "", false
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
	flagLits := 0
	eachGoFile(t, skipNoFiles, func(fset *token.FileSet, path string, file *ast.File) {
		rel, _ := filepath.Rel(filepath.Join("..", ".."), path)
		isFlagOwner := rel == filepath.Join("internal", "db", "flag.go")
		// Locate the single allowed definition: func DBFileFlag.
		var allowedBody *ast.BlockStmt
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "DBFileFlag" {
				allowedBody = fn.Body
				break
			}
		}
		inAllowed := func(pos token.Pos) bool {
			if allowedBody == nil {
				return false
			}
			return allowedBody.Pos() <= pos && pos <= allowedBody.End()
		}
		decls := sameFileDecls(file)
		sliceWithDBFile := sliceVarsContaining(file, decls, "db-file")
		ast.Inspect(file, func(n ast.Node) bool {
			// Assignments to flag fields outside a literal: Name, Value,
			// Aliases, Sources. Any += to .Name is rejected outright (no
			// legitimate code builds the flag name by appending).
			if assign, ok := n.(*ast.AssignStmt); ok {
				for i, lhs := range assign.Lhs {
					sel, ok := lhs.(*ast.SelectorExpr)
					if !ok {
						continue
					}
					switch sel.Sel.Name {
					case "Name":
						if assign.Tok == token.ADD_ASSIGN {
							t.Errorf("%s appends to .Name; mount db.DBFileFlag() instead so there is exactly one definition",
								fset.Position(assign.Pos()))
							continue
						}
						if i >= len(assign.Rhs) {
							continue
						}
						if v, ok := resolveValue(assign.Rhs[i], decls); ok && v == "db-file" {
							t.Errorf("%s assigns .Name = %q outside a flag literal; mount db.DBFileFlag() instead so there is exactly one definition",
								fset.Position(assign.Pos()), v)
						}
					case "Value":
						if i >= len(assign.Rhs) {
							continue
						}
						if v, ok := resolveValue(assign.Rhs[i], decls); ok && strings.Contains(v, ".db") {
							t.Errorf("%s assigns .Value = %q; mount db.DBFileFlag() instead so there is exactly one definition",
								fset.Position(assign.Pos()), v)
						}
					case "Aliases":
						if i >= len(assign.Rhs) {
							continue
						}
						if v, ok := resolveValue(assign.Rhs[i], decls); ok && v == "db-file" {
							t.Errorf("%s assigns .Aliases containing %q; mount db.DBFileFlag() instead so there is exactly one definition",
								fset.Position(assign.Pos()), v)
						}
						if ident, ok := assign.Rhs[i].(*ast.Ident); ok && sliceWithDBFile[ident.Name] {
							t.Errorf("%s assigns .Aliases from %q containing db-file; mount db.DBFileFlag() instead",
								fset.Position(assign.Pos()), ident.Name)
						}
					case "Sources":
						t.Errorf("%s assigns .Sources on a flag; mount db.DBFileFlag() instead so $DB_FILE handling stays single-sourced",
							fset.Position(assign.Pos()))
					}
				}
				return true
			}
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
				if !ok {
					continue
				}
				// Aliases: []string{"db-file"} still accepts --db-file.
				if key.Name == "Aliases" {
					if isFlagOwner && inAllowed(lit.Pos()) {
						continue
					}
					if comp, ok := kv.Value.(*ast.CompositeLit); ok {
						for _, e := range comp.Elts {
							if v, ok := resolveValue(e, decls); ok && v == "db-file" {
								t.Errorf("%s declares %s with Aliases containing %q; mount db.DBFileFlag() instead so there is exactly one definition",
									fset.Position(lit.Pos()), name, v)
							}
						}
					} else if ident, ok := kv.Value.(*ast.Ident); ok && sliceWithDBFile[ident.Name] {
						t.Errorf("%s declares %s with Aliases from %q containing \"db-file\"; mount db.DBFileFlag() instead",
							fset.Position(lit.Pos()), name, ident.Name)
					}
					continue
				}
				if key.Name != "Name" {
					continue
				}
				// A computed Name (string(n), Sprintf, ToLower, ...) is not a
				// plain alias: legitimate definitions spell the literal.
				if _, ok := kv.Value.(*ast.CallExpr); ok {
					t.Errorf("%s declares %s with computed Name; mount db.DBFileFlag() instead so there is exactly one definition",
						fset.Position(lit.Pos()), name)
					continue
				}
				unquoted, ok := resolveValue(kv.Value, decls)
				if !ok || unquoted != "db-file" {
					continue
				}
				if isFlagOwner && inAllowed(lit.Pos()) {
					flagLits++
					continue
				}
				if isFlagOwner {
					t.Errorf("%s declares a second %s{Name: %q} in flag.go outside DBFileFlag; keep exactly one definition",
						fset.Position(lit.Pos()), name, unquoted)
					continue
				}
				t.Errorf("%s declares its own %s{Name: %q}; mount db.DBFileFlag() instead so there is exactly one definition",
					fset.Position(lit.Pos()), name, unquoted)
			}
			return true
		})
	})
	assert.Equal(t, 1, flagLits, "expected exactly one db-file flag definition in DBFileFlag; found %d", flagLits)
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
// String concatenation ("kids-" + "checkin.db") is evaluated so the oldest
// grep-dodge does not pass. Anything else that computes the path -- os.Getenv,
// a helper's return value -- stays unresolved; see the limitations documented on
// TestDBInitCallSiteDoesNotHardcodePath.
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
	case *ast.BinaryExpr:
		// Only constant string concatenation. "a"+"b" resolves; anything with a
		// non-string side does not, so numeric or call expressions stay
		// unresolved rather than guessing.
		if value.Op != token.ADD {
			return "", false
		}
		left, ok := resolveValue(value.X, decls)
		if !ok {
			return "", false
		}
		right, ok := resolveValue(value.Y, decls)
		if !ok {
			return "", false
		}
		// A DefaultDBFile half poisons the whole expression: "x"+DefaultDBFile
		// still names the default without reading the flag.
		if left == defaultDBFileRef || right == defaultDBFileRef {
			return defaultDBFileRef, true
		}
		return left + right, true
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
// A single value for several names (`const a, b = "db-file"`) broadcasts to all
// of them, which valid Go assigns the same value.
//
// Short variable declarations (`alias := db.DefaultDBFile`) are collected too.
// They are not ValueSpecs, so the const/var loop cannot see them, and `:=` is how
// such an alias actually gets written -- handling only const and var left the
// obvious spelling of the alias undetected.
//
// Resolution runs to a fixed point so chained aliases (`const n1 = "db-file";
// const n2 = n1; Name: n2`) resolve. A single pass left depth-2 and deeper
// silently unresolved, which is the natural way to hide a constant.
//
// Cross-file and non-literal initialisers are skipped: a single-file parse cannot
// follow them, and guessing would be worse than not matching.
func sameFileDecls(file *ast.File) map[string]string {
	decls := make(map[string]string)
	collect := func(gen *ast.GenDecl, decls map[string]string) bool {
		if gen.Tok != token.CONST && gen.Tok != token.VAR {
			return false
		}
		changed := false
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// A spec may declare several names against several values; pair them
			// positionally. `const a, b = "x"` assigns the single value to both
			// names, so broadcast Values[0]. iota-style implicit repeats have
			// fewer values than names for a different reason and are still
			// skipped, since guessing an iota value would be worse than not
			// resolving the name at all.
			vals := valueSpec.Values
			if len(vals) == 1 && len(valueSpec.Names) > 1 {
				for _, name := range valueSpec.Names {
					if resolved, ok := resolveValue(vals[0], decls); ok {
						if decls[name.Name] != resolved {
							decls[name.Name] = resolved
							changed = true
						}
					}
				}
				continue
			}
			for i, name := range valueSpec.Names {
				if i >= len(valueSpec.Values) {
					continue
				}
				if resolved, ok := resolveValue(valueSpec.Values[i], decls); ok {
					if decls[name.Name] != resolved {
						decls[name.Name] = resolved
						changed = true
					}
				}
			}
		}
		return changed
	}

	// Gather GenDecls (package-level and function-local) and := assignments.
	// Assignments are collected separately so the fixed-point loop can resolve
	// `b := a` after `a := "db-file"` in the same pass set.
	var gens []*ast.GenDecl
	type assign struct {
		lhs []ast.Expr
		rhs []ast.Expr
		tok token.Token
	}
	var assigns []assign
	for _, decl := range file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok {
			gens = append(gens, gen)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.GenDecl:
			// Avoid double-adding package-level decls already in gens; harmless
			// if duplicated since collect is idempotent.
			found := false
			for _, g := range gens {
				if g == decl {
					found = true
					break
				}
			}
			if !found {
				gens = append(gens, decl)
			}
		case *ast.AssignStmt:
			// Both := and = are tracked; a later = rebinds (see the fixed-point
			// loop, which overwrites). Ignoring = let `var p string;
			// p="kids-checkin.db"` pass with p unresolved.
			assigns = append(assigns, assign{lhs: decl.Lhs, rhs: decl.Rhs, tok: decl.Tok})
		}
		return true
	})

	// Iterate to a fixed point: each pass may resolve names the next pass's
	// aliases depend on. Bound by table size so a long reverse chain still
	// converges; files are small.
	for range len(gens) + len(assigns) + 5 {
		changed := false
		for _, gen := range gens {
			if collect(gen, decls) {
				changed = true
			}
		}
		for _, a := range assigns {
			for i, lhs := range a.lhs {
				if i >= len(a.rhs) {
					break
				}
				name, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				if a.tok == token.DEFINE {
					// := introduces; keep first binding (declaration wins).
					if _, exists := decls[name.Name]; exists {
						continue
					}
				} // else = rebinds: fall through and overwrite.
				if resolved, ok := resolveValue(a.rhs[i], decls); ok {
					if decls[name.Name] != resolved {
						decls[name.Name] = resolved
						changed = true
					}
				}
			}
		}
		if !changed {
			break
		}
	}
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

// sliceVarsContaining returns local slice/array variables initialized with a
// composite literal containing want (e.g. als := []string{"db-file"}).
func sliceVarsContaining(file *ast.File, decls map[string]string, want string) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || assign.Tok != token.DEFINE {
			return true
		}
		for i, lhs := range assign.Lhs {
			if i >= len(assign.Rhs) {
				break
			}
			name, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			comp, ok := assign.Rhs[i].(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, e := range comp.Elts {
				if v, ok := resolveValue(e, decls); ok && v == want {
					out[name.Name] = true
				}
			}
		}
		return true
	})
	return out
}

// skipNoFiles skips nothing: the flag guard must see every file including
// flag.go (where only DBFileFlag's own literal is allowed).
func skipNoFiles(rel string) bool { return false }

// skipDBInitGuard reports whether rel is exempt from the InitDB hardcode guard.
// Only internal/db's own tests are exempt: they use literals like "relative.db"
// to exercise edge cases. prepare_test_db.go is NOT exempt -- it links into
// production binaries, so only its single InitDB(inMemoryDSN) call is allowed
// (see checkInitDBArg); a second hardcoded call there must fail.
func skipDBInitGuard(rel string) bool {
	if strings.HasPrefix(rel, filepath.Join("internal", "db")+string(filepath.Separator)) && strings.HasSuffix(rel, "_test.go") {
		return true
	}
	return false
}

// skipDBPackage kept for history; use skipDBInitGuard for the InitDB guard.
func skipDBPackage(rel string) bool {
	return skipDBInitGuard(rel)
}

// skipFlagOwner reports whether rel is internal/db/flag.go, the single file
// permitted to declare a db-file flag.
//
// This exempts one file rather than the whole package. Skipping the directory
// meant a second db-file flag added anywhere else in internal/db was invisible.
func skipFlagOwner(rel string) bool {
	return rel == filepath.Join("internal", "db", "flag.go")
}
