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

// Setup initializes tracing and metrics exporters based on the standard
// OTEL_* environment variables. If OTEL_EXPORTER_OTLP_ENDPOINT is unset or
// empty, telemetry is disabled and a no-op instance is returned.
func Setup(ctx context.Context, serviceName string) (*Telemetry, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		slog.Info("telemetry disabled", slog.String("reason", "OTEL_EXPORTER_OTLP_ENDPOINT not set"))
		return &Telemetry{}, nil
	}

	res, err := resource.New(ctx,
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

	slog.Info("telemetry enabled",
		slog.String("service_name", serviceName),
		slog.String("endpoint", endpoint),
	)

	return &Telemetry{TracerProvider: tp, MeterProvider: mp}, nil
}

// Enabled reports whether telemetry was configured with an OTLP endpoint.
func (t *Telemetry) Enabled() bool {
	return t.TracerProvider != nil && t.MeterProvider != nil
}

// Tracer returns a named tracer from the configured provider, falling back to
// the global tracer provider when telemetry is disabled.
func (t *Telemetry) Tracer(name string) trace.Tracer {
	if t == nil || !t.Enabled() {
		return otel.GetTracerProvider().Tracer(name)
	}
	return t.TracerProvider.Tracer(name)
}

// Meter returns a named meter from the configured provider, falling back to
// the global meter provider when telemetry is disabled.
func (t *Telemetry) Meter(name string) metric.Meter {
	if t == nil || !t.Enabled() {
		return otel.GetMeterProvider().Meter(name)
	}
	return t.MeterProvider.Meter(name)
}

// Shutdown flushes and releases the providers. It is safe to call on a
// disabled instance and multiple times.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil || !t.Enabled() {
		return nil
	}

	var errs []error
	errs = append(errs, t.TracerProvider.Shutdown(ctx))
	errs = append(errs, t.MeterProvider.Shutdown(ctx))
	return errors.Join(errs...)
}
