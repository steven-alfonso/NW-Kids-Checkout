package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"

	"github.com/XSAM/otelsql"
	_ "github.com/mattn/go-sqlite3"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

// RegisterOTelDriver registers the sqlite3 driver wrapped with OpenTelemetry
// instrumentation exactly once per process, bound to the given providers.
// Call it before the first InitDB when telemetry is enabled; InitDB falls back
// to the raw driver otherwise. Registration is a no-op after the first call,
// so the initial providers win regardless of later invocations.
var (
	driverOnce  sync.Once
	driverName  string
	registerErr error
)

func RegisterOTelDriver(tp trace.TracerProvider, mp metric.MeterProvider) error {
	driverOnce.Do(func() {
		driverName, registerErr = otelsql.Register(
			"sqlite3",
			otelsql.WithTracerProvider(tp),
			otelsql.WithMeterProvider(mp),
			otelsql.WithAttributes(semconv.DBSystemNameSQLite),
		)
		if registerErr != nil {
			registerErr = fmt.Errorf("registering otel sqlite driver: %w", registerErr)
			return
		}
		slog.Debug("registered otel-instrumented sqlite driver", slog.String("driver_name", driverName))
	})
	return registerErr
}

// InitDBInstrumented registers the OTel driver and opens an instrumented
// database in one step, so callers cannot open the DB before registering and
// silently run without instrumentation.
func InitDBInstrumented(dataSourceName string, tp trace.TracerProvider, mp metric.MeterProvider) (*sql.DB, error) {
	if err := RegisterOTelDriver(tp, mp); err != nil {
		return nil, err
	}
	return InitDB(dataSourceName)
}

// InitDB initializes the database connection. It uses the OTel-instrumented
// driver when RegisterOTelDriver has been called; the raw driver otherwise.
func InitDB(dataSourceName string) (*sql.DB, error) {
	if dataSourceName == "" {
		return nil, errors.New("missing database DSN")
	}

	slog.Info("initializing database connection", slog.String("dsn", dataSourceName))

	dsn := dataSourceName
	for _, kv := range [][2]string{
		{"_foreign_keys", "on"},
		{"_busy_timeout", "5000"},
		{"_txlock", "immediate"},
	} {
		dsn = ensureDSNParam(dsn, kv[0], kv[1])
	}

	name := "sqlite3"
	if driverName != "" {
		name = driverName
	}

	db, err := sql.Open(name, dsn)
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
