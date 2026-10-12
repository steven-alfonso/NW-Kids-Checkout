package logger

import (
	"context"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findAttr(r slog.Record, key string) (slog.Value, bool) {
	var found slog.Value
	ok := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			found = a.Value
			ok = true
			return false
		}
		return true
	})
	return found, ok
}

func Test_TraceHandler_adds_trace_ids_from_context(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSampler(trace.AlwaysSample()),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(t.Context(), "op")
	defer span.End()

	capture := &CaptureSlogHandler{}
	logger := slog.New(NewTraceHandler(capture))

	logger.InfoContext(ctx, "inside span")
	logger.InfoContext(t.Context(), "outside span")

	require.Len(t, capture.records, 2)

	inSpan := capture.records[0]
	traceIDVal, hasTraceID := findAttr(inSpan, "trace_id")
	spanIDVal, hasSpanID := findAttr(inSpan, "span_id")
	require.True(t, hasTraceID, "expected trace_id attr inside span")
	require.True(t, hasSpanID, "expected span_id attr inside span")
	assert.Equal(t, span.SpanContext().TraceID().String(), traceIDVal.String())
	assert.Equal(t, span.SpanContext().SpanID().String(), spanIDVal.String())

	outsideSpan := capture.records[1]
	_, hasTraceID = findAttr(outsideSpan, "trace_id")
	assert.False(t, hasTraceID, "expected no trace_id attr outside span")
}
