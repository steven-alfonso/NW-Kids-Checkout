package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/stretchr/testify/require"
)

func setupAuthedApp() (*fiber.App, *session.Store) {
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
	return app, store
}

func TestController_AdminPages(t *testing.T) {
	app, store := setupAuthedApp()

	controller := NewController(store)
	controller.RegisterRoutes(app)

	paths := []string{
		"/admin",
		"/admin/locations",
		"/admin/location-groups",
		"/admin/planningcenter/events",
		"/admin/planningcenter/events/evt-1",
	}

	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode, "path %s", path)
		require.True(t, strings.Contains(resp.Header.Get("Content-Type"), "text/html"), "path %s", path)
	}
}
