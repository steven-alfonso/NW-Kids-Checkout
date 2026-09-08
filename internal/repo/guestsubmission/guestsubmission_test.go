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

		Address1: "123 Main St",
		City:     "Seattle",
		State:    "WA",
		Zip:      "98101"}, []Child{
		{FirstName: "Timmy", LastName: "Smith", DOB: "2020-01-01", Grade: "1st Grade", Gender: "Boy", Relationship: "Parent"},
		{FirstName: "Sara", LastName: "Smith", DOB: "2018-06-15", Grade: "3rd Grade", Gender: "Boy", Relationship: "Parent"},
	}, true)
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

		Address1: "123 Main St",
		City:     "Seattle",
		State:    "WA",
		Zip:      "98101"}, []Child{{FirstName: "Timmy", LastName: "Smith", DOB: "2020-01-01", Grade: "k", Gender: "Boy", Relationship: "Parent"}}, true)
	require.NoError(t, err)

	b, err := s.CreateSubmission(t.Context(), Parent{
		FirstName: "Jane", LastName: "Doe", Phone: "2", Email: "j@d.com",

		Address1: "123 Main St",
		City:     "Seattle",
		State:    "WA",
		Zip:      "98101"}, []Child{{FirstName: "Sam", LastName: "Doe", DOB: "2019-02-02", Grade: "1", Gender: "Boy", Relationship: "Parent"}}, true)
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
		require.NoError(t, s.UpdateSubmissionStatus(t.Context(), a.PublicID, StatusEntered, now))

		entered, err := s.ListSubmissions(t.Context(), Filter{Status: StatusEntered})
		require.NoError(t, err)
		require.Len(t, entered, 1)
		assert.Equal(t, a.PublicID, entered[0].PublicID)
		assert.Equal(t, StatusEntered, entered[0].Status)

		pending, err := s.ListSubmissions(t.Context(), Filter{Status: StatusPending})
		require.NoError(t, err)
		require.Len(t, pending, 1)
		assert.Equal(t, b.PublicID, pending[0].PublicID)
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

			Address1: "123 Main St",
			City:     "Seattle",
			State:    "WA",
			Zip:      "98101"}, []Child{{FirstName: "SF", LastName: "Family", DOB: "2020-01-01", Grade: "k", Gender: "Boy", Relationship: "Parent"}}, true)
		require.NoError(t, err)
		entered, err := s2.CreateSubmission(t.Context(), Parent{
			FirstName: "Entered", LastName: "Only", Phone: "88", Email: "e@o.com",

			Address1: "123 Main St",
			City:     "Seattle",
			State:    "WA",
			Zip:      "98101"}, []Child{{FirstName: "EO", LastName: "Only", DOB: "2020-01-01", Grade: "k", Gender: "Boy", Relationship: "Parent"}}, true)
		require.NoError(t, err)

		// Auto-create already inserted manual_checkins; clear entered's backfill to simulate "without"
		_, err = testDB.ExecContext(t.Context(), `DELETE FROM manual_checkins WHERE child_id IN (SELECT id FROM children WHERE parent_id = ?)`, entered.ParentID)
		require.NoError(t, err)
		_, err = testDB.ExecContext(t.Context(), `UPDATE guest_submissions SET checkins_backfilled_at = NULL WHERE public_id = ?`, entered.PublicID)
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

			Address1: "123 Main St",
			City:     "Seattle",
			State:    "WA",
			Zip:      "98101"}, []Child{{FirstName: "Sam", LastName: "Doe", DOB: "2019-02-02", Grade: "1", Gender: "Boy", Relationship: "Parent"}}, true)
		require.NoError(t, err)
		res, err := s3.ListSubmissions(t.Context(), Filter{PublicID: b.PublicID})
		require.NoError(t, err)
		require.Len(t, res, 1)
		assert.Equal(t, "Sam", res[0].Children[0].FirstName)
	})

	t.Run("limit truncates and orders by created_at DESC", func(t *testing.T) {
		wipeAll(t)
		s4 := NewRepo(testDB)
		now := time.Now().UTC()
		subs := make([]Submission, 0, 3)
		for i := range 3 {
			sub, err := s4.CreateSubmission(t.Context(), Parent{
				FirstName: "Limit", LastName: string(rune('A' + i)), Phone: "1", Email: "l@test.com",

				Address1: "123 Main St",
				City:     "Seattle",
				State:    "WA",
				Zip:      "98101"}, []Child{{FirstName: "Kid", LastName: string(rune('A' + i)), DOB: "2020-01-01", Grade: "k", Gender: "Boy", Relationship: "Parent"}}, true)
			require.NoError(t, err)
			// Spread created_at by minutes to make ordering deterministic.
			createdAt := now.Add(time.Duration(i) * time.Minute)
			_, err = testDB.ExecContext(t.Context(), `UPDATE guest_submissions SET created_at = ? WHERE public_id = ?`, createdAt, sub.PublicID)
			require.NoError(t, err)
			_, err = testDB.ExecContext(t.Context(), `UPDATE parents SET created_at = ? WHERE id = ?`, createdAt, sub.ParentID)
			require.NoError(t, err)
			sub.CreatedAt = createdAt
			subs = append(subs, sub)
		}

		res, err := s4.ListSubmissions(t.Context(), Filter{Limit: 2})
		require.NoError(t, err)
		require.Len(t, res, 2)
		// Most recent first (highest created_at).
		assert.Equal(t, subs[2].PublicID, res[0].PublicID)
		assert.Equal(t, subs[1].PublicID, res[1].PublicID)
		assert.True(t, res[0].CreatedAt.After(res[1].CreatedAt) || res[0].CreatedAt.Equal(res[1].CreatedAt))

		// No limit returns all 3.
		all, err := s4.ListSubmissions(t.Context(), Filter{})
		require.NoError(t, err)
		require.Len(t, all, 3)

		// Limit larger than total still returns all.
		large, err := s4.ListSubmissions(t.Context(), Filter{Limit: 10})
		require.NoError(t, err)
		require.Len(t, large, 3)
	})
}

