package guestcheckinv1

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"kids-checkin/internal/db"
	"kids-checkin/internal/repo/guestsubmission"
	"kids-checkin/internal/repo/manualcheckin"

	"github.com/Masterminds/squirrel"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAuthedApp(t *testing.T, role string) (*fiber.App, *session.Store, *sql.DB) {
	app := fiber.New()
	store := session.New()

	testDB, cleanup, err := db.PrepareTestDB()
	if err != nil {
		panic(err)
	}
	t.Cleanup(cleanup)

	app.Use(func(c *fiber.Ctx) error {
		sess, _ := store.Get(c)
		sess.Set("authenticated", true)
		sess.Set("role", role)
		if err := sess.Save(); err != nil {
			return err
		}
		return c.Next()
	})
	NewController(testDB, store).RegisterRoutes(app)
	return app, store, testDB
}

func wipeSubmissionTables(t *testing.T, testDB *sql.DB) {
	t.Helper()
	for _, table := range []string{"guest_submissions", "children", "parents", "manual_checkins"} {
		_, err := squirrel.Delete(table).RunWith(testDB).ExecContext(t.Context())
		require.NoError(t, err)
	}
}

func TestController_CreateSubmission(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "")
	wipeSubmissionTables(t, testDB)

	payload := map[string]any{
		"parent":   map[string]any{"first_name": "John", "last_name": "Smith", "phone": "555-1234", "email": "john@example.com"},
		"children": []map[string]any{{"first_name": "Timmy", "last_name": "Smith", "dob": "2020-01-01", "grade": "1st"}},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/v1/checkins/guest-submissions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusCreated, resp.StatusCode)

	var created Submission
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	assert.NotEmpty(t, created.PublicID)
	assert.Equal(t, "pending", created.Status)
	assert.Equal(t, "john@example.com", created.Parent.Email)
	assert.Len(t, created.Children, 1)
	assert.Equal(t, "1st", created.Children[0].Grade)

	var parentCount int
	require.NoError(t, testDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM parents").Scan(&parentCount))
	assert.Equal(t, 1, parentCount)
}

func TestController_CreateSubmissionContentType(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "")
	wipeSubmissionTables(t, testDB)

	payload := map[string]any{
		"parent":   map[string]any{"first_name": "John", "last_name": "Smith", "phone": "555-1234", "email": "john@example.com"},
		"children": []map[string]any{{"first_name": "Timmy", "last_name": "Smith", "dob": "2020-01-01", "grade": "1st"}},
	}
	body, _ := json.Marshal(payload)

	t.Run("wrong content type returns 415", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/v1/checkins/guest-submissions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "text/plain")
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusUnsupportedMediaType, resp.StatusCode)
	})

	t.Run("missing content type returns 415", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/v1/checkins/guest-submissions", bytes.NewReader(body))
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusUnsupportedMediaType, resp.StatusCode)
	})

	t.Run("charset suffix still 201", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/v1/checkins/guest-submissions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusCreated, resp.StatusCode)
	})
}

func TestController_CreateSubmissionRequiresPhoneOrEmail(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "")
	wipeSubmissionTables(t, testDB)

	tests := []struct {
		name  string
		phone string
		email string
	}{
		{"phone only", "5551234", ""},
		{"email only", "", "john@example.com"},
		{"both", "5551234", "john@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := map[string]any{
				"parent":   map[string]any{"first_name": "A", "last_name": "B", "phone": tt.phone, "email": tt.email},
				"children": []map[string]any{{"first_name": "C", "last_name": "D", "dob": "2020-01-01", "grade": "1st"}},
			}
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest("POST", "/v1/checkins/guest-submissions", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, _ := app.Test(req)
			require.Equal(t, fiber.StatusCreated, resp.StatusCode)

			var created Submission
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
			assert.Equal(t, tt.phone, created.Parent.Phone)
			assert.Equal(t, tt.email, created.Parent.Email)
		})
	}
}

