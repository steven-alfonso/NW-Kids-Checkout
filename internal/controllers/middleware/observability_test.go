package middleware

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Test_RouteLabel covers both halves of the contract: a matched route keeps its
// pattern, and anything that did not match collapses to one label so raw request
// paths cannot reach a metric or span label.
func Test_RouteLabel(t *testing.T) {
	app := fiber.New()
	labels := map[string]string{}
	app.Use(func(c *fiber.Ctx) error {
		err := c.Next()
		labels[c.Method()+" "+c.Path()] = RouteLabel(c, responseStatus(c, err))
		return err
	})
	app.Get("/events/:id", func(c *fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/boom", func(c *fiber.Ctx) error { return fiber.ErrNotFound })

	tests := []struct {
		method string
		path   string
		want   string
	}{
		{fiber.MethodGet, "/events/42", "/events/:id"},
		// A matched route that renders a 404 keeps its pattern: only an
		// unmatched request collapses.
		{fiber.MethodGet, "/boom", "/boom"},
		{fiber.MethodGet, "/scanner/deep/path", UnmatchedRoute},
		{fiber.MethodDelete, "/events/42", UnmatchedRoute},
	}
	for _, tt := range tests {
		_, err := app.Test(httptest.NewRequest(tt.method, tt.path, nil))
		require.NoError(t, err)
		assert.Equal(t, tt.want, labels[tt.method+" "+tt.path],
			"%s %s", tt.method, tt.path)
	}
}

func Test_responseStatus(t *testing.T) {
	app := fiber.New()
	var got int
	app.Use(func(c *fiber.Ctx) error {
		err := c.Next()
		got = responseStatus(c, err)
		return err
	})
	app.Get("/rendered", func(c *fiber.Ctx) error {
		c.Status(fiber.StatusTeapot)
		return c.SendString("teapot")
	})
	app.Get("/fiber", func(c *fiber.Ctx) error { return fiber.ErrBadRequest })
	app.Get("/wrapped", func(c *fiber.Ctx) error {
		return fmt.Errorf("handler: %w", fiber.ErrBadRequest)
	})
	app.Get("/generic", func(c *fiber.Ctx) error { return errors.New("boom") })

	tests := []struct {
		path string
		want int
	}{
		{"/rendered", fiber.StatusTeapot},
		{"/fiber", fiber.StatusBadRequest},
		// errors.As must see through the wrap so the client's status and the
		// recorded status agree.
		{"/wrapped", fiber.StatusBadRequest},
		{"/generic", fiber.StatusInternalServerError},
	}
	for _, tt := range tests {
		_, err := app.Test(httptest.NewRequest(fiber.MethodGet, tt.path, nil))
		require.NoError(t, err)
		assert.Equal(t, tt.want, got, "%s", tt.path)
	}
}

// SpanCtx exists because the two contexts on a fiber.Ctx are each missing half
// of what a handler needs: UserContext carries the span but never cancels,
// Context cancels but carries no span.
func Test_SpanCtx_carries_the_request_span(t *testing.T) {
	tracer := sdktrace.NewTracerProvider().Tracer("test")

	var (
		carriedSpan bool
		sameTrace   bool
	)
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		ctx, span := tracer.Start(c.UserContext(), "request")
		defer span.End()
		c.SetUserContext(ctx)

		got := trace.SpanContextFromContext(SpanCtx(c))
		carriedSpan = got.IsValid()
		sameTrace = got.TraceID() == span.SpanContext().TraceID()
		return c.Next()
	})
	app.Get("/", func(c *fiber.Ctx) error { return c.SendString("ok") })

	_, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
	require.NoError(t, err)
	assert.True(t, carriedSpan, "expected SpanCtx to carry a valid span")
	assert.True(t, sameTrace, "expected SpanCtx to carry the request's own span")
}
