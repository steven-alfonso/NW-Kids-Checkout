package db

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// DefaultDBFile is the path used when neither --db-file nor DB_FILE is set.
// Commands take it from DBFileFlag rather than repeating it.
//
// It must match KIDS_CHECKIN_DB_FILE in the Makefile, since that is where
// `make db-reset` and `make db-init` write. The two once drifted apart, and a
// run without DB_FILE then opened a different file than the one just seeded.
// The compiler ties the flags to this constant but nothing ties it to the
// Makefile, so TestDefaultDBFileMatchesMakefile in internal/cmd does that.
const DefaultDBFile = "database/kids-checkin.db"

// InitDB initializes the database connection.
func InitDB(dataSourceName string) (*sql.DB, error) {
	if dataSourceName == "" {
		// A blank DB_FILE in the environment resolves to an empty flag value
		// rather than falling back to the default, so name the ways out.
		return nil, fmt.Errorf("missing database DSN: pass --db-file, or set $%s, or unset it to use the default %s", EnvDBFile, DefaultDBFile)
	}

	dsn := resolveDSN(dataSourceName)

	slog.Info("initializing database connection", slog.String("dsn", dsn))

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

	// DSN params _foreign_keys, _busy_timeout, _txlock are the load-bearing
	// per-connection settings (they apply to every pooled connection). The
	// Exec below only affects one connection and must not be relied on for
	// per-connection correctness — keep DSN params as the source of truth.
	_, err = db.Exec(`
  		PRAGMA synchronous = NORMAL;
  		PRAGMA temp_store = MEMORY;`)
	if err != nil {
		return nil, err
	}

	err = db.Ping()
	if err != nil {
		return nil, err
	}

	slog.Info("database connection established")
	return db, nil
}

// resolveDSN makes a plain file path absolute before it is opened, so the
// database a process ends up using does not depend on its working directory.
//
// DefaultDBFile is relative, and that is precisely how the db-file/Makefile
// drift stayed invisible: sqlite creates a missing file on demand, so a run
// from the wrong directory opened a brand new, empty database and started
// cleanly. Absolutizing makes the log line name the file that was actually
// opened.
//
// A DSN carrying a query string (":memory:", cache=shared, mode=memory) is not
// a file path and is passed through untouched -- absolutizing one would change
// its meaning.
func resolveDSN(dsn string) string {
	if strings.ContainsAny(dsn, "?") || strings.Contains(dsn, ":memory:") {
		return dsn
	}
	abs, err := filepath.Abs(dsn)
	if err != nil {
		return dsn
	}
	return abs
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
