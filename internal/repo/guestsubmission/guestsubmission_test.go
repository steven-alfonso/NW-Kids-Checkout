package guestsubmission

import (
	"database/sql"
	"log"
	"os"
	"testing"
	"time"

	"kids-checkin/internal/db"
	"kids-checkin/internal/repo"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	tDB, cleanup, err := db.PrepareTestDB()
	if err != nil {
		log.Fatalf("Failed to prepare test DB: %v", err)
	}
	testDB = tDB

	code := m.Run()
	cleanup()
	os.Exit(code)
}

func wipeAll(t *testing.T) {
	_, err := squirrel.Delete("guest_submissions").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)
	_, err = squirrel.Delete("manual_checkins").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)
	_, err = squirrel.Delete("children").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)
	_, err = squirrel.Delete("parents").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)
}

func Test_sqliteRepo_CreateSubmission(t *testing.T) {
	wipeAll(t)
	s := NewRepo(testDB)

	sub, err := s.CreateSubmission(t.Context(), Parent{
		FirstName: "John",
		LastName:  "Smith",
		Phone:     "555-1234",
		Email:     "john@example.com",
	}, []Child{
		{FirstName: "Timmy", LastName: "Smith", DOB: "2020-01-01", Grade: "1st Grade"},
		{FirstName: "Sara", LastName: "Smith", DOB: "2018-06-15", Grade: "3rd Grade"},
	})
	require.NoError(t, err)

	assert.NotZero(t, sub.ID)
	assert.NotEmpty(t, sub.PublicID)
	_, parseErr := uuid.Parse(sub.PublicID)
	require.NoError(t, parseErr)
	assert.Equal(t, StatusPending, sub.Status)
	assert.Len(t, sub.Children, 2)
	assert.NotZero(t, sub.Parent.ID)
	assert.Equal(t, "john@example.com", sub.Parent.Email)
	for _, c := range sub.Children {
		assert.NotZero(t, c.ID)
		assert.Equal(t, sub.Parent.ID, c.ParentID)
	}
}

func Test_sqliteRepo_ListSubmissions(t *testing.T) {
	wipeAll(t)
	s := NewRepo(testDB)

	a, err := s.CreateSubmission(t.Context(), Parent{
		FirstName: "John", LastName: "Smith", Phone: "1", Email: "a@b.com",
	}, []Child{{FirstName: "Timmy", LastName: "Smith", DOB: "2020-01-01", Grade: "k"}})
	require.NoError(t, err)

	b, err := s.CreateSubmission(t.Context(), Parent{
		FirstName: "Jane", LastName: "Doe", Phone: "2", Email: "j@d.com",
	}, []Child{{FirstName: "Sam", LastName: "Doe", DOB: "2019-02-02", Grade: "1"}})
	require.NoError(t, err)

	t.Run("filter by status", func(t *testing.T) {
		res, err := s.ListSubmissions(t.Context(), Filter{Status: StatusPending})
		require.NoError(t, err)
		require.Len(t, res, 2)
	})

	t.Run("filter by public id", func(t *testing.T) {
		res, err := s.ListSubmissions(t.Context(), Filter{PublicID: a.PublicID})
		require.NoError(t, err)
		require.Len(t, res, 1)
		assert.Equal(t, a.PublicID, res[0].PublicID)
		assert.Equal(t, "Timmy", res[0].Children[0].FirstName)
		assert.Equal(t, "a@b.com", res[0].Parent.Email)
	})

	t.Run("children belong to the right parent", func(t *testing.T) {
		res, err := s.ListSubmissions(t.Context(), Filter{PublicID: b.PublicID})
		require.NoError(t, err)
		require.Len(t, res, 1)
		assert.Equal(t, "Sam", res[0].Children[0].FirstName)
	})
}

func Test_sqliteRepo_UpdateSubmissionStatus(t *testing.T) {
	wipeAll(t)
	s := NewRepo(testDB)
	sub, err := s.CreateSubmission(t.Context(), Parent{
		FirstName: "John", LastName: "Smith", Phone: "1", Email: "a@b.com",
	}, []Child{{FirstName: "Timmy", LastName: "Smith", DOB: "2020-01-01", Grade: "k"}})
	require.NoError(t, err)

	t.Run("approve sets status and timestamp", func(t *testing.T) {
		now := time.Now().UTC()
		err := s.UpdateSubmissionStatus(t.Context(), sub.PublicID, StatusApproved, now)
		require.NoError(t, err)

		res, err := s.ListSubmissions(t.Context(), Filter{PublicID: sub.PublicID})
		require.NoError(t, err)
		require.Len(t, res, 1)
		assert.Equal(t, StatusApproved, res[0].Status)
		assert.WithinDuration(t, now, res[0].ApprovedAt, time.Second)
	})

	t.Run("entered", func(t *testing.T) {
		now := time.Now().UTC()
		err := s.UpdateSubmissionStatus(t.Context(), sub.PublicID, StatusEntered, now)
		require.NoError(t, err)

		res, err := s.ListSubmissions(t.Context(), Filter{PublicID: sub.PublicID})
		require.NoError(t, err)
		assert.Equal(t, StatusEntered, res[0].Status)
		assert.WithinDuration(t, now, res[0].EnteredAt, time.Second)
	})

	t.Run("unknown public id returns repo.ErrNotFound", func(t *testing.T) {
		err := s.UpdateSubmissionStatus(t.Context(), "does-not-exist", StatusApproved, time.Now().UTC())
		require.ErrorIs(t, err, repo.ErrNotFound)
	})

	t.Run("unknown status errors", func(t *testing.T) {
		err := s.UpdateSubmissionStatus(t.Context(), sub.PublicID, "bogus", time.Now().UTC())
		require.Error(t, err)
	})
}

func Test_sqliteRepo_ApproveSubmission(t *testing.T) {
	wipeAll(t)
	s := NewRepo(testDB)

	sub, err := s.CreateSubmission(t.Context(), Parent{
		FirstName: "John", LastName: "Smith", Phone: "555-1234", Email: "john@example.com",
	}, []Child{
		{FirstName: "Timmy", LastName: "Smith", DOB: "2020-01-01", Grade: "k"},
		{FirstName: "Sara", LastName: "Smith", DOB: "2018-06-15", Grade: "1"},
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	require.NoError(t, s.ApproveSubmission(t.Context(), sub.PublicID, now))

	res, err := s.ListSubmissions(t.Context(), Filter{PublicID: sub.PublicID})
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, StatusApproved, res[0].Status)
	assert.WithinDuration(t, now, res[0].ApprovedAt, time.Second)

	for _, child := range sub.Children {
		var firstName, lastName string
		err := testDB.QueryRowContext(t.Context(),
			"SELECT first_name, last_name FROM manual_checkins WHERE child_id = ?", child.ID).
			Scan(&firstName, &lastName)
		require.NoError(t, err)
		assert.Equal(t, child.FirstName, firstName)
		assert.Equal(t, child.LastName, lastName)
	}

	t.Run("unknown public id returns repo.ErrNotFound", func(t *testing.T) {
		err := s.ApproveSubmission(t.Context(), "does-not-exist", time.Now().UTC())
		require.ErrorIs(t, err, repo.ErrNotFound)
	})
}
