// Package conditional writes JSON responses with a weak ETag and a cache
// policy that lets a browser store and revalidate them.
package conditional

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// SendRevalidatedJSON marshals a GET/HEAD response payload, sets a weak ETag
// and the shared no-store policy, and returns 304 when If-None-Match matches.
// Each name in vary is added to the Vary header alongside Accept-Encoding.
func SendRevalidatedJSON(c *fiber.Ctx, payload any, vary ...string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not encode response")
	}

	// Weak validator: the identity representation is hashed here, but gzip may
	// content-code it on the way out, and a weak tag is the correct validator
	// when a weak transformation like compression is applied (RFC 9110 8.8.1).
	sum := sha256.Sum256(body)
	etag := `W/"` + hex.EncodeToString(sum[:16]) + `"`

	// private, no-store: these payloads carry children's names and check-in and
	// check-out times, so the browser must not retain them. The page keeps the
	// validator in memory and sends If-None-Match itself, which still lets an
	// unchanged poll collapse to a 304 without putting PII in the HTTP cache.
	// Logout also sends Clear-Site-Data: "cache" to evict older cached bodies.
	c.Set(fiber.HeaderCacheControl, "private, no-store")
	// The body varies once compression is applied.
	c.Vary(append([]string{fiber.HeaderAcceptEncoding}, vary...)...)
	c.Set(fiber.HeaderETag, etag)

	if matches(c.Get(fiber.HeaderIfNoneMatch), etag) {
		return c.SendStatus(fiber.StatusNotModified)
	}

	// Set the content type directly rather than via c.Type, which treats its
	// argument as a file extension and would fall back to octet-stream.
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	return c.Send(body)
}

// matches reports whether an If-None-Match header covers the current etag. The
// header may carry a list and may use the weak W/ prefix on either side.
func matches(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}
	if strings.TrimSpace(ifNoneMatch) == "*" {
		return true
	}
	candidate := strings.TrimPrefix(etag, "W/")
	for _, part := range strings.Split(ifNoneMatch, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.TrimPrefix(part, "W/") == candidate {
			return true
		}
	}
	return false
}
