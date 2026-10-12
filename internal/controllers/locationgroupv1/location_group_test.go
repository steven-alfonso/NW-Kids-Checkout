package locationgroupv1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"kids-checkin/internal/db"
	"kids-checkin/internal/repo/location"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestController_PostCreateLocationGroup(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	t.Run("creates a group", func(t *testing.T) {
		payload := map[string]any{"name": "K-3"}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/v1/admin/location_groups", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusCreated, resp.StatusCode)

		var created LocationGroup
		err = json.NewDecoder(resp.Body).Decode(&created)
		require.NoError(t, err)
		assert.NotZero(t, created.ID)
		assert.Equal(t, "K-3", created.Name)
	})

	t.Run("empty name returns bad request", func(t *testing.T) {
		payload := map[string]any{"name": "   "}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPost, "/v1/admin/location_groups", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})
}

func TestController_PatchUpdateLocationGroup(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	locRepo := location.NewRepo(testDB)
	created, err := locRepo.CreateLocationGroup(t.Context(), location.LocationGroup{Name: "Original"})
	require.NoError(t, err)

	t.Run("renames a group", func(t *testing.T) {
		payload := map[string]any{"name": "Renamed"}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, "/v1/admin/location_groups/"+itoa(created.ID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)

		groups, err := locRepo.ListLocationGroups(t.Context(), location.LocationGroupFilter{})
		require.NoError(t, err)
		require.Len(t, groups, 1)
		assert.Equal(t, "Renamed", groups[0].Name)
	})

	t.Run("unknown id returns not found", func(t *testing.T) {
		payload := map[string]any{"name": "Whatever"}
		body, _ := json.Marshal(payload)

		req := httptest.NewRequest(http.MethodPatch, "/v1/admin/location_groups/999999", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	})
}

func TestController_DeleteLocationGroup(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	locRepo := location.NewRepo(testDB)

	t.Run("unused group is deleted", func(t *testing.T) {
		created, err := locRepo.CreateLocationGroup(t.Context(), location.LocationGroup{Name: "Unused"})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodDelete, "/v1/admin/location_groups/"+itoa(created.ID), nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)

		groups, err := locRepo.ListLocationGroups(t.Context(), location.LocationGroupFilter{})
		require.NoError(t, err)
		assert.Empty(t, groups)
	})

	t.Run("in-use group is blocked", func(t *testing.T) {
		created, err := locRepo.CreateLocationGroup(t.Context(), location.LocationGroup{Name: "In Use"})
		require.NoError(t, err)

		_, err = locRepo.CreateLocation(t.Context(), location.Location{
			PlanningCenterID: "pcloc_group_in_use",
			EventID:          1,
			LocationGroupID:  &created.ID,
			Name:             "Room",
		})
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodDelete, "/v1/admin/location_groups/"+itoa(created.ID), nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("unknown group returns not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/v1/admin/location_groups/999999", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
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

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
