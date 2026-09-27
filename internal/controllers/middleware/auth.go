package middleware

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"kids-checkin/internal/controllers/session"
	"kids-checkin/internal/web/static"

	"github.com/gofiber/fiber/v2"
)

func AuthRequired(sessionStore session.Storer, allowedRoles ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		sess, _ := sessionStore.Get(c)
		if sess != nil {
			c.Locals("session", sess)
		}

		// Check if logged in
		if sess.Get("authenticated") != true {
			// For API/JSON clients, return JSON instead of redirecting to login page
			// (which would cause fetch to receive HTML and hang on JSON parse).
			acceptHeader := c.Get("Accept")

			// An Accept header is a deliberate statement of what the client can
			// read, so honor it in preference to the path heuristic below. A
			// browser navigation asks for text/html, so it gets the login page;
			// fetch sends */*, which Fiber resolves to the first offered type
			// (JSON), so it gets a 401 it can act on.
			//
			// A client that accepts neither type tells us nothing, so fall
			// through to the path heuristic rather than guessing JSON. Fiber
			// also reports "" for media types carrying parameters the offer
			// lacks (text/html;charset=utf-8) and compares types
			// case-sensitively, both of which are ambiguous rather than
			// evidence of an API client.
			if acceptHeader != "" {
				switch c.Accepts(fiber.MIMEApplicationJSON, fiber.MIMETextHTML) {
				case fiber.MIMETextHTML:
					return redirectToLogin(c)
				case fiber.MIMEApplicationJSON:
					return unauthorized(c)
				}
			}

			// Nothing to go on: either no Accept header, or one that accepts
			// neither type. /v1/ is overwhelmingly JSON API, but checkouts.html
			// is served from /v1/ too, so this is a heuristic and not a rule.
			// /api/ has no guarded routes today and is kept for symmetry.
			path := c.Path()
			if strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/api/") {
				return unauthorized(c)
			}
			return redirectToLogin(c)
		}

		userRole, ok := sess.Get("role").(string)
		if !ok {
			return c.Status(http.StatusInternalServerError).SendString("Internal Server Error: Failed to fetch user role")
		}

		for _, role := range allowedRoles {
			if role == "" || userRole == role {
				return c.Next()
			}
		}

		accepts := c.Accepts(fiber.MIMETextHTML, fiber.MIMEApplicationJSON)
		if accepts == fiber.MIMETextHTML {
			f, err := static.EmbeddedFS.Open("pages/errors/forbidden.html")
			if err != nil {
				return fiber.ErrInternalServerError
			}
			defer f.Close()

			c.Type("html")
			return c.Status(http.StatusForbidden).SendStream(f)
		}

		return c.Status(http.StatusForbidden).JSON(fiber.Map{"error": "Forbidden: Insufficient permissions"})
	}
}

// unauthorized answers an unauthenticated API caller with JSON. A redirect
// would hand the caller the login page's HTML and break its JSON parsing.
func unauthorized(c *fiber.Ctx) error {
	// This response is chosen by Accept, so a shared cache must not hand the
	// JSON 401 to a browser that would have been redirected (or vice versa).
	c.Vary(fiber.HeaderAccept)
	return c.Status(http.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
}

// redirectToLogin sends an unauthenticated browser to the login page,
// remembering where it was headed.
func redirectToLogin(c *fiber.Ctx) error {
	c.Vary(fiber.HeaderAccept)
	// OriginalURL, not Path: the checkouts page is always reached with a
	// query string carrying the user's location-group and time filters, and
	// login.go hands this value back verbatim as the post-login destination.
	requestedURL := c.OriginalURL()
	if requestedURL == "" {
		requestedURL = c.Path()
	}
	return c.Redirect(fmt.Sprintf("/login?next=%s", url.QueryEscape(requestedURL)))
}
