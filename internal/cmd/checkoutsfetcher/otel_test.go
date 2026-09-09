package checkoutsfetcher

import (
	"context"
	"sync"
	"testing"
	"time"

	"kids-checkin/internal/client/planningcenter"
	"kids-checkin/internal/repo/checkin"
	"kids-checkin/internal/repo/event"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// installTestTracerProvider registers a global tracer provider that records
// ended spans into an in-memory exporter and restores the previous provider
// on cleanup.
func installTestTracerProvider(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return exp
}

func Test_eventCheckoutLoop_creates_cycle_and_event_spans(t *testing.T) {
	exp := installTestTracerProvider(t)

	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}
	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return events, nil
		},
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			return events[0], nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error { return nil },
	}
	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			c.ID = 1
			return c, nil
		},
	}
	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return []planningcenter.Checkout{
				{ID: "pc_checkout_1", FirstName: "John", LastName: "Doe", SecurityCode: "ABCD", PlanningCenterLocationID: "pc_loc_1"},
			}, nil
		},
	}
	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 5*time.Minute, false)
	require.NoError(t, svc.eventCheckoutLoop(t.Context(), nil))

	var cycleSeen, eventSpanSeen bool
	for _, span := range exp.GetSpans() {
		if span.Name == "checkout_fetch_cycle" {
			cycleSeen = true
		}
		if span.Name == "process_event_checkouts" {
			eventSpanSeen = true
			for _, kv := range span.Attributes {
				if kv.Key == attribute.Key("pc_event_id") {
					assert.Equal(t, "evt_1", kv.Value.AsString())
				}
			}
		}
	}
	assert.True(t, cycleSeen, "expected a checkout_fetch_cycle span")
	assert.True(t, eventSpanSeen, "expected a process_event_checkouts span")
}
