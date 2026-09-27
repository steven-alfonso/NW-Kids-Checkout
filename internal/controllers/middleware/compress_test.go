package middleware

import (
	"compress/gzip"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gzipDecode returns the decoded body, reporting whether it was gzipped.
func gzipDecode(t *testing.T, encoding string, body io.Reader) (string, bool) {
	t.Helper()
	if encoding != "gzip" {
		raw, err := io.ReadAll(body)
		require.NoError(t, err)
		return string(raw), false
	}
	zr, err := gzip.NewReader(body)
	require.NoError(t, err)
	raw, err := io.ReadAll(zr)
	require.NoError(t, err)
	return string(raw), true
}

func TestCompress(t *testing.T) {
	// Long enough to clear fasthttp's 200-byte floor for compression.
	payload := strings.Repeat(`{"first_name":"sam","last_name":"alpha"},`, 20)

	newApp := func() *fiber.App {
		app := fiber.New()
		app.Use(Compress())
		return app
	}

	t.Run("compresses JSON responses", func(t *testing.T) {
		app := newApp()
		app.Get("/data", func(c *fiber.Ctx) error {
			return c.JSON(fiber.Map{"items": payload})
		})

		req := httptest.NewRequest("GET", "/data", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, "gzip", resp.Header.Get("Content-Encoding"))
		decoded, wasGzip := gzipDecode(t, resp.Header.Get("Content-Encoding"), resp.Body)
		assert.True(t, wasGzip)
		assert.Contains(t, decoded, "sam")
	})

	t.Run("compresses HTML responses", func(t *testing.T) {
		app := newApp()
		app.Get("/page", func(c *fiber.Ctx) error {
			c.Type("html")
			return c.SendString("<html><body>" + payload + "</body></html>")
		})

		req := httptest.NewRequest("GET", "/page", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, "gzip", resp.Header.Get("Content-Encoding"))
	})

	t.Run("leaves images alone", func(t *testing.T) {
		app := newApp()
		app.Get("/img/logo.png", func(c *fiber.Ctx) error {
			c.Type("png")
			return c.SendString(payload)
		})

		req := httptest.NewRequest("GET", "/img/logo.png", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Empty(t, resp.Header.Get("Content-Encoding"),
			"PNG is already compressed; re-encoding it wastes CPU on the slowest phones")
	})

	t.Run("skips the img static path", func(t *testing.T) {
		app := newApp()
		app.Get("/static/img/NWKids-logo.svg", func(c *fiber.Ctx) error {
			c.Type("svg")
			return c.SendString(payload)
		})

		req := httptest.NewRequest("GET", "/static/img/NWKids-logo.svg", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Empty(t, resp.Header.Get("Content-Encoding"),
			"the logo is a base64 PNG inside an SVG; compressing it burns CPU for no gain")
	})

	t.Run("skips the root-mounted icons", func(t *testing.T) {
		// These are served from img/ but registered at the route root, so a
		// /static/img/ prefix check misses them. Their content type is
		// application/octet-stream, which fasthttp treats as compressible.
		for _, path := range []string{"/favicon.ico", "/apple-touch-icon.png", "/apple-touch-icon-precomposed.png"} {
			t.Run(path, func(t *testing.T) {
				app := newApp()
				// Mirror the real handlers, which call c.Type("image/png") /
				// c.Type("image/x-icon"). c.Type treats its argument as a file
				// extension, so these land on application/octet-stream, which
				// fasthttp does consider compressible.
				app.Get(path, func(c *fiber.Ctx) error {
					if strings.HasSuffix(path, ".ico") {
						c.Type("image/x-icon")
					} else {
						c.Type("image/png")
					}
					return c.SendString(payload)
				})

				req := httptest.NewRequest("GET", path, nil)
				req.Header.Set("Accept-Encoding", "gzip")
				resp, err := app.Test(req)
				require.NoError(t, err)
				defer resp.Body.Close()

				assert.Empty(t, resp.Header.Get("Content-Encoding"),
					"icons are already-compressed raster; gzip makes the PNG larger and burns CPU")
			})
		}
	})

	t.Run("leaves a websocket upgrade alone", func(t *testing.T) {
		app := newApp()
		app.Get("/ws", func(c *fiber.Ctx) error {
			// Stand in for the hijack: the upgrade handler writes to the raw
			// connection, so the response must not be re-encoded underneath it.
			c.Set("Upgrade", "websocket")
			c.Set("Connection", "Upgrade")
			return c.Status(fiber.StatusSwitchingProtocols).SendString(payload)
		})

		req := httptest.NewRequest("GET", "/ws", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, fiber.StatusSwitchingProtocols, resp.StatusCode)
		assert.Empty(t, resp.Header.Get("Content-Encoding"),
			"compressing a hijacked connection would corrupt the websocket frames")
	})

	t.Run("sends nothing compressed when the client cannot accept it", func(t *testing.T) {
		app := newApp()
		app.Get("/data", func(c *fiber.Ctx) error {
			return c.JSON(fiber.Map{"items": payload})
		})

		req := httptest.NewRequest("GET", "/data", nil)
		req.Header.Set("Accept-Encoding", "identity")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Empty(t, resp.Header.Get("Content-Encoding"))
		decoded, wasGzip := gzipDecode(t, resp.Header.Get("Content-Encoding"), resp.Body)
		assert.False(t, wasGzip)
		assert.Contains(t, decoded, "sam")
	})

	t.Run("a 304 keeps its validator and gains no encoding", func(t *testing.T) {
		app := newApp()
		app.Get("/data", func(c *fiber.Ctx) error {
			c.Set(fiber.HeaderETag, `W/"abc"`)
			if c.Get(fiber.HeaderIfNoneMatch) == `W/"abc"` {
				return c.SendStatus(fiber.StatusNotModified)
			}
			return c.JSON(fiber.Map{"items": payload})
		})

		req := httptest.NewRequest("GET", "/data", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		req.Header.Set("If-None-Match", `W/"abc"`)
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, fiber.StatusNotModified, resp.StatusCode)
		assert.Equal(t, `W/"abc"`, resp.Header.Get("ETag"),
			"the poll relies on this header surviving to keep revalidating")
		assert.Empty(t, resp.Header.Get("Content-Encoding"),
			"a bodyless 304 must not claim to be encoded")
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Empty(t, body, "a 304 carries no body")
	})
}
