package controllers

import (
	"log/slog"
	"net/http/httptest"
	"testing"

	"kids-checkin/internal/logger"
	"kids-checkin/internal/telemetry"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A panicking handler must still be access-logged with the 500 that the
// client receives. That only holds when recover is the innermost middleware:
// if recover runs before the logging middleware, the panic unwinds through
// them and skips their post-c.Next() recording code.
func Test_core_middleware_access_logs_panics_as_500(t *testing.T) {
	capture := &logger.CaptureSlogHandler{}
	prevDefault := slog.Default()
	slog.SetDefault(slog.New(logger.NewTraceHandler(capture)))
	t.Cleanup(func() { slog.SetDefault(prevDefault) })

	app := fiber.New()
	require.NoError(t, registerCoreMiddleware(app, &telemetry.Telemetry{}))

	app.Get("/boom", func(c *fiber.Ctx) error {
		panic("boom")
	})

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/boom", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)

	assert.True(t, capture.ContainsInfoAttr("status", "500"),
		"panicked request must appear in the access log with status 500")
}
