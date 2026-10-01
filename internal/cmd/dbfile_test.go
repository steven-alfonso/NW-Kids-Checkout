package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"kids-checkin/internal/db"
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
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	m := regexp.MustCompile(`(?m)^KIDS_CHECKIN_DB_FILE\s*:=\s*(\S+)`)
	got := m.FindSubmatch(mk)
	if got == nil {
		t.Fatal("KIDS_CHECKIN_DB_FILE not found in Makefile")
	}
	if want := string(got[1]); db.DefaultDBFile != want {
		t.Errorf("db.DefaultDBFile = %q but Makefile KIDS_CHECKIN_DB_FILE = %q; make db-seed writes the latter, so the server would open a different file",
			db.DefaultDBFile, want)
	}
}