func TestController_CreateSubmissionValidation(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "")
	wipeSubmissionTables(t, testDB)

	tests := []struct {
		name    string
		payload map[string]interface{}
	}{
		{"empty children", map[string]interface{}{
			"parent":   map[string]interface{}{"first_name": "A", "last_name": "B", "phone": "1234567", "email": "a@b.com"},
			"children": []map[string]interface{}{},
		}},
		{"missing phone and email", map[string]interface{}{
			"parent":   map[string]interface{}{"first_name": "A", "last_name": "B", "phone": "", "email": ""},
			"children": []map[string]interface{}{{"first_name": "C", "last_name": "D", "dob": "2020-01-01", "grade": "1st"}},
		}},
		{"future dob", map[string]interface{}{
			"parent":   map[string]interface{}{"first_name": "A", "last_name": "B", "phone": "1234567", "email": "a@b.com"},
			"children": []map[string]interface{}{{"first_name": "C", "last_name": "D", "dob": "2999-01-01", "grade": "1st"}},
		}},
		{"bad phone", map[string]interface{}{
			"parent":   map[string]interface{}{"first_name": "A", "last_name": "B", "phone": "12", "email": "a@b.com"},
			"children": []map[string]interface{}{{"first_name": "C", "last_name": "D", "dob": "2020-01-01", "grade": "1st"}},
		}},
		{"whitespace-only parent names", map[string]interface{}{
			"parent":   map[string]interface{}{"first_name": "   ", "last_name": "   ", "phone": "1234567", "email": "a@b.com"},
			"children": []map[string]interface{}{{"first_name": "C", "last_name": "D", "dob": "2020-01-01", "grade": "1st"}},
		}},
		{"whitespace-only child name", map[string]interface{}{
			"parent":   map[string]interface{}{"first_name": "A", "last_name": "B", "phone": "1234567", "email": "a@b.com"},
			"children": []map[string]interface{}{{"first_name": " ", "last_name": "\t", "dob": "2020-01-01", "grade": "1st"}},
		}},
		{"more than 10 children", map[string]interface{}{
			"parent": map[string]interface{}{"first_name": "A", "last_name": "B", "phone": "1234567", "email": "a@b.com"},
			"children": func() []map[string]interface{} {
				children := make([]map[string]interface{}, 0, 11)
				for i := 0; i < 11; i++ {
					children = append(children, map[string]interface{}{"first_name": "C", "last_name": "D", "dob": "2020-01-01", "grade": "1st"})
				}
				return children
			}(),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.payload)
			req := httptest.NewRequest("POST", "/v1/checkins/guest-submissions", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, _ := app.Test(req)
			require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		})
	}
}

func TestController_ListSubmissionsNamesOnly(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "")
	wipeSubmissionTables(t, testDB)

	repo := guestsubmission.NewRepo(testDB)
	_, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
		FirstName: "John", LastName: "Smith", Phone: "555-1234", Email: "john@example.com",
	}, []guestsubmission.Child{{FirstName: "Timmy", LastName: "Smith", DOB: "2020-01-01", Grade: "1st"}})
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/v1/checkins/guest-submissions", nil)
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var list []SubmissionSummary
	require.NoError(t, json.Unmarshal(raw, &list))
	require.Len(t, list, 1)
	assert.Equal(t, "pending", list[0].Status)
	assert.Equal(t, "John", list[0].Parent.FirstName)
	assert.NotContains(t, string(raw), "email")
	assert.NotContains(t, string(raw), "grade")
}

func TestController_AdminListFullDetail(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "admin")
	wipeSubmissionTables(t, testDB)

	repo := guestsubmission.NewRepo(testDB)
	_, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
		FirstName: "John", LastName: "Smith", Phone: "555", Email: "j@e.com",
	}, []guestsubmission.Child{{FirstName: "Timmy", LastName: "Smith", DOB: "2020-01-01", Grade: "1st"}})
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/v1/admin/guest-submissions", nil)
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var page SubmissionPage
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
	require.Len(t, page.Items, 1)
	assert.Equal(t, 1, page.Page)
	assert.Equal(t, adminGuestPageSize, page.PageSize)
	assert.Equal(t, 1, page.Total)
	assert.Equal(t, 1, page.TotalPages)
	assert.Equal(t, "555", page.Items[0].Parent.Phone)
	assert.Equal(t, "1st", page.Items[0].Children[0].Grade)
}

