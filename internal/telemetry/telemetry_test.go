package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
