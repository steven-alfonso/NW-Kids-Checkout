package manualcheckinv1

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"kids-checkin/internal/db"
	"kids-checkin/internal/repo/manualcheckin"

	"github.com/Masterminds/squirrel"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestController_PostManualCheckin(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	_, err = squirrel.Delete("manual_checkins").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		payload := map[string]any{
			"public_id":          "manual-public-1",
			"first_name":         "jane",
			"last_name":          "zeta",
			"immediate_checkout": true,
		}
		body, err := json.Marshal(payload)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/v1/checkins/manual-checkins", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var response Checkin
		err = json.NewDecoder(resp.Body).Decode(&response)
		require.NoError(t, err)
		require.NotNil(t, response.CheckedOutAt)
		require.NotNil(t, response.CreatedAt)
		assert.Equal(t, "manual-public-1", response.PublicID)
		assert.Equal(t, "jane", response.FirstName)
		assert.Equal(t, "zeta", response.LastName)
		assert.Equal(t, "manual", response.Source)
		assert.WithinDuration(t, time.Now().UTC(), *response.CheckedOutAt, 2*time.Second)
		assert.WithinDuration(t, time.Now().UTC(), *response.CreatedAt, 2*time.Second)

		manualRepo := manualcheckin.NewRepo(testDB)
		manualCheckins, err := manualRepo.ListManualCheckins(t.Context(), manualcheckin.Filter{FirstName: "jane", LastName: "zeta"})
		require.NoError(t, err)
		require.Len(t, manualCheckins, 1)
		assert.Equal(t, "manual-public-1", manualCheckins[0].PublicID)
	})

	t.Run("not immediate", func(t *testing.T) {
		payload := map[string]any{
			"public_id":          "manual-public-2",
			"first_name":         "sam",
			"last_name":          "beta",
			"immediate_checkout": false,
		}
		body, err := json.Marshal(payload)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/v1/checkins/manual-checkins", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var response Checkin
		err = json.NewDecoder(resp.Body).Decode(&response)
		require.NoError(t, err)
		require.Nil(t, response.CheckedOutAt)
		assert.Equal(t, "manual-public-2", response.PublicID)
		assert.Equal(t, "sam", response.FirstName)
		assert.Equal(t, "beta", response.LastName)
		assert.Equal(t, "manual", response.Source)
	})

	t.Run("missing first name", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/v1/checkins/manual-checkins", bytes.NewBufferString("{\"last_name\":\"zeta\"}"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("missing last name", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/v1/checkins/manual-checkins", bytes.NewBufferString("{\"first_name\":\"jane\"}"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})
}

func TestController_GetManualCheckins(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	_, err = squirrel.Delete("manual_checkins").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)

	manualRepo := manualcheckin.NewRepo(testDB)
	now := time.Now().UTC()
	older := now.Add(-2 * time.Hour)
	newer := now.Add(-1 * time.Hour)

	_, err = manualRepo.CreateManualCheckin(t.Context(), manualcheckin.ManualCheckin{
		PublicID:     "manual-public-1",
		FirstName:    "sam",
		LastName:     "alpha",
		CheckedOutAt: older,
	})
	require.NoError(t, err)

	_, err = manualRepo.CreateManualCheckin(t.Context(), manualcheckin.ManualCheckin{
		PublicID:     "manual-public-2",
		FirstName:    "jane",
		LastName:     "zeta",
		CheckedOutAt: newer,
	})
	require.NoError(t, err)

	checkedOutAfter := now.Add(-3 * time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest("GET", "/v1/checkins/manual-checkins?checked_out_after="+checkedOutAfter, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var payload []Checkin
	err = json.NewDecoder(resp.Body).Decode(&payload)
	require.NoError(t, err)
	require.Len(t, payload, 2)

	require.NotNil(t, payload[0].CheckedOutAt)
	require.NotNil(t, payload[1].CheckedOutAt)
	assert.True(t, payload[0].CheckedOutAt.After(*payload[1].CheckedOutAt))
	assert.Equal(t, "manual-public-2", payload[0].PublicID)
	assert.Equal(t, "manual-public-1", payload[1].PublicID)
	assert.Equal(t, "manual", payload[0].Source)
	assert.Equal(t, "manual", payload[1].Source)
}

func TestController_PatchManualCheckedOutConfirmed(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	_, err = squirrel.Delete("manual_checkins").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)

	manualRepo := manualcheckin.NewRepo(testDB)
	created, err := manualRepo.CreateManualCheckin(t.Context(), manualcheckin.ManualCheckin{
		PublicID:     "manual-public-1",
		FirstName:    "sam",
		LastName:     "alpha",
		CheckedOutAt: time.Date(2022, 1, 1, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/v1/checkins/manual-checkins/manual-public-1/checked_out_confirmed", bytes.NewBufferString("{\"confirmed\":true}"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var payload Checkin
		err = json.NewDecoder(resp.Body).Decode(&payload)
		require.NoError(t, err)
		require.NotNil(t, payload.CheckedOutConfirmedAt)
		assert.Equal(t, created.PublicID, payload.PublicID)
		assert.WithinDuration(t, time.Now().UTC(), *payload.CheckedOutConfirmedAt, 2*time.Second)

		manualCheckins, err := manualRepo.ListManualCheckins(t.Context(), manualcheckin.Filter{PublicID: created.PublicID})
		require.NoError(t, err)
		require.Len(t, manualCheckins, 1)
		assert.WithinDuration(t, time.Now().UTC(), manualCheckins[0].CheckedOutConfirmedAt, 2*time.Second)
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/v1/checkins/manual-checkins/missing/checked_out_confirmed", bytes.NewBufferString("{\"confirmed\":true}"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	})

	t.Run("missing content type", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/v1/checkins/manual-checkins/manual-public-1/checked_out_confirmed", bytes.NewBufferString("{\"confirmed\":true}"))
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnsupportedMediaType, resp.StatusCode)
	})

	t.Run("unsupported content type", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/v1/checkins/manual-checkins/manual-public-1/checked_out_confirmed", bytes.NewBufferString("{\"confirmed\":true}"))
		req.Header.Set("Content-Type", "text/plain")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnsupportedMediaType, resp.StatusCode)
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/v1/checkins/manual-checkins/manual-public-1/checked_out_confirmed", bytes.NewBufferString("{bad"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("missing confirmed field", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/v1/checkins/manual-checkins/manual-public-1/checked_out_confirmed", bytes.NewBufferString("{}"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})
}

