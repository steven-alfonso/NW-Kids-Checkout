package middleware

import (
	"errors"
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

		status := c.Response().StatusCode()
		// The fiber error handler runs after middleware unwinds, so the
		// response status is not yet set for returned errors. Infer it the
		// same way the app error handler does: unwrap with errors.As so a
		// wrapped *fiber.Error keeps its own code; any other error (including
		// one recovered from a panic) renders as 500.
		var ferr *fiber.Error
		if errors.As(err, &ferr) {
			status = ferr.Code
		} else if err != nil {
			status = fiber.StatusInternalServerError
		}

		GetLogger(c).InfoContext(c.UserContext(), "request",
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
