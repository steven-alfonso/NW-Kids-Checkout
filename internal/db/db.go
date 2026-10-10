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
// `make db-reset` and `make db-init` write. The two once drifted apart, and a
// run without DB_FILE then opened a different file than the one just seeded.
// The compiler ties the flags to this constant but nothing ties it to the
// Makefile, so TestDefaultDBFileMatchesMakefile in internal/cmd does that.
const DefaultDBFile = "database/kids-checkin.db"

// InitDB initializes the database connection.
func InitDB(dataSourceName string) (*sql.DB, error) {
	trimmed := strings.TrimSpace(dataSourceName)
	if trimmed == "" {
		// A blank DB_FILE in the environment resolves to an empty flag value
		// rather than falling back to the default, so name the ways out.
		// Whitespace is rejected too: "   " is not a usable DSN, and accepting
		// it created a file whose name is three spaces, silently, under an
		// authoritative-looking absolute path in the log line below.
		return nil, fmt.Errorf("missing database DSN: pass --db-file, or set $%s, or unset it to use the default %s", EnvDBFile, DefaultDBFile)
	}
	if trimmed != dataSourceName {
		// A padded path ("  database/kids-checkin.db  ") would otherwise be
		// absolutized verbatim into "<cwd>/  database/kids-checkin.db  ", a
		// junk file under an authoritative-looking log line. Trim and use the
		// trimmed value so the log names the file actually opened.
		dataSourceName = trimmed
	}

	// Warn on the pre-move repo-root path. The default moved from
	// kids-checkin.db to database/kids-checkin.db; a host that wrote real data
	// with the old default would otherwise boot cleanly against a fresh empty
	// file while the old file sits orphaned (and gitignored) at the root.
	if dataSourceName == DefaultDBFile {
		if _, err := os.Stat("kids-checkin.db"); err == nil {
			slog.Warn("legacy database file ./kids-checkin.db exists while using the default database/kids-checkin.db; move it with `mkdir -p database && mv kids-checkin.db database/kids-checkin.db` if it holds real data")
		}
	}

	if err := ensureParentDir(dataSourceName); err != nil {
		return nil, err
	}

	dsn := ResolveDSN(dataSourceName)

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

// ResolveDSN makes a plain file path absolute before it is opened, so the log
// line names the file that was actually opened.
//
// It does not make the database independent of the working directory: a
// relative path is resolved against the CWD, so running the binary from
// elsewhere still opens a different file. That is a property of the default
// being relative, not of this function -- what this buys is that the failure is
// visible in the logs instead of silent.
//
// A DSN that is not a plain file path is passed through untouched. Absolutizing
// one would change its meaning: ":memory:" and cache=shared are not files, and
// a leading "file:" is a SQLite URI that mattn/go-sqlite3 recognises. Prepending
// the working directory to "file:foo.db" buries the prefix mid-path, the driver
// stops recognising it, and SQLite creates a file literally named "file:foo.db".
//
// A DSN that already carries query params is still a file path, so the path is
// absolutized and the query re-attached. InitDB appends _foreign_keys and
// friends to such a DSN rather than replacing them, so "data.db?_txlock=deferred"
// is a supported input and must not be the one shape that stays relative.
//
// Callers that need to open the same database a second time -- the Fiber
// session store points at the app database and builds its own DSN -- should
// take the path from here. Otherwise the app database is opened twice under two
// spellings of the same relative path, which is exactly the ambiguity the
// db-file/Makefile drift came from.
//
// It is idempotent: passing the result back through is a no-op.
func ResolveDSN(dsn string) string {
	// Split before classifying: ":memory:?cache=shared" is still memory, and
	// "file:foo.db?cache=shared" is still a URI. Checking Contains(":memory:")
	// on the whole DSN misclassified real filenames like
	// "backup-:memory:.db" as memory and left them relative.
	path, _, _ := strings.Cut(dsn, "?")
	if path == ":memory:" || strings.HasPrefix(path, "file:") {
		return dsn
	}
	path, query, hasQuery := strings.Cut(dsn, "?")
	if path == "" {
		// No path to absolutize. Turning "" into the working directory would
		// produce a DSN naming a directory rather than a database.
		return dsn
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return dsn
	}
	if hasQuery {
		return abs + "?" + query
	}
	return abs
}

// ensureParentDir creates the parent directory for a plain file-path DSN.
// sqlite creates a missing file but never a missing parent directory, and the
// default lives under gitignored database/, which is absent on a fresh clone.
// Without this every direct binary run (web, fetcher, checkins, random-data)
// failed with a bare "unable to open database file" while only db-init worked.
//
// Memory DSNs, file: URIs, and DSNs with no path are left alone: absolutizing
// or mkdir-ing those would change their meaning.
func ensureParentDir(dsn string) error {
	path, _, _ := strings.Cut(dsn, "?")
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil
	}
	// Reject leading ~/$VAR rather than resolving to "<cwd>/~/...": neither the
	// shell nor sqlite expands those, so mkdir-ing them creates junk.
	if strings.HasPrefix(path, "~") || strings.HasPrefix(path, "$") {
		return fmt.Errorf("database path %q starts with %q, which is not expanded; use an explicit relative or absolute path", path, string(path[0]))
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
//
// A malformed query (e.g. "data.db?%zz") makes ParseQuery return an error,
// which is intentionally ignored: the key is treated as absent and appended
// with "&". That is harmless for the three known keys and avoids failing open
// on inputs sqlite itself would still open.
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
