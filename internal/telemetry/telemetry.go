package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

// Telemetry holds the OpenTelemetry providers for a process. When no OTLP
// endpoint is configured, Telemetry is disabled and all operations are no-ops.
type Telemetry struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
}

// endpointVars are the OTLP endpoint variables that enable export. The spec
// says the signal-specific ones are sufficient on their own; checking only the
// generic one made a spec-following deployment silently export nothing.
var endpointVars = []string{
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
}

// Setup initializes tracing and metrics exporters based on the standard
// OTEL_* environment variables. Telemetry stays disabled unless one of the
// OTLP endpoint variables is set and OTEL_SDK_DISABLED is not "true"; a
// disabled Telemetry is returned with nil providers and costs nothing.
func Setup(ctx context.Context, serviceName string) (*Telemetry, error) {
	if os.Getenv("OTEL_SDK_DISABLED") == "true" {
		slog.Info("telemetry disabled", slog.String("reason", "OTEL_SDK_DISABLED is true"))
		return &Telemetry{}, nil
	}
	enabledBy := ""
	for _, key := range endpointVars {
		if os.Getenv(key) != "" {
			enabledBy = key
			break
		}
	}
	if enabledBy == "" {
		slog.Info("telemetry disabled", slog.String("reason", "no OTEL_EXPORTER_OTLP_*_ENDPOINT is set"))
		return &Telemetry{}, nil
	}

	// WithAttributes is schemaless and the detectors below share the SDK's own
	// schema URL, so composing them here cannot raise a schema conflict the way
	// resource.Merge with a differently-versioned semconv package would.
	// Without the detectors the resource carried service.name alone: no
	// telemetry.sdk.* (which the spec requires), no host identity to tell
	// replicas apart in a collector, and OTEL_RESOURCE_ATTRIBUTES ignored.
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithFromEnv(),
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("creating otel resource: %w", err)
	}

	traceExporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)

	metricExporter, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		shutdownErr := tp.Shutdown(ctx)
		return nil, errors.Join(fmt.Errorf("creating metric exporter: %w", err), shutdownErr)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	// Without this the global propagator is the no-op default, otelfiber
	// extracts nothing, and every inbound traceparent starts a fresh root
	// trace. Nothing in this process could correlate with an upstream caller.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	slog.Info("telemetry enabled",
		slog.String("service_name", serviceName),
		slog.String("enabled_by", enabledBy),
	)

	return &Telemetry{TracerProvider: tp, MeterProvider: mp}, nil
}

// Enabled reports whether telemetry was configured with an OTLP endpoint. It is
// safe on a nil receiver, matching Tracer, Meter, and Shutdown.
func (t *Telemetry) Enabled() bool {
	return t != nil && t.TracerProvider != nil && t.MeterProvider != nil
}

// Tracer returns a named tracer from the configured provider, falling back to
// the global tracer provider when telemetry is disabled.
func (t *Telemetry) Tracer(name string) trace.Tracer {
	if !t.Enabled() {
		return otel.GetTracerProvider().Tracer(name)
	}
	return t.TracerProvider.Tracer(name)
}

// Meter returns a named meter from the configured provider, falling back to
// the global meter provider when telemetry is disabled.
func (t *Telemetry) Meter(name string) metric.Meter {
	if !t.Enabled() {
		return otel.GetMeterProvider().Meter(name)
	}
	return t.MeterProvider.Meter(name)
}

// Shutdown flushes and releases the providers. It is safe on a nil receiver and
// on a disabled instance.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if !t.Enabled() {
		return nil
	}

	var errs []error
	errs = append(errs, t.TracerProvider.Shutdown(ctx))
	errs = append(errs, t.MeterProvider.Shutdown(ctx))
	return errors.Join(errs...)
}
