package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoIndex(t *testing.T) {
	newApp := func() *fiber.App {
		app := fiber.New()
		app.Use(NoIndex())
		return app
	}

	t.Run("sets the header on a 200 response", func(t *testing.T) {
		app := newApp()
		app.Get("/data", func(c *fiber.Ctx) error {
			return c.JSON(fiber.Map{"ok": true})
		})

		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/data", nil))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Equal(t, "noindex", resp.Header.Get("X-Robots-Tag"))
	})

	t.Run("sets the header on a 404 response", func(t *testing.T) {
		app := newApp()

		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/missing", nil))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
		assert.Equal(t, "noindex", resp.Header.Get("X-Robots-Tag"))
	})
}
