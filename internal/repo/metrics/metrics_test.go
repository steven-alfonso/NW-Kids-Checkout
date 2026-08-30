package metrics

import (
	"database/sql"
	"testing"
	"time"

	"kids-checkin/internal/db"

	"github.com/Masterminds/squirrel"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	testDB  *sql.DB
	eventID int64
	locID   int64
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	res, err := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id").
		Values("Kids Service", "pc_evt_metrics").
		ExecContext(t.Context())
	require.NoError(t, err)
	eventID, err := res.LastInsertId()
	require.NoError(t, err)

	res, err = squirrel.Insert("locations").
		RunWith(testDB).
		Columns("name", "planning_center_id", "event_id").
		Values("Room 1", "pc_loc_metrics", eventID).
		ExecContext(t.Context())
	require.NoError(t, err)
	locID, err := res.LastInsertId()
	require.NoError(t, err)

	return fixture{testDB: testDB, eventID: eventID, locID: locID}
}

func (f fixture) insertCheckin(t *testing.T, pcID string, checkedOutAt time.Time, confirmedAt *time.Time) {
	t.Helper()
	f.insertCheckinWithFetchedAt(t, pcID, checkedOutAt, confirmedAt, nil)
}

func (f fixture) insertCheckinWithFetchedAt(t *testing.T, pcID string, checkedOutAt time.Time, confirmedAt *time.Time, fetchedAt *time.Time) {
	t.Helper()
	_, err := squirrel.Insert("checkins").
		RunWith(f.testDB).
		Columns("planning_center_id", "location_id", "event_id", "first_name", "last_name", "security_code", "checked_out_at", "checked_out_confirmed_at", "fetched_at").
		Values(pcID, f.locID, f.eventID, "Alice", "Smith", "SEC1", checkedOutAt, confirmedAt, fetchedAt).
		ExecContext(t.Context())
	require.NoError(t, err)
}

func (f fixture) insertManualCheckin(t *testing.T) {
	t.Helper()
	_, err := squirrel.Insert("manual_checkins").
		RunWith(f.testDB).
		Columns("first_name", "last_name").
		Values("Manny", "Manual").
		ExecContext(t.Context())
	require.NoError(t, err)
}

func today(t *testing.T) string {
	t.Helper()
	return time.Now().UTC().Format("2006-01-02")
}

func findMetric(t *testing.T, daily []DailyMetric, eventName string) DailyMetric {
	t.Helper()
	for _, dm := range daily {
		if dm.EventName == eventName {
			return dm
		}
	}
	return DailyMetric{}
}

func Test_sqliteRepo_ListDailyMetrics_confirmed(t *testing.T) {
	f := newFixture(t)

	checkedOut := time.Now().UTC().Add(-10 * time.Minute)
	confirmed := checkedOut.Add(5 * time.Minute)
	f.insertCheckin(t, "chk_confirmed", checkedOut, &confirmed)

	repo := NewRepo(f.testDB)
	daily, err := repo.ListDailyMetrics(t.Context(), Filter{Days: 14})
	require.NoError(t, err)

	dm := findMetric(t, daily, "Kids Service")
	require.NotEmpty(t, dm, "expected a Kids Service metric row")
	assert.Equal(t, today(t), dm.Date)
	assert.Equal(t, 1, dm.Called)
	assert.Equal(t, 1, dm.Confirmed)
	assert.Equal(t, 0, dm.Unconfirmed)
	assert.InDelta(t, 5, dm.AvgConfirmMinutes, 0.01)
}

func Test_sqliteRepo_ListDailyMetrics_unconfirmed(t *testing.T) {
	f := newFixture(t)

	checkedOut := time.Now().UTC().Add(-10 * time.Minute)
	f.insertCheckin(t, "chk_unconfirmed", checkedOut, nil)

	repo := NewRepo(f.testDB)
	daily, err := repo.ListDailyMetrics(t.Context(), Filter{Days: 14})
	require.NoError(t, err)

	dm := findMetric(t, daily, "Kids Service")
	require.NotEmpty(t, dm, "expected a Kids Service metric row")
	assert.Equal(t, today(t), dm.Date)
	assert.Equal(t, 1, dm.Called)
	assert.Equal(t, 0, dm.Confirmed)
	assert.Equal(t, 1, dm.Unconfirmed)
	assert.Equal(t, float64(0), dm.AvgConfirmMinutes)
}

func Test_sqliteRepo_ListDailyMetrics_excludes_manual(t *testing.T) {
	f := newFixture(t)

	checkedOut := time.Now().UTC().Add(-10 * time.Minute)
	f.insertCheckin(t, "chk_manual", checkedOut, nil)
	f.insertManualCheckin(t)

	repo := NewRepo(f.testDB)
	daily, err := repo.ListDailyMetrics(t.Context(), Filter{Days: 14})
	require.NoError(t, err)

	require.Len(t, daily, 1, "manual checkins are not tied to an event and must not appear in daily metrics")
	assert.Equal(t, "Kids Service", daily[0].EventName)
}

