package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/XSAM/otelsql"
	_ "github.com/mattn/go-sqlite3"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// instrumentedDriver registers the sqlite3 driver wrapped with OpenTelemetry
// instrumentation exactly once per process. It resolves against the global
// tracer/meter providers at registration time, so telemetry.Setup must run
// before the first InitDB call. When telemetry is disabled the global
// providers are no-ops and the wrapper adds no overhead.
var (
	driverOnce  sync.Once
	driverName  string
	registerErr error
)

func instrumentedDriver() (string, error) {
	driverOnce.Do(func() {
		driverName, registerErr = otelsql.Register(
			"sqlite3",
			otelsql.WithAttributes(semconv.DBSystemNameSQLite),
		)
		if registerErr != nil {
			registerErr = fmt.Errorf("registering otel sqlite driver: %w", registerErr)
			return
		}
		slog.Debug("registered otel-instrumented sqlite driver", slog.String("driver_name", driverName))
	})
	return driverName, registerErr
}

// InitDB initializes the database connection.
func InitDB(dataSourceName string) (*sql.DB, error) {
	if dataSourceName == "" {
		return nil, errors.New("missing database DSN")
	}

	slog.Info("initializing database connection", slog.String("dsn", dataSourceName))

	name, err := instrumentedDriver()
	if err != nil {
		return nil, err
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
