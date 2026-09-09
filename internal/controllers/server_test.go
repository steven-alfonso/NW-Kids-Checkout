package controllers

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"kids-checkin/internal/logger"
	"kids-checkin/internal/telemetry"
	"kids-checkin/internal/web/menu"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A panicking handler must still be access-logged with the 500 that the
// client receives. That only holds when recover is the innermost middleware:
// if recover runs before the logging middleware, the panic unwinds through
// them and skips their post-c.Next() recording code.
func Test_core_middleware_access_logs_panics_as_500(t *testing.T) {
	capture := &logger.CaptureSlogHandler{}
	prevDefault := slog.Default()
	slog.SetDefault(slog.New(logger.NewTraceHandler(capture)))
	t.Cleanup(func() { slog.SetDefault(prevDefault) })

	app := fiber.New()
	require.NoError(t, registerCoreMiddleware(app, &telemetry.Telemetry{}))

	app.Get("/boom", func(c *fiber.Ctx) error {
		panic("boom")
	})

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/boom", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)

	assert.True(t, capture.ContainsInfoAttr("status", "500"),
		"panicked request must appear in the access log with status 500")
}

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
	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
}

func TestHomePageMenu(t *testing.T) {
	t.Run("admin sees guest, manual, admin, logout but no login link", func(t *testing.T) {
		app, _ := setupHomeApp()
		html := getHomeHTML(t, app)
		assert.Contains(t, html, `id="guest-checkin-link"`)
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
		assert.Contains(t, html, `href="/guest-checkin"`)
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

	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
}
