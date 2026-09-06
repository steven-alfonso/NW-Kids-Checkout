package checkoutsfetcher

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"kids-checkin/internal/client/planningcenter"
	"kids-checkin/internal/db"
	"kids-checkin/internal/logger"
	"kids-checkin/internal/repo/checkin"
	"kids-checkin/internal/repo/event"
	"kids-checkin/internal/repo/eventcheckwindow"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// noCheckWindows returns a window repo with no configured windows, so all
// auto-fetch events are treated as active unless a test configures windows.
func noCheckWindows() *eventcheckwindow.MockRepo {
	return &eventcheckwindow.MockRepo{
		ListCheckWindowsFunc: func(ctx context.Context, filter eventcheckwindow.Filter) ([]eventcheckwindow.EventCheckWindow, error) {
			return nil, nil
		},
	}
}

// testService builds a Service with the given collaborators so tests can call
// its methods directly.
func testService(checkWindowRepo eventcheckwindow.Repo, eventRepo event.Repo, checkinRepo checkin.Repo, pcClient planningcenter.Client, locationIDMap *sync.Map, eventUpdateInterval time.Duration, useCheckWindows bool) *Service {
	return &Service{
		pcClient:            pcClient,
		eventRepo:           eventRepo,
		checkinRepo:         checkinRepo,
		checkWindowRepo:     checkWindowRepo,
		locationIDMap:       locationIDMap,
		eventUpdateInterval: eventUpdateInterval,
		useCheckWindows:     useCheckWindows,
		now:                 time.Now,
	}
}

type concurrentEventRepo struct {
	events      []event.Event
	onUpdate    func()
	mu          sync.Mutex
	updateCount atomic.Int64
}

func (m *concurrentEventRepo) ListEvents(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
	return m.events, nil
}

func (m *concurrentEventRepo) GetEventByID(ctx context.Context, id int64) (event.Event, error) {
	for _, e := range m.events {
		if e.ID == id {
			return e, nil
		}
	}
	return event.Event{}, nil
}

func (m *concurrentEventRepo) GetEventByPlanningCenterID(ctx context.Context, planningCenterID string) (event.Event, error) {
	for _, e := range m.events {
		if e.PlanningCenterID == planningCenterID {
			return e, nil
		}
	}
	return event.Event{}, nil
}

func (m *concurrentEventRepo) CreateEvent(ctx context.Context, ev event.Event) (event.Event, error) {
	ev.ID = int64(len(m.events) + 1)
	m.events = append(m.events, ev)
	return ev, nil
}

func (m *concurrentEventRepo) UpdateEvent(ctx context.Context, ev event.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCount.Add(1)
	if m.onUpdate != nil {
		m.onUpdate()
	}
	for i, e := range m.events {
		if e.ID == ev.ID {
			m.events[i] = ev
			return nil
		}
	}
	return nil
}

func Test_eventCheckoutLoop_noEvents(t *testing.T) {
	var events []event.Event
	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return events, nil
		},
	}

	checkinRepo := &checkin.MockRepo{}
	pcClient := &planningcenter.MockClient{}
	locationIDMap := sync.Map{}

	err := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 3*time.Second, false).eventCheckoutLoop(t.Context(), nil)
	require.NoError(t, err)
}

func Test_eventCheckoutLoop_noEventsNeedingUpdate(t *testing.T) {
	now := time.Now()
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: now},
		{ID: 2, PlanningCenterID: "evt_2", AutoFetch: true, LastCheckedOutTime: now.Add(1 * time.Second)},
	}

	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return events, nil
		},
	}

	checkinRepo := &checkin.MockRepo{}
	pcClient := &planningcenter.MockClient{}
	locationIDMap := sync.Map{}

	err := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 1*time.Hour, false).eventCheckoutLoop(t.Context(), nil)
	require.NoError(t, err)
}

func Test_eventCheckoutLoop_updatesEventsNeedingUpdate(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 2, PlanningCenterID: "evt_2", AutoFetch: true, LastCheckedOutTime: time.Now().Add(-10 * time.Minute)},
	}

	var eventsMu sync.Mutex
	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return events, nil
		},
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			eventsMu.Lock()
			defer eventsMu.Unlock()
			for _, e := range events {
				if e.ID == id {
					return e, nil
				}
			}
			return event.Event{}, nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			eventsMu.Lock()
			defer eventsMu.Unlock()
			for i, e := range events {
				if e.ID == ev.ID {
					events[i] = ev
					return nil
				}
			}
			return nil
		},
	}

	var createdMu sync.Mutex
	var createdCheckins []checkin.Checkin
	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			createdMu.Lock()
			defer createdMu.Unlock()
			c.ID = int64(len(createdCheckins) + 1)
			createdCheckins = append(createdCheckins, c)
			return c, nil
		},
	}

	checkouts := map[string][]planningcenter.Checkout{
		"evt_1": {
			{ID: "pc_checkout_1", FirstName: "John", LastName: "Doe", SecurityCode: "ABCD", PlanningCenterLocationID: "pc_loc_1"},
		},
		"evt_2": {
			{ID: "pc_checkout_2", FirstName: "Jane", LastName: "Smith", SecurityCode: "EFGH", PlanningCenterLocationID: "pc_loc_1"},
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return checkouts[eventID], nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	err := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 5*time.Minute, false).eventCheckoutLoop(t.Context(), nil)
	require.NoError(t, err)

	require.Len(t, createdCheckins, 2, "this test requires checkinRepo to return checkins")

	checkinIDs := map[string]bool{}
	for _, c := range createdCheckins {
		checkinIDs[c.PlanningCenterID] = true
	}
	assert.True(t, checkinIDs["pc_checkout_1"])
	assert.True(t, checkinIDs["pc_checkout_2"])
	assert.Equal(t, int64(1), createdCheckins[0].LocationID)

	assert.True(t, events[0].LastCheckedOutTime.After(time.Now().Add(-1*time.Minute)))
	assert.True(t, events[1].LastCheckedOutTime.After(time.Now().Add(-1*time.Minute)))
}

func Test_processEventCheckouts_createsCheckinsAndUpdatesEvent(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			return events[0], nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			events[0] = ev
			return nil
		},
	}

	var createdCheckins []checkin.Checkin
	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			c.ID = int64(len(createdCheckins) + 1)
			createdCheckins = append(createdCheckins, c)
			return c, nil
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return []planningcenter.Checkout{
				{ID: "pc_checkout_1", FirstName: "John", LastName: "Doe", SecurityCode: "ABCD", PlanningCenterLocationID: "pc_loc_1", CheckedOutAt: time.Now().Add(-1 * time.Hour)},
				{ID: "pc_checkout_2", FirstName: "Jane", LastName: "Smith", SecurityCode: "EFGH", PlanningCenterLocationID: "pc_loc_1", CheckedOutAt: time.Now().Add(-30 * time.Minute)},
			}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(5))

	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 0, false)
	err := svc.processEventCheckouts(t.Context(), events[0], svc.now())
	require.NoError(t, err)

	assert.Len(t, createdCheckins, 2)
	assert.Equal(t, int64(5), createdCheckins[0].LocationID)
	assert.Equal(t, int64(5), createdCheckins[1].LocationID)
	assert.Equal(t, int64(1), createdCheckins[0].EventID, "checkin should have EventID set to the source event")
	assert.Equal(t, int64(1), createdCheckins[1].EventID, "checkin should have EventID set to the source event")
	assert.Equal(t, "John", createdCheckins[0].FirstName)
	assert.Equal(t, "Jane", createdCheckins[1].FirstName)

	assert.True(t, events[0].LastCheckedOutTime.After(time.Now().Add(-1*time.Minute)))
}

func Test_processEventCheckouts_passesThroughFetchedAt(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			return events[0], nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			return nil
		},
	}

	var createdCheckins []checkin.Checkin
	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			c.ID = int64(len(createdCheckins) + 1)
			createdCheckins = append(createdCheckins, c)
			return c, nil
		},
	}

	fetchedAt := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return []planningcenter.Checkout{
				{ID: "pc_checkout_1", FirstName: "John", LastName: "Doe", SecurityCode: "ABCD", PlanningCenterLocationID: "pc_loc_1", CheckedOutAt: fetchedAt.Add(-5 * time.Minute), FetchedAt: fetchedAt},
			}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(5))

	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 0, false)
	err := svc.processEventCheckouts(t.Context(), events[0], svc.now())
	require.NoError(t, err)

	require.Len(t, createdCheckins, 1)
	assert.Equal(t, fetchedAt, createdCheckins[0].FetchedAt)
}

