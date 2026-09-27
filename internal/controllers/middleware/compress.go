package middleware

import (
	"strings"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	fibercompress "github.com/gofiber/fiber/v2/middleware/compress"
)

// Compress gzips text responses. Staff use this app on phones over slow
// connections, so the checkouts poll and the page assets are worth shrinking
// on the wire.
func Compress() fiber.Handler {
	return fibercompress.New(fibercompress.Config{
		Next: func(c *fiber.Ctx) bool {
			// The WebSocket upgrade hijacks the connection and writes raw frames
			// to the net.Conn, so the response must be left untouched.
			if websocket.IsWebSocketUpgrade(c) {
				return true
			}
			// Everything under img/ is already-compressed raster data (the logo
			// is a base64 PNG inside an SVG). fasthttp would happily re-encode
			// image/x-icon and image/svg+xml, and these assets are served with
			// application/octet-stream, which it also treats as compressible.
			// Re-encoding wastes CPU and can make a PNG larger on the wire.
			if strings.HasPrefix(c.Path(), "/static/img/") {
				return true
			}
			// The icons are served from img/ but registered at the route root,
			// so the prefix above does not match them.
			return isImagePath(c.Path())
		},
	})
}

// isImagePath reports whether a request path names a pre-compressed image.
// Matched by extension because the icon handlers serve them with a
// content type that would otherwise look compressible.
func isImagePath(path string) bool {
	switch {
	case strings.HasSuffix(path, ".ico"), strings.HasSuffix(path, ".png"):
		return true
	default:
		return false
	}
}
