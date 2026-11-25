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

// SendJSON marshals payload, sets a weak ETag and the shared cache policy, and
// returns 304 when the request's If-None-Match matches. Each name in vary is
// added to the Vary header alongside Accept-Encoding.
func SendJSON(c *fiber.Ctx, payload any, vary ...string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not encode response")
	}

	// Weak validator: the identity representation is hashed here, but gzip may
	// content-code it on the way out, and a weak tag is the correct validator
	// when a weak transformation like compression is applied (RFC 9110 8.8.1).
	sum := sha256.Sum256(body)
	etag := `W/"` + hex.EncodeToString(sum[:16]) + `"`

	// private, no-cache: these payloads carry children's names and check-in and
	// check-out times, so only the user's browser may store them, and they must
	// revalidate on every request. The 304 path is what makes an unchanged poll
	// cheap. Logout sends Clear-Site-Data: "cache" to evict the stored body. If
	// devices ever become shared, switch to no-store and accept a full payload
	// on every request.
	c.Set(fiber.HeaderCacheControl, "private, no-cache")
	// The body varies once compression is applied.
	c.Vary(append([]string{fiber.HeaderAcceptEncoding}, vary...)...)
	c.Set(fiber.HeaderETag, etag)

	if Matches(c.Get(fiber.HeaderIfNoneMatch), etag) {
		return c.SendStatus(fiber.StatusNotModified)
	}

	// Set the content type directly rather than via c.Type, which treats its
	// argument as a file extension and would fall back to octet-stream.
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	return c.Send(body)
}

// Matches reports whether an If-None-Match header covers the current etag. The
// header may carry a list and may use the weak W/ prefix on either side.
func Matches(ifNoneMatch, etag string) bool {
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
