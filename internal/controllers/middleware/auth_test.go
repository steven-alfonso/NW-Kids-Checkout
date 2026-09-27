package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	fibersession "kids-checkin/internal/controllers/session"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// browserAccept is the Accept header Chrome sends for a top-level document
// navigation, captured rather than invented. Only the leading text/html and
// its ranking above the catch-all actually affect the decision; the trailing
// entries are included so the constant cannot drift into a shape no browser
// sends.
const browserAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"

// realCheckoutsURL is how home.js actually links to the checkouts page. The
// query string carries the user's filters, so the redirect has to preserve it
// or they land back on an unfiltered page after logging in.
const realCheckoutsURL = "/v1/checkins/checkouts?location_group_name=Kids%20Jr&checked_out_after=-31m"

// newAuthTestApp builds an app with AuthRequired guarding /v1/checkins/checkouts
// (a path that serves BOTH an HTML page and JSON) and /admin/locations (HTML
// only). The middleware that stamps the session runs before AuthRequired.
func newAuthTestApp(authenticated bool) *fiber.App {
	app := fiber.New()
	// Mirrors server.go: auth.go calls sess.Get outside the nil check that
	// guards c.Locals, so a store failure panics. Production has recover
	// ahead of the routes; without it here the panic would kill the test
	// binary instead of failing one assertion.
	app.Use(recover.New())
	store := session.New()

	app.Use(func(c *fiber.Ctx) error {
		if authenticated {
			sess, _ := store.Get(c)
			sess.Set("authenticated", true)
			sess.Set("role", "admin")
			if err := sess.Save(); err != nil {
				return err
			}
		}
		return c.Next()
	})

	handler := func(c *fiber.Ctx) error { return c.SendString("ok") }
	app.Get("/v1/checkins/checkouts", AuthRequired(store, ""), handler)
	app.Get("/admin/locations", AuthRequired(store, ""), handler)

	return app
}

func TestAuthRequired_unauthenticated_clientTypes(t *testing.T) {
	t.Run("browser navigation to a v1 HTML page redirects to login", func(t *testing.T) {
		app := newAuthTestApp(false)

		req := httptest.NewRequest(http.MethodGet, "/v1/checkins/checkouts", nil)
		req.Header.Set("Accept", browserAccept)
		resp, err := app.Test(req)
		require.NoError(t, err)

		// The checkouts page is a browser-facing HTML document served from a
		// /v1/ path, so the path prefix must not force a JSON 401.
		assert.Equal(t, fiber.StatusFound, resp.StatusCode)
		// Assert the whole Location, not just that it mentions /login. The
		// next= round trip is the only thing carrying the user back to where
		// they were going, and it has to survive a dropped or unescaped value.
		assert.Equal(t, "/login?next=%2Fv1%2Fcheckins%2Fcheckouts", resp.Header.Get("Location"))
	})

	t.Run("redirect preserves the query string the user was filtering by", func(t *testing.T) {
		app := newAuthTestApp(false)

		req := httptest.NewRequest(http.MethodGet, realCheckoutsURL, nil)
		req.Header.Set("Accept", browserAccept)
		resp, err := app.Test(req)
		require.NoError(t, err)

		assert.Equal(t, fiber.StatusFound, resp.StatusCode)
		// Doubly escaped: QueryEscape over a URL that already contains %20.
		// login.go reads it back through a single c.Query decode, so this
		// round trips to the original URL.
		assert.Equal(t,
			"/login?next=%2Fv1%2Fcheckins%2Fcheckouts%3Flocation_group_name%3DKids%2520Jr%26checked_out_after%3D-31m",
			resp.Header.Get("Location"))
	})

	t.Run("client that accepts neither type falls back to the path heuristic", func(t *testing.T) {
		app := newAuthTestApp(false)

		// text/plain satisfies neither offered type, so Fiber reports no
		// match. That is ambiguity, not evidence of an API client, so a
		// non-/v1/ page must still redirect rather than 401.
		req := httptest.NewRequest(http.MethodGet, "/admin/locations", nil)
		req.Header.Set("Accept", "text/plain")
		resp, err := app.Test(req)
		require.NoError(t, err)

		assert.Equal(t, fiber.StatusFound, resp.StatusCode)
		assert.Equal(t, "/login?next=%2Fadmin%2Flocations", resp.Header.Get("Location"))
	})

	t.Run("JSON client gets 401 instead of an HTML redirect", func(t *testing.T) {
		app := newAuthTestApp(false)

		req := httptest.NewRequest(http.MethodGet, "/v1/checkins/checkouts", nil)
		req.Header.Set("Accept", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)

		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
		assert.Equal(t, fiber.MIMEApplicationJSON, resp.Header.Get("Content-Type"))
	})

	t.Run("fetch default Accept gets 401 instead of an HTML redirect", func(t *testing.T) {
		app := newAuthTestApp(false)

		// fetch() sends Accept: */* by default. A redirect here would hand the
		// caller the login page's HTML and break JSON parsing.
		req := httptest.NewRequest(http.MethodGet, "/v1/checkins/checkouts", nil)
		req.Header.Set("Accept", "*/*")
		resp, err := app.Test(req)
		require.NoError(t, err)

		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
		assert.Equal(t, fiber.MIMEApplicationJSON, resp.Header.Get("Content-Type"))
	})

	t.Run("no Accept header falls back to the v1 path heuristic", func(t *testing.T) {
		app := newAuthTestApp(false)

		req := httptest.NewRequest(http.MethodGet, "/v1/checkins/checkouts", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)

		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
		assert.Equal(t, fiber.MIMEApplicationJSON, resp.Header.Get("Content-Type"))
	})

	t.Run("no Accept header on an admin page still redirects", func(t *testing.T) {
		app := newAuthTestApp(false)

		req := httptest.NewRequest(http.MethodGet, "/admin/locations", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)

		assert.Equal(t, fiber.StatusFound, resp.StatusCode)
		assert.Equal(t, "/login?next=%2Fadmin%2Flocations", resp.Header.Get("Location"))
	})
}

func TestAuthRequired_authenticated_passesThrough(t *testing.T) {
	app := newAuthTestApp(true)

	req := httptest.NewRequest(http.MethodGet, "/v1/checkins/checkouts", nil)
	req.Header.Set("Accept", browserAccept)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
}

var _ fibersession.Storer = session.New()