func Test_processEventCheckouts_persistsFetchedAtToDB(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err)
	t.Cleanup(cleanup)

	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			return events[0], nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			return nil
		},
	}

	fetchedAt := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return []planningcenter.Checkout{
				{ID: "pc_checkout_1", FirstName: "John", LastName: "Doe", SecurityCode: "ABCD", PlanningCenterLocationID: "pc_loc_1", CheckedOutAt: fetchedAt.Add(-5 * time.Minute), FetchedAt: fetchedAt},
			}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(5))

	checkinRepo := checkin.NewRepo(testDB)
	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 0, false)
	err = svc.processEventCheckouts(t.Context(), events[0], svc.now())
	require.NoError(t, err)

	checkins, err := checkinRepo.ListCheckins(t.Context(), checkin.Filter{PlanningCenterID: "pc_checkout_1"})
	require.NoError(t, err)
	require.Len(t, checkins, 1)
	assert.Equal(t, fetchedAt, checkins[0].FetchedAt)
}

func Test_processEventCheckouts_genericFetchError_nonFatal(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			return events[0], nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			events[0] = ev
			return nil
		},
	}

	checkinRepo := &checkin.MockRepo{}
	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return nil, &planningcenter.ServerError{}
		},
	}
	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 0, false)
	err := svc.processEventCheckouts(t.Context(), events[0], svc.now())
	require.Error(t, err)
	assert.True(t, isEventFetchError(err), "a generic per-event fetch failure should be a non-fatal per-event failure")
	assert.Contains(t, err.Error(), "failed to fetch checkouts for event")
	assert.True(t, events[0].LastCheckedOutTime.IsZero(), "event should not advance LastCheckedOutTime on a generic fetch failure")
	assert.Zero(t, eventRepo.UpdateEventFuncCallCount.Load(), "event should not be marked checked out when the fetch failed")

	svc.retryMu.Lock()
	st, ok := svc.eventRetries[events[0].ID]
	svc.retryMu.Unlock()
	require.True(t, ok, "event should be placed in retry backoff after a generic fetch failure")
	assert.Equal(t, 1, st.consecutiveFailures)
}

func Test_processEventCheckouts_paginationLimit_nonFatal(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			return events[0], nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			events[0] = ev
			return nil
		},
	}

	checkinRepo := &checkin.MockRepo{}
	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return nil, planningcenter.ErrPaginationLimitExceeded
		},
	}
	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 0, false)
	err := svc.processEventCheckouts(t.Context(), events[0], svc.now())
	require.Error(t, err)
	assert.True(t, isEventFetchFailure(err), "pagination-limit truncation should be a non-fatal per-event failure")
	assert.True(t, events[0].LastCheckedOutTime.IsZero(), "event should not advance LastCheckedOutTime on pagination-limit truncation")
	assert.Zero(t, eventRepo.UpdateEventFuncCallCount.Load(), "event should not be marked checked out when pagination was truncated")
}

func Test_eventCheckoutLoop_paginationLimit_nonFatal(t *testing.T) {
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
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			events[0] = ev
			return nil
		},
	}

	checkinRepo := &checkin.MockRepo{}
	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return nil, planningcenter.ErrPaginationLimitExceeded
		},
	}
	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	err := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 5*time.Minute, false).eventCheckoutLoop(t.Context(), nil)
	require.NoError(t, err, "pagination-limit truncation should be non-fatal and not fail the loop batch")
	assert.True(t, events[0].LastCheckedOutTime.IsZero(), "event with pagination-limit truncation should not advance LastCheckedOutTime")
}

func Test_eventCheckoutLoop_prunesRetryStateForRemovedEvents(t *testing.T) {
	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return []event.Event{{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}}}, nil
		},
	}

	svc := testService(noCheckWindows(), eventRepo, &checkin.MockRepo{}, &planningcenter.MockClient{}, &sync.Map{}, 5*time.Minute, false)
	svc.retryMu.Lock()
	svc.eventRetries = map[int64]retryState{
		1: {consecutiveFailures: 1, backoffUntil: time.Now().Add(time.Hour)},
		2: {consecutiveFailures: 1, backoffUntil: time.Now().Add(time.Hour)},
	}
	svc.retryMu.Unlock()

	err := svc.eventCheckoutLoop(t.Context(), nil)
	require.NoError(t, err)

	svc.retryMu.Lock()
	_, stillPresent := svc.eventRetries[1]
	_, removed := svc.eventRetries[2]
	svc.retryMu.Unlock()
	assert.True(t, stillPresent, "retry state for an event still auto-fetched should be preserved")
	assert.False(t, removed, "retry state for an event no longer auto-fetched should be pruned")
}

func Test_eventCheckoutLoop_retryBackoff_skipsEventDuringBackoff(t *testing.T) {
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
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			events[0] = ev
			return nil
		},
	}

	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			return c, nil
		},
	}

	// The checkout's location is never resolvable, so every cycle drops it.
	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return []planningcenter.Checkout{
				{ID: "pc_checkout_1", PlanningCenterLocationID: "unknown_loc"},
			}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	cur := time.Now()
	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, time.Millisecond, false)
	svc.now = func() time.Time { return cur }

	require.NoError(t, svc.eventCheckoutLoop(t.Context(), nil))
	require.Equal(t, int64(1), eventRepo.GetEventByIDFuncCallCount.Load(), "first cycle should process the event")

	require.NoError(t, svc.eventCheckoutLoop(t.Context(), nil))
	assert.Equal(t, int64(1), eventRepo.GetEventByIDFuncCallCount.Load(), "an event in retry backoff should be skipped until the backoff expires")

	cur = cur.Add(10 * time.Minute)
	require.NoError(t, svc.eventCheckoutLoop(t.Context(), nil))
	assert.Equal(t, int64(2), eventRepo.GetEventByIDFuncCallCount.Load(), "an event should be reprocessed once its backoff expires")
}

func Test_processEventCheckouts_consecutiveFailures_escalatesToError(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			return events[0], nil
		},
	}

	checkinRepo := &checkin.MockRepo{}
	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return []planningcenter.Checkout{
				{ID: "pc_checkout_1", PlanningCenterLocationID: "unknown_loc"},
			}, nil
		},
	}
	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	capture := &logger.CaptureSlogHandler{}
	ctx := logger.WithLogger(t.Context(), slog.New(capture))

	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 0, false)

	for range retryEscalationThreshold - 1 {
		require.True(t, isEventDropped(svc.processEventCheckouts(ctx, events[0], svc.now())), "dropped checkouts should surface as a non-fatal error")
	}
	assert.False(t, capture.ContainsError("consecutive"), "no escalation before the consecutive-failure threshold")
	assert.False(t, capture.ContainsErrorAttr("alert_name", "event-checkouts-escalation"), "no alert field before the consecutive-failure threshold")

	require.True(t, isEventDropped(svc.processEventCheckouts(ctx, events[0], svc.now())), "dropped checkouts should surface as a non-fatal error")
	assert.True(t, capture.ContainsError("consecutive"), "an escalation ERROR should be logged once the threshold is crossed")
	assert.True(t, capture.ContainsErrorAttr("alert_name", "event-checkouts-escalation"), "the escalation ERROR should carry the machine-alertable alert_name field")
}

func Test_processEventCheckouts_successResetsFailureCount(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			return events[0], nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			events[0] = ev
			return nil
		},
	}

	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			return c, nil
		},
	}

	resolvable := false
	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			loc := "unknown_loc"
			if resolvable {
				loc = "pc_loc_1"
			}
			return []planningcenter.Checkout{
				{ID: "pc_checkout_1", PlanningCenterLocationID: loc},
			}, nil
		},
	}
	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	capture := &logger.CaptureSlogHandler{}
	ctx := logger.WithLogger(t.Context(), slog.New(capture))

	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 0, false)

	require.True(t, isEventDropped(svc.processEventCheckouts(ctx, events[0], svc.now())), "dropped checkouts should surface as a non-fatal error")
	resolvable = true
	require.NoError(t, svc.processEventCheckouts(ctx, events[0], svc.now()))
	resolvable = false

	for range retryEscalationThreshold - 1 {
		require.True(t, isEventDropped(svc.processEventCheckouts(ctx, events[0], svc.now())), "dropped checkouts should surface as a non-fatal error")
	}
	assert.False(t, capture.ContainsError("consecutive"), "a success should reset the consecutive-failure counter so earlier failures do not count toward escalation")
}

func Test_eventCheckoutLoop_contextCancellation(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 2, PlanningCenterID: "evt_2", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 3, PlanningCenterID: "evt_3", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return events, nil
		},
	}

	checkinRepo := &checkin.MockRepo{}
	pcClient := &planningcenter.MockClient{}
	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 5*time.Minute, false).eventCheckoutLoop(ctx, nil)
	assert.ErrorIs(t, err, context.Canceled)
}