func Test_sqliteRepo_ListDailyMetrics_excludes_manual_only_days(t *testing.T) {
	f := newFixture(t)

	f.insertManualCheckin(t)

	repo := NewRepo(f.testDB)
	daily, err := repo.ListDailyMetrics(t.Context(), Filter{Days: 14})
	require.NoError(t, err)

	require.Empty(t, daily, "days with only manual checkins must not appear in daily metrics")
}

func Test_sqliteRepo_ListDailyMetrics_days_filter(t *testing.T) {
	f := newFixture(t)

	checkedOutToday := time.Now().UTC().Add(-10 * time.Minute)
	f.insertCheckin(t, "chk_today", checkedOutToday, nil)

	old := time.Now().UTC().AddDate(0, 0, -3)
	f.insertCheckin(t, "chk_old", old, nil)

	repo := NewRepo(f.testDB)

	daily, err := repo.ListDailyMetrics(t.Context(), Filter{Days: 0})
	require.NoError(t, err)
	require.Len(t, daily, 2, "Days:0 should default to 14 and include both rows")

	daily, err = repo.ListDailyMetrics(t.Context(), Filter{Days: 1})
	require.NoError(t, err)
	require.Len(t, daily, 1, "Days:1 should exclude the 3-day-old row")
	assert.Equal(t, today(t), daily[0].Date)
	assert.Equal(t, "Kids Service", daily[0].EventName)
}

func findFetchLatency(t *testing.T, rows []FetchLatencyMetric, date string) FetchLatencyMetric {
	t.Helper()
	for _, fm := range rows {
		if fm.Date == date {
			return fm
		}
	}
	return FetchLatencyMetric{}
}

func Test_sqliteRepo_ListFetchLatency_aggregates(t *testing.T) {
	f := newFixture(t)

	checkedOut := time.Now().UTC().Add(-10 * time.Minute)
	// Deltas in milliseconds: 1000, 2000, 3000, 4000, 5000
	fetched := checkedOut.Add(1000 * time.Millisecond)
	f.insertCheckinWithFetchedAt(t, "lat_1", checkedOut, nil, &fetched)
	fetched = checkedOut.Add(2000 * time.Millisecond)
	f.insertCheckinWithFetchedAt(t, "lat_2", checkedOut, nil, &fetched)
	fetched = checkedOut.Add(3000 * time.Millisecond)
	f.insertCheckinWithFetchedAt(t, "lat_3", checkedOut, nil, &fetched)
	fetched = checkedOut.Add(4000 * time.Millisecond)
	f.insertCheckinWithFetchedAt(t, "lat_4", checkedOut, nil, &fetched)
	fetched = checkedOut.Add(5000 * time.Millisecond)
	f.insertCheckinWithFetchedAt(t, "lat_5", checkedOut, nil, &fetched)

	repo := NewRepo(f.testDB)
	rows, err := repo.ListFetchLatency(t.Context(), Filter{Days: 14})
	require.NoError(t, err)

	fm := findFetchLatency(t, rows, today(t))
	require.NotEmpty(t, fm, "expected a latency row for today")
	assert.Equal(t, 5, fm.Count)
	assert.InDelta(t, 3000, fm.AvgMs, 0.01)
	assert.Equal(t, float64(5000), fm.P95Ms, "nearest-rank p95 of 5 values should be the max")
	assert.Equal(t, float64(5000), fm.P99Ms, "nearest-rank p99 of 5 values should be the max")
}

func Test_sqliteRepo_ListFetchLatency_excludes_null_and_negative(t *testing.T) {
	f := newFixture(t)

	checkedOut := time.Now().UTC().Add(-10 * time.Minute)
	// Valid latency.
	fetched := checkedOut.Add(2 * time.Second)
	f.insertCheckinWithFetchedAt(t, "lat_ok", checkedOut, nil, &fetched)
	// NULL fetched_at -> excluded.
	f.insertCheckin(t, "lat_null", checkedOut, nil)
	// Negative delta (clock skew) -> excluded.
	before := checkedOut.Add(-1 * time.Second)
	f.insertCheckinWithFetchedAt(t, "lat_neg", checkedOut, nil, &before)

	repo := NewRepo(f.testDB)
	rows, err := repo.ListFetchLatency(t.Context(), Filter{Days: 14})
	require.NoError(t, err)

	fm := findFetchLatency(t, rows, today(t))
	require.NotEmpty(t, fm, "expected a latency row for today")
	assert.Equal(t, 1, fm.Count, "NULL fetched_at and negative deltas should be excluded")
	assert.InDelta(t, 2000, fm.AvgMs, 0.01)
}

