package manualcheckinv1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kids-checkin/internal/db"
	"kids-checkin/internal/repo/manualcheckin"

	"github.com/Masterminds/squirrel"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
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

		req := httptest.NewRequest(http.MethodPost, "/v1/checkins/manual-checkins", bytes.NewBuffer(body))
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

		req := httptest.NewRequest(http.MethodPost, "/v1/checkins/manual-checkins", bytes.NewBuffer(body))
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
		req := httptest.NewRequest(http.MethodPost, "/v1/checkins/manual-checkins", bytes.NewBufferString("{\"last_name\":\"zeta\"}"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("missing last name", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/checkins/manual-checkins", bytes.NewBufferString("{\"first_name\":\"jane\"}"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("whitespace first name maps to 400 via ErrInvalidManualCheckin", func(t *testing.T) {
		payload := map[string]any{"first_name": "   ", "last_name": "zeta"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/v1/checkins/manual-checkins", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("whitespace last name maps to 400 via ErrInvalidManualCheckin", func(t *testing.T) {
		payload := map[string]any{"first_name": "jane", "last_name": "   "}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/v1/checkins/manual-checkins", bytes.NewBuffer(body))
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
	req := httptest.NewRequest(http.MethodGet, "/v1/checkins/manual-checkins?checked_out_after="+checkedOutAfter, nil)
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

func TestController_GetManualCheckins_queryCap(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	_, err = squirrel.Delete("manual_checkins").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)

	// buildManualFilter is unexported but this package can exercise it directly.
	filterApp := fiber.New()
	filterApp.Get("/filter", func(c *fiber.Ctx) error {
		f, err := buildManualFilter(c)
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"limit": f.Limit})
	})
	decodeLimit := func(t *testing.T, target string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		resp, err := filterApp.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		defer resp.Body.Close()

		var payload struct {
			Limit int `json:"limit"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		return payload.Limit
	}

	// manual-checkins.js sends no limit on every poll; an omitted limit must
	// get a bound rather than the whole table.
	t.Run("omitted limit defaults to 100", func(t *testing.T) {
		assert.Equal(t, 100, decodeLimit(t, "/filter"))
	})

	t.Run("explicit limit overrides the default", func(t *testing.T) {
		assert.Equal(t, 5, decodeLimit(t, "/filter?limit=5"))
	})

	t.Run("limit at the maximum is accepted", func(t *testing.T) {
		assert.Equal(t, 1000, decodeLimit(t, "/filter?limit=1000"))
	})

	pollManual := func(t *testing.T, target string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Accept", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.StatusCode, string(body)
	}

	for _, target := range []string{
		"/v1/checkins/manual-checkins?limit=0",
		"/v1/checkins/manual-checkins?limit=-1",
		"/v1/checkins/manual-checkins?limit=abc",
		"/v1/checkins/manual-checkins?limit=1001",
	} {
		t.Run("rejects "+target, func(t *testing.T) {
			status, _ := pollManual(t, target)
			assert.Equal(t, fiber.StatusBadRequest, status)
		})
	}

	t.Run("checked_out_after within 24 hours returns 200", func(t *testing.T) {
		status, _ := pollManual(t, "/v1/checkins/manual-checkins?checked_out_after=-31m")
		assert.Equal(t, fiber.StatusOK, status)
	})

	t.Run("checked_out_after at the 24 hour boundary returns 200", func(t *testing.T) {
		status, _ := pollManual(t, "/v1/checkins/manual-checkins?checked_out_after=-24h")
		assert.Equal(t, fiber.StatusOK, status)
	})

	t.Run("future checked_out_after returns 200", func(t *testing.T) {
		status, _ := pollManual(t, "/v1/checkins/manual-checkins?checked_out_after=1h")
		assert.Equal(t, fiber.StatusOK, status)
	})

	t.Run("checked_out_after older than 24 hours returns 400", func(t *testing.T) {
		status, body := pollManual(t, "/v1/checkins/manual-checkins?checked_out_after=-100000h")
		assert.Equal(t, fiber.StatusBadRequest, status)
		assert.Contains(t, body, "checked_out_after must be within the last 24 hours")
	})

	t.Run("far-past RFC3339 checked_out_after returns 400", func(t *testing.T) {
		farPast := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
		status, body := pollManual(t, "/v1/checkins/manual-checkins?checked_out_after="+farPast)
		assert.Equal(t, fiber.StatusBadRequest, status)
		assert.Contains(t, body, "checked_out_after must be within the last 24 hours")
	})
}

func TestController_GetManualCheckins_createdSortKeepsNewestPendingRows(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	_, err = squirrel.Delete("manual_checkins").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)

	// Seed more pending rows than the cap. They have a NULL checked_out_at, so
	// ordering by checked_out_at sorts them last. Fill the 100 slots with
	// recent checked-out rows so that ordering loses every pending row, which
	// is the failure the created-order branch prevents.
	const pendingCount = 101
	base := time.Now().UTC().Add(-3 * time.Hour)
	for i := 1; i <= pendingCount; i++ {
		_, err := squirrel.Insert("manual_checkins").
			Columns("created_at", "public_id", "first_name", "last_name").
			Values(base.Add(time.Duration(i)*time.Second),
				fmt.Sprintf("pending-%03d", i),
				"Pending",
				fmt.Sprintf("Row%03d", i)).
			RunWith(testDB).
			ExecContext(t.Context())
		require.NoError(t, err)
	}

	// Older than every pending row by created_at, and recent enough by
	// checked_out_at to match the -12h filter.
	const checkedOutCount = 100
	for i := 1; i <= checkedOutCount; i++ {
		_, err := squirrel.Insert("manual_checkins").
			Columns("created_at", "public_id", "first_name", "last_name", "checked_out_at").
			Values(base.Add(-time.Hour).Add(time.Duration(i)*time.Second),
				fmt.Sprintf("checked-out-%03d", i),
				"Recent",
				fmt.Sprintf("Checkout%03d", i),
				time.Now().UTC().Add(-time.Duration(i)*time.Second)).
			RunWith(testDB).
			ExecContext(t.Context())
		require.NoError(t, err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/v1/checkins/manual-checkins?include_unchecked=true&sort=created&checked_out_after=-12h", nil)
	req.Header.Set("Accept", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var payload []Checkin
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.NotEmpty(t, payload)
	assert.LessOrEqual(t, len(payload), 100, "the response must stay within the default cap")

	foundNewestPending := false
	for _, row := range payload {
		if row.PublicID == "pending-101" {
			foundNewestPending = true
			break
		}
	}
	assert.True(t, foundNewestPending,
		"the newest pending check-in must survive the limit when created order is requested")
}

func TestController_GetManualCheckins_conditionalPolling(t *testing.T) {
	app, store := setupAuthedApp()

	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	controller := NewController(testDB, store)
	controller.RegisterRoutes(app)

	_, err = squirrel.Delete("manual_checkins").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)

	manualRepo := manualcheckin.NewRepo(testDB)
	create := func(t *testing.T, publicID string) {
		t.Helper()
		_, err := manualRepo.CreateManualCheckin(t.Context(), manualcheckin.ManualCheckin{
			PublicID:     publicID,
			FirstName:    "sam",
			LastName:     "alpha",
			CheckedOutAt: time.Now().UTC().Add(-time.Minute),
		})
		require.NoError(t, err)
	}
	create(t, "manual-cache-1")

	poll := func(t *testing.T, ifNoneMatch string) *http.Response {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/v1/checkins/manual-checkins", nil)
		req.Header.Set("Accept", "application/json")
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		resp, err := app.Test(req)
		require.NoError(t, err)
		return resp
	}

	t.Run("200 carries a validator but forbids storing the body", func(t *testing.T) {
		resp := poll(t, "")
		defer resp.Body.Close()
		require.Equal(t, fiber.StatusOK, resp.StatusCode)

		assert.NotEmpty(t, resp.Header.Get("ETag"),
			"a conditional client gets a validator to revalidate with")
		assert.Equal(t, "private, no-store", resp.Header.Get("Cache-Control"),
			"the browser must not retain PII; the page keeps the validator in memory instead")
		assert.Equal(t, fiber.MIMEApplicationJSON, resp.Header.Get("Content-Type"),
			"the poll is a JSON API; anything sniffing the content type depends on this")
		assert.True(t, varyHas(resp.Header.Get("Vary"), fiber.HeaderAcceptEncoding),
			"the body varies once compression is applied")
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
			"the validator should be echoed so the client can keep using it")
		assert.Equal(t, "private, no-store", second.Header.Get("Cache-Control"),
			"a 304 still updates cache metadata, so it must repeat the no-store directive")
		assert.True(t, varyHas(second.Header.Get("Vary"), fiber.HeaderAcceptEncoding),
			"a 304 varies on the encoding it selected")
	})

	t.Run("changed data returns 200 with a new validator", func(t *testing.T) {
		before := poll(t, "")
		beforeETag := before.Header.Get("ETag")
		before.Body.Close()

		create(t, "manual-cache-2")

		after := poll(t, beforeETag)
		defer after.Body.Close()
		assert.Equal(t, fiber.StatusOK, after.StatusCode,
			"a newly arrived manual check-in must not be hidden behind a stale validator")
		assert.NotEqual(t, beforeETag, after.Header.Get("ETag"))

		var payload []Checkin
		require.NoError(t, json.NewDecoder(after.Body).Decode(&payload))
		assert.Len(t, payload, 2)
	})

	t.Run("confirmed mutation returns 200 with a new validator", func(t *testing.T) {
		before := poll(t, "")
		beforeETag := before.Header.Get("ETag")
		require.NotEmpty(t, beforeETag)
		before.Body.Close()

		req := httptest.NewRequest(http.MethodPatch,
			"/v1/checkins/manual-checkins/manual-cache-1/checked_out_confirmed",
			bytes.NewBufferString(`{"confirmed":true}`))
		req.Header.Set("Content-Type", "application/json")
		patchResp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, fiber.StatusOK, patchResp.StatusCode)
		patchResp.Body.Close()

		after := poll(t, beforeETag)
		defer after.Body.Close()
		assert.Equal(t, fiber.StatusOK, after.StatusCode,
			"a confirmed manual check-in must not be hidden behind a stale validator")
		assert.NotEqual(t, beforeETag, after.Header.Get("ETag"))
	})
}

func varyHas(vary, token string) bool {
	for _, part := range strings.Split(vary, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
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
		req := httptest.NewRequest(http.MethodPatch, "/v1/checkins/manual-checkins/manual-public-1/checked_out_confirmed", bytes.NewBufferString("{\"confirmed\":true}"))
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
		req := httptest.NewRequest(http.MethodPatch, "/v1/checkins/manual-checkins/missing/checked_out_confirmed", bytes.NewBufferString("{\"confirmed\":true}"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	})

	t.Run("missing content type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/v1/checkins/manual-checkins/manual-public-1/checked_out_confirmed", bytes.NewBufferString("{\"confirmed\":true}"))
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnsupportedMediaType, resp.StatusCode)
	})

	t.Run("unsupported content type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/v1/checkins/manual-checkins/manual-public-1/checked_out_confirmed", bytes.NewBufferString("{\"confirmed\":true}"))
		req.Header.Set("Content-Type", "text/plain")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnsupportedMediaType, resp.StatusCode)
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/v1/checkins/manual-checkins/manual-public-1/checked_out_confirmed", bytes.NewBufferString("{bad"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	})

	t.Run("missing confirmed field", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/v1/checkins/manual-checkins/manual-public-1/checked_out_confirmed", bytes.NewBufferString("{}"))
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
		req := httptest.NewRequest(http.MethodPatch, "/v1/checkins/manual-checkins/"+created.PublicID+"/checked_out", body)
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
		req := httptest.NewRequest(http.MethodPatch, "/v1/checkins/manual-checkins/"+created.PublicID+"/checked_out", body)
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

func TestController_ManualCheckinsPage_ServerRenderedMenu(t *testing.T) {
	app, store := setupAuthedApp()
	controller := NewController(nil, store)
	controller.RegisterRoutes(app)

	req := httptest.NewRequest(http.MethodGet, "/manual-checkins", nil)
	req.Header.Set("Accept", "text/html")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	html := string(body)

	// setupAuthedApp sets role=admin, so admin and logout links are rendered.
	assert.Contains(t, html, `id="menu-button"`)
	assert.Contains(t, html, `id="kebab-menu"`)
	assert.NotContains(t, html, `id="guest-checkin-link"`)
	assert.Contains(t, html, `id="manual-checkins-link"`)
	assert.Contains(t, html, `id="admin-link"`)
	assert.Contains(t, html, `id="logout-link"`)
	assert.NotContains(t, html, `id="login-link"`)
	assert.NotContains(t, html, "<!-- kebab-menu-links -->")
}

func TestController_ManualCheckinsPage_cacheHeaders(t *testing.T) {
	app, store := setupAuthedApp()
	controller := NewController(nil, store)
	controller.RegisterRoutes(app)

	req := httptest.NewRequest(http.MethodGet, "/manual-checkins", nil)
	req.Header.Set("Accept", "text/html")
	resp, err := app.Test(req)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	assert.Equal(t, "private, no-store", resp.Header.Get("Cache-Control"),
		"the page is the authenticated route that loads the PII poll and must not be stored on a shared device")
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

// The /manual-checkins route sits behind authRequired, so a failing session
// store panics inside the middleware (AuthRequired discards the error from
// sessionStore.Get) and recover.New converts it to a 500. This asserts the app
// does not crash the process; ManualCheckinsPage's own session-error branch is
// not what answers here.
// TODO(auth): see TestCheckouts_SessionStoreFailureDoesNotCrash in checkinv1.
func TestManualCheckinsPage_SessionStoreFailureDoesNotCrash(t *testing.T) {
	app := fiber.New()
	app.Use(recover.New())
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err)
	t.Cleanup(cleanup)
	controller := NewController(testDB, &errSessionStore{})
	controller.RegisterRoutes(app)

	req := httptest.NewRequest(http.MethodGet, "/manual-checkins", nil)
	req.Header.Set("Accept", "text/html")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusInternalServerError, resp.StatusCode)
}