func Test_processEventCheckouts_unknownLocation(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			return events[0], nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			events[0] = ev
			return nil
		},
	}

	var createdCheckins []checkin.Checkin
	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			c.ID = int64(len(createdCheckins) + 1)
			createdCheckins = append(createdCheckins, c)
			return c, nil
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return []planningcenter.Checkout{
				{ID: "pc_checkout_unknown", FirstName: "Unknown", LastName: "Person", SecurityCode: "UNKN", PlanningCenterLocationID: "unknown_loc"},
				{ID: "pc_checkout_known", FirstName: "Known", LastName: "Person", SecurityCode: "KNOW", PlanningCenterLocationID: "pc_loc_1"},
			}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(5))

	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 0, false)
	err := svc.processEventCheckouts(t.Context(), events[0], svc.now())
	require.Error(t, err, "a dropped checkout should surface as a non-fatal error so the cycle summary can count it")
	require.True(t, isEventDropped(err), "dropped checkouts should be distinguishable from other failures")

	assert.Len(t, createdCheckins, 1, "should only create checkin for known location")
	assert.Equal(t, "Known", createdCheckins[0].FirstName)
	assert.Zero(t, eventRepo.UpdateEventFuncCallCount.Load(), "event should not be marked checked out when a checkout was dropped")
	assert.True(t, events[0].LastCheckedOutTime.IsZero(), "LastCheckedOutTime should remain unchanged so the event is retried next cycle")
}

func Test_processEventCheckouts_unexpectedLocationType(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			return events[0], nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			events[0] = ev
			return nil
		},
	}

	var createdCheckins []checkin.Checkin
	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			c.ID = int64(len(createdCheckins) + 1)
			createdCheckins = append(createdCheckins, c)
			return c, nil
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return []planningcenter.Checkout{
				{ID: "pc_checkout_bad_type", FirstName: "Bad", LastName: "Type", SecurityCode: "BADT", PlanningCenterLocationID: "pc_loc_bad"},
				{ID: "pc_checkout_known", FirstName: "Known", LastName: "Person", SecurityCode: "KNOW", PlanningCenterLocationID: "pc_loc_1"},
			}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_bad", "not-an-int64")
	locationIDMap.Store("pc_loc_1", int64(5))

	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 0, false)
	err := svc.processEventCheckouts(t.Context(), events[0], svc.now())
	require.Error(t, err, "a dropped checkout should surface as a non-fatal error so the cycle summary can count it")
	require.True(t, isEventDropped(err), "dropped checkouts should be distinguishable from other failures")

	assert.Len(t, createdCheckins, 1, "checkout with non-int64 location id should be skipped, not panic")
	assert.Equal(t, "Known", createdCheckins[0].FirstName)
	assert.Zero(t, eventRepo.UpdateEventFuncCallCount.Load(), "event should not be marked checked out when a checkout was dropped")
	assert.True(t, events[0].LastCheckedOutTime.IsZero(), "LastCheckedOutTime should remain unchanged so the event is retried next cycle")
}

func Test_processEventCheckouts_validBatch_advancesWindow(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			return events[0], nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			events[0] = ev
			return nil
		},
	}

	var createdCheckins []checkin.Checkin
	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			c.ID = int64(len(createdCheckins) + 1)
			createdCheckins = append(createdCheckins, c)
			return c, nil
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return []planningcenter.Checkout{
				{ID: "pc_checkout_1", FirstName: "John", LastName: "Doe", SecurityCode: "ABCD", PlanningCenterLocationID: "pc_loc_1"},
				{ID: "pc_checkout_2", FirstName: "Jane", LastName: "Smith", SecurityCode: "EFGH", PlanningCenterLocationID: "pc_loc_1"},
			}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(5))

	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 0, false)
	err := svc.processEventCheckouts(t.Context(), events[0], svc.now())
	require.NoError(t, err)

	assert.Len(t, createdCheckins, 2, "all checkouts with known locations should be created")
	assert.Equal(t, int64(1), eventRepo.UpdateEventFuncCallCount.Load(), "event should be marked checked out when the whole batch resolves")
	assert.False(t, events[0].LastCheckedOutTime.IsZero(), "LastCheckedOutTime should advance when no checkouts are dropped")
}

func Test_processEventCheckouts_advanceUsesInjectedClock(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			return events[0], nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			events[0] = ev
			return nil
		},
	}

	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			return c, nil
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return []planningcenter.Checkout{
				{ID: "pc_checkout_1", PlanningCenterLocationID: "pc_loc_1"},
			}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(5))

	fixedNow := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 0, false)
	svc.now = func() time.Time { return fixedNow }

	err := svc.processEventCheckouts(t.Context(), events[0], svc.now())
	require.NoError(t, err)

	assert.Equal(t, fixedNow, events[0].LastCheckedOutTime, "LastCheckedOutTime should advance to the injected clock time, not time.Now()")
}

func Test_processEventCheckouts_createFailureDoesNotAbortBatch(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			return events[0], nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			events[0] = ev
			return nil
		},
	}

	var attempts []string
	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			attempts = append(attempts, c.PlanningCenterID)
			if c.PlanningCenterID == "pc_checkout_fail" {
				return c, assert.AnError
			}
			return c, nil
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return []planningcenter.Checkout{
				{ID: "pc_checkout_fail", FirstName: "Failing", LastName: "Row", SecurityCode: "FAIL", PlanningCenterLocationID: "pc_loc_1"},
				{ID: "pc_checkout_ok", FirstName: "Succeeding", LastName: "Row", SecurityCode: "OKOK", PlanningCenterLocationID: "pc_loc_1"},
			}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(5))

	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 0, false)
	err := svc.processEventCheckouts(t.Context(), events[0], svc.now())
	require.Error(t, err, "aggregated create failures should be returned")
	assert.True(t, isEventCheckinFailure(err), "batch create failures should be a non-fatal per-event failure")
	assert.Contains(t, err.Error(), "create checkin pc_checkout_fail")

	assert.Equal(t, []string{"pc_checkout_fail", "pc_checkout_ok"}, attempts, "both checkouts should be attempted even though the first fails")
	assert.Zero(t, eventRepo.UpdateEventFuncCallCount.Load(), "event should not be marked checked out when a checkin failed to create")
	assert.True(t, events[0].LastCheckedOutTime.IsZero(), "LastCheckedOutTime should remain unchanged so the event is retried next cycle")
}

func Test_eventCheckoutLoop_genericFetchError_nonFatal(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_ok", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 2, PlanningCenterID: "evt_fail", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	var eventsMu sync.Mutex
	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return events, nil
		},
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			eventsMu.Lock()
			defer eventsMu.Unlock()
			for _, e := range events {
				if e.ID == id {
					return e, nil
				}
			}
			return event.Event{}, nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			eventsMu.Lock()
			defer eventsMu.Unlock()
			for i, e := range events {
				if e.ID == ev.ID {
					events[i] = ev
					return nil
				}
			}
			return nil
		},
	}

	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			return c, nil
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			if eventID == "evt_fail" {
				return nil, &planningcenter.ServerError{}
			}
			return []planningcenter.Checkout{{ID: "c_ok", PlanningCenterLocationID: "pc_loc_1"}}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	err := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 5*time.Minute, false).eventCheckoutLoop(t.Context(), nil)
	require.NoError(t, err, "a generic per-event fetch failure should not fail the loop batch")

	assert.False(t, events[0].LastCheckedOutTime.IsZero(), "successful event should still be updated despite another event failing")
	assert.True(t, events[1].LastCheckedOutTime.IsZero(), "failed event should not advance LastCheckedOutTime")
}

func Test_eventCheckoutLoop_timeout_nonFatal(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 2, PlanningCenterID: "evt_2", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return events, nil
		},
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			for _, e := range events {
				if e.ID == id {
					return e, nil
				}
			}
			return event.Event{}, nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			for i, e := range events {
				if e.ID == ev.ID {
					events[i] = ev
					return nil
				}
			}
			return nil
		},
	}

	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			return c, nil
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return nil, &planningcenter.TimeoutError{Err: context.DeadlineExceeded}
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	err := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 5*time.Minute, false).eventCheckoutLoop(t.Context(), nil)
	require.NoError(t, err, "timeouts should be non-fatal and not fail the loop batch")
}

func Test_eventCheckoutLoop_timeout_placesEventInBackoff(t *testing.T) {
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
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			events[0] = ev
			return nil
		},
	}

	checkinRepo := &checkin.MockRepo{}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return nil, &planningcenter.TimeoutError{Err: context.DeadlineExceeded}
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	cur := time.Now()
	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, time.Millisecond, false)
	svc.now = func() time.Time { return cur }

	require.NoError(t, svc.eventCheckoutLoop(t.Context(), nil))
	require.Equal(t, int64(1), eventRepo.GetEventByIDFuncCallCount.Load(), "first cycle should process the event")

	require.NoError(t, svc.eventCheckoutLoop(t.Context(), nil))
	assert.Equal(t, int64(1), eventRepo.GetEventByIDFuncCallCount.Load(), "a timed-out event should be in backoff and skipped until it expires")

	cur = cur.Add(10 * time.Minute)
	require.NoError(t, svc.eventCheckoutLoop(t.Context(), nil))
	assert.Equal(t, int64(2), eventRepo.GetEventByIDFuncCallCount.Load(), "a timed-out event should be reprocessed once its backoff expires")
}