func TestController_AdminListPaginated(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "admin")
	wipeSubmissionTables(t, testDB)

	repo := guestsubmission.NewRepo(testDB)
	now := time.Now().UTC()
	publicIDs := make([]string, 0, 11)
	for i := range 11 {
		sub, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
			FirstName: "Page", LastName: fmt.Sprintf("P%d", i), Phone: "555", Email: "p@test.com",
		}, []guestsubmission.Child{{FirstName: "Kid", LastName: fmt.Sprintf("P%d", i), DOB: "2020-01-01", Grade: "1st"}})
		require.NoError(t, err)
		publicIDs = append(publicIDs, sub.PublicID)
		createdAt := now.Add(time.Duration(i) * time.Minute)
		_, err = testDB.ExecContext(t.Context(), `UPDATE guest_submissions SET created_at = ? WHERE public_id = ?`, createdAt, sub.PublicID)
		require.NoError(t, err)
	}

	t.Run("page 1 returns the first 10 newest first", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/admin/guest-submissions?page=1", nil)
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		var page SubmissionPage
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
		assert.Equal(t, 1, page.Page)
		assert.Equal(t, 10, page.PageSize)
		assert.Equal(t, 11, page.Total)
		assert.Equal(t, 2, page.TotalPages)
		require.Len(t, page.Items, 10)
		assert.Equal(t, publicIDs[10], page.Items[0].PublicID)
	})

	t.Run("page 2 returns the remainder", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/admin/guest-submissions?page=2", nil)
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		var page SubmissionPage
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
		assert.Equal(t, 2, page.Page)
		require.Len(t, page.Items, 1)
		assert.Equal(t, publicIDs[0], page.Items[0].PublicID)
	})

	t.Run("page beyond the end returns empty items", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/admin/guest-submissions?page=5", nil)
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		var page SubmissionPage
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
		assert.Equal(t, 5, page.Page)
		assert.Empty(t, page.Items)
		assert.Equal(t, 11, page.Total)
	})

	t.Run("invalid page errors", func(t *testing.T) {
		for _, p := range []string{"0", "-1", "abc"} {
			req := httptest.NewRequest("GET", "/v1/admin/guest-submissions?page="+p, nil)
			resp, _ := app.Test(req)
			assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode, "page=%s", p)
		}
	})
}

func TestController_StaffCannotMarkEntered(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "")
	wipeSubmissionTables(t, testDB)

	repo := guestsubmission.NewRepo(testDB)
	sub, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
		FirstName: "A", LastName: "B", Phone: "1234567", Email: "a@b.com",
	}, []guestsubmission.Child{{FirstName: "C", LastName: "D", DOB: "2020-01-01", Grade: "1st"}})
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]interface{}{"status": "entered"})
	req := httptest.NewRequest("PATCH", fmt.Sprintf("/v1/checkins/guest-submissions/%s/status", sub.PublicID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestController_AdminCanMarkEntered(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "admin")
	wipeSubmissionTables(t, testDB)

	repo := guestsubmission.NewRepo(testDB)
	sub, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
		FirstName: "A", LastName: "B", Phone: "1234567", Email: "a@b.com",
	}, []guestsubmission.Child{{FirstName: "C", LastName: "D", DOB: "2020-01-01", Grade: "1st"}})
	require.NoError(t, err)

	approveBody, _ := json.Marshal(map[string]interface{}{"status": "approved"})
	approveReq := httptest.NewRequest("PATCH", fmt.Sprintf("/v1/checkins/guest-submissions/%s/status", sub.PublicID), bytes.NewReader(approveBody))
	approveReq.Header.Set("Content-Type", "application/json")
	approveResp, _ := app.Test(approveReq)
	require.Equal(t, fiber.StatusOK, approveResp.StatusCode)

	body, _ := json.Marshal(map[string]interface{}{"status": "entered"})
	req := httptest.NewRequest("PATCH", fmt.Sprintf("/v1/checkins/guest-submissions/%s/status", sub.PublicID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
}

func TestController_AdminCanEnterFromPending(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "admin")
	wipeSubmissionTables(t, testDB)

	repo := guestsubmission.NewRepo(testDB)
	sub, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
		FirstName: "A", LastName: "B", Phone: "1234567", Email: "a@b.com",
	}, []guestsubmission.Child{{FirstName: "C", LastName: "D", DOB: "2020-01-01", Grade: "1st"}})
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]interface{}{"status": "entered"})
	req := httptest.NewRequest("PATCH", fmt.Sprintf("/v1/checkins/guest-submissions/%s/status", sub.PublicID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var out struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "entered", out.Status)

	var enteredAtSet int
	require.NoError(t, testDB.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM guest_submissions WHERE public_id = ? AND entered_at IS NOT NULL AND approved_at IS NULL", sub.PublicID).
		Scan(&enteredAtSet))
	assert.Equal(t, 1, enteredAtSet)
}

