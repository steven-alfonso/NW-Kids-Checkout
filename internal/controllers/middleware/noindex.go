package middleware

import "github.com/gofiber/fiber/v2"

// NoIndex marks every response as not indexable. Cloudflare Access already
// keeps crawlers out, but a misconfiguration there should not put the app in
// search results. Setting the header on every response, including JSON and
// static assets, is harmless.
func NoIndex() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set("X-Robots-Tag", "noindex")
		return c.Next()
	}
}
