package metricsv1

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"kids-checkin/internal/repo/metrics"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestController_GetMetrics(t *testing.T) {
	app, store := setupAuthedApp("admin")

	mockRepo := &metrics.MockRepo{
		ListDailyMetricsFunc: func(ctx context.Context, filter metrics.Filter) ([]metrics.DailyMetric, error) {
			assert.Equal(t, 14, filter.Days)
			return []metrics.DailyMetric{
				{
					Date:              "2026-08-18",
					EventName:         "Kids",
					Called:            5,
					Confirmed:         4,
					Unconfirmed:       1,
					AvgConfirmMinutes: 3.567,
				},
			}, nil
		},
	}

	controller := NewController(mockRepo, store)
	controller.RegisterRoutes(app)

	t.Run("returns daily metrics", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/admin/metrics", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var payload MetricsResponse
		err = json.NewDecoder(resp.Body).Decode(&payload)
		require.NoError(t, err)
		require.Len(t, payload.Daily, 1)
		assert.Equal(t, 14, payload.Days)
		assert.Equal(t, "2026-08-18", payload.Daily[0].Date)
		assert.Equal(t, "Kids", payload.Daily[0].EventName)
		assert.Equal(t, 5, payload.Daily[0].Called)
		assert.Equal(t, 4, payload.Daily[0].Confirmed)
		assert.Equal(t, 1, payload.Daily[0].Unconfirmed)
		assert.Equal(t, 3.57, payload.Daily[0].AvgConfirmMinutes)
	})

	t.Run("invalid days returns bad request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/admin/metrics?days=abc", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("days below range returns bad request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/admin/metrics?days=0", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("days above range returns bad request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/admin/metrics?days=91", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})
}

func TestController_GetMetrics_RequiresAdmin(t *testing.T) {
	app, store := setupAuthedApp("user")

	controller := NewController(&metrics.MockRepo{}, store)
	controller.RegisterRoutes(app)

	req := httptest.NewRequest("GET", "/v1/admin/metrics", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusForbidden, resp.StatusCode)
}

func TestController_GetFetchLatency(t *testing.T) {
	app, store := setupAuthedApp("admin")

	mockRepo := &metrics.MockRepo{
		ListFetchLatencyFunc: func(ctx context.Context, filter metrics.Filter) ([]metrics.FetchLatencyMetric, error) {
			assert.Equal(t, 14, filter.Days)
			return []metrics.FetchLatencyMetric{
				{Date: "2026-08-18", Count: 120, AvgMs: 1234.567, P95Ms: 3456.789, P99Ms: 9876.543},
			}, nil
		},
	}

	controller := NewController(mockRepo, store)
	controller.RegisterRoutes(app)

	t.Run("returns fetch latency rows", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/admin/metrics/fetch-latency", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var payload FetchLatencyResponse
		err = json.NewDecoder(resp.Body).Decode(&payload)
		require.NoError(t, err)
		require.Len(t, payload.Rows, 1)
		assert.Equal(t, 14, payload.Days)
		assert.Equal(t, "2026-08-18", payload.Rows[0].Date)
		assert.Equal(t, 120, payload.Rows[0].Count)
		assert.Equal(t, 1234.57, payload.Rows[0].AvgMs)
		assert.Equal(t, 3456.79, payload.Rows[0].P95Ms)
		assert.Equal(t, 9876.54, payload.Rows[0].P99Ms)
	})

	t.Run("invalid days returns bad request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/admin/metrics/fetch-latency?days=abc", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("empty rows", func(t *testing.T) {
		app2, store2 := setupAuthedApp("admin")
		mockRepo2 := &metrics.MockRepo{
			ListFetchLatencyFunc: func(ctx context.Context, filter metrics.Filter) ([]metrics.FetchLatencyMetric, error) {
				return nil, nil
			},
		}
		controller2 := NewController(mockRepo2, store2)
		controller2.RegisterRoutes(app2)

		req := httptest.NewRequest("GET", "/v1/admin/metrics/fetch-latency?days=7", nil)
		resp, err := app2.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var payload FetchLatencyResponse
		err = json.NewDecoder(resp.Body).Decode(&payload)
		require.NoError(t, err)
		assert.Equal(t, 7, payload.Days)
		assert.Empty(t, payload.Rows)
	})
}

func TestController_GetFetchLatency_RequiresAdmin(t *testing.T) {
	app, store := setupAuthedApp("user")

	controller := NewController(&metrics.MockRepo{}, store)
	controller.RegisterRoutes(app)

	req := httptest.NewRequest("GET", "/v1/admin/metrics/fetch-latency", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusForbidden, resp.StatusCode)
}

func TestController_GetGuestMetrics(t *testing.T) {
	app, store := setupAuthedApp("admin")

	mockRepo := &metrics.MockRepo{
		ListGuestMetricsFunc: func(ctx context.Context, filter metrics.Filter) ([]metrics.GuestMetric, error) {
			assert.Equal(t, 14, filter.Days)
			return []metrics.GuestMetric{
				{Date: "2026-08-18", Submissions: 5, Children: 9, Entered: 2, Approved: 1, Rejected: 1, Pending: 1},
			}, nil
		},
	}

	controller := NewController(mockRepo, store)
	controller.RegisterRoutes(app)

	t.Run("returns guest metrics rows", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/admin/metrics/guest", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var payload GuestMetricsResponse
		err = json.NewDecoder(resp.Body).Decode(&payload)
		require.NoError(t, err)
		require.Len(t, payload.Rows, 1)
		assert.Equal(t, 14, payload.Days)
		assert.Equal(t, "2026-08-18", payload.Rows[0].Date)
		assert.Equal(t, 5, payload.Rows[0].Submissions)
		assert.Equal(t, 9, payload.Rows[0].Children)
		assert.Equal(t, 2, payload.Rows[0].Entered)
		assert.Equal(t, 1, payload.Rows[0].Approved)
		assert.Equal(t, 1, payload.Rows[0].Rejected)
		assert.Equal(t, 1, payload.Rows[0].Pending)
	})

	t.Run("invalid days returns bad request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/admin/metrics/guest?days=abc", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("empty rows", func(t *testing.T) {
		app2, store2 := setupAuthedApp("admin")
		mockRepo2 := &metrics.MockRepo{
			ListGuestMetricsFunc: func(ctx context.Context, filter metrics.Filter) ([]metrics.GuestMetric, error) {
				return nil, nil
			},
		}
		controller2 := NewController(mockRepo2, store2)
		controller2.RegisterRoutes(app2)

		req := httptest.NewRequest("GET", "/v1/admin/metrics/guest?days=7", nil)
		resp, err := app2.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var payload GuestMetricsResponse
		err = json.NewDecoder(resp.Body).Decode(&payload)
		require.NoError(t, err)
		assert.Equal(t, 7, payload.Days)
		assert.Empty(t, payload.Rows)
	})
}

func TestController_GetGuestMetrics_RequiresAdmin(t *testing.T) {
	app, store := setupAuthedApp("user")

	controller := NewController(&metrics.MockRepo{}, store)
	controller.RegisterRoutes(app)

	req := httptest.NewRequest("GET", "/v1/admin/metrics/guest", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusForbidden, resp.StatusCode)
}

func setupAuthedApp(role string) (*fiber.App, *session.Store) {
	app := fiber.New()
	store := session.New()
	app.Use(func(c *fiber.Ctx) error {
		sess, _ := store.Get(c)
		sess.Set("authenticated", true)
		sess.Set("role", role)
		if err := sess.Save(); err != nil {
			return err
		}
		return c.Next()
	})
	return app, store
}