func TestController_ApproveCreatesManualCheckins(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "")
	wipeSubmissionTables(t, testDB)

	repo := guestsubmission.NewRepo(testDB)
	sub, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
		FirstName: "A", LastName: "B", Phone: "1234567", Email: "a@b.com",
	}, []guestsubmission.Child{
		{FirstName: "C", LastName: "D", DOB: "2020-01-01", Grade: "1st"},
		{FirstName: "E", LastName: "F", DOB: "2021-02-02", Grade: "1st"},
	})
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]interface{}{"status": "approved"})
	req := httptest.NewRequest("PATCH", fmt.Sprintf("/v1/checkins/guest-submissions/%s/status", sub.PublicID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	rows, err := manualcheckin.NewRepo(testDB).ListManualCheckins(t.Context(), manualcheckin.Filter{})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.NotZero(t, rows[0].ChildID)
}

func TestController_PatchSubmissionStatusNamesOnly(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "")
	wipeSubmissionTables(t, testDB)

	repo := guestsubmission.NewRepo(testDB)
	sub, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
		FirstName: "John", LastName: "Smith", Phone: "555-1234", Email: "john@example.com",
	}, []guestsubmission.Child{{FirstName: "Timmy", LastName: "Smith", DOB: "2020-01-01", Grade: "1st"}})
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]interface{}{"status": "approved"})
	req := httptest.NewRequest("PATCH", fmt.Sprintf("/v1/checkins/guest-submissions/%s/status", sub.PublicID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "phone")
	assert.NotContains(t, string(raw), "email")
	assert.NotContains(t, string(raw), "dob")
	assert.NotContains(t, string(raw), "grade")
}

func TestController_CreateCheckinsFromEntered(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "")
	wipeSubmissionTables(t, testDB)

	repo := guestsubmission.NewRepo(testDB)
	sub, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
		FirstName: "A", LastName: "B", Phone: "1234567", Email: "a@b.com",
	}, []guestsubmission.Child{{FirstName: "C", LastName: "D", DOB: "2020-01-01", Grade: "1st"}})
	require.NoError(t, err)
	require.NoError(t, repo.UpdateSubmissionStatus(t.Context(), sub.PublicID, guestsubmission.StatusEntered, time.Now().UTC()))

	req := httptest.NewRequest("POST", fmt.Sprintf("/v1/checkins/guest-submissions/%s/checkins", sub.PublicID), nil)
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "phone")
	assert.NotContains(t, string(raw), "email")
	assert.NotContains(t, string(raw), "dob")

	var count int
	require.NoError(t, testDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM manual_checkins").Scan(&count))
	assert.Equal(t, 1, count)
}

func TestController_CreateCheckinsAlreadyExist(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "")
	wipeSubmissionTables(t, testDB)

	repo := guestsubmission.NewRepo(testDB)
	sub, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
		FirstName: "A", LastName: "B", Phone: "1234567", Email: "a@b.com",
	}, []guestsubmission.Child{{FirstName: "C", LastName: "D", DOB: "2020-01-01", Grade: "1st"}})
	require.NoError(t, err)
	require.NoError(t, repo.UpdateSubmissionStatus(t.Context(), sub.PublicID, guestsubmission.StatusEntered, time.Now().UTC()))
	require.NoError(t, repo.CreateManualCheckins(t.Context(), sub.PublicID))

	req := httptest.NewRequest("POST", fmt.Sprintf("/v1/checkins/guest-submissions/%s/checkins", sub.PublicID), nil)
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var count int
	require.NoError(t, testDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM manual_checkins").Scan(&count))
	assert.Equal(t, 1, count)
}

func TestController_CreateCheckinsNotFound(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "")
	wipeSubmissionTables(t, testDB)

	req := httptest.NewRequest("POST", "/v1/checkins/guest-submissions/does-not-exist/checkins", nil)
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusNotFound, resp.StatusCode)
}

