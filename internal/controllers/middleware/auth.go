package middleware

import (
	"fmt"
	"net/http"
	"net/url"

	"kids-checkin/internal/controllers/session"
	"kids-checkin/internal/web/static"

	"github.com/gofiber/fiber/v2"
)

func AuthRequired(sessionStore session.Storer, allowedRoles ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		sess, _ := sessionStore.Get(c)

		// Check if logged in
		if sess.Get("authenticated") != true {
			// For API/JSON clients, return JSON instead of redirecting to login page
			// (which would cause fetch to receive HTML and hang on JSON parse).
			accepts := c.Accepts(fiber.MIMEApplicationJSON, fiber.MIMETextHTML)
			isAPI := accepts == fiber.MIMEApplicationJSON
			// Also treat /v1/ and /api/ paths as API even if Accept is ambiguous
			path := c.Path()
			if !isAPI && len(path) >= 4 && path[:4] == "/v1/" {
				isAPI = true
			}
			if !isAPI && len(path) >= 5 && path[:5] == "/api/" {
				isAPI = true
			}
			if isAPI {
				return c.Status(http.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
			}
			requestedURL := c.OriginalURL()
			if requestedURL == "" {
				requestedURL = c.Path()
			}
			return c.Redirect(fmt.Sprintf("/login?next=%s", url.QueryEscape(requestedURL)))
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
