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
			// fasthttp would happily compress image/x-icon and image/svg+xml, but
			// everything under img/ is already-compressed raster data (the logo
			// is a base64 PNG inside an SVG). Re-encoding it wastes CPU.
			return strings.HasPrefix(c.Path(), "/static/img/")
		},
	})
}