func Test_eventCheckoutLoop_checkinFailure_nonFatal(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return events, nil
		},
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			for _, e := range events {
				if e.ID == id {
					return e, nil
				}
			}
			return event.Event{}, nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			for i, e := range events {
				if e.ID == ev.ID {
					events[i] = ev
					return nil
				}
			}
			return nil
		},
	}

	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			return c, assert.AnError
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return []planningcenter.Checkout{
				{ID: "c_fail", FirstName: "Failing", LastName: "Row", SecurityCode: "FAIL", PlanningCenterLocationID: "pc_loc_1"},
			}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	err := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 5*time.Minute, false).eventCheckoutLoop(t.Context(), nil)
	require.NoError(t, err, "checkin-creation failures should be non-fatal and not fail the loop batch")
	assert.True(t, events[0].LastCheckedOutTime.IsZero(), "event with a failed checkin should not advance LastCheckedOutTime")
}

func Test_eventCheckoutLoop_drops_nonFatal(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_dropped", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 2, PlanningCenterID: "evt_ok", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return events, nil
		},
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			for _, e := range events {
				if e.ID == id {
					return e, nil
				}
			}
			return event.Event{}, nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			for i, e := range events {
				if e.ID == ev.ID {
					events[i] = ev
					return nil
				}
			}
			return nil
		},
	}

	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			return c, nil
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			if eventID == "evt_dropped" {
				return []planningcenter.Checkout{
					{ID: "c_dropped", FirstName: "Dropped", LastName: "Row", SecurityCode: "DROP", PlanningCenterLocationID: "unknown_loc"},
				}, nil
			}
			return []planningcenter.Checkout{
				{ID: "c_ok", FirstName: "Ok", LastName: "Row", SecurityCode: "OKOK", PlanningCenterLocationID: "pc_loc_1"},
			}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	err := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 5*time.Minute, false).eventCheckoutLoop(t.Context(), nil)
	require.NoError(t, err, "dropped checkouts should be non-fatal and not fail the loop batch")
	assert.True(t, events[0].LastCheckedOutTime.IsZero(), "event with a dropped checkout should not advance LastCheckedOutTime")
	assert.False(t, events[1].LastCheckedOutTime.IsZero(), "event with no drops should advance LastCheckedOutTime normally")
}

func Test_processEventCheckouts_updateEventFailureDoesNotCorruptState(t *testing.T) {
	originalTime := time.Now().Add(-1 * time.Hour).UTC()
	ev := event.Event{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: originalTime}

	eventRepo := &event.MockRepo{
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			return ev, nil
		},
		UpdateEventFunc: func(ctx context.Context, event event.Event) error {
			return assert.AnError
		},
	}

	var createdCheckins []checkin.Checkin
	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			c.ID = int64(len(createdCheckins) + 1)
			createdCheckins = append(createdCheckins, c)
			return c, nil
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return []planningcenter.Checkout{
				{ID: "pc_checkout_1", FirstName: "John", LastName: "Doe", SecurityCode: "ABCD", PlanningCenterLocationID: "pc_loc_1", CheckedOutAt: time.Now().Add(-30 * time.Minute)},
			}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(5))

	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 0, false)
	err := svc.processEventCheckouts(t.Context(), ev, svc.now())
	require.Error(t, err, "should return error when UpdateEvent fails")
	assert.Contains(t, err.Error(), "failed to update event")

	assert.Equal(t, originalTime, ev.LastCheckedOutTime, "LastCheckedOutTime should not be updated when UpdateEvent fails")

	assert.Len(t, createdCheckins, 1, "checkin should still be created before UpdateEvent failure")
}

func Test_getEventMutex_boundedStripedPool(t *testing.T) {
	svc := testService(noCheckWindows(), &event.MockRepo{}, &checkin.MockRepo{}, &planningcenter.MockClient{}, &sync.Map{}, 0, false)

	// Exercise the pool concurrently to make sure loads of distinct event IDs
	// share the fixed stripe array without races or nil mutexes.
	const eventCount = 10000
	errCh := make(chan error, eventCount)
	var wg sync.WaitGroup
	for i := range eventCount {
		id := int64(i + 1)
		wg.Go(func() {
			if svc.getEventMutex(id) == nil {
				errCh <- fmt.Errorf("getEventMutex(%d) returned nil", id)
				return
			}
			errCh <- nil
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	seen := make(map[*sync.Mutex]bool)
	for i := range eventCount {
		seen[svc.getEventMutex(int64(i+1))] = true
	}
	assert.LessOrEqual(t, len(seen), eventMutexStripeCount,
		"the striped pool must not grow beyond its fixed size regardless of distinct event IDs")
	assert.Greater(t, len(seen), 1, "distinct event IDs should spread across more than one stripe")

	for i := range eventCount {
		assert.Same(t, svc.getEventMutex(int64(i+1)), svc.getEventMutex(int64(i+1)), "the same event ID must always map to the same stripe mutex")
	}

	assert.Same(t, svc.getEventMutex(5), svc.getEventMutex(eventMutexStripeCount+5), "event IDs congruent modulo the stripe count share a lock")
}

func Test_getEventMutex_negativeEventID_doesNotPanic(t *testing.T) {
	svc := testService(noCheckWindows(), &event.MockRepo{}, &checkin.MockRepo{}, &planningcenter.MockClient{}, &sync.Map{}, 0, false)
	require.NotPanics(t, func() {
		mu := svc.getEventMutex(-1)
		require.NotNil(t, mu)
	})
}

func Test_processEventCheckouts_concurrentSameEvent_mutexProtection(t *testing.T) {
	var maxConcurrent atomic.Int64
	var currentConcurrent atomic.Int64

	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			return events[0], nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			current := currentConcurrent.Add(1)
			prev := maxConcurrent.Load()
			for current > prev {
				if maxConcurrent.CompareAndSwap(prev, current) {
					break
				}
				prev = maxConcurrent.Load()
			}
			time.Sleep(10 * time.Millisecond)
			currentConcurrent.Add(-1)
			events[0] = ev
			return nil
		},
	}

	var createdCheckins []checkin.Checkin
	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			c.ID = int64(len(createdCheckins) + 1)
			createdCheckins = append(createdCheckins, c)
			return c, nil
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return []planningcenter.Checkout{
				{ID: "c1", FirstName: "A", LastName: "A", SecurityCode: "AAA", PlanningCenterLocationID: "pc_loc_1"},
				{ID: "c2", FirstName: "B", LastName: "B", SecurityCode: "BBB", PlanningCenterLocationID: "pc_loc_1"},
			}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	const goroutines = 10
	errCh := make(chan error, goroutines)
	svc := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 0, false)

	var wg sync.WaitGroup

	ev := events[0]

	for range goroutines {
		wg.Go(func() {
			if err := svc.processEventCheckouts(t.Context(), ev, svc.now()); err != nil {
				errCh <- err
			}
		})
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}

	assert.Equal(t, int64(1), maxConcurrent.Load(), "at most 1 UpdateEvent should run concurrently due to mutex protection")
	assert.False(t, events[0].LastCheckedOutTime.IsZero(), "LastCheckedOutTime should be set after processing")
	assert.Len(t, createdCheckins, 2*goroutines, "each goroutine creates 2 checkins")
}

func Test_eventCheckoutLoop_contextCancellationMidLoop_breaksEarly(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 2, PlanningCenterID: "evt_2", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 3, PlanningCenterID: "evt_3", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 4, PlanningCenterID: "evt_4", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 5, PlanningCenterID: "evt_5", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 6, PlanningCenterID: "evt_6", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	ctx, cancel := context.WithCancel(t.Context())

	releaseCh := make(chan struct{})
	var blockedWG sync.WaitGroup
	blockedWG.Add(5)

	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return events, nil
		},
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			for _, e := range events {
				if e.ID == id {
					return e, nil
				}
			}
			return event.Event{}, nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			return nil
		},
	}

	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			return c, nil
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			blockedWG.Done()
			<-releaseCh
			return []planningcenter.Checkout{
				{ID: "c_" + eventID, FirstName: "F", LastName: "L", SecurityCode: "CODE", PlanningCenterLocationID: "pc_loc_1"},
			}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	errCh := make(chan error, 1)
	go func() {
		errCh <- testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 5*time.Minute, false).eventCheckoutLoop(ctx, nil)
	}()

	blockedWG.Wait()
	// All 5 semaphore slots are held; the 6th event is blocked on send.

	cancel()

	close(releaseCh)

	err := <-errCh
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func Test_eventCheckoutLoop_stopChClosed_noDispatch(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return events, nil
		},
	}

	var createdCheckins atomic.Int64
	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			createdCheckins.Add(1)
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

	stopCh := make(chan struct{})
	close(stopCh)

	err := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 5*time.Minute, false).eventCheckoutLoop(t.Context(), stopCh)
	require.NoError(t, err)
	assert.Zero(t, createdCheckins.Load(), "no checkouts should be fetched when stopCh is already closed")
}

