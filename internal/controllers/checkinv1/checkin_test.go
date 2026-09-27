package checkinv1

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"kids-checkin/internal/controllers/middleware"
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
		req := httptest.NewRequest(http.MethodPatch, "/v1/checkins/plc_1234/checked_out_confirmed", bytes.NewBufferString("{\"confirmed\":true}"))
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
		req := httptest.NewRequest(http.MethodPatch, "/v1/checkins/missing/checked_out_confirmed", bytes.NewBufferString("{\"confirmed\":true}"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	})

	t.Run("missing content type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/v1/checkins/plc_1234/checked_out_confirmed", bytes.NewBufferString("{\"confirmed\":true}"))
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnsupportedMediaType, resp.StatusCode)
	})

	t.Run("unsupported content type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/v1/checkins/plc_1234/checked_out_confirmed", bytes.NewBufferString("{\"confirmed\":true}"))
		req.Header.Set("Content-Type", "text/plain")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnsupportedMediaType, resp.StatusCode)
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/v1/checkins/plc_1234/checked_out_confirmed", bytes.NewBufferString("{bad"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("missing confirmed field", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/v1/checkins/plc_1234/checked_out_confirmed", bytes.NewBufferString("{}"))
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
		req := httptest.NewRequest(http.MethodGet, "/v1/checkins/checkouts", nil)
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
		assert.Contains(t, html, `id="manual-checkins-link"`)
		assert.NotContains(t, html, `id="guest-checkin-link"`)
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
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			_, err := app.Test(req)
			require.NoError(t, err)
			tc.assert(t, got, gotErr)
		})
	}
}

func TestController_Checkouts_conditionalPolling(t *testing.T) {
	app, store := setupAuthedApp()
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err)
	t.Cleanup(cleanup)
	c := NewController(testDB, store)
	c.RegisterRoutes(app)

	_, err = squirrel.Delete("checkins").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)

	locRes, err := squirrel.Insert("locations").
		RunWith(testDB).
		Columns("name", "planning_center_id", "event_id").
		Values("nursery", "plloc_cond", 1).
		ExecContext(t.Context())
	require.NoError(t, err)
	locationID, err := locRes.LastInsertId()
	require.NoError(t, err)

	addChild := func(t *testing.T, pcID, first, last string) {
		t.Helper()
		_, err := squirrel.Insert("checkins").
			RunWith(testDB).
			Columns("planning_center_id", "location_id", "first_name", "last_name", "security_code", "checked_out_at").
			Values(pcID, locationID, first, last, "ABC123", time.Now().UTC()).
			ExecContext(t.Context())
		require.NoError(t, err)
	}

	poll := func(t *testing.T, ifNoneMatch string) *http.Response {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/v1/checkins/checkouts?limit=100", nil)
		req.Header.Set("Accept", "application/json")
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		resp, err := app.Test(req)
		require.NoError(t, err)
		return resp
	}

	t.Run("200 carries a validator and tells the client to revalidate", func(t *testing.T) {
		resp := poll(t, "")
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.NotEmpty(t, resp.Header.Get("ETag"),
			"the 3s poll can only revalidate if the response carries an ETag")
		assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"),
			"no-cache stores the body but forces revalidation; no-store would defeat the ETag")
		assert.Contains(t, resp.Header.Get("Vary"), fiber.HeaderAcceptEncoding,
			"the body varies once compression is applied")
		assert.Contains(t, resp.Header.Get("Vary"), fiber.HeaderAccept,
			"this route content-negotiates on Accept, so that is a selection variable too")
	})

	t.Run("matching validator returns 304 with no body", func(t *testing.T) {
		first := poll(t, "")
		etag := first.Header.Get("ETag")
		require.NotEmpty(t, etag)
		first.Body.Close()

		second := poll(t, etag)
		defer second.Body.Close()
		assert.Equal(t, fiber.StatusNotModified, second.StatusCode)

		body, err := io.ReadAll(second.Body)
		require.NoError(t, err)
		assert.Empty(t, body, "a 304 must not resend the payload")
		assert.Equal(t, etag, second.Header.Get("ETag"),
			"the validator should be echoed so the client keeps caching it")
	})

	t.Run("changed data returns 200 with a new validator", func(t *testing.T) {
		before := poll(t, "")
		beforeETag := before.Header.Get("ETag")
		before.Body.Close()

		addChild(t, "plc_new", "jamie", "zeta")

		after := poll(t, beforeETag)
		defer after.Body.Close()
		assert.Equal(t, fiber.StatusOK, after.StatusCode,
			"a newly arrived child must not be hidden behind a stale validator")
		assert.NotEqual(t, beforeETag, after.Header.Get("ETag"))

		var payload CheckoutsResponse
		require.NoError(t, json.NewDecoder(after.Body).Decode(&payload))
		assert.Len(t, payload.Checkins, 1)
		assert.Equal(t, "jamie", payload.Checkins[0].FirstName)
	})

	t.Run("200 declares a JSON content type", func(t *testing.T) {
		resp := poll(t, "")
		defer resp.Body.Close()
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Equal(t, fiber.MIMEApplicationJSON, resp.Header.Get("Content-Type"),
			"the poll is a JSON API; anything sniffing the content type depends on this")
	})

	t.Run("HTML branch is unaffected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/checkins/checkouts", nil)
		req.Header.Set("Accept", "text/html")
		resp, err := app.Test(req)
		require.NoError(t, err)
		t.Cleanup(func() { resp.Body.Close() })
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Empty(t, resp.Header.Get("ETag"),
			"only the poll payload is validated; the page itself is already cache-busted by asset hashing")
	})
}

