package locationv1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"kids-checkin/internal/db"
	"kids-checkin/internal/repo/location"

	"github.com/Masterminds/squirrel"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestController_ListLocationsIncludesEventID(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	builder := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id")
	res, err := builder.Values("Evening Service", "pc_evt_33").ExecContext(t.Context())
	require.NoError(t, err)
	insertedID, _ := res.LastInsertId()

	locRepo := location.NewRepo(testDB)
	_, err = locRepo.CreateLocation(t.Context(), location.Location{
		PlanningCenterID: "pcloc_evt",
		EventID:          insertedID,
		Name:             "Event Room",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/v1/locations", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var payload []Location
	err = json.NewDecoder(resp.Body).Decode(&payload)
	require.NoError(t, err)
	require.Len(t, payload, 1)
	assert.Equal(t, insertedID, payload[0].EventID)
	assert.Equal(t, "Event Room", payload[0].Name)
	assert.Equal(t, "pcloc_evt", payload[0].PlanningCenterID)
}

func TestController_PatchUpdateLocation(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	locRepo := location.NewRepo(testDB)
	created, err := locRepo.CreateLocation(t.Context(), location.Location{
		PlanningCenterID: "pcloc_patch_1",
		EventID:          1,
		Name:             "Patch Test Room",
		LocationGroupID:  nil,
	})
	require.NoError(t, err)

	t.Run("ignores auto_fetch in payload", func(t *testing.T) {
		payload := map[string]any{
			"auto_fetch": true,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/locations/%d", created.ID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var updated map[string]any
		err = json.NewDecoder(resp.Body).Decode(&updated)
		require.NoError(t, err)
		_, hasAutoFetch := updated["auto_fetch"]
		assert.False(t, hasAutoFetch)
	})

	t.Run("not found", func(t *testing.T) {
		payload := map[string]any{}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, "/v1/locations/9999", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	})

	t.Run("invalid location id", func(t *testing.T) {
		payload := map[string]any{}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, "/v1/locations/not-a-number", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/locations/%d", created.ID), bytes.NewReader([]byte("not json")))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("set location_group_id", func(t *testing.T) {
		groupID := int64(42)
		payload := map[string]any{
			"location_group_id": groupID,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/locations/%d", created.ID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var updated Location
		err = json.NewDecoder(resp.Body).Decode(&updated)
		require.NoError(t, err)
		assert.Equal(t, int64(42), *updated.LocationGroupID)
	})

	t.Run("clear location_group_id with null", func(t *testing.T) {
		payload := map[string]any{
			"location_group_id": nil,
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/locations/%d", created.ID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var updated Location
		err = json.NewDecoder(resp.Body).Decode(&updated)
		require.NoError(t, err)
		assert.Nil(t, updated.LocationGroupID)
	})

	t.Run("invalid location_group_id", func(t *testing.T) {
		payload := map[string]any{
			"location_group_id": "not-a-number",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/v1/locations/%d", created.ID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})
}

func TestController_PostCreateLocation(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	t.Run("success", func(t *testing.T) {
		payload := map[string]any{
			"name":               "New Test Location",
			"planning_center_id": "pc_test_loc_123",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/v1/locations", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		locRepo := location.NewRepo(testDB)
		locations, err := locRepo.ListLocations(t.Context(), location.LocationFilter{})
		require.NoError(t, err)
		require.Len(t, locations, 1)
		assert.Equal(t, "New Test Location", locations[0].Name)
		assert.Equal(t, "pc_test_loc_123", locations[0].PlanningCenterID)
	})

	t.Run("missing name returns bad request", func(t *testing.T) {
		payload := map[string]any{
			"planning_center_id": "pc_test_loc_456",
		}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/v1/locations", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("invalid json returns bad request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/locations", bytes.NewReader([]byte("not json")))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})
}

func TestController_GetListLocations(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	locRepo := location.NewRepo(testDB)
	_, err = locRepo.CreateLocation(t.Context(), location.Location{
		PlanningCenterID: "filter_pc_1",
		Name:             "Filter Room 1",
	})
	require.NoError(t, err)
	_, err = locRepo.CreateLocation(t.Context(), location.Location{
		PlanningCenterID: "filter_pc_2",
		Name:             "Filter Room 2",
	})
	require.NoError(t, err)

	t.Run("filter by name", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/locations?name=Filter+Room+1", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var payload []Location
		err = json.NewDecoder(resp.Body).Decode(&payload)
		require.NoError(t, err)
		require.Len(t, payload, 1)
		assert.Equal(t, "Filter Room 1", payload[0].Name)
	})

	t.Run("filter by planning_center_id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/locations?planning_center_id=filter_pc_2", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var payload []Location
		err = json.NewDecoder(resp.Body).Decode(&payload)
		require.NoError(t, err)
		require.Len(t, payload, 1)
		assert.Equal(t, "filter_pc_2", payload[0].PlanningCenterID)
	})

	t.Run("no filter returns all", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/locations", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var payload []Location
		err = json.NewDecoder(resp.Body).Decode(&payload)
		require.NoError(t, err)
		require.Len(t, payload, 2)
	})

	t.Run("response contains no auto_fetch field", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/locations", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var payload []map[string]any
		err = json.NewDecoder(resp.Body).Decode(&payload)
		require.NoError(t, err)
		require.Len(t, payload, 2)
		for _, item := range payload {
			_, hasAutoFetch := item["auto_fetch"]
			assert.False(t, hasAutoFetch)
		}
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