func Test_eventCheckoutLoop_stopChMidLoop_drainsInFlight(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 2, PlanningCenterID: "evt_2", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 3, PlanningCenterID: "evt_3", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 4, PlanningCenterID: "evt_4", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 5, PlanningCenterID: "evt_5", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 6, PlanningCenterID: "evt_6", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	releaseCh := make(chan struct{})
	var blockedWG sync.WaitGroup
	blockedWG.Add(5)

	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return events, nil
		},
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			for _, e := range events {
				if e.ID == id {
					return e, nil
				}
			}
			return event.Event{}, nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			return nil
		},
	}

	var createdCheckins atomic.Int64
	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			return c, nil
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			blockedWG.Done()
			<-releaseCh
			createdCheckins.Add(1)
			return []planningcenter.Checkout{
				{ID: "c_" + eventID, FirstName: "F", LastName: "L", SecurityCode: "CODE", PlanningCenterLocationID: "pc_loc_1"},
			}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	stopCh := make(chan struct{})

	errCh := make(chan error, 1)
	go func() {
		errCh <- testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 5*time.Minute, false).eventCheckoutLoop(t.Context(), stopCh)
	}()

	blockedWG.Wait()
	// All 5 semaphore slots are held; the 6th event is blocked on send.
	close(stopCh)
	close(releaseCh)

	err := <-errCh
	require.NoError(t, err)
	assert.Equal(t, int64(5), createdCheckins.Load(), "only in-flight events should complete after stopCh closes")
	assert.Equal(t, int64(5), eventRepo.GetEventByIDFuncCallCount.Load(), "the 6th event should not have been dispatched after stopCh closes")
}

func Test_shouldSwallowLoopError(t *testing.T) {
	t.Run("running normally", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		assert.False(t, shouldSwallowLoopError(ctx, make(chan struct{})))
	})

	t.Run("stopCh closed during graceful drain", func(t *testing.T) {
		stopCh := make(chan struct{})
		close(stopCh)
		assert.True(t, shouldSwallowLoopError(t.Context(), stopCh))
	})

	t.Run("context cancelled (runtime expired)", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		assert.True(t, shouldSwallowLoopError(ctx, make(chan struct{})))
	})

	t.Run("both stopCh closed and context cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		stopCh := make(chan struct{})
		close(stopCh)
		assert.True(t, shouldSwallowLoopError(ctx, stopCh))
	})
}

func TestMinutesSinceWeekStartUTC(t *testing.T) {
	tests := []struct {
		name     string
		mockTime time.Time
		want     int
	}{
		{
			name:     "monday midnight",
			mockTime: time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC),
			want:     0,
		},
		{
			name:     "monday 09:30",
			mockTime: time.Date(2026, 3, 23, 9, 30, 0, 0, time.UTC),
			want:     570,
		},
		{
			name:     "wednesday 12:00",
			mockTime: time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC),
			want:     3600,
		},
		{
			name:     "saturday midday",
			mockTime: time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC),
			want:     7920,
		},
		{
			name:     "sunday 23:59",
			mockTime: time.Date(2026, 3, 29, 23, 59, 0, 0, time.UTC),
			want:     10079,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := minutesSinceWeekStartUTC(tt.mockTime)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLocalToUTCWeekMinutes(t *testing.T) {
	now := time.Date(2026, 3, 29, 22, 0, 0, 0, time.UTC)

	got, err := localToUTCWeekMinutes(1, 9, 0, "America/New_York", now)
	require.NoError(t, err)
	assert.Equal(t, 780, got, "Monday 09:00 NY should be 780 minutes (13:00 UTC)")
}

func TestLocalToUTCWeekMinutes_DSTWeek_usesPerDateOffset(t *testing.T) {
	// US DST begins Sunday 2026-03-08 02:00 local. A Sunday window must be
	// converted with the offset that actually applies on that Sunday (EDT,
	// UTC-4), not the offset of the anchor day (Monday). The minute value is
	// therefore different in the DST week than in the EST week.
	dstWeek := time.Date(2026, 3, 11, 16, 0, 0, 0, time.UTC) // Wed after DST start

	got, err := localToUTCWeekMinutes(7, 10, 0, "America/New_York", dstWeek)
	require.NoError(t, err)
	assert.Equal(t, 9480, got, "Sunday 10:00 in the DST week is 14:00 UTC = 6 days + 14h after Monday 00:00 UTC")

	estWeek := time.Date(2026, 2, 25, 17, 0, 0, 0, time.UTC) // Wed in a week fully before DST
	got, err = localToUTCWeekMinutes(7, 10, 0, "America/New_York", estWeek)
	require.NoError(t, err)
	assert.Equal(t, 9540, got, "Sunday 10:00 in the EST week is 15:00 UTC = 6 days + 15h after Monday 00:00 UTC")
}

func TestLocalToUTCWeekMinutes_FallBack_RepeatedHour_UsesEarlierOccurrence(t *testing.T) {
	// On the UK fall-back night (2026-10-25, 02:00 BST -> 01:00 GMT) the local
	// hour 01:00 occurs twice. A boundary at "Sunday 01:00" must resolve to the
	// earlier occurrence, 01:00 BST = 00:00 UTC = 6 days after Monday 00:00 UTC,
	// not to the post-transition 01:00 GMT. Otherwise a "Sat 23:00 -> Sun 01:00"
	// window runs an hour too long on the repeated night.
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

	start, err := localToUTCWeekMinutes(6, 23, 0, "Europe/London", now)
	require.NoError(t, err)
	assert.Equal(t, 8520, start, "Sat 23:00 BST = 22:00 UTC = 5 days + 22h after Monday 00:00 UTC")

	end, err := localToUTCWeekMinutes(7, 1, 0, "Europe/London", now)
	require.NoError(t, err)
	assert.Equal(t, 8640, end, "Sun 01:00 BST = 00:00 UTC = 6 days after Monday 00:00 UTC")
}

func Test_activeEventIDs_FallBack_RepeatedHour_DoesNotExtendWindow(t *testing.T) {
	// Europe/London falls back at 2026-10-25 02:00 BST -> 01:00 GMT, so local
	// 01:00 occurs twice. The window "Sat 23:00 -> Sun 01:00" must end at the
	// first 01:00 (01:00 BST = 00:00 UTC); it must not stay active into the
	// repeated hour.
	windowsByEvent := map[int64][]eventcheckwindow.EventCheckWindow{
		1: {
			{StartDayOfWeek: 6, StartTime: "23:00", EndDayOfWeek: 7, EndTime: "01:00", Timezone: "Europe/London"},
		},
	}

	// 00:30 UTC = 01:30 BST, inside the repeated hour but past the window end.
	now := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)
	active := activeEventIDs(slog.Default(), windowsByEvent, now)
	assert.False(t, active[1], "window ended at 01:00 BST; must not be active at 01:30 BST")

	// Saturday 22:30 UTC = 23:30 BST on a normal night (no transition) is inside
	// the window.
	normal := time.Date(2026, 10, 17, 22, 30, 0, 0, time.UTC)
	activeNormal := activeEventIDs(slog.Default(), windowsByEvent, normal)
	assert.True(t, activeNormal[1], "window Sat 23:00 BST -> Sun 01:00 BST must be active at 23:30 BST on a normal night")
}

func TestLocalToUTCWeekMinutes_SameDay(t *testing.T) {
	now := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)

	got, err := localToUTCWeekMinutes(3, 14, 30, "America/Los_Angeles", now)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, got, 0)
	assert.Less(t, got, minutesPerWeek)
}

func TestLocalToUTCWeekMinutes_InvalidTimezone(t *testing.T) {
	now := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)

	_, err := localToUTCWeekMinutes(1, 9, 0, "Invalid/Timezone", now)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timezone")
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		name      string
		timeStr   string
		wantHour  int
		wantMin   int
		wantError bool
	}{
		{"valid 24hr", "14:30", 14, 30, false},
		{"valid with leading zero", "09:05", 9, 5, false},
		{"valid midnight", "00:00", 0, 0, false},
		{"valid end of day", "23:59", 23, 59, false},
		{"invalid format", "14-30", 0, 0, true},
		{"invalid hour", "25:00", 0, 0, true},
		{"invalid minute", "12:60", 0, 0, true},
		{"empty", "", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hour, minute, err := parseTime(tt.timeStr)
			if tt.wantError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantHour, hour)
			assert.Equal(t, tt.wantMin, minute)
		})
	}
}

