package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// DefaultDBFile is the path used when neither --db-file nor DB_FILE is set.
// Commands take it from DBFileFlag rather than repeating it.
//
// It must match KIDS_CHECKIN_DB_FILE in the Makefile, since that is where
// `make db-reset` and `make db-seed` write. The two once drifted apart, and a
// run without DB_FILE then opened a different file than the one just seeded.
// The compiler ties the flags to this constant but nothing ties it to the
// Makefile, so TestDefaultDBFileMatchesMakefile in internal/cmd does that.
//
// Production note: moving the default from kids-checkin.db to
// database/kids-checkin.db orphans a bare-metal host that already has data at
// the old repo-root path -- it boots an empty DB and writes to the new file.
// Containers are unaffected (the Dockerfile sets DB_FILE=/data/kids-checkin.db
// explicitly). Likewise, ensureParentDir below plus sqlite auto-create can mask
// a misspelled --db-file/DB_FILE by creating an empty DB instead of failing.
// Both are accepted tradeoffs for fresh-clone UX (database/ is gitignored and
// absent on clone); the absolute-path log in InitDB is the mitigation, and
// there is deliberately no auto-migration -- an operator with data at the old
// path moves it once.
const DefaultDBFile = "database/kids-checkin.db"

// InitDB initializes the database connection.
func InitDB(dataSourceName string) (*sql.DB, error) {
	dataSourceName = strings.TrimSpace(dataSourceName)
	if dataSourceName == "" {
		// A blank DB_FILE in the environment resolves to an empty flag value
		// rather than falling back to the default, so name the ways out.
		return nil, fmt.Errorf("missing database DSN: pass --db-file, or set $%s, or unset it to use the default %s", EnvDBFile, DefaultDBFile)
	}

	if err := ensureParentDir(dataSourceName); err != nil {
		return nil, err
	}

	// Log the absolute path so a wrong working directory shows up in the first
	// log line instead of surfacing as a query error later. The open below
	// uses the given spelling; both name the same file. Strip query params
	// before resolving (a DSN like "db.sqlite?cache=shared" names db.sqlite)
	// and leave file: URIs and :memory: alone (driver-handled, not file paths).
	logPath, _, _ := strings.Cut(dataSourceName, "?")
	var abs string
	if strings.HasPrefix(logPath, "file:") || logPath == ":memory:" {
		abs = dataSourceName
	} else {
		var err error
		abs, err = filepath.Abs(logPath)
		if err != nil {
			abs = logPath
		}
	}
	slog.Info("initializing database connection", slog.String("dsn", abs))

	dsn := dataSourceName
	for _, kv := range [][2]string{
		{"_foreign_keys", "on"},
		{"_busy_timeout", "5000"},
		{"_txlock", "immediate"},
	} {
		dsn = ensureDSNParam(dsn, kv[0], kv[1])
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	// Both failures below return an open *sql.DB. Closing it here matters because
	// the pool may now hold open connections by the time Exec and Ping have run,
	// and *sql.DB has no finalizer: a caller that only sees an error has no
	// handle to close and the pool would outlive the process's interest in it.
	//
	// DSN params _foreign_keys, _busy_timeout, _txlock are the load-bearing
	// per-connection settings (they apply to every pooled connection). The
	// Exec below only affects one connection and must not be relied on for
	// per-connection correctness — keep DSN params as the source of truth.
	_, err = db.Exec(`
  		PRAGMA synchronous = NORMAL;
  		PRAGMA temp_store = MEMORY;`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	err = db.Ping()
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	slog.Info("database connection established")
	return db, nil
}

// ensureParentDir creates the parent directory for a plain file-path DSN.
// sqlite creates a missing file but never a missing parent directory, and the
// default lives under gitignored database/, which is absent on a fresh clone.
// Without this every direct binary run (web, fetcher, checkins, random-data)
// failed with a bare "unable to open database file".
func ensureParentDir(dsn string) error {
	path, _, _ := strings.Cut(dsn, "?")
	if path == "" {
		return nil
	}
	// A file: URI is handled by the driver, not the filesystem: creating
	// ./file:/tmp/ in the working directory would be junk.
	if strings.HasPrefix(path, "file:") {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create database directory %s: %w", dir, err)
	}
	return nil
}

// ensureDSNParam appends "?key=value" or "&key=value" only if key is absent
// in the DSN query string. It uses url.ParseQuery for exact key matching to
// avoid substring false positives (e.g., file path "/tmp/my_foreign_keys.db"
// should not count as having _foreign_keys). It preserves the original DSN
// verbatim and avoids re-encoding that would sort keys.
func ensureDSNParam(dsn, key, value string) string {
	_, query, found := strings.Cut(dsn, "?")
	if !found {
		return dsn + "?" + key + "=" + value
	}
	vals, _ := url.ParseQuery(query)
	if vals.Has(key) {
		return dsn
	}
	if query == "" {
		return dsn + key + "=" + value
	}
	return dsn + "&" + key + "=" + value
}