func (f fixture) insertGuestSubmission(t *testing.T, publicID string, createdAt time.Time, kids int, approvedAt, rejectedAt, enteredAt *time.Time) {
	t.Helper()
	res, err := squirrel.Insert("parents").
		RunWith(f.testDB).
		Columns("first_name", "last_name", "phone", "email", "created_at").
		Values("Guest", "Parent", "1234567", "guest@example.com", createdAt).
		ExecContext(t.Context())
	require.NoError(t, err)
	parentID, err := res.LastInsertId()
	require.NoError(t, err)

	for range kids {
		_, err := squirrel.Insert("children").
			RunWith(f.testDB).
			Columns("parent_id", "first_name", "last_name", "dob", "grade", "created_at").
			Values(parentID, "Kid", "One", "2020-01-01", "k", createdAt).
			ExecContext(t.Context())
		require.NoError(t, err)
	}

	_, err = squirrel.Insert("guest_submissions").
		RunWith(f.testDB).
		Columns("public_id", "parent_id", "approved_at", "rejected_at", "entered_at", "created_at").
		Values(publicID, parentID, approvedAt, rejectedAt, enteredAt, createdAt).
		ExecContext(t.Context())
	require.NoError(t, err)
}

func findGuestMetric(t *testing.T, rows []GuestMetric, date string) GuestMetric {
	t.Helper()
	for _, gm := range rows {
		if gm.Date == date {
			return gm
		}
	}
	return GuestMetric{}
}

func Test_sqliteRepo_ListGuestMetrics_status_breakdown(t *testing.T) {
	f := newFixture(t)

	now := time.Now().UTC()
	f.insertGuestSubmission(t, "sub_entered", now, 2, nil, nil, &now)
	f.insertGuestSubmission(t, "sub_approved", now, 1, &now, nil, nil)
	f.insertGuestSubmission(t, "sub_rejected", now, 3, nil, &now, nil)
	f.insertGuestSubmission(t, "sub_pending", now, 2, nil, nil, nil)
	f.insertGuestSubmission(t, "sub_entered_and_approved", now, 1, &now, nil, &now)

	repo := NewRepo(f.testDB)
	rows, err := repo.ListGuestMetrics(t.Context(), Filter{Days: 14})
	require.NoError(t, err)

	gm := findGuestMetric(t, rows, today(t))
	require.NotEmpty(t, gm, "expected a guest metric row for today")
	assert.Equal(t, 5, gm.Submissions)
	assert.Equal(t, 9, gm.Children)
	assert.Equal(t, 2, gm.Entered, "entered-beats-approved precedence should count both entered rows")
	assert.Equal(t, 1, gm.Approved)
	assert.Equal(t, 1, gm.Rejected)
	assert.Equal(t, 1, gm.Pending)
}

func Test_sqliteRepo_ListGuestMetrics_days_filter(t *testing.T) {
	f := newFixture(t)

	now := time.Now().UTC()
	f.insertGuestSubmission(t, "sub_today", now, 1, nil, nil, nil)
	old := now.AddDate(0, 0, -3)
	f.insertGuestSubmission(t, "sub_old", old, 1, nil, nil, nil)

	repo := NewRepo(f.testDB)

	rows, err := repo.ListGuestMetrics(t.Context(), Filter{Days: 0})
	require.NoError(t, err)
	require.Len(t, rows, 2, "Days:0 should default to 14 and include both rows")

	rows, err = repo.ListGuestMetrics(t.Context(), Filter{Days: 1})
	require.NoError(t, err)
	require.Len(t, rows, 1, "Days:1 should exclude the 3-day-old row")
	assert.Equal(t, today(t), rows[0].Date)
}

func Test_sqliteRepo_ListFetchLatency_days_filter(t *testing.T) {
	f := newFixture(t)

	checkedOutToday := time.Now().UTC().Add(-10 * time.Minute)
	fetched := checkedOutToday.Add(3 * time.Second)
	f.insertCheckinWithFetchedAt(t, "lat_today", checkedOutToday, nil, &fetched)

	old := time.Now().UTC().AddDate(0, 0, -3)
	fetchedOld := old.Add(3 * time.Second)
	f.insertCheckinWithFetchedAt(t, "lat_old", old, nil, &fetchedOld)

	repo := NewRepo(f.testDB)

	rows, err := repo.ListFetchLatency(t.Context(), Filter{Days: 0})
	require.NoError(t, err)
	require.Len(t, rows, 2, "Days:0 should default to 14 and include both rows")

	rows, err = repo.ListFetchLatency(t.Context(), Filter{Days: 1})
	require.NoError(t, err)
	require.Len(t, rows, 1, "Days:1 should exclude the 3-day-old row")
	assert.Equal(t, today(t), rows[0].Date)
}
