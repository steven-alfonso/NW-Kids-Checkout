package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGuestRoutesAreNotRegistered(t *testing.T) {
	app := fiber.New()
	registerRoutes(app, nil, session.New(), nil)

	tests := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{
			name:   "kiosk page is gone",
			method: http.MethodGet,
			path:   "/guest-checkin",
			status: fiber.StatusNotFound,
		},
		{
			name:   "public submission create is gone",
			method: http.MethodPost,
			path:   "/v1/checkins/guest-submissions",
			status: fiber.StatusNotFound,
		},
		{
			name:   "submission list is gone",
			method: http.MethodGet,
			path:   "/v1/checkins/guest-submissions",
			status: fiber.StatusNotFound,
		},
		{
			name:   "status update is gone",
			method: http.MethodPatch,
			path:   "/v1/checkins/guest-submissions/abc/status",
			status: fiber.StatusNotFound,
		},
		{
			name:   "submission checkins are gone",
			method: http.MethodPost,
			path:   "/v1/checkins/guest-submissions/abc/checkins",
			status: fiber.StatusNotFound,
		},
		{
			name:   "admin submission list is gone",
			method: http.MethodGet,
			path:   "/v1/admin/guest-submissions",
			status: fiber.StatusNotFound,
		},
		{
			// The /admin group's auth middleware answers anonymous requests
			// before Fiber looks for a matching route, so this pins the
			// redirect rather than the missing route.
			name:   "anonymous admin guest entries redirects to login via group middleware",
			method: http.MethodGet,
			path:   "/admin/guest-entries",
			status: fiber.StatusFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil)
			resp, err := app.Test(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.status, resp.StatusCode)
		})
	}

	t.Run("admin guest entries page is gone for an authenticated admin", func(t *testing.T) {
		app := fiber.New()
		store := session.New()
		app.Use(func(c *fiber.Ctx) error {
			sess, err := store.Get(c)
			if err != nil {
				return err
			}
			sess.Set("authenticated", true)
			sess.Set("role", "admin")
			if err := sess.Save(); err != nil {
				return err
			}
			return c.Next()
		})
		registerRoutes(app, nil, store, nil)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/admin/guest-entries", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode,
			"an authenticated admin must hit the 404, so re-registering the controller fails this test")
	})
}
