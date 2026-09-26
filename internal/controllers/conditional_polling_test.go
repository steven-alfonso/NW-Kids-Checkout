package controllers

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"kids-checkin/internal/controllers/middleware"
	"kids-checkin/internal/web/static"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServer_compressionAndPolling covers the two transport behaviours the
// checkouts screen depends on when a phone is on a slow connection: an
// unchanged poll costs almost nothing, and payloads are compressed on the wire.
func TestServer_compressionAndPolling(t *testing.T) {
	newApp := func() *fiber.App {
		app := fiber.New()
		app.Use(middleware.Compress())
		return app
	}

	// pollHandler mimics what checkinv1 serves, without needing a database.
	pollHandler := func(c *fiber.Ctx) error {
		etag := `W/"static-for-transport-test"`
		c.Set(fiber.HeaderCacheControl, "no-cache")
		c.Set(fiber.HeaderVary, fiber.HeaderAcceptEncoding)
		c.Set(fiber.HeaderETag, etag)
		if c.Get(fiber.HeaderIfNoneMatch) == etag {
			return c.SendStatus(fiber.StatusNotModified)
		}
		return c.JSON(checkoutsTransportFixture)
	}

	t.Run("unchanged poll costs a 304 instead of the full list", func(t *testing.T) {
		app := newApp()
		app.Get("/v1/checkins/checkouts", pollHandler)

		first := httptest.NewRequest("GET", "/v1/checkins/checkouts?limit=100", nil)
		first.Header.Set("Accept", "application/json")
		first.Header.Set("Accept-Encoding", "gzip")
		resp, err := app.Test(first)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		etag := resp.Header.Get(fiber.HeaderETag)
		require.NotEmpty(t, etag)
		assert.Equal(t, "no-cache", resp.Header.Get(fiber.HeaderCacheControl),
			"the body must be storable, or the ETag never gets used")
		assert.Equal(t, "gzip", resp.Header.Get("Content-Encoding"),
			"the poll payload should be compressed for phones on slow links")

		zr, err := gzip.NewReader(resp.Body)
		require.NoError(t, err)
		decoded, err := io.ReadAll(zr)
		require.NoError(t, err)
		assert.Contains(t, string(decoded), "sam")

		// app.Test transparently decompresses, so measure the real wire size
		// against a live listener.
		raw, err := json.Marshal(checkoutsTransportFixture)
		require.NoError(t, err)
		wire := serveOverTCP(t, app, "/v1/checkins/checkouts")
		assert.Less(t, wire, len(raw)/2,
			"gzip must actually shrink the poll payload, not just be present as a header")

		second := httptest.NewRequest("GET", "/v1/checkins/checkouts?limit=100", nil)
		second.Header.Set("Accept", "application/json")
		second.Header.Set("Accept-Encoding", "gzip")
		second.Header.Set("If-None-Match", etag)
		revalidated, err := app.Test(second)
		require.NoError(t, err)
		defer revalidated.Body.Close()

		require.Equal(t, fiber.StatusNotModified, revalidated.StatusCode)
		empty, err := io.ReadAll(revalidated.Body)
		require.NoError(t, err)
		assert.Empty(t, empty, "an unchanged poll must not resend the list")
	})

	t.Run("static assets are compressed but images are not", func(t *testing.T) {
		app := newApp()
		app.Use("/static", filesystemForTest())

		css := httptest.NewRequest("GET", "/static/css/tailwind.css", nil)
		css.Header.Set("Accept-Encoding", "gzip")
		cssResp, err := app.Test(css)
		require.NoError(t, err)
		defer cssResp.Body.Close()
		require.Equal(t, fiber.StatusOK, cssResp.StatusCode)
		assert.Equal(t, "gzip", cssResp.Header.Get("Content-Encoding"),
			"the stylesheet is the largest cold-load asset after the logo")

		logo := httptest.NewRequest("GET", "/static/img/NWKids-logo.svg", nil)
		logo.Header.Set("Accept-Encoding", "gzip")
		logoResp, err := app.Test(logo)
		require.NoError(t, err)
		defer logoResp.Body.Close()
		require.Equal(t, fiber.StatusOK, logoResp.StatusCode)
		assert.Empty(t, logoResp.Header.Get("Content-Encoding"),
			"the logo is base64 raster data and does not benefit from gzip")
	})
}

// checkoutsTransportFixture is sized like a real poll (dozens of children),
// which keeps it above fasthttp's 200-byte compression floor. A toy payload
// would silently skip compression and make these assertions meaningless.
var checkoutsTransportFixture = fiber.Map{
	"checkins":        transportChildren(),
	"manual_checkins": []fiber.Map{},
}

func transportChildren() []fiber.Map {
	first := []string{"sam", "jamie", "robin", "dana", "kai", "morgan", "ellis", "quinn"}
	last := []string{"alpha", "zeta", "beta", "gamma", "delta", "omega", "kappa", "sigma"}
	out := make([]fiber.Map, 0, 32)
	for i := range 32 {
		out = append(out, fiber.Map{
			"planning_center_id": "plc_1000",
			"location_id":        int64(i + 1),
			"first_name":         first[i%len(first)],
			"last_name":          last[i%len(last)],
			"security_code":      "ABC123",
			"checked_out_at":     "2026-09-26T14:03:11.123456789Z",
			"source":             "planning_center",
		})
	}
	return out
}

// serveOverTCP returns the number of body bytes a real client receives.
// app.Test transparently decompresses, which hides the actual wire size.
func serveOverTCP(t *testing.T, app *fiber.App, path string) int {
	t.Helper()
	server := httptest.NewServer(adaptor.FiberApp(app))
	t.Cleanup(server.Close)

	req, err := http.NewRequest("GET", server.URL+path, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")

	// Disable the transport's transparent gzip so the sizes we read are the
	// bytes that actually cross the wire.
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}

	measure := func(acceptEncoding string) int {
		t.Helper()
		r := req.Clone(req.Context())
		r.Header.Set("Accept-Encoding", acceptEncoding)
		resp, err := client.Do(r)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		if size := len(body); size > 0 {
			return size
		}
		// chunked responses report no length
		return int(resp.ContentLength)
	}

	plainSize := measure("identity")
	gzipSize := measure("gzip")

	t.Logf("poll payload on the wire: %d bytes identity, %d bytes gzip", plainSize, gzipSize)
	return gzipSize
}

// filesystemForTest mirrors the /static mount in server.go so the test
// exercises the real embedded assets.
func filesystemForTest() fiber.Handler {
	return filesystem.New(filesystem.Config{
		Root:       http.FS(static.NewFilteredFS()),
		PathPrefix: "",
		Browse:     true,
	})
}
