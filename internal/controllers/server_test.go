package controllers

import (
	"errors"
	"io"
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
	// Dev is the case that was broken. cmd/assets returns early when
	// ENVIRONMENT=dev, leaving ?v=dev constant in the HTML, so an immutable
	// header would pin the last build's JS in the browser while the HTML
	// revalidates underneath it.
	t.Run("dev revalidates instead of pinning for a year", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "dev")
		assert.Equal(t, "no-cache", staticCacheControl(),
			"a constant ?v= cannot bust a cache, so dev must not be immutable")
	})

	// no-store would also fix the staleness but forces a full re-download on
	// every reload; no-cache keeps conditional requests cheap.
	t.Run("dev still allows conditional requests", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "dev")
		assert.NotEqual(t, "no-store", staticCacheControl(),
			"no-store defeats revalidation and re-downloads unchanged assets every load")
	})

	// Production is unaffected: cmd/assets bakes a content hash into ?v=, so a
	// changed file is a changed URL and there is nothing to revalidate.
	t.Run("production stays immutable", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "production")
		assert.Equal(t, "public, max-age=31536000, immutable", staticCacheControl())
	})

	// Unset must not be treated as dev. A container that forgets ENVIRONMENT
	// would otherwise silently lose the immutable caching that production
	// depends on.
	t.Run("unset environment is treated as production", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "")
		assert.Equal(t, "public, max-age=31536000, immutable", staticCacheControl(),
			"an unset ENVIRONMENT must not silently downgrade to dev caching")
	})
}
