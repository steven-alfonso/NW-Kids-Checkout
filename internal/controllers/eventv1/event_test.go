package eventv1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"kids-checkin/internal/client/planningcenter"
	"kids-checkin/internal/db"
	"kids-checkin/internal/repo/eventcheckwindow"

	"github.com/Masterminds/squirrel"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestController_GetEventByID(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	builder := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id")
	res, err := builder.Values("Family Service", "pc_evt_22").ExecContext(t.Context())
	require.NoError(t, err)
	insertedID, _ := res.LastInsertId()

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/events/%d", insertedID), nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var payload Event
		err = json.NewDecoder(resp.Body).Decode(&payload)
		require.NoError(t, err)
		assert.Equal(t, insertedID, payload.ID)
		assert.Equal(t, "Family Service", payload.Name)
		assert.Equal(t, "pc_evt_22", payload.PlanningCenterID)
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/events/9999", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	})

	t.Run("invalid id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/events/not-a-number", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		assert.Equal(t, "invalid id", bodyString(t, resp.Body))
	})
}

func TestController_PostCreateEvent(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	mockClient := &planningcenter.MockClient{}
	controller.client = mockClient
	controller.RegisterRoutes(app)

	payload := EventInput{
		PlanningCenterID: "pc_evt_77",
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	mockClient.GetEventByIDFunc = func(ctx context.Context, eventID string) (planningcenter.Event, error) {
		require.Equal(t, payload.PlanningCenterID, eventID)
		return planningcenter.Event{ID: eventID, Name: "Kids Service"}, nil
	}

	mockClient.GetLocationsForEventFunc = func(ctx context.Context, eventID string) ([]planningcenter.Location, error) {
		require.Equal(t, payload.PlanningCenterID, eventID)
		return []planningcenter.Location{
			{ID: "loc-1", Name: "Room A"},
			{ID: "loc-2", Name: "Room B"},
		}, nil
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusCreated, resp.StatusCode)

	var created Event
	err = json.NewDecoder(resp.Body).Decode(&created)
	require.NoError(t, err)
	assert.NotZero(t, created.ID)
	assert.Equal(t, "Kids Service", created.Name)
	assert.Equal(t, payload.PlanningCenterID, created.PlanningCenterID)
}

func TestController_PostCreateEvent_MissingPlanningCenterID(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.client = &planningcenter.MockClient{}
	controller.RegisterRoutes(app)

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/events", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestController_PostCreateEvent_SyncExisting(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	mockClient := &planningcenter.MockClient{}
	controller.client = mockClient
	controller.RegisterRoutes(app)

	payload := EventInput{
		PlanningCenterID: "pc_evt_77",
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	_, err = squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id").
		Values("Existing Event", payload.PlanningCenterID).
		ExecContext(t.Context())
	require.NoError(t, err)

	mockClient.GetEventByIDFunc = func(ctx context.Context, eventID string) (planningcenter.Event, error) {
		require.Equal(t, payload.PlanningCenterID, eventID)
		return planningcenter.Event{ID: eventID, Name: "Kids Service"}, nil
	}

	mockClient.GetLocationsForEventFunc = func(ctx context.Context, eventID string) ([]planningcenter.Location, error) {
		require.Equal(t, payload.PlanningCenterID, eventID)
		return []planningcenter.Location{{ID: "loc-1", Name: "Room A"}}, nil
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var output Event
	err = json.NewDecoder(resp.Body).Decode(&output)
	require.NoError(t, err)
	assert.Equal(t, "Kids Service", output.Name)
	assert.Equal(t, payload.PlanningCenterID, output.PlanningCenterID)
}

func TestController_GetEventByPlanningCenterID(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	res, err := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id").
		Values("Family Service", "pc_evt_22").
		ExecContext(t.Context())
	require.NoError(t, err)
	insertedID, _ := res.LastInsertId()
	require.NotZero(t, insertedID)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/events/lookup?planning_center_id=pc_evt_22", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var payload Event
	err = json.NewDecoder(resp.Body).Decode(&payload)
	require.NoError(t, err)
	assert.Equal(t, insertedID, payload.ID)
	assert.Equal(t, "Family Service", payload.Name)
	assert.Equal(t, "pc_evt_22", payload.PlanningCenterID)

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/admin/events/lookup?planning_center_id=missing", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	})

	t.Run("missing query", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/admin/events/lookup", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})
}

func setupAuthedApp() (*fiber.App, *session.Store) {
	app := fiber.New()
	store := session.New()
	app.Use(func(c *fiber.Ctx) error {
		sess, _ := store.Get(c)
		sess.Set("authenticated", true)
		sess.Set("role", "admin")
		if err := sess.Save(); err != nil {
			return err
		}
		return c.Next()
	})
	return app, store
}

func TestController_GetEventCheckWindows(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	eventRes, err := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id").
		Values("Sunday Service", "pc_evt_1").
		ExecContext(t.Context())
	require.NoError(t, err)
	eventID, _ := eventRes.LastInsertId()

	_, err = squirrel.Insert("event_check_windows").
		RunWith(testDB).
		Columns("event_id", "start_day_of_week", "start_time", "end_day_of_week", "end_time", "timezone").
		Values(eventID, 1, "09:00", 1, "12:00", "America/Los_Angeles").
		ExecContext(t.Context())
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/admin/events/%d/check-windows", eventID), nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var payload []EventCheckWindowOutput
		err = json.NewDecoder(resp.Body).Decode(&payload)
		require.NoError(t, err)
		assert.Len(t, payload, 1)
		assert.Equal(t, 1, payload[0].StartDayOfWeek)
		assert.Equal(t, "09:00", payload[0].StartTime)
	})

	t.Run("empty list", func(t *testing.T) {
		otherRes, err := squirrel.Insert("events").
			RunWith(testDB).
			Columns("name", "planning_center_id").
			Values("Other Service", "pc_evt_2").
			ExecContext(t.Context())
		require.NoError(t, err)
		otherEventID, _ := otherRes.LastInsertId()

		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/admin/events/%d/check-windows", otherEventID), nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var payload []EventCheckWindowOutput
		err = json.NewDecoder(resp.Body).Decode(&payload)
		require.NoError(t, err)
		assert.Len(t, payload, 0)
	})

	t.Run("invalid event id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/admin/events/not-a-number/check-windows", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		assert.Equal(t, "invalid event id", bodyString(t, resp.Body))
	})
}

func TestController_PostCreateCheckWindow(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	eventRes, err := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id").
		Values("Sunday Service", "pc_evt_1").
		ExecContext(t.Context())
	require.NoError(t, err)
	eventID, _ := eventRes.LastInsertId()

	t.Run("success", func(t *testing.T) {
		payload := EventCheckWindowInput{
			StartDayOfWeek: 1,
			StartTime:      "09:00",
			EndDayOfWeek:   1,
			EndTime:        "12:00",
			Timezone:       "America/Los_Angeles",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v1/admin/events/%d/check-windows", eventID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusCreated, resp.StatusCode)

		var created EventCheckWindowOutput
		err = json.NewDecoder(resp.Body).Decode(&created)
		require.NoError(t, err)
		assert.NotZero(t, created.ID)
		assert.Equal(t, eventID, created.EventID)
		assert.Equal(t, 1, created.StartDayOfWeek)
	})

	t.Run("invalid input", func(t *testing.T) {
		payload := EventCheckWindowInput{
			StartDayOfWeek: 0,
			StartTime:      "09:00",
			EndDayOfWeek:   1,
			EndTime:        "12:00",
			Timezone:       "America/Los_Angeles",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v1/admin/events/%d/check-windows", eventID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("invalid event id", func(t *testing.T) {
		payload := EventCheckWindowInput{
			StartDayOfWeek: 1,
			StartTime:      "09:00",
			EndDayOfWeek:   1,
			EndTime:        "12:00",
			Timezone:       "America/Los_Angeles",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/v1/admin/events/not-a-number/check-windows", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("event does not exist", func(t *testing.T) {
		payload := EventCheckWindowInput{
			StartDayOfWeek: 1,
			StartTime:      "09:00",
			EndDayOfWeek:   1,
			EndTime:        "12:00",
			Timezone:       "America/Los_Angeles",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v1/admin/events/%d/check-windows", 999999), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	})
}

func TestController_PutUpdateCheckWindow(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	eventRes, err := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id").
		Values("Sunday Service", "pc_evt_1").
		ExecContext(t.Context())
	require.NoError(t, err)
	eventID, _ := eventRes.LastInsertId()

	windowRes, err := squirrel.Insert("event_check_windows").
		RunWith(testDB).
		Columns("event_id", "start_day_of_week", "start_time", "end_day_of_week", "end_time", "timezone").
		Values(eventID, 1, "09:00", 1, "12:00", "America/Los_Angeles").
		ExecContext(t.Context())
	require.NoError(t, err)
	windowID, _ := windowRes.LastInsertId()

	t.Run("success", func(t *testing.T) {
		payload := EventCheckWindowInput{
			StartDayOfWeek: 1,
			StartTime:      "10:00",
			EndDayOfWeek:   1,
			EndTime:        "13:00",
			Timezone:       "America/Los_Angeles",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v1/admin/events/%d/check-windows/%d", eventID, windowID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var updated EventCheckWindowOutput
		err = json.NewDecoder(resp.Body).Decode(&updated)
		require.NoError(t, err)
		assert.Equal(t, "10:00", updated.StartTime)
		assert.Equal(t, "13:00", updated.EndTime)
	})

	t.Run("not found", func(t *testing.T) {
		payload := EventCheckWindowInput{
			StartDayOfWeek: 1,
			StartTime:      "10:00",
			EndDayOfWeek:   1,
			EndTime:        "13:00",
			Timezone:       "America/Los_Angeles",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v1/admin/events/%d/check-windows/9999", eventID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	})

	t.Run("invalid window id", func(t *testing.T) {
		payload := EventCheckWindowInput{
			StartDayOfWeek: 1,
			StartTime:      "10:00",
			EndDayOfWeek:   1,
			EndTime:        "13:00",
			Timezone:       "America/Los_Angeles",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v1/admin/events/%d/check-windows/not-a-number", eventID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		assert.Equal(t, "invalid window id", bodyString(t, resp.Body))
	})

	t.Run("window belongs to another event", func(t *testing.T) {
		otherEventRes, err := squirrel.Insert("events").
			RunWith(testDB).
			Columns("name", "planning_center_id").
			Values("Wednesday Service", "pc_evt_other").
			ExecContext(t.Context())
		require.NoError(t, err)
		otherEventID, _ := otherEventRes.LastInsertId()

		ownedRes, err := squirrel.Insert("event_check_windows").
			RunWith(testDB).
			Columns("event_id", "start_day_of_week", "start_time", "end_day_of_week", "end_time", "timezone").
			Values(eventID, 1, "09:00", 1, "12:00", "America/Los_Angeles").
			ExecContext(t.Context())
		require.NoError(t, err)
		ownedWindowID, _ := ownedRes.LastInsertId()

		payload := EventCheckWindowInput{
			StartDayOfWeek: 2,
			StartTime:      "10:00",
			EndDayOfWeek:   2,
			EndTime:        "13:00",
			Timezone:       "America/Los_Angeles",
		}
		body, _ := json.Marshal(payload)

		// ownedWindowID belongs to eventID; the URL uses a different event, so
		// the update must be rejected rather than silently mutating another
		// event's window.
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v1/admin/events/%d/check-windows/%d", otherEventID, ownedWindowID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)

		// The window must be untouched and still owned by the original event.
		var stored eventcheckwindow.EventCheckWindow
		err = squirrel.Select("event_id", "start_day_of_week", "start_time", "end_day_of_week", "end_time", "timezone").
			From("event_check_windows").
			Where(squirrel.Eq{"id": ownedWindowID}).
			RunWith(testDB).
			QueryRowContext(t.Context()).
			Scan(&stored.EventID, &stored.StartDayOfWeek, &stored.StartTime, &stored.EndDayOfWeek, &stored.EndTime, &stored.Timezone)
		require.NoError(t, err)
		assert.Equal(t, eventID, stored.EventID)
		assert.Equal(t, "09:00", stored.StartTime)
		assert.Equal(t, "12:00", stored.EndTime)
	})
}

func TestController_DeleteCheckWindow(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	eventRes, err := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id").
		Values("Sunday Service", "pc_evt_1").
		ExecContext(t.Context())
	require.NoError(t, err)
	eventID, _ := eventRes.LastInsertId()

	windowRes, err := squirrel.Insert("event_check_windows").
		RunWith(testDB).
		Columns("event_id", "start_day_of_week", "start_time", "end_day_of_week", "end_time", "timezone").
		Values(eventID, 1, "09:00", 1, "12:00", "America/Los_Angeles").
		ExecContext(t.Context())
	require.NoError(t, err)
	windowID, _ := windowRes.LastInsertId()

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/v1/admin/events/%d/check-windows/%d", eventID, windowID), nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusNoContent, resp.StatusCode)
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/v1/admin/events/%d/check-windows/9999", eventID), nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	})

	t.Run("invalid window id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/v1/admin/events/%d/check-windows/not-a-number", eventID), nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		assert.Equal(t, "invalid window id", bodyString(t, resp.Body))
	})

	t.Run("window belongs to another event", func(t *testing.T) {
		otherEventRes, err := squirrel.Insert("events").
			RunWith(testDB).
			Columns("name", "planning_center_id").
			Values("Wednesday Service", "pc_evt_other").
			ExecContext(t.Context())
		require.NoError(t, err)
		otherEventID, _ := otherEventRes.LastInsertId()

		ownedRes, err := squirrel.Insert("event_check_windows").
			RunWith(testDB).
			Columns("event_id", "start_day_of_week", "start_time", "end_day_of_week", "end_time", "timezone").
			Values(eventID, 1, "09:00", 1, "12:00", "America/Los_Angeles").
			ExecContext(t.Context())
		require.NoError(t, err)
		ownedWindowID, _ := ownedRes.LastInsertId()

		// ownedWindowID belongs to eventID; the URL uses a different event, so
		// the delete must be rejected rather than removing another event's window.
		req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/v1/admin/events/%d/check-windows/%d", otherEventID, ownedWindowID), nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)

		var count int
		err = squirrel.Select("COUNT(*)").
			From("event_check_windows").
			Where(squirrel.Eq{"id": ownedWindowID}).
			RunWith(testDB).
			QueryRowContext(t.Context()).
			Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "window must still exist after rejected cross-event delete")
	})
}

func TestController_PatchUpdateEvent(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	eventRes, err := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id", "auto_fetch").
		Values("Sunday Service", "pc_evt_1", 0).
		ExecContext(t.Context())
	require.NoError(t, err)
	eventID, _ := eventRes.LastInsertId()

	t.Run("success update auto_fetch", func(t *testing.T) {
		payload := map[string]any{
			"auto_fetch": true,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/admin/events/%d", eventID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var updated Event
		err = json.NewDecoder(resp.Body).Decode(&updated)
		require.NoError(t, err)
		assert.True(t, updated.AutoFetch)
	})

	t.Run("update auto_fetch to false", func(t *testing.T) {
		payload := map[string]any{
			"auto_fetch": false,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/admin/events/%d", eventID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var updated Event
		err = json.NewDecoder(resp.Body).Decode(&updated)
		require.NoError(t, err)
		assert.False(t, updated.AutoFetch)
	})

	t.Run("not found", func(t *testing.T) {
		payload := map[string]any{
			"auto_fetch": true,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, "/v1/admin/events/9999", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	})

	t.Run("invalid event id", func(t *testing.T) {
		payload := map[string]any{
			"auto_fetch": true,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, "/v1/admin/events/not-a-number", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		assert.Equal(t, "invalid event id", bodyString(t, resp.Body))
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/admin/events/%d", eventID), bytes.NewReader([]byte("not json")))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})
}

func TestController_PatchUpdateEvent_withLocationGroup(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	groupRes, err := squirrel.Insert("location_groups").
		RunWith(testDB).
		Columns("name").
		Values("Nursery").
		ExecContext(t.Context())
	require.NoError(t, err)
	groupID, _ := groupRes.LastInsertId()

	eventRes, err := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id", "auto_fetch").
		Values("Sunday Service", "pc_evt_1", 0).
		ExecContext(t.Context())
	require.NoError(t, err)
	eventID, _ := eventRes.LastInsertId()

	t.Run("set location_group_id", func(t *testing.T) {
		payload := map[string]any{
			"location_group_id": groupID,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/admin/events/%d", eventID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var updated Event
		err = json.NewDecoder(resp.Body).Decode(&updated)
		require.NoError(t, err)
		require.NotNil(t, updated.LocationGroupID)
		assert.Equal(t, groupID, *updated.LocationGroupID)
	})

	t.Run("clear location_group_id with null", func(t *testing.T) {
		payload := map[string]any{
			"location_group_id": nil,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/admin/events/%d", eventID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var updated Event
		err = json.NewDecoder(resp.Body).Decode(&updated)
		require.NoError(t, err)
		assert.Nil(t, updated.LocationGroupID)
	})

	t.Run("invalid location_group_id", func(t *testing.T) {
		payload := map[string]any{
			"location_group_id": "not-a-number",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/admin/events/%d", eventID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})
}

func TestController_DeleteEvent(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	eventRes, err := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id").
		Values("Sunday Service", "pc_evt_1").
		ExecContext(t.Context())
	require.NoError(t, err)
	eventID, _ := eventRes.LastInsertId()

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/v1/admin/events/%d", eventID), nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/v1/admin/events/%d", 99999), nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	})

	t.Run("invalid id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/v1/admin/events/not-a-number", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		assert.Equal(t, "invalid event id", bodyString(t, resp.Body))
	})
}

// bodyString reads a response body as a string for error-message assertions.
func bodyString(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(b)
}
