package controllers

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kids-checkin/internal/web/menu"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupHomeApp() (*fiber.App, *session.Store) {
	app := fiber.New()
	store := session.New()
	app.Use(func(c *fiber.Ctx) error {
		sess, _ := store.Get(c)
		sess.Set("authenticated", true)
		sess.Set("role", "admin")
		if err := sess.Save(); err != nil {
			return err
		}
		return c.Next()
	})
	app.Get("/", homePageHandler(store))
	return app, store
}

func getHomeHTML(t *testing.T, app *fiber.App) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
}

func TestHomePageMenu(t *testing.T) {
	t.Run("admin sees manual, admin, logout but no login or guest link", func(t *testing.T) {
		app, _ := setupHomeApp()
		html := getHomeHTML(t, app)
		assert.NotContains(t, html, `id="guest-checkin-link"`)
		assert.Contains(t, html, `id="manual-checkins-link"`)
		assert.Contains(t, html, `id="admin-link"`)
		assert.Contains(t, html, `id="logout-link"`)
		assert.NotContains(t, html, `id="login-link"`)
		assert.NotContains(t, html, menu.Placeholder)
	})

	t.Run("anonymous home page does not expose admin or logout routes", func(t *testing.T) {
		app := fiber.New()
		store := session.New()
		app.Get("/", homePageHandler(store))

		html := getHomeHTML(t, app)
		assert.Contains(t, html, `id="login-link"`)
		assert.Contains(t, html, `id="manual-checkins-link"`)
		assert.NotContains(t, html, `id="guest-checkin-link"`)
		assert.NotContains(t, html, `href="/guest-checkin"`)
		assert.NotContains(t, html, "id=\"admin-link\"")
		assert.NotContains(t, html, "id=\"logout-link\"")
		assert.False(t, strings.Contains(html, `href="/admin"`), "home HTML must not expose /admin href")
		assert.False(t, strings.Contains(html, `href="/logout"`), "home HTML must not expose /logout href")
	})
}

type errSessionStore struct{}

func (e *errSessionStore) RegisterType(i any) {}
func (e *errSessionStore) Get(c *fiber.Ctx) (*session.Session, error) {
	return nil, errors.New("session error")
}
func (e *errSessionStore) Reset() error           { return nil }
func (e *errSessionStore) Delete(id string) error { return nil }

func TestHomePage_SessionErrorReturns500(t *testing.T) {
	app := fiber.New()
	app.Use(recover.New())
	app.Get("/", homePageHandler(&errSessionStore{}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
}

func TestStaticCacheControl(t *testing.T) {
	t.Run("dev forbids reuse outright", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "dev")
		assert.Equal(t, "no-store", staticCacheControl(),
			"no-cache is unenforceable without an ETag or Last-Modified to revalidate against")
	})

	t.Run("production stays immutable", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "production")
		assert.Equal(t, "public, max-age=31536000, immutable", staticCacheControl())
	})

	// A container that forgets ENVIRONMENT should not lose production caching.
	t.Run("unset environment is treated as production", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "")
		assert.Equal(t, "public, max-age=31536000, immutable", staticCacheControl(),
			"an unset ENVIRONMENT must not silently downgrade to dev caching")
	})
}

func TestParseCookieSecure(t *testing.T) {
	t.Run("empty defaults to false", func(t *testing.T) {
		assert.False(t, parseCookieSecure(""))
	})

	t.Run("true enables it", func(t *testing.T) {
		assert.True(t, parseCookieSecure("true"))
	})

	t.Run("uppercase true enables it", func(t *testing.T) {
		assert.True(t, parseCookieSecure("TRUE"))
	})

	t.Run("surrounding whitespace is ignored", func(t *testing.T) {
		assert.True(t, parseCookieSecure(" true "))
	})

	t.Run("false stays disabled", func(t *testing.T) {
		assert.False(t, parseCookieSecure("false"))
	})

	t.Run("numeric one is not true", func(t *testing.T) {
		assert.False(t, parseCookieSecure("1"))
	})
}

func TestNewCookieSecureReadsEnvironmentOnce(t *testing.T) {
	calls := 0
	getenv := func(string) string {
		calls++
		return "true"
	}
	secure := newCookieSecure(getenv)

	assert.True(t, secure())
	assert.True(t, secure())
	assert.Equal(t, 1, calls, "the environment must be read once")
}