func TestMergeWindows(t *testing.T) {
	tests := []struct {
		name         string
		checkWindows []eventcheckwindow.EventCheckWindow
		wantLen      int
		wantErr      bool
	}{
		{
			name: "single window same day",
			checkWindows: []eventcheckwindow.EventCheckWindow{
				{StartDayOfWeek: 1, StartTime: "09:00", EndDayOfWeek: 1, EndTime: "12:00", Timezone: "America/New_York"},
			},
			wantLen: 1,
		},
		{
			name: "multiple non-overlapping",
			checkWindows: []eventcheckwindow.EventCheckWindow{
				{StartDayOfWeek: 1, StartTime: "09:00", EndDayOfWeek: 1, EndTime: "11:00", Timezone: "America/New_York"},
				{StartDayOfWeek: 1, StartTime: "14:00", EndDayOfWeek: 1, EndTime: "16:00", Timezone: "America/New_York"},
			},
			wantLen: 2,
		},
		{
			name: "overlapping windows merge",
			checkWindows: []eventcheckwindow.EventCheckWindow{
				{StartDayOfWeek: 1, StartTime: "09:00", EndDayOfWeek: 1, EndTime: "12:00", Timezone: "America/New_York"},
				{StartDayOfWeek: 1, StartTime: "11:00", EndDayOfWeek: 1, EndTime: "14:00", Timezone: "America/New_York"},
			},
			wantLen: 1,
		},
		{
			name: "crosses week boundary",
			checkWindows: []eventcheckwindow.EventCheckWindow{
				{StartDayOfWeek: 5, StartTime: "22:00", EndDayOfWeek: 6, EndTime: "02:00", Timezone: "America/New_York"},
			},
			wantLen: 1,
		},
		{
			name: "splits true Sunday/Monday crossing into two ranges",
			checkWindows: []eventcheckwindow.EventCheckWindow{
				{StartDayOfWeek: 7, StartTime: "22:00", EndDayOfWeek: 1, EndTime: "02:00", Timezone: "UTC"},
			},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Anchor all windows to the week of Wednesday 2026-03-25 so the
			// merged minute ranges are deterministic regardless of wall clock.
			now := time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC)
			got, err := mergeWindows(tt.checkWindows, now)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tt.wantLen)
		})
	}
}

func Test_activeEventIDs_sundayEvening_boundary(t *testing.T) {
	// Sunday evening in America/New_York overlaps UTC Monday (local Sunday
	// 21:00 EST = Monday 02:00 UTC). The window comparison must be anchored to
	// the local week so events whose windows are not yet open are not
	// spuriously marked active during that overlap.
	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	now := time.Date(2026, 3, 8, 21, 0, 0, 0, loc)

	windowsByEvent := map[int64][]eventcheckwindow.EventCheckWindow{
		1: {
			{StartDayOfWeek: 7, StartTime: "20:00", EndDayOfWeek: 1, EndTime: "02:00", Timezone: "America/New_York"},
		},
		2: {
			{StartDayOfWeek: 1, StartTime: "09:00", EndDayOfWeek: 1, EndTime: "11:00", Timezone: "America/New_York"},
		},
	}

	active := activeEventIDs(slog.Default(), windowsByEvent, now)
	assert.True(t, active[1], "Sunday 20:00-Monday 02:00 window should be active at Sunday 21:00 local")
	assert.False(t, active[2], "Monday 09:00-11:00 window must not be active at Sunday 21:00 local")
}

func Test_eventCheckoutLoop_windowMergeError_logsViaContextLogger(t *testing.T) {
	capture := &logger.CaptureSlogHandler{}
	logCtx := logger.WithLogger(t.Context(), slog.New(capture))

	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return []event.Event{{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true, LastCheckedOutTime: time.Time{}}}, nil
		},
	}
	checkWindowRepo := &eventcheckwindow.MockRepo{
		ListCheckWindowsFunc: func(ctx context.Context, filter eventcheckwindow.Filter) ([]eventcheckwindow.EventCheckWindow, error) {
			return []eventcheckwindow.EventCheckWindow{
				{EventID: 1, StartDayOfWeek: 1, StartTime: "09:00", EndDayOfWeek: 1, EndTime: "10:00", Timezone: "Invalid/Timezone"},
			}, nil
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			t.Fatal("event with a merge-failing window must not be fetched")
			return nil, nil
		},
	}

	svc := testService(checkWindowRepo, eventRepo, &checkin.MockRepo{}, pcClient, &sync.Map{}, 5*time.Minute, true)
	err := svc.eventCheckoutLoop(logCtx, nil)
	require.NoError(t, err)

	assert.True(t, capture.ContainsWarn("could not merge check windows for event"), "window merge failure should be logged via the context logger")
	assert.Equal(t, uint64(0), pcClient.GetCheckoutsForEventFuncCallCount.Load(), "event whose window fails to merge must be skipped, not fetched")
}

func Test_resolveLookBackTime_logsWarningViaLogger(t *testing.T) {
	capture := &logger.CaptureSlogHandler{}
	log := slog.New(capture)

	got := resolveLookBackTime(log, "not-a-duration")
	assert.Equal(t, -12*time.Hour, got)
	assert.True(t, capture.ContainsWarn("could not parse CHECKOUT_FETCHER_LOOKBACK_TIME"))

	got = resolveLookBackTime(log, "5m")
	assert.Equal(t, -12*time.Hour, got)
	assert.True(t, capture.ContainsWarn("CHECKOUT_FETCHER_LOOKBACK_TIME must not be greater than 0"))

	got = resolveLookBackTime(log, "-30m")
	assert.Equal(t, -30*time.Minute, got)
}

func Test_eventCheckoutLoop_windowFiltersEvents(t *testing.T) {
	events := []event.Event{
		{ID: 1, PlanningCenterID: "evt_active", AutoFetch: true, LastCheckedOutTime: time.Time{}},
		{ID: 2, PlanningCenterID: "evt_inactive", AutoFetch: true, LastCheckedOutTime: time.Time{}},
	}

	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return events, nil
		},
		GetEventByIDFunc: func(ctx context.Context, id int64) (event.Event, error) {
			for _, e := range events {
				if e.ID == id {
					return e, nil
				}
			}
			return event.Event{}, nil
		},
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			return nil
		},
	}

	var createdCheckins []checkin.Checkin
	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			c.ID = int64(len(createdCheckins) + 1)
			createdCheckins = append(createdCheckins, c)
			return c, nil
		},
	}

	pcClient := &planningcenter.MockClient{
		GetCheckoutsForEventFunc: func(ctx context.Context, eventID string, checkedOutOnOrAfter time.Time, limit int) ([]planningcenter.Checkout, error) {
			return []planningcenter.Checkout{
				{ID: "pc_" + eventID, FirstName: "John", LastName: "Doe", SecurityCode: "ABCD", PlanningCenterLocationID: "pc_loc_1"},
			}, nil
		},
	}

	locationIDMap := sync.Map{}
	locationIDMap.Store("pc_loc_1", int64(1))

	checkWindowRepo := &eventcheckwindow.MockRepo{
		ListCheckWindowsFunc: func(ctx context.Context, filter eventcheckwindow.Filter) ([]eventcheckwindow.EventCheckWindow, error) {
			return []eventcheckwindow.EventCheckWindow{
				{EventID: 1, StartDayOfWeek: 3, StartTime: "10:00", EndDayOfWeek: 3, EndTime: "14:00", Timezone: "UTC"},
				{EventID: 2, StartDayOfWeek: 3, StartTime: "08:00", EndDayOfWeek: 3, EndTime: "10:00", Timezone: "UTC"},
			}, nil
		},
	}

	// Pin the service clock to Wednesday 2026-03-25 12:00 UTC so the check
	// window evaluation is deterministic and independent of the wall clock.
	svc := testService(checkWindowRepo, eventRepo, checkinRepo, pcClient, &locationIDMap, 5*time.Minute, true)
	svc.now = func() time.Time { return time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC) }
	err := svc.eventCheckoutLoop(t.Context(), nil)
	require.NoError(t, err)

	require.Len(t, createdCheckins, 1, "only the event inside its check window should be fetched")
	assert.Equal(t, "pc_evt_active", createdCheckins[0].PlanningCenterID)
}

func Test_eventCheckoutLoop_noWindows_alwaysActive(t *testing.T) {
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
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			return nil
		},
	}

	var createdCheckins []checkin.Checkin
	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			c.ID = int64(len(createdCheckins) + 1)
			createdCheckins = append(createdCheckins, c)
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

	err := testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 5*time.Minute, true).eventCheckoutLoop(t.Context(), nil)
	require.NoError(t, err)
	assert.Len(t, createdCheckins, 1, "events without configured windows should still be fetched")
}

