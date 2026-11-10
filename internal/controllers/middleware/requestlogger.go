package middleware

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const loggerKey = "logger"

func RequestLogger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		rid := c.Get("X-Request-ID")
		if rid == "" {
			rid = uuid.New().String()
		}
		c.Set("X-Request-ID", rid)
		c.Locals("request_id", rid)

		log := slog.With(slog.String("request_id", rid))
		c.Locals(loggerKey, &log)

		return c.Next()
	}
}

func GetLogger(c *fiber.Ctx) *slog.Logger {
	if l, ok := c.Locals(loggerKey).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

func HTTPAccessLogger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		dur := time.Since(start)

		status := responseStatus(c, err)

		GetLogger(c).InfoContext(SpanCtx(c), "request",
			slog.String("method", c.Method()),
			slog.String("path", c.Path()),
			slog.Int("status", status),
			slog.Int64("duration_ms", dur.Milliseconds()),
			slog.String("ip", c.IP()),
			slog.String("user_agent", c.Get("User-Agent")),
		)
		return err
	}
}
