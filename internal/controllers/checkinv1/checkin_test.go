package checkinv1

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"kids-checkin/internal/db"
	"kids-checkin/internal/repo/checkin"
	"kids-checkin/internal/repo/manualcheckin"

	"github.com/Masterminds/squirrel"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
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

	t.Run("menu links rendered server-side", func(t *testing.T) {
		t.Setenv("ENVIRONMENT", "production")
		html := request(t)
		// setupAuthedApp sets role=admin, so admin and logout links appear
		assert.Contains(t, html, `id="guest-checkin-link"`)
		assert.Contains(t, html, `id="admin-link"`)
		assert.Contains(t, html, `id="logout-link"`)
		assert.NotContains(t, html, `id="login-link"`)
		assert.NotContains(t, html, "<!-- kebab-menu-links -->")
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
			name: "zero id treated as no filter",
			url:  "/test?location_group_id=0",
			assert: func(t *testing.T, f checkin.Filter, err error) {
				require.NoError(t, err)
				assert.Empty(t, f.LocationGroupIDs)
			},
		},
		{
			name: "malformed unrelated param does not drop repeated ids",
			url:  "/test?a=1;2&location_group_id=1&location_group_id=2",
			assert: func(t *testing.T, f checkin.Filter, err error) {
				require.NoError(t, err)
				assert.ElementsMatch(t, []int64{1, 2}, f.LocationGroupIDs)
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

func TestController_Checkouts_location_group_filtering(t *testing.T) {
	app, store := setupAuthedApp()
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err)
	t.Cleanup(cleanup)
	c := NewController(testDB, store)
	c.RegisterRoutes(app)

	_, err = testDB.Exec(`INSERT INTO location_groups (id, name) VALUES (10, 'grp10'), (20, 'grp20'), (30, 'grp30')`)
	require.NoError(t, err)

	locID := func(name, group string) int64 {
		t.Helper()
		var groupSQL any
		if group == "" {
			groupSQL = nil
		} else {
			groupSQL = group
		}
		res, err := squirrel.Insert("locations").
			RunWith(testDB).
			Columns("name", "planning_center_id", "event_id", "location_group_id").
			Values(name, name, 1, groupSQL).
			ExecContext(t.Context())
		require.NoError(t, err)
		id, _ := res.LastInsertId()
		return id
	}
	loc10 := locID("plc_10", "10")
	loc20 := locID("plc_20", "20")
	locNull := locID("plc_null", "")
	locExcluded := locID("plc_excluded", "30")

	checkinRepo := checkin.NewRepo(testDB)
	for _, l := range []struct {
		pc string
		id int64
	}{{"plc_10", loc10}, {"plc_20", loc20}, {"plc_null", locNull}, {"plc_excluded", locExcluded}} {
		_, err = checkinRepo.CreateCheckin(t.Context(), checkin.Checkin{
			PlanningCenterID: l.pc,
			LocationID:       l.id,
			FirstName:        "f",
			LastName:         "l",
			SecurityCode:     "S1",
			CheckedOutAt:     time.Now().UTC().Add(-time.Minute),
		})
		require.NoError(t, err)
	}

	manualRepo := manualcheckin.NewRepo(testDB)
	_, err = manualRepo.CreateManualCheckin(t.Context(), manualcheckin.ManualCheckin{
		FirstName:    "manual",
		LastName:     "kid",
		CheckedOutAt: time.Now().UTC().Add(-time.Minute),
	})
	require.NoError(t, err)

	idsFor := func(t *testing.T, url string) (map[string]bool, CheckoutsResponse) {
		t.Helper()
		req := httptest.NewRequest("GET", url, nil)
		req.Header.Set("Accept", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		var payload CheckoutsResponse
		err = json.NewDecoder(resp.Body).Decode(&payload)
		require.NoError(t, err)
		ids := map[string]bool{}
		for _, c := range payload.Checkins {
			ids[c.PlanningCenterID] = true
		}
		return ids, payload
	}

	t.Run("multiple groups plus unassigned returns union", func(t *testing.T) {
		ids, payload := idsFor(t, "/v1/checkins/checkouts?location_group_id=10&location_group_id=20&include_unassigned=1")
		assert.True(t, ids["plc_10"])
		assert.True(t, ids["plc_20"])
		assert.True(t, ids["plc_null"])
		assert.False(t, ids["plc_excluded"])
		assert.Len(t, payload.ManualCheckins, 0)
	})

	t.Run("multiple groups without unassigned returns assigned only", func(t *testing.T) {
		ids, _ := idsFor(t, "/v1/checkins/checkouts?location_group_id=10&location_group_id=20")
		assert.True(t, ids["plc_10"])
		assert.True(t, ids["plc_20"])
		assert.False(t, ids["plc_null"])
		assert.False(t, ids["plc_excluded"])
	})

	t.Run("no group filter still returns manual checkins", func(t *testing.T) {
		_, payload := idsFor(t, "/v1/checkins/checkouts")
		assert.Len(t, payload.ManualCheckins, 1)
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

type errSessionStore struct{}

func (e *errSessionStore) RegisterType(i any) {}
func (e *errSessionStore) Get(c *fiber.Ctx) (*session.Session, error) {
	return nil, errors.New("session error")
}
func (e *errSessionStore) Reset() error           { return nil }
func (e *errSessionStore) Delete(id string) error { return nil }

func TestCheckouts_SessionErrorReturns500(t *testing.T) {
	app := fiber.New()
	app.Use(recover.New())
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err)
	t.Cleanup(cleanup)
	controller := NewController(testDB, &errSessionStore{})
	controller.RegisterRoutes(app)

	t.Run("json checkouts returns 500 on session error via auth panic", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/checkins/checkouts", nil)
		req.Header.Set("Accept", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
	})

	t.Run("html checkouts returns 500 on session error via auth panic", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/checkins/checkouts", nil)
		req.Header.Set("Accept", "text/html")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
	})
}