func Test_eventCheckoutLoop_windowRepoError(t *testing.T) {
	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return []event.Event{{ID: 1, PlanningCenterID: "evt_1", AutoFetch: true}}, nil
		},
	}

	checkWindowRepo := &eventcheckwindow.MockRepo{
		ListCheckWindowsFunc: func(ctx context.Context, filter eventcheckwindow.Filter) ([]eventcheckwindow.EventCheckWindow, error) {
			return nil, assert.AnError
		},
	}

	err := testService(checkWindowRepo, eventRepo, &checkin.MockRepo{}, &planningcenter.MockClient{}, &sync.Map{}, 5*time.Minute, true).eventCheckoutLoop(t.Context(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list check windows")
}

func Test_eventCheckoutLoop_windowsDisabled_ignoresWindows(t *testing.T) {
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
		UpdateEventFunc: func(ctx context.Context, ev event.Event) error {
			return nil
		},
	}

	var createdCheckins []checkin.Checkin
	checkinRepo := &checkin.MockRepo{
		CreateCheckinFunc: func(ctx context.Context, c checkin.Checkin) (checkin.Checkin, error) {
			c.ID = int64(len(createdCheckins) + 1)
			createdCheckins = append(createdCheckins, c)
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

	checkWindowRepo := &eventcheckwindow.MockRepo{
		ListCheckWindowsFunc: func(ctx context.Context, filter eventcheckwindow.Filter) ([]eventcheckwindow.EventCheckWindow, error) {
			return nil, assert.AnError
		},
	}

	err := testService(checkWindowRepo, eventRepo, checkinRepo, pcClient, &locationIDMap, 5*time.Minute, false).eventCheckoutLoop(t.Context(), nil)
	require.NoError(t, err)
	assert.Len(t, createdCheckins, 1, "events should be fetched regardless of windows when the flag is off")
	assert.Zero(t, checkWindowRepo.ListCheckWindowsFuncCallCount, "check windows should not be queried when the flag is off")
}

// fetchCheckoutsCommand mirrors the checkout-fetcher subcommand flags in
// internal/cmd/root.go so tests can exercise FetchCheckouts directly.
func fetchCheckoutsCommand() *cli.Command {
	return &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "db-file", Value: "kids-checkin.db"},
			&cli.DurationFlag{Name: "interval", Value: 3 * time.Second},
			&cli.DurationFlag{Name: "event-update-interval", Value: 3 * time.Second},
			&cli.DurationFlag{Name: "runtime", Value: 5 * time.Second},
			&cli.BoolFlag{Name: "service"},
			&cli.BoolFlag{Name: "use-check-windows"},
		},
	}
}

// Test_FetchCheckouts_dbInitFailure returns a wrapped error instead of
// panicking when the database cannot be initialized (bad path, permissions,
// corrupt file).
func Test_FetchCheckouts_dbInitFailure(t *testing.T) {
	missingDB := filepath.Join(t.TempDir(), "does-not-exist", "kids-checkin.db")

	cmd := fetchCheckoutsCommand()
	require.NoError(t, cmd.Set("db-file", missingDB))

	// Must return a normal error (not panic) so main can print a clean message.
	require.NotPanics(t, func() {
		err := FetchCheckouts(t.Context(), cmd)
		require.Error(t, err)
		assert.ErrorContains(t, err, "init db")
		assert.ErrorContains(t, err, missingDB)
	})
}

// channelClosed reports whether ch was closed within timeout.
func channelClosed(t *testing.T, ch <-chan struct{}, timeout time.Duration) bool {
	t.Helper()
	select {
	case <-ch:
		return true
	case <-time.After(timeout):
		return false
	}
}

// runWithBlocks calls s.run in a background goroutine and returns the channel
// that will receive its result.
func runWithBlocks(svc *Service, ctx context.Context, stopCh, forceExitCh <-chan struct{}, interval time.Duration) <-chan error {
	resultCh := make(chan error, 1)
	go func() {
		resultCh <- svc.run(ctx, stopCh, forceExitCh, interval)
	}()
	return resultCh
}

// Test_run_preclosedStopCh_exitsWithoutPolling asserts the pre-loop shutdown
// check returns immediately when stopCh is already closed, so no polling round
// is scheduled.
func Test_run_preclosedStopCh_exitsWithoutPolling(t *testing.T) {
	listCalled := atomic.Bool{}
	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			listCalled.Store(true)
			return nil, fmt.Errorf("eventCheckoutLoop should not run when stopCh is already closed")
		},
	}
	checkinRepo := &checkin.MockRepo{}
	pcClient := &planningcenter.MockClient{}
	locationIDMap := sync.Map{}

	stopCh := make(chan struct{})
	close(stopCh)
	resultCh := runWithBlocks(testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 3*time.Second, false), t.Context(), stopCh, make(chan struct{}), 5*time.Minute)

	select {
	case err := <-resultCh:
		require.NoError(t, err, "a pre-closed stopCh should exit cleanly")
	case <-time.After(time.Second):
		t.Fatal("run did not return despite stopCh being pre-closed")
	}
	assert.False(t, listCalled.Load(), "no polling round should be started when stopCh is already closed")
}

// Test_run_cancelledContext_exitsWithoutPolling asserts the pre-loop shutdown
// check returns immediately when the context is already done.
func Test_run_cancelledContext_exitsWithoutPolling(t *testing.T) {
	listCalled := atomic.Bool{}
	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			listCalled.Store(true)
			return nil, fmt.Errorf("eventCheckoutLoop should not run when the context is already done")
		},
	}
	checkinRepo := &checkin.MockRepo{}
	pcClient := &planningcenter.MockClient{}
	locationIDMap := sync.Map{}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	resultCh := runWithBlocks(testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 3*time.Second, false), ctx, make(chan struct{}), make(chan struct{}), 5*time.Minute)

	select {
	case err := <-resultCh:
		require.NoError(t, err, "a done context should exit cleanly")
	case <-time.After(time.Second):
		t.Fatal("run did not return despite the context being done")
	}
	assert.False(t, listCalled.Load(), "no polling round should be started when the context is already done")
}

// Test_run_forceExitChClosed_forceExits asserts that a closed forceExitCh is
// caught by the blocking select after a polling round and surfaced as the
// forced-exit error.
func Test_run_forceExitChClosed_forceExits(t *testing.T) {
	var listCalls atomic.Int64
	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			listCalls.Add(1)
			return nil, nil
		},
	}
	checkinRepo := &checkin.MockRepo{}
	pcClient := &planningcenter.MockClient{}
	locationIDMap := sync.Map{}

	stopCh := make(chan struct{})
	forceExitCh := make(chan struct{})
	close(forceExitCh)
	resultCh := runWithBlocks(testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 3*time.Second, false), t.Context(), stopCh, forceExitCh, 5*time.Minute)

	select {
	case err := <-resultCh:
		require.Error(t, err)
		assert.ErrorContains(t, err, "graceful shutdown did not complete")
	case <-time.After(time.Second):
		t.Fatal("run did not return despite forceExitCh being closed")
	}
	assert.Equal(t, int64(1), listCalls.Load(), "one polling round should run before the blocking select sees forceExitCh")
}

// Test_run_stopChClosedDuringRun_exitsCleanly asserts that closing stopCh while
// the loop is sleeping on its interval exits cleanly via the blocking select.
func Test_run_stopChClosedDuringRun_exitsCleanly(t *testing.T) {
	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			return nil, nil
		},
	}
	checkinRepo := &checkin.MockRepo{}
	pcClient := &planningcenter.MockClient{}
	locationIDMap := sync.Map{}

	stopCh := make(chan struct{})
	forceExitCh := make(chan struct{})
	resultCh := runWithBlocks(testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 3*time.Second, false), t.Context(), stopCh, forceExitCh, 30*time.Second)

	time.Sleep(50 * time.Millisecond)
	close(stopCh)

	select {
	case err := <-resultCh:
		require.NoError(t, err, "stopCh during the polling sleep should end the loop cleanly")
	case <-time.After(time.Second):
		t.Fatal("run did not return after stopCh was closed during the polling sleep")
	}
}