// TestController_Checkouts_compressionIntegration runs the real poll route
// behind the real compress middleware. Both pieces set Vary and both touch the
// response body, so they are worth exercising together rather than only in
// isolation: this is the only test that would notice if the compress
// middleware stopped being registered in server.go.
func TestController_Checkouts_compressionIntegration(t *testing.T) {
	app, store := setupAuthedApp()
	app.Use(middleware.Compress())
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err)
	t.Cleanup(cleanup)
	NewController(testDB, store).RegisterRoutes(app)

	// Enough children to clear fasthttp's 200-byte compression floor.
	locRes, err := squirrel.Insert("locations").
		RunWith(testDB).
		Columns("name", "planning_center_id", "event_id").
		Values("nursery", "plloc_ci", 1).
		ExecContext(t.Context())
	require.NoError(t, err)
	locationID, err := locRes.LastInsertId()
	require.NoError(t, err)
	first := []string{"sam", "jamie", "robin", "dana", "kai", "morgan", "ellis", "quinn"}
	last := []string{"alpha", "zeta", "beta", "gamma", "delta", "omega", "kappa", "sigma"}
	for i := range 30 {
		_, err := squirrel.Insert("checkins").
			RunWith(testDB).
			Columns("planning_center_id", "location_id", "first_name", "last_name", "security_code", "checked_out_at").
			Values(fmt.Sprintf("plc_ci_%02d", i), locationID, first[i%len(first)], last[i%len(last)], "ABC123", time.Now().UTC()).
			ExecContext(t.Context())
		require.NoError(t, err)
	}

	poll := func(t *testing.T, ifNoneMatch string) *http.Response {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/v1/checkins/checkouts?limit=100", nil)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Accept-Encoding", "gzip")
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		resp, err := app.Test(req)
		require.NoError(t, err)
		return resp
	}

	t.Run("poll is compressed and still identifies itself as JSON", func(t *testing.T) {
		resp := poll(t, "")
		defer resp.Body.Close()
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		assert.Equal(t, "gzip", resp.Header.Get("Content-Encoding"),
			"the poll is the largest recurring transfer; it must be compressed")
		assert.Equal(t, fiber.MIMEApplicationJSON, resp.Header.Get("Content-Type"))

		// app.Test reads the response with http.ReadResponse, which does no
		// decompression, so this is the byte count that crosses the wire.
		compressed, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		zr, err := gzip.NewReader(bytes.NewReader(compressed))
		require.NoError(t, err)
		decoded, err := io.ReadAll(zr)
		require.NoError(t, err)
		assert.Contains(t, string(decoded), "sam")

		var payload CheckoutsResponse
		require.NoError(t, json.Unmarshal(decoded, &payload))
		assert.Len(t, payload.Checkins, 30)
	})

	t.Run("Vary is not duplicated by the compress middleware", func(t *testing.T) {
		resp := poll(t, "")
		defer resp.Body.Close()
		assert.Equal(t, []string{"Accept-Encoding, Accept"}, resp.Header.Values("Vary"),
			"the handler sets Vary and fasthttp also sets it; it must not be listed twice")
	})

	t.Run("revalidation through the compress middleware returns an empty 304", func(t *testing.T) {
		first := poll(t, "")
		etag := first.Header.Get("ETag")
		require.NotEmpty(t, etag)
		first.Body.Close()

		second := poll(t, etag)
		defer second.Body.Close()
		require.Equal(t, fiber.StatusNotModified, second.StatusCode)
		assert.Empty(t, second.Header.Get("Content-Encoding"),
			"a bodyless 304 must not claim to be encoded")
		body, err := io.ReadAll(second.Body)
		require.NoError(t, err)
		assert.Empty(t, body)
	})
}

