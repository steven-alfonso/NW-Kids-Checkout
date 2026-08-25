package middleware

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func collectMetric(t *testing.T, reader *sdkmetric.ManualReader, name string) metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m
			}
		}
	}
	return metricdata.Metrics{}
}

func Test_HTTPMetrics_records_duration_and_requests(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(t.Context()) })

	mw, err := HTTPMetrics(mp.Meter("test"))
	require.NoError(t, err)

	app := fiber.New()
	app.Use(mw)
	app.Get("/hello/:name", func(c *fiber.Ctx) error {
		return c.SendString("hi")
	})

	req, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/hello/world", nil))
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, req.StatusCode)

	hist := collectMetric(t, reader, "http.server.request.duration")
	require.Equal(t, "s", hist.Unit)
	data, ok := hist.Data.(metricdata.Histogram[float64])
	require.True(t, ok, "expected histogram data point")

	count := 0
	statusSeen := false
	routeSeen := false
	for _, dp := range data.DataPoints {
		count += int(dp.Count)
		_, hasStatus := dp.Attributes.Value(attribute.Key("http.response.status_code"))
		_, hasRoute := dp.Attributes.Value(attribute.Key("http.route"))
		statusSeen = statusSeen || hasStatus
		routeSeen = routeSeen || hasRoute
	}
	assert.Equal(t, 1, count)
	assert.True(t, statusSeen, "expected status code attr on datapoint")
	assert.True(t, routeSeen, "expected http.route attr on datapoint")

	requests := collectMetric(t, reader, "http.server.requests")
	reqData, ok := requests.Data.(metricdata.Sum[int64])
	require.True(t, ok, "expected sum data for request counter")
	total := int64(0)
	for _, dp := range reqData.DataPoints {
		total += dp.Value
	}
	assert.Equal(t, int64(1), total)
}

func Test_HTTPMetrics_counts_server_errors(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(t.Context()) })

	mw, err := HTTPMetrics(mp.Meter("test"))
	require.NoError(t, err)

	app := fiber.New()
	app.Use(mw)
	app.Get("/boom", func(c *fiber.Ctx) error {
		return fiber.NewError(fiber.StatusInternalServerError, "boom")
	})
	app.Get("/ok", func(c *fiber.Ctx) error {
		return c.SendString("fine")
	})

	_, err = app.Test(httptest.NewRequest(fiber.MethodGet, "/boom", nil))
	require.NoError(t, err)
	_, err = app.Test(httptest.NewRequest(fiber.MethodGet, "/ok", nil))
	require.NoError(t, err)

	errMetric := collectMetric(t, reader, "http.server.errors")
	errSum, ok := errMetric.Data.(metricdata.Sum[int64])
	require.True(t, ok, "expected sum data for error counter")
	total := int64(0)
	for _, dp := range errSum.DataPoints {
		total += dp.Value
	}
	assert.Equal(t, int64(1), total, "only the 5xx response should be counted as an error")
}

func Test_HTTPMetrics_treats_wrapped_errors_as_server_errors(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(t.Context()) })

	mw, err := HTTPMetrics(mp.Meter("test"))
	require.NoError(t, err)

	app := fiber.New()
	app.Use(mw)
	// Handlers that return plain (non-*fiber.Error) errors render as 500 via
	// the error handler after middleware unwinds; metrics must infer that.
	app.Get("/wrapped", func(c *fiber.Ctx) error {
		return fmt.Errorf("db exploded: %w", errors.New("root cause"))
	})

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/wrapped", nil))
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)

	errMetric := collectMetric(t, reader, "http.server.errors")
	errSum, ok := errMetric.Data.(metricdata.Sum[int64])
	require.True(t, ok, "expected sum data for error counter")
	total := int64(0)
	for _, dp := range errSum.DataPoints {
		total += dp.Value
	}
	assert.Equal(t, int64(1), total, "wrapped non-fiber errors must be counted as server errors")

	hist := collectMetric(t, reader, "http.server.request.duration")
	data, ok := hist.Data.(metricdata.Histogram[float64])
	require.True(t, ok, "expected histogram data point")
	for _, dp := range data.DataPoints {
		statusVal, has := dp.Attributes.Value(attribute.Key("http.response.status_code"))
		if has {
			assert.Equal(t, int64(fiber.StatusInternalServerError), statusVal.AsInt64(),
				"wrapped non-fiber errors must be recorded with a 500 status attr")
		}
	}
}

// A fiber.Error wrapped in fmt.Errorf renders with its own status via the
// app error handler (which uses errors.As); metrics must attribute the same
// status instead of assuming 500.
func Test_HTTPMetrics_unwraps_wrapped_fiber_errors_for_status(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(t.Context()) })

	mw, err := HTTPMetrics(mp.Meter("test"))
	require.NoError(t, err)

	app := fiber.New()
	app.Use(mw)
	app.Get("/wrapped-4xx", func(c *fiber.Ctx) error {
		return fmt.Errorf("lookup failed: %w", fiber.NewError(fiber.StatusBadRequest, "bad input"))
	})

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/wrapped-4xx", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)

	hist := collectMetric(t, reader, "http.server.request.duration")
	data, ok := hist.Data.(metricdata.Histogram[float64])
	require.True(t, ok, "expected histogram data point")
	statuses := map[int64]int{}
	for _, dp := range data.DataPoints {
		if statusVal, has := dp.Attributes.Value(attribute.Key("http.response.status_code")); has {
			statuses[statusVal.AsInt64()]++
		}
	}
	assert.Equal(t, map[int64]int{int64(fiber.StatusBadRequest): 1}, statuses,
		"wrapped fiber.Error must be recorded with its real status code")

	errMetric := collectMetric(t, reader, "http.server.errors")
	total := int64(0)
	if errSum, ok := errMetric.Data.(metricdata.Sum[int64]); ok {
		for _, dp := range errSum.DataPoints {
			total += dp.Value
		}
	}
	assert.Equal(t, int64(0), total, "a 4xx must not be counted as a server error")
}

func Test_HTTPMetrics_collapses_unmatched_routes(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(t.Context()) })

	mw, err := HTTPMetrics(mp.Meter("test"))
	require.NoError(t, err)

	app := fiber.New()
	app.Use(mw)
	app.Get("/known", func(c *fiber.Ctx) error {
		return c.SendString("hi")
	})

	for _, path := range []string{"/missing-one", "/missing-two"} {
		_, err := app.Test(httptest.NewRequest(fiber.MethodGet, path, nil))
		require.NoError(t, err)
	}

	requests := collectMetric(t, reader, "http.server.requests")
	reqData, ok := requests.Data.(metricdata.Sum[int64])
	require.True(t, ok, "expected sum data for request counter")

	unmatchedTotal := int64(0)
	routeLabels := map[string]bool{}
	for _, dp := range reqData.DataPoints {
		routeVal, has := dp.Attributes.Value(attribute.Key("http.route"))
		require.True(t, has, "expected http.route attr on every datapoint")
		routeLabels[routeVal.AsString()] = true
		if routeVal.AsString() == "UNMATCHED" {
			unmatchedTotal += dp.Value
			// Raw request paths must never leak into the route label.
			for _, kv := range dp.Attributes.ToSlice() {
				assert.NotEqual(t, "/missing-one", string(kv.Key))
				assert.NotEqual(t, "/missing-two", string(kv.Key))
			}
		}
	}
	assert.Equal(t, int64(2), unmatchedTotal,
		"both unmatched requests should be recorded")
	assert.Equal(t, map[string]bool{"UNMATCHED": true}, routeLabels,
		"both unmatched requests should share the single UNMATCHED route label")
}
