package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	metricRequestDuration = "http.server.request.duration"
	metricRequests        = "http.server.requests"
	metricServerErrors    = "http.server.errors"
)

// HTTPMetrics records per-request OTel metrics: a duration histogram, a
// request counter, and a 5xx error counter. Attributes include the route
// pattern (not the raw path), method, and response status.
func HTTPMetrics(meter metric.Meter) (fiber.Handler, error) {
	duration, err := meter.Float64Histogram(
		metricRequestDuration,
		metric.WithUnit("s"),
		metric.WithDescription("Duration of HTTP server requests"),
	)
	if err != nil {
		return nil, err
	}
	requests, err := meter.Int64Counter(
		metricRequests,
		metric.WithDescription("Total number of HTTP requests"),
	)
	if err != nil {
		return nil, err
	}
	serverErrors, err := meter.Int64Counter(
		metricServerErrors,
		metric.WithDescription("Total number of HTTP responses with status >= 500"),
	)
	if err != nil {
		return nil, err
	}

	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		status := c.Response().StatusCode()
		// The fiber error handler runs after middleware unwinds, so the
		// response status is not yet set for returned errors. Infer it:
		// *fiber.Error carries its own code; any other error renders as 500.
		if ferr, ok := err.(*fiber.Error); ok {
			status = ferr.Code
		} else if err != nil {
			status = fiber.StatusInternalServerError
		}
		route := c.Route().Path
		// Unmatched requests keep c.route pointed at the outermost use-route
		// (path "/") and fall back to raw paths in some cases; collapse them
		// onto a single sentinel to bound label cardinality. A not-found or
		// method-not-allowed response on the root path can only come from an
		// unmatched request.
		if route == "" || len(c.Route().Handlers) == 0 ||
			((status == fiber.StatusNotFound || status == fiber.StatusMethodNotAllowed) && route == "/") {
			route = "UNMATCHED"
		}

		attrs := metric.WithAttributes(
			attribute.String("http.request.method", c.Method()),
			attribute.String("http.route", route),
			attribute.Int("http.response.status_code", status),
		)
		duration.Record(c.UserContext(), time.Since(start).Seconds(), attrs)
		requests.Add(c.UserContext(), 1, attrs)
		if status >= fiber.StatusInternalServerError {
			serverErrors.Add(c.UserContext(), 1, attrs)
		}
		return err
	}, nil
}