func TestController_PatchSubmissionNotFound(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "")
	wipeSubmissionTables(t, testDB)

	body, _ := json.Marshal(map[string]interface{}{"status": "approved"})
	req := httptest.NewRequest("PATCH", "/v1/checkins/guest-submissions/does-not-exist/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusNotFound, resp.StatusCode)
}

type conflictApproveRepo struct {
	guestsubmission.Repo
	submission guestsubmission.Submission
}

func (r *conflictApproveRepo) ListSubmissions(ctx context.Context, filter guestsubmission.Filter) ([]guestsubmission.Submission, error) {
	return []guestsubmission.Submission{r.submission}, nil
}

func (r *conflictApproveRepo) ApproveSubmission(ctx context.Context, publicID string, now time.Time) error {
	return guestsubmission.ErrConflict
}

func TestController_ApproveConflictReturnsBadRequest(t *testing.T) {
	app := fiber.New()
	store := session.New()
	app.Use(func(c *fiber.Ctx) error {
		sess, _ := store.Get(c)
		sess.Set("authenticated", true)
		sess.Set("role", "")
		if err := sess.Save(); err != nil {
			return err
		}
		return c.Next()
	})
	ctrl := Controller{
		submissionRepo: &conflictApproveRepo{submission: guestsubmission.Submission{
			PublicID: "abc123",
			Status:   guestsubmission.StatusPending,
		}},
		sessionStore: store,
	}
	ctrl.RegisterRoutes(app)

	body, _ := json.Marshal(map[string]interface{}{"status": "approved"})
	req := httptest.NewRequest("PATCH", "/v1/checkins/guest-submissions/abc123/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestController_CreateCheckinsInvalidStatusReturnsBadRequest(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "")
	wipeSubmissionTables(t, testDB)

	repo := guestsubmission.NewRepo(testDB)

	t.Run("pending returns 400 not 500", func(t *testing.T) {
		sub, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
			FirstName: "A", LastName: "B", Phone: "1234567", Email: "a@b.com",
		}, []guestsubmission.Child{{FirstName: "C", LastName: "D", DOB: "2020-01-01", Grade: "1st"}})
		require.NoError(t, err)

		req := httptest.NewRequest("POST", fmt.Sprintf("/v1/checkins/guest-submissions/%s/checkins", sub.PublicID), nil)
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		assert.NotEqual(t, fiber.StatusInternalServerError, resp.StatusCode)
	})

	t.Run("rejected returns 400 not 500", func(t *testing.T) {
		wipeSubmissionTables(t, testDB)
		sub, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
			FirstName: "E", LastName: "F", Phone: "1234567", Email: "e@f.com",
		}, []guestsubmission.Child{{FirstName: "G", LastName: "H", DOB: "2020-01-01", Grade: "1st"}})
		require.NoError(t, err)
		require.NoError(t, repo.UpdateSubmissionStatus(t.Context(), sub.PublicID, guestsubmission.StatusRejected, time.Now().UTC()))

		req := httptest.NewRequest("POST", fmt.Sprintf("/v1/checkins/guest-submissions/%s/checkins", sub.PublicID), nil)
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
		assert.NotEqual(t, fiber.StatusInternalServerError, resp.StatusCode)
	})
}

func TestController_InvalidStatusReturnsBadRequest(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "")
	wipeSubmissionTables(t, testDB)

	req := httptest.NewRequest("GET", "/v1/checkins/guest-submissions?status=bogus", nil)
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assert.NotEqual(t, fiber.StatusInternalServerError, resp.StatusCode)

	// Test admin endpoint with same DB but admin role
	adminApp := fiber.New()
	adminStore := session.New()
	adminApp.Use(func(c *fiber.Ctx) error {
		sess, _ := adminStore.Get(c)
		sess.Set("authenticated", true)
		sess.Set("role", "admin")
		if err := sess.Save(); err != nil {
			return err
		}
		return c.Next()
	})
	NewController(testDB, adminStore).RegisterRoutes(adminApp)

	adminReq := httptest.NewRequest("GET", "/v1/admin/guest-submissions?status=bogus", nil)
	adminResp, _ := adminApp.Test(adminReq)
	require.Equal(t, fiber.StatusBadRequest, adminResp.StatusCode)
	assert.NotEqual(t, fiber.StatusInternalServerError, adminResp.StatusCode)
}

