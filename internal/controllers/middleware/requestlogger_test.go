package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newLoggedAuthApp builds the production middleware chain in order: request
// logger, access logger, recover, then the session stamping and AuthRequired.
// recover follows the loggers in server.go, and the stamping middleware runs
// before AuthRequired so the session exists when auth reads it.
func newLoggedAuthApp(authenticated bool, role string) *fiber.App {
	app := fiber.New()
	store := session.New()

	app.Use(RequestLogger(), HTTPAccessLogger())
	app.Use(recover.New())

	app.Use(func(c *fiber.Ctx) error {
		if authenticated {
			sess, _ := store.Get(c)
			sess.Set("authenticated", true)
			sess.Set("role", role)
			sess.Set(usernameKey, "alice")
			if err := sess.Save(); err != nil {
				return err
			}
		}
		return c.Next()
	})

	handler := func(c *fiber.Ctx) error { return c.SendString("ok") }
	app.Post("/v1/checkins", AuthRequired(store, ""), handler)
	app.Patch("/v1/checkins", AuthRequired(store, ""), handler)
	app.Put("/v1/checkins", AuthRequired(store, ""), handler)
	app.Delete("/v1/checkins", AuthRequired(store, ""), handler)
	app.Get("/v1/checkins", AuthRequired(store, ""), handler)
	app.Head("/v1/checkins", AuthRequired(store, ""), handler)
	app.Post("/admin/locations", AuthRequired(store, "admin"), handler)

	return app
}

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return &buf
}

func TestHTTPAccessLogger_authenticatedWritesLogUsername(t *testing.T) {
	methods := []string{
		http.MethodPost,
		http.MethodPatch,
		http.MethodPut,
		http.MethodDelete,
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			buf := captureSlog(t)

			app := newLoggedAuthApp(true, "admin")
			resp, err := app.Test(httptest.NewRequest(method, "/v1/checkins", nil))
			require.NoError(t, err)

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			out := buf.String()
			assert.Contains(t, out, "msg=request")
			assert.Contains(t, out, "method="+method)
			assert.Contains(t, out, "path=/v1/checkins")
			assert.Contains(t, out, "username=alice")
			assert.NotContains(t, out, "session_id=")
			assert.NotContains(t, out, "token=")
		})
	}
}

func TestHTTPAccessLogger_authenticatedGetDoesNotLogUsername(t *testing.T) {
	buf := captureSlog(t)

	app := newLoggedAuthApp(true, "admin")
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/v1/checkins", nil))
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	out := buf.String()
	assert.Contains(t, out, "msg=request")
	assert.Contains(t, out, "method=GET")
	assert.NotContains(t, out, "username=")
}

func TestHTTPAccessLogger_authenticatedHeadDoesNotLogUsername(t *testing.T) {
	buf := captureSlog(t)

	app := newLoggedAuthApp(true, "admin")
	resp, err := app.Test(httptest.NewRequest(http.MethodHead, "/v1/checkins", nil))
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	out := buf.String()
	assert.Contains(t, out, "msg=request")
	assert.Contains(t, out, "method=HEAD")
	assert.NotContains(t, out, "username=")
}

func TestHTTPAccessLogger_forbiddenWriteStillLogsUsername(t *testing.T) {
	buf := captureSlog(t)

	// The user is authenticated but lacks the admin role, so AuthRequired
	// denies the request. The action failed, but the actor is still known and
	// belongs in the audit trail.
	app := newLoggedAuthApp(true, "user")
	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/admin/locations", nil))
	require.NoError(t, err)

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	out := buf.String()
	assert.Contains(t, out, "msg=request")
	assert.Contains(t, out, "method=POST")
	assert.Contains(t, out, "path=/admin/locations")
	assert.Contains(t, out, "username=alice")
}

func TestHTTPAccessLogger_unauthenticatedPostDoesNotLogUsername(t *testing.T) {
	buf := captureSlog(t)

	app := newLoggedAuthApp(false, "")
	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/v1/checkins", nil))
	require.NoError(t, err)

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	out := buf.String()
	assert.Contains(t, out, "msg=request")
	assert.Contains(t, out, "method=POST")
	assert.NotContains(t, out, "username=")
}

func TestGetLoggerReturnsTheRequestScopedLogger(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	app := fiber.New()
	app.Use(RequestLogger())
	app.Get("/", func(c *fiber.Ctx) error {
		GetLogger(c).InfoContext(c.Context(), "scoped")
		return c.SendStatus(http.StatusOK)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, buf.String(), "request_id=")
	assert.Contains(t, buf.String(), "msg=scoped")
}
