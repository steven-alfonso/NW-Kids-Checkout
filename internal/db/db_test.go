package db_test

import (
	"path/filepath"
	"testing"

	"kids-checkin/internal/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func Test_InitDB_creates_otel_spans_for_queries(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	prevProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	))
	t.Cleanup(func() { otel.SetTracerProvider(prevProvider) })

	dsn := filepath.Join(t.TempDir(), "otel.db")
	database, err := db.InitDB(dsn)
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
