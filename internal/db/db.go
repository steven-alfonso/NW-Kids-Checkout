package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
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

// InitDB initializes the database connection. It uses the OTel-instrumented
// driver when RegisterOTelDriver has been called; the raw driver otherwise.
func InitDB(dataSourceName string) (*sql.DB, error) {
	if dataSourceName == "" {
		return nil, errors.New("missing database DSN")
	}

	slog.Info("initializing database connection", slog.String("dsn", dataSourceName))

	name := "sqlite3"
	if driverName != "" {
		name = driverName
	}

	db, err := sql.Open(name, dataSourceName)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(`
  		PRAGMA synchronous = NORMAL;
  		PRAGMA temp_store = MEMORY;
  		PRAGMA busy_timeout = 5000;`)
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