func TestController_RequiresAuth(t *testing.T) {
	app := fiber.New()
	store := session.New()
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err)
	t.Cleanup(cleanup)
	NewController(testDB, store).RegisterRoutes(app)

	// Create a submission for routes that need a public_id; unauthenticated should 302 before checking existence
	repo := guestsubmission.NewRepo(testDB)
	sub, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
		FirstName: "A", LastName: "B", Phone: "1234567", Email: "a@b.com",
	}, []guestsubmission.Child{{FirstName: "C", LastName: "D", DOB: "2020-01-01", Grade: "1st"}})
	require.NoError(t, err)

	t.Run("unauthenticated GET guest-submissions redirects to login", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/checkins/guest-submissions", nil)
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusFound, resp.StatusCode)
		require.Contains(t, resp.Header.Get("Location"), "/login")
	})

	t.Run("unauthenticated PATCH guest-submissions redirects to login", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{"status": "approved"})
		req := httptest.NewRequest("PATCH", fmt.Sprintf("/v1/checkins/guest-submissions/%s/status", sub.PublicID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusFound, resp.StatusCode)
		require.Contains(t, resp.Header.Get("Location"), "/login")
	})

	t.Run("unauthenticated POST checkins redirects to login", func(t *testing.T) {
		req := httptest.NewRequest("POST", fmt.Sprintf("/v1/checkins/guest-submissions/%s/checkins", sub.PublicID), nil)
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusFound, resp.StatusCode)
		require.Contains(t, resp.Header.Get("Location"), "/login")
	})

	t.Run("unauthenticated GET admin guest-submissions redirects to login", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/admin/guest-submissions", nil)
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusFound, resp.StatusCode)
		require.Contains(t, resp.Header.Get("Location"), "/login")
	})

	t.Run("unauthenticated GET admin guest-entries redirects to login", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/admin/guest-entries", nil)
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusFound, resp.StatusCode)
		require.Contains(t, resp.Header.Get("Location"), "/login")
	})

	t.Run("unauthenticated POST guest-submissions is public 201", func(t *testing.T) {
		payload := map[string]any{
			"parent":   map[string]any{"first_name": "John", "last_name": "Smith", "phone": "555-1234", "email": "john@example.com"},
			"children": []map[string]any{{"first_name": "Timmy", "last_name": "Smith", "dob": "2020-01-01", "grade": "1st"}},
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/v1/checkins/guest-submissions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusCreated, resp.StatusCode)
	})

	t.Run("unauthenticated GET checkin is public 200", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/checkin", nil)
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	})
}

func TestController_UnauthenticatedPostIsPublic(t *testing.T) {
	app := fiber.New()
	store := session.New()
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err)
	t.Cleanup(cleanup)
	NewController(testDB, store).RegisterRoutes(app)

	payload := map[string]any{
		"parent":   map[string]any{"first_name": "John", "last_name": "Smith", "phone": "555-1234", "email": "john@example.com"},
		"children": []map[string]any{{"first_name": "Timmy", "last_name": "Smith", "dob": "2020-01-01", "grade": "1st"}},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/v1/checkins/guest-submissions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusCreated, resp.StatusCode)
}

func TestController_AdminRoutesRequireAdminRole(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "") // non-admin role
	_ = testDB

	t.Run("non-admin GET admin guest-submissions returns 403", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/admin/guest-submissions", nil)
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusForbidden, resp.StatusCode)
	})

	t.Run("non-admin GET admin guest-entries returns 403", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/admin/guest-entries", nil)
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusForbidden, resp.StatusCode)
	})

	t.Run("non-admin GET guest-submissions allowed", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/v1/checkins/guest-submissions", nil)
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
	})

	t.Run("non-admin PATCH guest-submissions allowed for staff transition", func(t *testing.T) {
		repo := guestsubmission.NewRepo(testDB)
		sub, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
			FirstName: "A", LastName: "B", Phone: "1234567", Email: "a@b.com",
		}, []guestsubmission.Child{{FirstName: "C", LastName: "D", DOB: "2020-01-01", Grade: "1st"}})
		require.NoError(t, err)
		body, _ := json.Marshal(map[string]interface{}{"status": "approved"})
		req := httptest.NewRequest("PATCH", fmt.Sprintf("/v1/checkins/guest-submissions/%s/status", sub.PublicID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
	})

	t.Run("non-admin POST checkins allowed when entered", func(t *testing.T) {
		repo := guestsubmission.NewRepo(testDB)
		sub, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
			FirstName: "A", LastName: "B", Phone: "1234567", Email: "a@b.com",
		}, []guestsubmission.Child{{FirstName: "C", LastName: "D", DOB: "2020-01-01", Grade: "1st"}})
		require.NoError(t, err)
		require.NoError(t, repo.UpdateSubmissionStatus(t.Context(), sub.PublicID, guestsubmission.StatusEntered, time.Now().UTC()))
		req := httptest.NewRequest("POST", fmt.Sprintf("/v1/checkins/guest-submissions/%s/checkins", sub.PublicID), nil)
		resp, _ := app.Test(req)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
	})
}

