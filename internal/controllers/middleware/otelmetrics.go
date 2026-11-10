package middleware

import (
	"time"

	"github.com/gofiber/contrib/websocket"
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
		// A websocket upgrade blocks the handler for the life of the socket.
		// Recording it as one request would put a multi-hour sample in the
		// duration histogram on every reconnect, so the connection's own
		// lifecycle is the signal for this route instead.
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}

		start := time.Now()
		err := c.Next()
		status := responseStatus(c, err)
		attrs := metric.WithAttributes(
			attribute.String("http.request.method", c.Method()),
			attribute.String("http.route", RouteLabel(c, status)),
			attribute.Int("http.response.status_code", status),
		)
		ctx := SpanCtx(c)
		duration.Record(ctx, time.Since(start).Seconds(), attrs)
		requests.Add(ctx, 1, attrs)
		if status >= fiber.StatusInternalServerError {
			serverErrors.Add(ctx, 1, attrs)
		}
		return err
	}, nil
}
