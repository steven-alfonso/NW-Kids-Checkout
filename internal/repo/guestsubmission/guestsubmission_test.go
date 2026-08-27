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

	t.Run("filter by derived statuses", func(t *testing.T) {
		res, err := s.ListSubmissions(t.Context(), Filter{Status: StatusPending})
		require.NoError(t, err)
		require.Len(t, res, 2)

		now := time.Now().UTC()
		require.NoError(t, s.UpdateSubmissionStatus(t.Context(), a.PublicID, StatusApproved, now))
		require.NoError(t, s.UpdateSubmissionStatus(t.Context(), b.PublicID, StatusEntered, now))

		approved, err := s.ListSubmissions(t.Context(), Filter{Status: StatusApproved})
		require.NoError(t, err)
		require.Len(t, approved, 1)
		assert.Equal(t, a.PublicID, approved[0].PublicID)
		assert.Equal(t, StatusApproved, approved[0].Status)

		entered, err := s.ListSubmissions(t.Context(), Filter{Status: StatusEntered})
		require.NoError(t, err)
		require.Len(t, entered, 1)
		assert.Equal(t, b.PublicID, entered[0].PublicID)
		assert.Equal(t, StatusEntered, entered[0].Status)

		pending, err := s.ListSubmissions(t.Context(), Filter{Status: StatusPending})
		require.NoError(t, err)
		require.Len(t, pending, 0)
	})

	t.Run("unknown status filter errors", func(t *testing.T) {
		_, err := s.ListSubmissions(t.Context(), Filter{Status: "bogus"})
		require.Error(t, err)
	})

	t.Run("without manual checkins excludes entered families with rows", func(t *testing.T) {
		wipeAll(t)
		s2 := NewRepo(testDB)
		standalone, err := s2.CreateSubmission(t.Context(), Parent{
			FirstName: "Standalone", LastName: "Family", Phone: "99", Email: "s@f.com",
		}, []Child{{FirstName: "SF", LastName: "Family", DOB: "2020-01-01", Grade: "k"}})
		require.NoError(t, err)
		entered, err := s2.CreateSubmission(t.Context(), Parent{
			FirstName: "Entered", LastName: "Only", Phone: "88", Email: "e@o.com",
		}, []Child{{FirstName: "EO", LastName: "Only", DOB: "2020-01-01", Grade: "k"}})
		require.NoError(t, err)

		now := time.Now().UTC()
		require.NoError(t, s2.UpdateSubmissionStatus(t.Context(), standalone.PublicID, StatusEntered, now))
		require.NoError(t, s2.CreateManualCheckins(t.Context(), standalone.PublicID))

		require.NoError(t, s2.UpdateSubmissionStatus(t.Context(), entered.PublicID, StatusEntered, now))

		res, err := s2.ListSubmissions(t.Context(), Filter{Status: StatusEntered, WithoutManualCheckins: true})
		require.NoError(t, err)
		require.Len(t, res, 1)
		assert.Equal(t, entered.PublicID, res[0].PublicID)
	})

	t.Run("children belong to the right parent", func(t *testing.T) {
		wipeAll(t)
		s3 := NewRepo(testDB)
		b, err := s3.CreateSubmission(t.Context(), Parent{
			FirstName: "Jane", LastName: "Doe", Phone: "2", Email: "j@d.com",
		}, []Child{{FirstName: "Sam", LastName: "Doe", DOB: "2019-02-02", Grade: "1"}})
		require.NoError(t, err)
		res, err := s3.ListSubmissions(t.Context(), Filter{PublicID: b.PublicID})
		require.NoError(t, err)
		require.Len(t, res, 1)
		assert.Equal(t, "Sam", res[0].Children[0].FirstName)
	})
}

func Test_statusPredicate(t *testing.T) {
	t.Run("rejected predicate excludes rows with both approved_at and rejected_at", func(t *testing.T) {
		now := time.Now().UTC()

		sub, err := createSubmissionDirect(t, testDB, Parent{
			FirstName: "Dual", LastName: "Timestamp", Phone: "555-9999", Email: "dual@test.com",
		}, []Child{{FirstName: "DT", LastName: "Timestamp", DOB: "2020-01-01", Grade: "k"}})
		require.NoError(t, err)

		// Set both approved_at and rejected_at directly in the DB
		_, err = testDB.ExecContext(t.Context(),
			`UPDATE guest_submissions SET approved_at = ?, rejected_at = ? WHERE public_id = ?`,
			now, now, sub.PublicID)
		require.NoError(t, err)

		// Should NOT appear under "rejected" filter
		rejected, err := squirrel.Select("id").From("guest_submissions").
			Where(statusPredicateForTest(t, StatusRejected)).
			RunWith(testDB).QueryContext(t.Context())
		require.NoError(t, err)
		defer rejected.Close()
		count := 0
		for rejected.Next() {
			count++
		}
		assert.Equal(t, 0, count, "should not appear under StatusRejected when both timestamps are set")
	})
}

func statusPredicateForTest(t *testing.T, status string) squirrel.Sqlizer {
	t.Helper()
	p, err := statusPredicate(status)
	require.NoError(t, err)
	return p
}