func TestController_PatchManualCheckedOut(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	_, err = squirrel.Delete("manual_checkins").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)

	manualRepo := manualcheckin.NewRepo(testDB)
	checkedOutAt := time.Now().UTC().Add(-30 * time.Minute)
	confirmedAt := time.Now().UTC().Add(-15 * time.Minute)

	created, err := manualRepo.CreateManualCheckin(t.Context(), manualcheckin.ManualCheckin{
		PublicID:              "manual-public-3",
		FirstName:             "lena",
		LastName:              "rivers",
		CheckedOutAt:          checkedOutAt,
		CheckedOutConfirmedAt: confirmedAt,
	})
	require.NoError(t, err)

	t.Run("clear checked out", func(t *testing.T) {
		body := bytes.NewBufferString("{\"checked_out\":false}")
		req := httptest.NewRequest("PATCH", "/v1/checkins/manual-checkins/"+created.PublicID+"/checked_out", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var response Checkin
		err = json.NewDecoder(resp.Body).Decode(&response)
		require.NoError(t, err)
		assert.Nil(t, response.CheckedOutAt)
		assert.Nil(t, response.CheckedOutConfirmedAt)
	})

	t.Run("set checked out", func(t *testing.T) {
		body := bytes.NewBufferString("{\"checked_out\":true}")
		req := httptest.NewRequest("PATCH", "/v1/checkins/manual-checkins/"+created.PublicID+"/checked_out", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var response Checkin
		err = json.NewDecoder(resp.Body).Decode(&response)
		require.NoError(t, err)
		require.NotNil(t, response.CheckedOutAt)
		assert.WithinDuration(t, time.Now().UTC(), *response.CheckedOutAt, 2*time.Second)
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
