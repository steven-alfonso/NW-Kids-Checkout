package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func Test_Setup_returns_disabled_telemetry_without_endpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")

	tel, err := Setup(t.Context(), "test-service")

	require.NoError(t, err)
	assert.False(t, tel.Enabled())
	assert.Nil(t, tel.TracerProvider)
	assert.Nil(t, tel.MeterProvider)
	assert.NoError(t, tel.Shutdown(t.Context()))
}

func Test_Setup_enabled_telemetry_sets_global_providers(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:14317")
	t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "250")

	tel, err := Setup(t.Context(), "test-service")
	require.NoError(t, err)
	t.Cleanup(func() { _ = tel.Shutdown(context.Background()) })

	assert.True(t, tel.Enabled())
	assert.NotNil(t, tel.TracerProvider)
	assert.NotNil(t, tel.MeterProvider)

	tracer := tel.Tracer("test")
	_, span := tracer.Start(t.Context(), "op")
	assert.True(t, span.IsRecording(), "expected real tracer producing recording spans")
	span.End()
}

func Test_Setup_tracer_is_usable_noop_when_disabled(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	tel, err := Setup(t.Context(), "test-service")
	require.NoError(t, err)

	tracer := tel.Tracer("test")
	_, span := tracer.Start(t.Context(), "op")
	defer span.End()
	assert.False(t, span.IsRecording())
}

// The OTLP spec says a signal-specific endpoint is sufficient on its own;
// checking only the generic variable made a spec-following deployment export
// nothing.
func Test_Setup_enables_on_signal_specific_endpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://127.0.0.1:14317")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_TIMEOUT", "250")

	tel, err := Setup(t.Context(), "test-service")
	require.NoError(t, err)
	t.Cleanup(func() { _ = tel.Shutdown(context.Background()) })

	assert.True(t, tel.Enabled())
}

func Test_Setup_respects_OTEL_SDK_DISABLED(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:14317")
	t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "250")
	t.Setenv("OTEL_SDK_DISABLED", "true")

	tel, err := Setup(t.Context(), "test-service")
	require.NoError(t, err)

	assert.False(t, tel.Enabled())
	assert.Nil(t, tel.TracerProvider)
}

// Without a global propagator otelfiber extracts nothing and every inbound
// traceparent starts a fresh root trace, so nothing correlates with a caller.
func Test_Setup_installs_trace_context_propagator(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:14317")
	t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "250")

	tel, err := Setup(t.Context(), "test-service")
	require.NoError(t, err)
	t.Cleanup(func() { _ = tel.Shutdown(context.Background()) })

	carrier := propagation.HeaderCarrier{}
	carrier.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	got := trace.SpanContextFromContext(otel.GetTextMapPropagator().Extract(t.Context(), carrier))
	require.True(t, got.IsValid(), "expected the global propagator to extract a span context")
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", got.TraceID().String())
}