func createSubmissionDirect(t *testing.T, db *sql.DB, parent Parent, children []Child) (Submission, error) {
	t.Helper()
	s := NewRepo(db)
	return s.CreateSubmission(t.Context(), parent, children)
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
		assert.True(t, res[0].ApprovedAt.IsZero(), "approved_at should be cleared when entered")
		assert.True(t, res[0].RejectedAt.IsZero(), "rejected_at should be cleared when entered")
	})

	t.Run("rejected", func(t *testing.T) {
		rejSub, err := s.CreateSubmission(t.Context(), Parent{
			FirstName: "Reject", LastName: "Test", Phone: "2", Email: "rej@test.com",
		}, []Child{{FirstName: "RT", LastName: "Test", DOB: "2020-01-01", Grade: "k"}})
		require.NoError(t, err)

		now := time.Now().UTC()
		err = s.UpdateSubmissionStatus(t.Context(), rejSub.PublicID, StatusRejected, now)
		require.NoError(t, err)

		res, err := s.ListSubmissions(t.Context(), Filter{PublicID: rejSub.PublicID})
		require.NoError(t, err)
		assert.Equal(t, StatusRejected, res[0].Status)
		assert.WithinDuration(t, now, res[0].RejectedAt, time.Second)
		assert.True(t, res[0].ApprovedAt.IsZero(), "approved_at should be cleared when rejected")
		assert.True(t, res[0].EnteredAt.IsZero(), "entered_at should be cleared when rejected")
	})

	t.Run("concurrent status change returns ErrConflict", func(t *testing.T) {
		raceSub, err := s.CreateSubmission(t.Context(), Parent{
			FirstName: "Race", LastName: "Test", Phone: "3", Email: "race@test.com",
		}, []Child{{FirstName: "RC", LastName: "Test", DOB: "2020-01-01", Grade: "k"}})
		require.NoError(t, err)
		assert.Equal(t, StatusPending, raceSub.Status)

		now := time.Now().UTC()
		err = s.UpdateSubmissionStatus(t.Context(), raceSub.PublicID, StatusRejected, now)
		require.NoError(t, err)

		err = s.UpdateSubmissionStatus(t.Context(), raceSub.PublicID, StatusApproved, now)
		require.ErrorIs(t, err, ErrConflict)

		res, err := s.ListSubmissions(t.Context(), Filter{PublicID: raceSub.PublicID})
		require.NoError(t, err)
		require.Len(t, res, 1)
		assert.Equal(t, StatusRejected, res[0].Status, "status must not be overwritten by stale caller")
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

func Test_sqliteRepo_CreateManualCheckins(t *testing.T) {
	wipeAll(t)
	s := NewRepo(testDB)

	sub, err := s.CreateSubmission(t.Context(), Parent{
		FirstName: "John", LastName: "Smith", Phone: "555-1234", Email: "john@example.com",
	}, []Child{
		{FirstName: "Timmy", LastName: "Smith", DOB: "2020-01-01", Grade: "k"},
		{FirstName: "Sara", LastName: "Smith", DOB: "2018-06-15", Grade: "1"},
	})
	require.NoError(t, err)

	t.Run("creates rows without changing entered status", func(t *testing.T) {
		err := s.UpdateSubmissionStatus(t.Context(), sub.PublicID, StatusEntered, time.Now().UTC())
		require.NoError(t, err)

		require.NoError(t, s.CreateManualCheckins(t.Context(), sub.PublicID))

		for _, child := range sub.Children {
			var firstName, lastName string
			err := testDB.QueryRowContext(t.Context(),
				"SELECT first_name, last_name FROM manual_checkins WHERE child_id = ?", child.ID).
				Scan(&firstName, &lastName)
			require.NoError(t, err)
			assert.Equal(t, child.FirstName, firstName)
			assert.Equal(t, child.LastName, lastName)
		}

		res, err := s.ListSubmissions(t.Context(), Filter{PublicID: sub.PublicID})
		require.NoError(t, err)
		require.Len(t, res, 1)
		assert.Equal(t, StatusEntered, res[0].Status)
		assert.True(t, res[0].ApprovedAt.IsZero(), "status must remain entered")
	})

	t.Run("duplicate creation returns ErrManualCheckinsExist", func(t *testing.T) {
		err := s.CreateManualCheckins(t.Context(), sub.PublicID)
		require.ErrorIs(t, err, ErrManualCheckinsExist)
	})

	t.Run("rejected submission errors", func(t *testing.T) {
		rejSub, err := s.CreateSubmission(t.Context(), Parent{
			FirstName: "Jane", LastName: "Doe", Phone: "555-0000", Email: "j@d.com",
		}, []Child{{FirstName: "Sam", LastName: "Doe", DOB: "2019-02-02", Grade: "1"}})
		require.NoError(t, err)
		require.NoError(t, s.UpdateSubmissionStatus(t.Context(), rejSub.PublicID, StatusRejected, time.Now().UTC()))

		err = s.CreateManualCheckins(t.Context(), rejSub.PublicID)
		require.Error(t, err)
	})

	t.Run("unknown public id returns repo.ErrNotFound", func(t *testing.T) {
		err := s.CreateManualCheckins(t.Context(), "does-not-exist")
		require.ErrorIs(t, err, repo.ErrNotFound)
	})
}
