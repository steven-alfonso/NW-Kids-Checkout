package controllers

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"kids-checkin/internal/web/menu"

	"github.com/gofiber/fiber/v2"
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
		assert.Contains(t, html, `href="/checkin"`)
		assert.NotContains(t, html, "id=\"admin-link\"")
		assert.NotContains(t, html, "id=\"logout-link\"")
		assert.False(t, strings.Contains(html, `href="/admin"`), "home HTML must not expose /admin href")
		assert.False(t, strings.Contains(html, `href="/logout"`), "home HTML must not expose /logout href")
	})
}
