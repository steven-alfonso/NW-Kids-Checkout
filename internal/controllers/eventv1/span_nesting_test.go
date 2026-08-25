package eventv1

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	kdb "kids-checkin/db"
	"kids-checkin/internal/db"

	"github.com/gofiber/contrib/otelfiber"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// Test_routes_propagate_otel_span_context_to_repos guards the wiring between
// the otelfiber request span and the contexts controllers hand to repos.
// Repos must receive the fiber user context (carrying the active span), not
// the raw fasthttp request context, otherwise every DB query shows up as a
// disconnected root trace instead of nesting under its request.
func Test_routes_propagate_otel_span_context_to_repos(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	prevTracerProvider := otel.GetTracerProvider()
	prevMeterProvider := otel.GetMeterProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	))
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTracerProvider)
		otel.SetMeterProvider(prevMeterProvider)
	})

	database, err := db.InitDBInstrumented(filepath.Join(t.TempDir(), "nest.db"),
		otel.GetTracerProvider(),
		// Reader-less provider: meters are usable but export nothing.
		sdkmetric.NewMeterProvider())
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })

	_, err = database.Exec(kdb.Schema)
	require.NoError(t, err)

	store := session.New()
	app := fiber.New()
	app.Use(otelfiber.Middleware(
		otelfiber.WithTracerProvider(otel.GetTracerProvider()),
		otelfiber.WithSpanNameFormatter(func(c *fiber.Ctx) string {
			return c.Route().Path
		}),
	))
	app.Use(func(c *fiber.Ctx) error {
		sess, err := store.Get(c)
		if err != nil {
			return err
		}
		sess.Set("authenticated", true)
		sess.Set("role", "admin")
		if saveErr := sess.Save(); saveErr != nil {
			return saveErr
		}
		return c.Next()
	})

	controller := NewController(database, store)
	controller.RegisterRoutes(app)

	req := httptest.NewRequest("GET", "/v1/events", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	spans := exp.GetSpans()

	var requestSpan *tracetest.SpanStub
	for i, s := range spans {
		if s.Name == "/v1/events" {
			requestSpan = &spans[i]
			break
		}
	}
	require.NotNil(t, requestSpan, "expected the otelfiber request span")

	var sawNestedDBQuery bool
	for _, s := range spans {
		isDBSpan := false
		for _, kv := range s.Attributes {
			if kv.Key == "db.system.name" {
				isDBSpan = true
				break
			}
		}
		if !isDBSpan {
			continue
		}
		if s.Parent.TraceID() == (trace.TraceID{}) {
			continue // unparented DB span: the bug this test guards against
		}
		if s.Parent.SpanID() == requestSpan.SpanContext.SpanID() &&
			s.Parent.TraceID() == requestSpan.SpanContext.TraceID() {
			sawNestedDBQuery = true
		}
	}
	assert.True(t, sawNestedDBQuery,
		"expected at least one DB span nested under the %s request span", requestSpan.Name)
}
