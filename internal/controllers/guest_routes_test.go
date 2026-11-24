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
			name:   "admin submission list is gone",
			method: http.MethodGet,
			path:   "/v1/admin/guest-submissions",
			status: fiber.StatusNotFound,
		},
		{
			name: "admin guest entries page redirects to login before routing decides 404",
			// The /admin group's auth middleware answers anonymous requests
			// before Fiber looks for a matching route.
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
}
