package middleware

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel/trace"
)

// UnmatchedRoute is the route label used for requests that matched no route.
// Collapsing them keeps request paths out of the metric and span labels, which
// would otherwise be unbounded.
const UnmatchedRoute = "UNMATCHED"

// SpanCtx returns the context a handler should pass to repos and loggers.
//
// It keeps the fasthttp request context and grafts the active span onto it.
// The two are not interchangeable. c.Context() is cancelled when the server
// shuts down, which aborts in-flight queries during a graceful drain; it is not
// cancelled when a client disconnects, because fasthttp only closes that channel
// in Server.ShutdownWithContext. c.UserContext() carries the span but is
// Background-derived and never cancels at all. Handlers want both.
func SpanCtx(c *fiber.Ctx) context.Context {
	return trace.ContextWithSpan(c.Context(), trace.SpanFromContext(c.UserContext()))
}

// RouteLabel returns the low-cardinality label for a request's route: the
// registered route pattern, or UnmatchedRoute when nothing matched. Raw paths
// must never reach a metric or span label.
//
// status must be the response status. An unmatched request leaves c.Route()
// pointing at the outermost use-route, whose path is "/", so a 404 or 405
// against "/" can only be an unmatched request; real routes carry a pattern.
// Both the metrics middleware and the otelfiber span-name formatter read this,
// so a request is labelled the same way in traces and in metrics.
func RouteLabel(c *fiber.Ctx, status int) string {
	route := c.Route().Path
	if route == "" {
		return UnmatchedRoute
	}
	if (status == fiber.StatusNotFound || status == fiber.StatusMethodNotAllowed) && route == "/" {
		return UnmatchedRoute
	}
	return route
}

// responseStatus infers the status code the client will see for err.
//
// Fiber runs its ErrorHandler after middleware unwinds, so at this point a
// returned error has not been rendered and c.Response() still holds 200. Infer
// it the way the app ErrorHandler does: unwrap with errors.As so a wrapped
// *fiber.Error keeps its own code, and treat anything else, including a panic
// recovered further in, as 500.
func responseStatus(c *fiber.Ctx, err error) int {
	if err == nil {
		return c.Response().StatusCode()
	}
	var ferr *fiber.Error
	if errors.As(err, &ferr) {
		return ferr.Code
	}
	return fiber.StatusInternalServerError
}