func TestController_AuthEnforcementTableDriven(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err)
	t.Cleanup(cleanup)

	// Create a submission for ID-dependent routes
	repo := guestsubmission.NewRepo(testDB)
	sub, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
		FirstName: "Auth", LastName: "Test", Phone: "1234567", Email: "auth@test.com",
	}, []guestsubmission.Child{{FirstName: "Kid", LastName: "Auth", DOB: "2020-01-01", Grade: "1st"}})
	require.NoError(t, err)

	unauthApp := fiber.New()
	unauthStore := session.New()
	NewController(testDB, unauthStore).RegisterRoutes(unauthApp)

	staffApp2 := fiber.New()
	staffStore := session.New()
	staffApp2.Use(func(c *fiber.Ctx) error {
		sess, _ := staffStore.Get(c)
		sess.Set("authenticated", true)
		sess.Set("role", "")
		if err := sess.Save(); err != nil {
			return err
		}
		return c.Next()
	})
	NewController(testDB, staffStore).RegisterRoutes(staffApp2)

	type routeCase struct {
		name           string
		method         string
		path           string
		body           []byte
		contentType    string
		expectedStatus int
	}

	unauthCases := []routeCase{
		{"GET staff list", "GET", "/v1/checkins/guest-submissions", nil, "", fiber.StatusFound},
		{"PATCH staff status", "PATCH", fmt.Sprintf("/v1/checkins/guest-submissions/%s/status", sub.PublicID), []byte(`{"status":"approved"}`), "application/json", fiber.StatusFound},
		{"POST staff checkins", "POST", fmt.Sprintf("/v1/checkins/guest-submissions/%s/checkins", sub.PublicID), nil, "", fiber.StatusFound},
		{"GET admin list", "GET", "/v1/admin/guest-submissions", nil, "", fiber.StatusFound},
		{"GET admin page", "GET", "/admin/guest-entries", nil, "", fiber.StatusFound},
	}
	for _, tc := range unauthCases {
		t.Run("unauth "+tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			resp, _ := unauthApp.Test(req)
			require.Equal(t, tc.expectedStatus, resp.StatusCode, tc.name)
			require.Contains(t, resp.Header.Get("Location"), "/login")
		})
	}

	t.Run("unauth public POST still 201", func(t *testing.T) {
		payload := map[string]any{
			"parent":   map[string]any{"first_name": "John", "last_name": "Smith", "phone": "555-1234", "email": "john@example.com"},
			"children": []map[string]any{{"first_name": "Timmy", "last_name": "Smith", "dob": "2020-01-01", "grade": "1st"}},
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/v1/checkins/guest-submissions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := unauthApp.Test(req)
		require.Equal(t, fiber.StatusCreated, resp.StatusCode)
	})

	t.Run("unauth public GET checkin still 200", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/checkin", nil)
		resp, _ := unauthApp.Test(req)
		require.Equal(t, fiber.StatusOK, resp.StatusCode)
	})

	nonAdminCases := []routeCase{
		{"GET admin list", "GET", "/v1/admin/guest-submissions", nil, "", fiber.StatusForbidden},
		{"GET admin page", "GET", "/admin/guest-entries", nil, "", fiber.StatusForbidden},
	}
	for _, tc := range nonAdminCases {
		t.Run("non-admin "+tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			resp, _ := staffApp2.Test(req)
			require.Equal(t, tc.expectedStatus, resp.StatusCode, tc.name)
		})
	}
}