func TestErrorHandler(t *testing.T) {
	newApp := func() *fiber.App {
		return fiber.New(fiber.Config{ErrorHandler: errorHandler})
	}

	do := func(t *testing.T, app *fiber.App, path, accept string) (*http.Response, string) {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		resp, err := app.Test(req)
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp, string(body)
	}

	t.Run("5xx hides the message and returns JSON", func(t *testing.T) {
		app := newApp()
		app.Get("/boom", func(c *fiber.Ctx) error {
			return fiber.NewError(fiber.StatusInternalServerError, "database exploded")
		})

		resp, body := do(t, app, "/boom", "")
		assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
		assert.JSONEq(t, `{"sorry":"Internal Server Error"}`, body)
		assert.NotContains(t, body, "database exploded")
	})

	t.Run("5xx with HTML accept still returns JSON", func(t *testing.T) {
		app := newApp()
		app.Get("/boom-html", func(c *fiber.Ctx) error {
			return fiber.NewError(fiber.StatusInternalServerError, "database exploded")
		})

		resp, body := do(t, app, "/boom-html", "text/html")
		assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
		assert.JSONEq(t, `{"sorry":"Internal Server Error"}`, body)
		assert.NotContains(t, body, "database exploded")
	})

	t.Run("4xx keeps the message and returns JSON", func(t *testing.T) {
		app := newApp()
		app.Get("/bad", func(c *fiber.Ctx) error {
			return fiber.NewError(fiber.StatusBadRequest, "bad input")
		})

		resp, body := do(t, app, "/bad", "")
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
		assert.JSONEq(t, `{"sorry":"bad input"}`, body)
	})

	t.Run("plain error is a generic 500", func(t *testing.T) {
		app := newApp()
		app.Get("/raw", func(c *fiber.Ctx) error {
			return errors.New("raw")
		})

		resp, body := do(t, app, "/raw", "")
		assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
		assert.JSONEq(t, `{"sorry":"Internal Server Error"}`, body)
		assert.NotContains(t, body, "raw")
	})

	t.Run("unknown path with HTML accept serves the 404 page", func(t *testing.T) {
		app := newApp()

		resp, body := do(t, app, "/missing", "text/html")
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
		assert.Contains(t, body, "Page not found")
	})

	t.Run("unknown path with .html suffix serves the 404 page without accept", func(t *testing.T) {
		app := newApp()

		resp, body := do(t, app, "/missing.html", "")
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
		assert.Contains(t, body, "Page not found")
	})

	t.Run("unknown path with JSON accept returns JSON", func(t *testing.T) {
		app := newApp()

		resp, body := do(t, app, "/missing", "application/json")
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
		assert.JSONEq(t, `{"sorry":"Cannot GET /missing"}`, body)
		assert.NotContains(t, body, "<html")
	})

	t.Run("fiber.ErrNotFound keeps its message as JSON", func(t *testing.T) {
		app := newApp()
		app.Get("/gone", func(c *fiber.Ctx) error {
			return fiber.ErrNotFound
		})

		resp, body := do(t, app, "/gone", "application/json")
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
		assert.JSONEq(t, `{"sorry":"Not Found"}`, body)
	})
}

func TestErrorHandlerLogs5xx(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	app := fiber.New(fiber.Config{ErrorHandler: errorHandler})
	app.Get("/boom", func(c *fiber.Ctx) error {
		return fiber.NewError(fiber.StatusInternalServerError, "database exploded")
	})
	app.Get("/bad", func(c *fiber.Ctx) error {
		return fiber.NewError(fiber.StatusBadRequest, "bad input")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/boom", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)

	logged := buf.String()
	assert.Contains(t, logged, "level=ERROR")
	assert.Contains(t, logged, "database exploded")
	assert.Contains(t, logged, "method=GET")
	assert.Contains(t, logged, "path=/boom")
	assert.Contains(t, logged, "status=500")

	buf.Reset()
	req = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/bad", nil)
	resp, err = app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assert.NotContains(t, buf.String(), "level=ERROR")
}

func TestShouldSuppressListenError(t *testing.T) {
	bindErr := errors.New("failed to listen: address already in use")
	serveErr := errors.New("accept tcp: too many open files")

	assert.False(t, shouldSuppressListenError(nil, true))
	assert.False(t, shouldSuppressListenError(nil, false))
	assert.False(t, shouldSuppressListenError(bindErr, true),
		"a bind failure racing cancel must surface, not exit 0 with no server")
	assert.False(t, shouldSuppressListenError(bindErr, false))
	assert.True(t, shouldSuppressListenError(serveErr, true))
	assert.False(t, shouldSuppressListenError(serveErr, false))
}