func Test_etagMatches(t *testing.T) {
	const etag = `W/"abc123"`

	tests := []struct {
		name        string
		ifNoneMatch string
		want        bool
	}{
		{"absent", "", false},
		{"exact weak match", `W/"abc123"`, true},
		{"strong form of the same opaque tag", `"abc123"`, true},
		{"one of several", `"other", W/"abc123"`, true},
		{"several, match last", `"a", "b", W/"abc123"`, true},
		{"wildcard", "*", true},
		{"wildcard with padding", " * ", true},
		{"padding around the tag", `  W/"abc123"  `, true},
		{"trailing comma", `W/"abc123",`, true},
		{"different tag", `W/"other"`, false},
		{"prefix of the tag", `W/"abc12"`, false},
		{"tag with trailing junk", `"abc123"junk`, false},
		{"unquoted", `abc123`, false},
		{"quoted wildcard", `"*"`, false},
		{"lowercase weak prefix is a different token", `w/"abc123"`, false},
		{"uppercase hex does not match", `W/"ABC123"`, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, etagMatches(tc.ifNoneMatch, etag))
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
		req := httptest.NewRequest(http.MethodGet, "/v1/checkins/checkouts?location_group_id=abc", nil)
		req.Header.Set("Accept", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("negative location_group_id returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/checkins/checkouts?location_group_id=-1", nil)
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
		req := httptest.NewRequest(http.MethodGet, url, nil)
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
		assert.Len(t, payload.ManualCheckins, 1)
	})

	t.Run("multiple groups without unassigned returns assigned only", func(t *testing.T) {
		ids, payload := idsFor(t, "/v1/checkins/checkouts?location_group_id=10&location_group_id=20")
		assert.True(t, ids["plc_10"])
		assert.True(t, ids["plc_20"])
		assert.False(t, ids["plc_null"])
		assert.False(t, ids["plc_excluded"])
		assert.Len(t, payload.ManualCheckins, 1)
	})

	t.Run("no group filter still returns manual checkins", func(t *testing.T) {
		_, payload := idsFor(t, "/v1/checkins/checkouts")
		assert.Len(t, payload.ManualCheckins, 1)
	})

	t.Run("empty location filter hides manual checkins", func(t *testing.T) {
		_, payload := idsFor(t, "/v1/checkins/checkouts?location_group_id=")
		assert.Len(t, payload.ManualCheckins, 0)
	})

	// A populated group filter and include_unassigned both take the same
	// "filter applies to checkins only" branch, so assert them together.
	t.Run("checkin-only filters leave manual checkins visible", func(t *testing.T) {
		_, payload := idsFor(t, "/v1/checkins/checkouts?location_group_id=10")
		assert.Len(t, payload.ManualCheckins, 1)

		_, payload = idsFor(t, "/v1/checkins/checkouts?include_unassigned=1")
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

// A session store that errors returns a nil *session.Session. AuthRequired
// discards that error (sess, _ := sessionStore.Get(c)) and then calls
// sess.Get, so the request dies in the middleware and recover.New turns the
// panic into a 500. This asserts the app does not crash the process on a
// session-store failure; the handler's own 500 branch is never reached.
// TODO(auth): AuthRequired should handle the error from sessionStore.Get and
// return a 500 itself, at which point this should assert that body instead.
func TestCheckouts_SessionStoreFailureDoesNotCrash(t *testing.T) {
	app := fiber.New()
	app.Use(recover.New())
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err)
	t.Cleanup(cleanup)
	controller := NewController(testDB, &errSessionStore{})
	controller.RegisterRoutes(app)

	req := httptest.NewRequest(http.MethodGet, "/v1/checkins/checkouts", nil)
	req.Header.Set("Accept", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
}
