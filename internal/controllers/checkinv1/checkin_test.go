package checkinv1

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"kids-checkin/internal/db"
	"kids-checkin/internal/repo/checkin"

	"github.com/Masterminds/squirrel"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestController_PatchCheckedOutConfirmed(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	_, err = squirrel.Delete("checkins").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)
	_, err = squirrel.Delete("locations").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)

	res, err := squirrel.Insert("locations").
		RunWith(testDB).
		Columns("name", "planning_center_id", "event_id").
		Values("location1", "plloc_1234", 1).
		ExecContext(t.Context())
	require.NoError(t, err)
	locationID, _ := res.LastInsertId()

	checkinRepo := checkin.NewRepo(testDB)
	created, err := checkinRepo.CreateCheckin(t.Context(), checkin.Checkin{
		PlanningCenterID: "plc_1234",
		LocationID:       locationID,
		FirstName:        "sam",
		LastName:         "alpha",
		SecurityCode:     "ABC123",
		CheckedOutAt:     time.Date(2022, 1, 1, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/v1/checkins/plc_1234/checked_out_confirmed", bytes.NewBufferString("{\"confirmed\":true}"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		var payload Checkin
		err = json.NewDecoder(resp.Body).Decode(&payload)
		require.NoError(t, err)
		require.NotNil(t, payload.CheckedOutConfirmedAt)
		assert.WithinDuration(t, time.Now().UTC(), *payload.CheckedOutConfirmedAt, 2*time.Second)

		checkins, err := checkinRepo.ListCheckins(t.Context(), checkin.Filter{PlanningCenterID: created.PlanningCenterID})
		require.NoError(t, err)
		require.Len(t, checkins, 1)
		assert.WithinDuration(t, time.Now().UTC(), checkins[0].CheckedOutConfirmedAt, 2*time.Second)
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/v1/checkins/missing/checked_out_confirmed", bytes.NewBufferString("{\"confirmed\":true}"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	})

	t.Run("missing content type", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/v1/checkins/plc_1234/checked_out_confirmed", bytes.NewBufferString("{\"confirmed\":true}"))
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnsupportedMediaType, resp.StatusCode)
	})

	t.Run("unsupported content type", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/v1/checkins/plc_1234/checked_out_confirmed", bytes.NewBufferString("{\"confirmed\":true}"))
		req.Header.Set("Content-Type", "text/plain")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnsupportedMediaType, resp.StatusCode)
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/v1/checkins/plc_1234/checked_out_confirmed", bytes.NewBufferString("{bad"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("missing confirmed field", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/v1/checkins/plc_1234/checked_out_confirmed", bytes.NewBufferString("{}"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})
}

func TestController_CheckoutsWeb_PreviewTag(t *testing.T) {
	app, store := setupAuthedApp()
	controller := NewController(nil, store)
	controller.RegisterRoutes(app)

	request := func(t *testing.T) string {
		req := httptest.NewRequest("GET", "/v1/checkins/checkouts", nil)
		req.Header.Set("Accept", "text/html")
		resp, err := app.Test(req)
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return string(body)
	}

	t.Run("dev injects preview script", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "dev")
		assert.Contains(t, request(t), "preview.js")
	})

	t.Run("non-dev omits preview script", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "production")
		assert.NotContains(t, request(t), "preview.js")
	})
}

func Test_buildFilter_location_group_id(t *testing.T) {
	cases := []struct {
		name   string
		url    string
		assert func(t *testing.T, f checkin.Filter, err error)
	}{
		{
			name: "single id",
			url:  "/test?location_group_id=5",
			assert: func(t *testing.T, f checkin.Filter, err error) {
				require.NoError(t, err)
				require.Len(t, f.LocationGroupIDs, 1)
				assert.Equal(t, int64(5), f.LocationGroupIDs[0])
				assert.Equal(t, int64(5), f.LocationGroupID)
			},
		},
		{
			name: "repeated",
			url:  "/test?location_group_id=1&location_group_id=2",
			assert: func(t *testing.T, f checkin.Filter, err error) {
				require.NoError(t, err)
				assert.ElementsMatch(t, []int64{1, 2}, f.LocationGroupIDs)
			},
		},
		{
			name: "comma",
			url:  "/test?location_group_id=1,2",
			assert: func(t *testing.T, f checkin.Filter, err error) {
				require.NoError(t, err)
				assert.ElementsMatch(t, []int64{1, 2}, f.LocationGroupIDs)
			},
		},
		{
			name: "comma with spaces",
			url:  "/test?location_group_id=1,%202",
			assert: func(t *testing.T, f checkin.Filter, err error) {
				require.NoError(t, err)
				assert.ElementsMatch(t, []int64{1, 2}, f.LocationGroupIDs)
			},
		},
		{
			name: "mixed repeated and comma",
			url:  "/test?location_group_id=1,2&location_group_id=3",
			assert: func(t *testing.T, f checkin.Filter, err error) {
				require.NoError(t, err)
				assert.ElementsMatch(t, []int64{1, 2, 3}, f.LocationGroupIDs)
			},
		},
		{
			name: "include_unassigned=1",
			url:  "/test?include_unassigned=1",
			assert: func(t *testing.T, f checkin.Filter, err error) {
				require.NoError(t, err)
				assert.True(t, f.IncludeUnassigned)
			},
		},
		{
			name: "include_unassigned=true",
			url:  "/test?include_unassigned=true",
			assert: func(t *testing.T, f checkin.Filter, err error) {
				require.NoError(t, err)
				assert.True(t, f.IncludeUnassigned)
			},
		},
		{
			name: "include_unassigned with filter",
			url:  "/test?location_group_id=10&include_unassigned=1",
			assert: func(t *testing.T, f checkin.Filter, err error) {
				require.NoError(t, err)
				assert.True(t, f.IncludeUnassigned)
				assert.ElementsMatch(t, []int64{10}, f.LocationGroupIDs)
				assert.Equal(t, int64(10), f.LocationGroupID)
			},
		},
		{
			name: "parse error non-numeric",
			url:  "/test?location_group_id=abc",
			assert: func(t *testing.T, f checkin.Filter, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "cannot parse location_group_id")
			},
		},
		{
			name: "parse error in comma list",
			url:  "/test?location_group_id=1,abc",
			assert: func(t *testing.T, f checkin.Filter, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "cannot parse location_group_id")
			},
		},
		{
			name: "negative id",
			url:  "/test?location_group_id=-1",
			assert: func(t *testing.T, f checkin.Filter, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "location_group_id must be positive")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			var got checkin.Filter
			var gotErr error
			app.Get("/test", func(c *fiber.Ctx) error {
				got, gotErr = buildFilter(c)
				return c.SendStatus(fiber.StatusOK)
			})
			req := httptest.NewRequest("GET", tc.url, nil)
			_, err := app.Test(req)
			require.NoError(t, err)
			tc.assert(t, got, gotErr)
		})
	}
}

func TestController_Checkouts_filter_validation(t *testing.T) {
	app, store := setupAuthedApp()
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err)
	t.Cleanup(cleanup)
	c := NewController(testDB, store)
	c.RegisterRoutes(app)

	t.Run("invalid location_group_id returns 400", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/checkins/checkouts?location_group_id=abc", nil)
		req.Header.Set("Accept", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("negative location_group_id returns 400", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/checkins/checkouts?location_group_id=-1", nil)
		req.Header.Set("Accept", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
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