// Test_run_cadenceMeasuredFromLoopStart asserts the polling cadence is measured
// from the start of each iteration, not its completion. The mock event loop
// takes longer than the interval; the gap between consecutive loop starts must
// therefore track the work duration (no full extra interval sleep is added on
// top), while iterations must still never overlap.
func Test_run_cadenceMeasuredFromLoopStart(t *testing.T) {
	const (
		interval = 100 * time.Millisecond
		workTime = 120 * time.Millisecond
	)

	var (
		mu     sync.Mutex
		starts []time.Time
	)
	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			mu.Lock()
			starts = append(starts, time.Now())
			mu.Unlock()
			time.Sleep(workTime)
			return nil, nil
		},
	}
	checkinRepo := &checkin.MockRepo{}
	pcClient := &planningcenter.MockClient{}
	locationIDMap := sync.Map{}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stopCh := make(chan struct{})
	forceExitCh := make(chan struct{})
	resultCh := runWithBlocks(testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 3*time.Second, false), ctx, stopCh, forceExitCh, interval)

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(starts)
		mu.Unlock()
		if n >= 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for four polling iterations")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-resultCh:
		require.NoError(t, err, "cancelling the context should end the loop cleanly")
	case <-time.After(time.Second):
		t.Fatal("run did not return after the context was cancelled")
	}

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(starts), 4, "expected at least four polling iterations")
	for i := 1; i < len(starts); i++ {
		gap := starts[i].Sub(starts[i-1])
		// Iterations must not overlap: a new iteration only begins after the
		// previous one's work completes.
		require.GreaterOrEqual(t, gap, workTime-25*time.Millisecond,
			"iteration %d started before the previous one finished", i)
		// Cadence is measured from loop start: a slow iteration must not add a
		// full extra interval on top of its runtime. The old model produced
		// ~workTime+interval gaps; this model produces ~workTime ones. The
		// tolerance is deliberately loose on the high side: it only needs to
		// reject the ~workTime+interval signature while absorbing scheduler
		// jitter on a loaded CI.
		require.Less(t, gap, workTime+interval,
			"iteration %d gap suggests the interval was added on top of the work duration", i)
	}
}

// Test_run_clampsMinimumGapWhenWorkExceedsInterval asserts that when a polling
// iteration's work takes longer than the interval, the loop still waits a
// minimum idle gap (interval/2) before starting the next iteration instead of
// busy-looping back-to-back. Cadence remains measured from loop start, so the
// gap tracks the work duration plus the clamped floor rather than adding a full
// extra interval on top.
func Test_run_clampsMinimumGapWhenWorkExceedsInterval(t *testing.T) {
	const (
		interval = 100 * time.Millisecond
		workTime = 400 * time.Millisecond
		minGap   = interval / 2
	)

	var (
		mu     sync.Mutex
		starts []time.Time
	)
	eventRepo := &event.MockRepo{
		ListEventsFunc: func(ctx context.Context, filter event.EventFilter) ([]event.Event, error) {
			mu.Lock()
			starts = append(starts, time.Now())
			mu.Unlock()
			time.Sleep(workTime)
			return nil, nil
		},
	}
	checkinRepo := &checkin.MockRepo{}
	pcClient := &planningcenter.MockClient{}
	locationIDMap := sync.Map{}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stopCh := make(chan struct{})
	forceExitCh := make(chan struct{})
	resultCh := runWithBlocks(testService(noCheckWindows(), eventRepo, checkinRepo, pcClient, &locationIDMap, 3*time.Second, false), ctx, stopCh, forceExitCh, interval)

	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		n := len(starts)
		mu.Unlock()
		if n >= 4 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for four polling iterations")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-resultCh:
		require.NoError(t, err, "cancelling the context should end the loop cleanly")
	case <-time.After(time.Second):
		t.Fatal("run did not return after the context was cancelled")
	}

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(starts), 4, "expected at least four polling iterations")
	for i := 1; i < len(starts); i++ {
		gap := starts[i].Sub(starts[i-1])
		// A slow iteration must not be followed by a back-to-back start: the
		// clamped floor (interval/2) guarantees a minimum idle gap after work
		// that exceeds the interval.
		require.GreaterOrEqual(t, gap, workTime+minGap-25*time.Millisecond,
			"iteration %d started with less than the minimum clamped gap after the previous work", i)
		// The cadence is still measured from loop start: a slow iteration must
		// not add a full extra interval on top of its runtime plus the floor.
		require.Less(t, gap, workTime+interval,
			"iteration %d gap suggests the interval was added on top of the work duration", i)
	}
}

// runSignalLoopInGoroutine runs runSignalLoop in a background goroutine and
// returns the channel that will receive its result.
func runSignalLoopInGoroutine(logCtx context.Context, sigCh <-chan os.Signal, sigDone <-chan struct{}, stopOnce *sync.Once, stopCh, forceExitCh chan struct{}, gracePeriod time.Duration) <-chan error {
	resultCh := make(chan error, 1)
	go func() {
		resultCh <- runSignalLoop(logCtx, sigCh, sigDone, stopOnce, stopCh, forceExitCh, gracePeriod)
	}()
	return resultCh
}

func Test_runSignalLoop_firstSignal_gracefulStop(t *testing.T) {
	sigCh := make(chan os.Signal, 1)
	sigDone := make(chan struct{})
	stopCh := make(chan struct{})
	forceExitCh := make(chan struct{})
	var stopOnce sync.Once

	resultCh := runSignalLoopInGoroutine(t.Context(), sigCh, sigDone, &stopOnce, stopCh, forceExitCh, time.Hour)

	sigCh <- os.Interrupt

	assert.True(t, channelClosed(t, stopCh, time.Second), "first signal should close stopCh to start a graceful drain")

	close(sigDone)
	select {
	case err := <-resultCh:
		require.NoError(t, err, "clean shutdown should produce no error")
	case <-time.After(time.Second):
		t.Fatal("runSignalLoop did not exit after sigDone was closed (goroutine leak)")
	}
}

func Test_runSignalLoop_secondSignal_forcesImmediateExit(t *testing.T) {
	sigCh := make(chan os.Signal, 1)
	sigDone := make(chan struct{})
	stopCh := make(chan struct{})
	forceExitCh := make(chan struct{})
	var stopOnce sync.Once

	resultCh := runSignalLoopInGoroutine(t.Context(), sigCh, sigDone, &stopOnce, stopCh, forceExitCh, time.Hour)

	sigCh <- os.Interrupt
	assert.True(t, channelClosed(t, stopCh, time.Second), "first signal should close stopCh")

	sigCh <- syscall.SIGTERM
	select {
	case err := <-resultCh:
		require.ErrorIs(t, err, errForceExitNow, "second signal should request an immediate force exit")
	case <-time.After(time.Second):
		t.Fatal("runSignalLoop did not return after the second signal")
	}
}

func Test_runSignalLoop_secondSignal_stopsForceExitTimer(t *testing.T) {
	const shortGrace = 50 * time.Millisecond

	sigCh := make(chan os.Signal, 1)
	sigDone := make(chan struct{})
	stopCh := make(chan struct{})
	forceExitCh := make(chan struct{})
	var stopOnce sync.Once

	resultCh := runSignalLoopInGoroutine(t.Context(), sigCh, sigDone, &stopOnce, stopCh, forceExitCh, shortGrace)

	sigCh <- os.Interrupt
	assert.True(t, channelClosed(t, stopCh, time.Second), "first signal should close stopCh")

	sigCh <- syscall.SIGTERM
	select {
	case err := <-resultCh:
		require.ErrorIs(t, err, errForceExitNow, "second signal should request an immediate force exit")
	case <-time.After(time.Second):
		t.Fatal("runSignalLoop did not return after the second signal")
	}

	assert.False(t, channelClosed(t, forceExitCh, 2*shortGrace), "the pending force-exit timer should be stopped on the second signal")
}

func Test_runSignalLoop_forceExitTimer_closesForceExitCh(t *testing.T) {
	const shortGrace = 50 * time.Millisecond

	sigCh := make(chan os.Signal, 1)
	sigDone := make(chan struct{})
	stopCh := make(chan struct{})
	forceExitCh := make(chan struct{})
	var stopOnce sync.Once

	resultCh := runSignalLoopInGoroutine(t.Context(), sigCh, sigDone, &stopOnce, stopCh, forceExitCh, shortGrace)

	sigCh <- os.Interrupt

	assert.True(t, channelClosed(t, stopCh, time.Second), "first signal should close stopCh")
	assert.True(t, channelClosed(t, forceExitCh, 2*time.Second), "force exit channel should close after the grace period")

	close(sigDone)
	select {
	case err := <-resultCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("runSignalLoop did not exit after sigDone was closed (goroutine leak)")
	}
}

func Test_runSignalLoop_sigDoneClosed_noSignal_exitsCleanly(t *testing.T) {
	sigCh := make(chan os.Signal, 1)
	sigDone := make(chan struct{})
	close(sigDone)
	stopCh := make(chan struct{})
	forceExitCh := make(chan struct{})
	var stopOnce sync.Once

	resultCh := runSignalLoopInGoroutine(t.Context(), sigCh, sigDone, &stopOnce, stopCh, forceExitCh, time.Second)

	select {
	case err := <-resultCh:
		require.NoError(t, err, "closing sigDone without any signal should be a clean no-op exit")
	case <-time.After(time.Second):
		t.Fatal("runSignalLoop did not exit after sigDone was closed")
	}

	assert.False(t, channelClosed(t, stopCh, 20*time.Millisecond), "no signal should never close stopCh")
}