func Test_sqliteRepo_ListSubmissions_Pagination(t *testing.T) {
	wipeAll(t)
	s := NewRepo(testDB)

	now := time.Now().UTC()
	subs := make([]Submission, 0, 5)
	for i := range 5 {
		sub, err := s.CreateSubmission(t.Context(), Parent{
			FirstName: "Page", LastName: string(rune('A' + i)), Phone: "1", Email: "p@test.com",

			Address1: "123 Main St",
			City:     "Seattle",
			State:    "WA",
			Zip:      "98101"}, []Child{{FirstName: "Kid", LastName: string(rune('A' + i)), DOB: "2020-01-01", Grade: "k", Gender: "Boy", Relationship: "Parent"}}, true)
		require.NoError(t, err)
		createdAt := now.Add(time.Duration(i) * time.Minute)
		_, err = testDB.ExecContext(t.Context(), `UPDATE guest_submissions SET created_at = ? WHERE public_id = ?`, createdAt, sub.PublicID)
		require.NoError(t, err)
		sub.CreatedAt = createdAt
		subs = append(subs, sub)
	}

	t.Run("offset pages through ordered results", func(t *testing.T) {
		page1, err := s.ListSubmissions(t.Context(), Filter{Limit: 2, Offset: 0})
		require.NoError(t, err)
		require.Len(t, page1, 2)
		assert.Equal(t, subs[4].PublicID, page1[0].PublicID)
		assert.Equal(t, subs[3].PublicID, page1[1].PublicID)

		page2, err := s.ListSubmissions(t.Context(), Filter{Limit: 2, Offset: 2})
		require.NoError(t, err)
		require.Len(t, page2, 2)
		assert.Equal(t, subs[2].PublicID, page2[0].PublicID)
		assert.Equal(t, subs[1].PublicID, page2[1].PublicID)

		page3, err := s.ListSubmissions(t.Context(), Filter{Limit: 2, Offset: 4})
		require.NoError(t, err)
		require.Len(t, page3, 1)
		assert.Equal(t, subs[0].PublicID, page3[0].PublicID)
	})

	t.Run("offset beyond total returns empty", func(t *testing.T) {
		res, err := s.ListSubmissions(t.Context(), Filter{Limit: 2, Offset: 10})
		require.NoError(t, err)
		require.Len(t, res, 0)
	})
}

