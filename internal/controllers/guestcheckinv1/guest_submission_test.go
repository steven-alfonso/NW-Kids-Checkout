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
		"children": []map[string]any{{"first_name": "Timmy", "last_name": "Smith", "dob": "2020-01-01", "grade": "1st Grade"}},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/v1/checkins/guest-submissions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var created Submission
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	assert.NotEmpty(t, created.PublicID)
	assert.Equal(t, "pending", created.Status)
	assert.Equal(t, "john@example.com", created.Parent.Email)
	assert.Len(t, created.Children, 1)
	assert.Equal(t, "1st Grade", created.Children[0].Grade)

	var parentCount int
	require.NoError(t, testDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM parents").Scan(&parentCount))
	assert.Equal(t, 1, parentCount)
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
				"children": []map[string]any{{"first_name": "C", "last_name": "D", "dob": "2020-01-01", "grade": "k"}},
			}
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest("POST", "/v1/checkins/guest-submissions", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, _ := app.Test(req)
			require.Equal(t, fiber.StatusOK, resp.StatusCode)

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
			"children": []map[string]interface{}{{"first_name": "C", "last_name": "D", "dob": "2020-01-01", "grade": "k"}},
		}},
		{"future dob", map[string]interface{}{
			"parent":   map[string]interface{}{"first_name": "A", "last_name": "B", "phone": "1234567", "email": "a@b.com"},
			"children": []map[string]interface{}{{"first_name": "C", "last_name": "D", "dob": "2999-01-01", "grade": "k"}},
		}},
		{"bad phone", map[string]interface{}{
			"parent":   map[string]interface{}{"first_name": "A", "last_name": "B", "phone": "12", "email": "a@b.com"},
			"children": []map[string]interface{}{{"first_name": "C", "last_name": "D", "dob": "2020-01-01", "grade": "k"}},
		}},
		{"whitespace-only parent names", map[string]interface{}{
			"parent":   map[string]interface{}{"first_name": "   ", "last_name": "   ", "phone": "1234567", "email": "a@b.com"},
			"children": []map[string]interface{}{{"first_name": "C", "last_name": "D", "dob": "2020-01-01", "grade": "k"}},
		}},
		{"whitespace-only child name", map[string]interface{}{
			"parent":   map[string]interface{}{"first_name": "A", "last_name": "B", "phone": "1234567", "email": "a@b.com"},
			"children": []map[string]interface{}{{"first_name": " ", "last_name": "\t", "dob": "2020-01-01", "grade": "k"}},
		}},
		{"more than 10 children", map[string]interface{}{
			"parent": map[string]interface{}{"first_name": "A", "last_name": "B", "phone": "1234567", "email": "a@b.com"},
			"children": func() []map[string]interface{} {
				children := make([]map[string]interface{}, 0, 11)
				for i := 0; i < 11; i++ {
					children = append(children, map[string]interface{}{"first_name": "C", "last_name": "D", "dob": "2020-01-01", "grade": "k"})
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
	}, []guestsubmission.Child{{FirstName: "Timmy", LastName: "Smith", DOB: "2020-01-01", Grade: "1st Grade"}})
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
	}, []guestsubmission.Child{{FirstName: "Timmy", LastName: "Smith", DOB: "2020-01-01", Grade: "1st Grade"}})
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/v1/admin/guest-submissions", nil)
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var list []Submission
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&list))
	require.Len(t, list, 1)
	assert.Equal(t, "555", list[0].Parent.Phone)
	assert.Equal(t, "1st Grade", list[0].Children[0].Grade)
}

func TestController_StaffCannotMarkEntered(t *testing.T) {
	app, _, testDB := setupAuthedApp(t, "")
	wipeSubmissionTables(t, testDB)

	repo := guestsubmission.NewRepo(testDB)
	sub, err := repo.CreateSubmission(t.Context(), guestsubmission.Parent{
		FirstName: "A", LastName: "B", Phone: "1234567", Email: "a@b.com",
	}, []guestsubmission.Child{{FirstName: "C", LastName: "D", DOB: "2020-01-01", Grade: "k"}})
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
	}, []guestsubmission.Child{{FirstName: "C", LastName: "D", DOB: "2020-01-01", Grade: "k"}})
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
	}, []guestsubmission.Child{{FirstName: "C", LastName: "D", DOB: "2020-01-01", Grade: "k"}})
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
		{FirstName: "C", LastName: "D", DOB: "2020-01-01", Grade: "k"},
		{FirstName: "E", LastName: "F", DOB: "2021-02-02", Grade: "1"},
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
	}, []guestsubmission.Child{{FirstName: "Timmy", LastName: "Smith", DOB: "2020-01-01", Grade: "1st Grade"}})
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
	}, []guestsubmission.Child{{FirstName: "C", LastName: "D", DOB: "2020-01-01", Grade: "k"}})
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
	}, []guestsubmission.Child{{FirstName: "C", LastName: "D", DOB: "2020-01-01", Grade: "k"}})
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

func TestController_RequiresAuth(t *testing.T) {
	app := fiber.New()
	store := session.New()
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err)
	t.Cleanup(cleanup)
	NewController(testDB, store).RegisterRoutes(app)

	req := httptest.NewRequest("GET", "/v1/checkins/guest-submissions", nil)
	resp, _ := app.Test(req)
	require.Equal(t, fiber.StatusFound, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Location"), "/login")
}
