package db_test

import (
	"path/filepath"
	"testing"

	"kids-checkin/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// InitDBInstrumented is the production path: it registers the OTel driver and
// opens the DB in one step. Driver registration is process-global (sync.Once),
// so this package has a single span-producing test to avoid order-dependent
// registration between tests.
func Test_InitDBInstrumented_creates_otel_spans_for_queries(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	dsn := filepath.Join(t.TempDir(), "otel.db")
	database, err := db.InitDBInstrumented(dsn, tp,
		// Reader-less provider: meters are usable but export nothing.
		sdkmetric.NewMeterProvider())
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	var one int
	require.NoError(t, database.QueryRowContext(t.Context(), "SELECT 1").Scan(&one))
	assert.Equal(t, 1, one)

	spans := exp.GetSpans()
	require.NotEmpty(t, spans, "expected at least one span from DB operations")

	foundDBSystem := false
	for _, span := range spans {
		for _, kv := range span.Attributes {
			if kv.Key == attribute.Key("db.system.name") {
				foundDBSystem = true
			}
		}
	}
	assert.True(t, foundDBSystem, "expected a span carrying the db.system.name attribute")
}

func Test_InitDB_rejects_empty_dsn(t *testing.T) {
	_, err := db.InitDB("")
	assert.Error(t, err)
}

// InitDBInstrumented must both register the OTel driver and open the DB, so
// callers cannot get the ordering wrong and silently run uninstrumented.
func Test_InitDBInstrumented_rejects_empty_dsn(t *testing.T) {
	_, err := db.InitDBInstrumented("", sdktrace.NewTracerProvider(), sdkmetric.NewMeterProvider())
	assert.Error(t, err)
}