func Test_sqliteRepo_CountSubmissions(t *testing.T) {
	wipeAll(t)
	s := NewRepo(testDB)

	for i := range 3 {
		_, err := s.CreateSubmission(t.Context(), Parent{
			FirstName: "Count", LastName: string(rune('A' + i)), Phone: "1", Email: "c@test.com",

			Address1: "123 Main St",
			City:     "Seattle",
			State:    "WA",
			Zip:      "98101"}, []Child{{FirstName: "Kid", LastName: string(rune('A' + i)), DOB: "2020-01-01", Grade: "k", Gender: "Boy", Relationship: "Parent"}}, true)
		require.NoError(t, err)
	}

	t.Run("counts all", func(t *testing.T) {
		total, err := s.CountSubmissions(t.Context(), Filter{})
		require.NoError(t, err)
		assert.Equal(t, 3, total)
	})

	t.Run("counts with status filter", func(t *testing.T) {
		total, err := s.CountSubmissions(t.Context(), Filter{Status: StatusPending})
		require.NoError(t, err)
		assert.Equal(t, 3, total)
	})

	t.Run("counts with public id filter", func(t *testing.T) {
		res, err := s.ListSubmissions(t.Context(), Filter{Limit: 1})
		require.NoError(t, err)
		require.Len(t, res, 1)
		total, err := s.CountSubmissions(t.Context(), Filter{PublicID: res[0].PublicID})
		require.NoError(t, err)
		assert.Equal(t, 1, total)
	})

	t.Run("counts per derived status", func(t *testing.T) {
		res, err := s.ListSubmissions(t.Context(), Filter{Status: StatusPending, Limit: 1})
		require.NoError(t, err)
		require.Len(t, res, 1)
		require.NoError(t, s.UpdateSubmissionStatus(t.Context(), res[0].PublicID, StatusEntered, time.Now().UTC()))

		pending, err := s.CountSubmissions(t.Context(), Filter{Status: StatusPending})
		require.NoError(t, err)
		assert.Equal(t, 2, pending)

		entered, err := s.CountSubmissions(t.Context(), Filter{Status: StatusEntered})
		require.NoError(t, err)
		assert.Equal(t, 1, entered)
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
	return s.CreateSubmission(t.Context(), parent, children, true)
}

func Test_statusPredicate(t *testing.T) {
	t.Run("pending vs entered predicates", func(t *testing.T) {
		sub, err := createSubmissionDirect(t, testDB, Parent{
			FirstName: "Pred", LastName: "Test", Phone: "555-9999", Email: "pred@test.com",

			Address1: "123 Main St",
			City:     "Seattle",
			State:    "WA",
			Zip:      "98101"}, []Child{{FirstName: "DT", LastName: "Test", DOB: "2020-01-01", Grade: "k", Gender: "Boy", Relationship: "Parent"}})
		require.NoError(t, err)

		// pending should be found under pending, not entered
		pendingRows, err := squirrel.Select("id").From("guest_submissions").
			Where(statusPredicateForTest(t, StatusPending)).
			Where(squirrel.Eq{"public_id": sub.PublicID}).
			RunWith(testDB).QueryContext(t.Context())
		require.NoError(t, err)
		defer pendingRows.Close()
		count := 0
		for pendingRows.Next() {
			count++
		}
		assert.Equal(t, 1, count)

		// mark entered
		require.NoError(t, NewRepo(testDB).UpdateSubmissionStatus(t.Context(), sub.PublicID, StatusEntered, time.Now().UTC()))

		enteredRows, err := squirrel.Select("id").From("guest_submissions").
			Where(statusPredicateForTest(t, StatusEntered)).
			Where(squirrel.Eq{"public_id": sub.PublicID}).
			RunWith(testDB).QueryContext(t.Context())
		require.NoError(t, err)
		defer enteredRows.Close()
		count = 0
		for enteredRows.Next() {
			count++
		}
		assert.Equal(t, 1, count)
	})
}

func Test_sqliteRepo_UpdateSubmissionStatus(t *testing.T) {
	wipeAll(t)
	s := NewRepo(testDB)
	sub, err := s.CreateSubmission(t.Context(), Parent{
		FirstName: "John", LastName: "Smith", Phone: "1", Email: "a@b.com",

		Address1: "123 Main St",
		City:     "Seattle",
		State:    "WA",
		Zip:      "98101"}, []Child{{FirstName: "Timmy", LastName: "Smith", DOB: "2020-01-01", Grade: "k", Gender: "Boy", Relationship: "Parent"}}, true)
	require.NoError(t, err)

	t.Run("entered", func(t *testing.T) {
		// sub is pending initially, mark entered
		// First create a fresh pending sub for this test
		pendingSub, err := s.CreateSubmission(t.Context(), Parent{
			FirstName: "Enter", LastName: "Test", Phone: "9", Email: "enter@test.com",
			Address1: "123 Main St", City: "Seattle", State: "WA", Zip: "98101"}, []Child{{FirstName: "ET", LastName: "Test", DOB: "2020-01-01", Grade: "k", Gender: "Boy", Relationship: "Parent"}}, true)
		require.NoError(t, err)
		now := time.Now().UTC()
		err = s.UpdateSubmissionStatus(t.Context(), pendingSub.PublicID, StatusEntered, now)
		require.NoError(t, err)

		res, err := s.ListSubmissions(t.Context(), Filter{PublicID: pendingSub.PublicID})
		require.NoError(t, err)
		assert.Equal(t, StatusEntered, res[0].Status)
		assert.WithinDuration(t, now, res[0].EnteredAt, time.Second)
	})

	t.Run("concurrent status change returns ErrConflict", func(t *testing.T) {
		raceSub, err := s.CreateSubmission(t.Context(), Parent{
			FirstName: "Race", LastName: "Test", Phone: "3", Email: "race@test.com",

			Address1: "123 Main St",
			City:     "Seattle",
			State:    "WA",
			Zip:      "98101"}, []Child{{FirstName: "RC", LastName: "Test", DOB: "2020-01-01", Grade: "k", Gender: "Boy", Relationship: "Parent"}}, true)
		require.NoError(t, err)
		assert.Equal(t, StatusPending, raceSub.Status)

		now := time.Now().UTC()
		err = s.UpdateSubmissionStatus(t.Context(), raceSub.PublicID, StatusEntered, now)
		require.NoError(t, err)

		// second attempt should conflict (already entered)
		err = s.UpdateSubmissionStatus(t.Context(), raceSub.PublicID, StatusEntered, now)
		require.ErrorIs(t, err, ErrConflict)

		res, err := s.ListSubmissions(t.Context(), Filter{PublicID: raceSub.PublicID})
		require.NoError(t, err)
		require.Len(t, res, 1)
		assert.Equal(t, StatusEntered, res[0].Status, "status must not be overwritten by stale caller")
	})

	t.Run("unknown public id via UpdateSubmissionStatus returns repo.ErrNotFound", func(t *testing.T) {
		err := s.UpdateSubmissionStatus(t.Context(), "does-not-exist", StatusEntered, time.Now().UTC())
		require.ErrorIs(t, err, repo.ErrNotFound)
	})

	t.Run("unknown status errors", func(t *testing.T) {
		err := s.UpdateSubmissionStatus(t.Context(), sub.PublicID, "bogus", time.Now().UTC())
		require.Error(t, err)
	})
}

func Test_sqliteRepo_CreateSubmission_AutoCreatesManualCheckins(t *testing.T) {
	wipeAll(t)
	s := NewRepo(testDB)

	sub, err := s.CreateSubmission(t.Context(), Parent{
		FirstName: "John", LastName: "Smith", Phone: "555-1234", Email: "john@example.com",

		Address1: "123 Main St",
		City:     "Seattle",
		State:    "WA",
		Zip:      "98101"}, []Child{
		{FirstName: "Timmy", LastName: "Smith", DOB: "2020-01-01", Grade: "k", Gender: "Boy", Relationship: "Parent"},
		{FirstName: "Sara", LastName: "Smith", DOB: "2018-06-15", Grade: "1", Gender: "Boy", Relationship: "Parent"},
	}, true)
	require.NoError(t, err)

	res, err := s.ListSubmissions(t.Context(), Filter{PublicID: sub.PublicID})
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, StatusPending, res[0].Status)

	for _, child := range sub.Children {
		var firstName, lastName string
		err := testDB.QueryRowContext(t.Context(),
			"SELECT first_name, last_name FROM manual_checkins WHERE child_id = ?", child.ID).
			Scan(&firstName, &lastName)
		require.NoError(t, err)
		assert.Equal(t, child.FirstName, firstName)
		assert.Equal(t, child.LastName, lastName)
	}
	// checkins_backfilled_at should be set
	assert.False(t, res[0].CheckinsBackfilledAt.IsZero())
}

func Test_sqliteRepo_CreateManualCheckins(t *testing.T) {
	wipeAll(t)
	s := NewRepo(testDB)

	sub, err := s.CreateSubmission(t.Context(), Parent{
		FirstName: "John", LastName: "Smith", Phone: "555-1234", Email: "john@example.com",

		Address1: "123 Main St",
		City:     "Seattle",
		State:    "WA",
		Zip:      "98101"}, []Child{
		{FirstName: "Timmy", LastName: "Smith", DOB: "2020-01-01", Grade: "k", Gender: "Boy", Relationship: "Parent"},
		{FirstName: "Sara", LastName: "Smith", DOB: "2018-06-15", Grade: "1", Gender: "Boy", Relationship: "Parent"},
	}, true)
	require.NoError(t, err)

	t.Run("creates rows without changing entered status", func(t *testing.T) {
		// manual checkins already auto-created on submission; this should be no-op but still entered
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
	})

	t.Run("duplicate creation is a no-op", func(t *testing.T) {
		err := s.CreateManualCheckins(t.Context(), sub.PublicID)
		require.NoError(t, err)

		err = s.CreateManualCheckins(t.Context(), sub.PublicID)
		require.NoError(t, err)

		for _, child := range sub.Children {
			var count int
			err := testDB.QueryRowContext(t.Context(),
				"SELECT COUNT(*) FROM manual_checkins WHERE child_id = ?", child.ID).
				Scan(&count)
			require.NoError(t, err)
			assert.Equal(t, 1, count, "each child should have exactly 1 manual_checkins row")
		}
	})

	t.Run("pending submission now succeeds (auto-created)", func(t *testing.T) {
		pendingSub, err := s.CreateSubmission(t.Context(), Parent{
			FirstName: "Jim", LastName: "Bean", Phone: "555-1111", Email: "j@b.com",

			Address1: "123 Main St",
			City:     "Seattle",
			State:    "WA",
			Zip:      "98101"}, []Child{{FirstName: "Kid", LastName: "Bean", DOB: "2019-02-02", Grade: "1", Gender: "Boy", Relationship: "Parent"}}, true)
		require.NoError(t, err)

		// Should not error now; manual checkins already exist
		err = s.CreateManualCheckins(t.Context(), pendingSub.PublicID)
		require.NoError(t, err)
	})

	t.Run("unknown public id returns repo.ErrNotFound", func(t *testing.T) {
		err := s.CreateManualCheckins(t.Context(), "does-not-exist")
		require.ErrorIs(t, err, repo.ErrNotFound)
	})
}

func Test_sqliteRepo_CreateManualCheckins_PartialCoverage(t *testing.T) {
	wipeAll(t)
	s := NewRepo(testDB)

	sub, err := s.CreateSubmission(t.Context(), Parent{
		FirstName: "Partial", LastName: "Family", Phone: "555-1234", Email: "partial@test.com",

		Address1: "123 Main St",
		City:     "Seattle",
		State:    "WA",
		Zip:      "98101"}, []Child{
		{FirstName: "Kid1", LastName: "Family", DOB: "2020-01-01", Grade: "k", Gender: "Boy", Relationship: "Parent"},
		{FirstName: "Kid2", LastName: "Family", DOB: "2019-02-02", Grade: "1", Gender: "Boy", Relationship: "Parent"},
	}, true)
	require.NoError(t, err)
	require.Len(t, sub.Children, 2)
	// Clear auto-created manual checkins to simulate partial coverage setup
	_, err = testDB.ExecContext(t.Context(), `DELETE FROM manual_checkins WHERE child_id IN (?,?)`, sub.Children[0].ID, sub.Children[1].ID)
	require.NoError(t, err)
	_, err = testDB.ExecContext(t.Context(), `UPDATE guest_submissions SET checkins_backfilled_at = NULL WHERE public_id = ?`, sub.PublicID)
	require.NoError(t, err)
	require.NoError(t, s.UpdateSubmissionStatus(t.Context(), sub.PublicID, StatusEntered, time.Now().UTC()))

	// Simulate partially covered family: manually insert checkin for only first child.
	_, err = testDB.ExecContext(t.Context(),
		`INSERT INTO manual_checkins (public_id, child_id, first_name, last_name, checked_out_at, checked_out_confirmed_at) VALUES (?, ?, ?, ?, NULL, NULL)`,
		uuid.New().String(), sub.Children[0].ID, sub.Children[0].FirstName, sub.Children[0].LastName)
	require.NoError(t, err)

	// WithoutManualCheckins should include partially covered family (per-child semantics).
	res, err := s.ListSubmissions(t.Context(), Filter{Status: StatusEntered, WithoutManualCheckins: true})
	require.NoError(t, err)
	found := false
	for _, r := range res {
		if r.PublicID == sub.PublicID {
			found = true
			break
		}
	}
	assert.True(t, found, "partially covered family should be visible via WithoutManualCheckins")

	// CreateManualCheckins should backfill only the missing child.
	require.NoError(t, s.CreateManualCheckins(t.Context(), sub.PublicID))

	for _, child := range sub.Children {
		var count int
		err := testDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM manual_checkins WHERE child_id = ?", child.ID).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "each child should have exactly 1 manual_checkins row after partial backfill (child %d)", child.ID)
	}

	// Second call is still idempotent.
	require.NoError(t, s.CreateManualCheckins(t.Context(), sub.PublicID))
	var total int
	err = testDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM manual_checkins WHERE child_id IN (?,?)", sub.Children[0].ID, sub.Children[1].ID).Scan(&total)
	require.NoError(t, err)
	assert.Equal(t, 2, total)

	// Now fully covered family should be hidden from WithoutManualCheckins.
	res, err = s.ListSubmissions(t.Context(), Filter{Status: StatusEntered, WithoutManualCheckins: true})
	require.NoError(t, err)
	for _, r := range res {
		assert.NotEqual(t, sub.PublicID, r.PublicID, "fully covered family should not appear in WithoutManualCheckins")
	}
}

func Test_sqliteRepo_ListSubmissions_WithoutManualCheckins_PartialAfterCleanup(t *testing.T) {
	wipeAll(t)
	s := NewRepo(testDB)

	sub, err := s.CreateSubmission(t.Context(), Parent{
		FirstName: "Cleanup", LastName: "Family", Phone: "555-1234", Email: "cleanup@test.com",

		Address1: "123 Main St",
		City:     "Seattle",
		State:    "WA",
		Zip:      "98101"}, []Child{
		{FirstName: "Kid1", LastName: "Family", DOB: "2020-01-01", Grade: "k", Gender: "Boy", Relationship: "Parent"},
		{FirstName: "Kid2", LastName: "Family", DOB: "2019-02-02", Grade: "1", Gender: "Boy", Relationship: "Parent"},
	}, true)
	require.NoError(t, err)
	require.NoError(t, s.UpdateSubmissionStatus(t.Context(), sub.PublicID, StatusEntered, time.Now().UTC()))
	require.NoError(t, s.CreateManualCheckins(t.Context(), sub.PublicID))

	// Simulate RemoveOldManualCheckins deleting only one child's row (per-row delete).
	_, err = testDB.ExecContext(t.Context(), "DELETE FROM manual_checkins WHERE child_id = ?", sub.Children[0].ID)
	require.NoError(t, err)

	// After backfill, deletion of a manual checkin should NOT resurrect the family.
	res, err := s.ListSubmissions(t.Context(), Filter{Status: StatusEntered, WithoutManualCheckins: true})
	require.NoError(t, err)
	found := false
	for _, r := range res {
		if r.PublicID == sub.PublicID {
			found = true
			break
		}
	}
	assert.False(t, found, "cleaned family should stay hidden after per-row delete (checkins_backfilled_at prevents resurrection)")

	// Backfill should restore missing child only and keep family hidden.
	require.NoError(t, s.CreateManualCheckins(t.Context(), sub.PublicID))
	for _, child := range sub.Children {
		var count int
		err := testDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM manual_checkins WHERE child_id = ?", child.ID).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	}
	res, err = s.ListSubmissions(t.Context(), Filter{Status: StatusEntered, WithoutManualCheckins: true})
	require.NoError(t, err)
	for _, r := range res {
		assert.NotEqual(t, sub.PublicID, r.PublicID, "family should remain hidden after backfill")
	}
}

func Test_sqliteRepo_CreateSubmission_ErrorBranches(t *testing.T) {
	wipeAll(t)
	s := NewRepo(testDB)

	t.Run("zero children returns error", func(t *testing.T) {
		_, err := s.CreateSubmission(t.Context(), Parent{
			FirstName: "No", LastName: "Kids", Phone: "555-1234", Email: "nokids@test.com",

			Address1: "123 Main St",
			City:     "Seattle",
			State:    "WA",
			Zip:      "98101"}, []Child{}, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one child")

		var count int
		require.NoError(t, testDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM parents").Scan(&count))
		assert.Equal(t, 0, count)
		var subCount int
		require.NoError(t, testDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM guest_submissions").Scan(&subCount))
		assert.Equal(t, 0, subCount)
	})

	t.Run("empty phone and email rolls back no orphan parent", func(t *testing.T) {
		wipeAll(t)
		_, err := s.CreateSubmission(t.Context(), Parent{
			FirstName: "No", LastName: "Contact", Phone: "", Email: "",

			Address1: "123 Main St",
			City:     "Seattle",
			State:    "WA",
			Zip:      "98101"}, []Child{{FirstName: "Kid", LastName: "Contact", DOB: "2020-01-01", Grade: "k", Gender: "Boy", Relationship: "Parent"}}, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "inserting parent")

		var parentCount int
		require.NoError(t, testDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM parents").Scan(&parentCount))
		assert.Equal(t, 0, parentCount, "no orphan parent row should persist after CHECK failure")
		var subCount int
		require.NoError(t, testDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM guest_submissions").Scan(&subCount))
		assert.Equal(t, 0, subCount)
		var childCount int
		require.NoError(t, testDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM children").Scan(&childCount))
		assert.Equal(t, 0, childCount)
	})
}
