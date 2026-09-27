package eventcheckwindow

import (
	"testing"

	"kids-checkin/internal/db"
	"kids-checkin/internal/repo"

	"github.com/Masterminds/squirrel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_sqliteRepo_GetCheckWindowByID(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	windowRepo := NewRepo(testDB)

	eventRes, err := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id").
		Values("Sunday Service", "pc_evt_1").
		ExecContext(t.Context())
	require.NoError(t, err)
	eventID, _ := eventRes.LastInsertId()

	windowRes, err := squirrel.Insert("event_check_windows").
		RunWith(testDB).
		Columns("event_id", "start_day_of_week", "start_time", "end_day_of_week", "end_time", "timezone").
		Values(eventID, 1, "09:00", 1, "12:00", "America/Los_Angeles").
		ExecContext(t.Context())
	require.NoError(t, err)
	windowID, _ := windowRes.LastInsertId()

	got, err := windowRepo.GetCheckWindowByID(t.Context(), windowID)
	require.NoError(t, err)
	assert.Equal(t, windowID, got.ID)
	assert.Equal(t, eventID, got.EventID)
	assert.Equal(t, 1, got.StartDayOfWeek)
	assert.Equal(t, "09:00", got.StartTime)
	assert.Equal(t, 1, got.EndDayOfWeek)
	assert.Equal(t, "12:00", got.EndTime)
	assert.Equal(t, "America/Los_Angeles", got.Timezone)

	t.Run("not found", func(t *testing.T) {
		_, err := windowRepo.GetCheckWindowByID(t.Context(), 9999)
		assert.ErrorIs(t, err, repo.ErrNotFound)
	})
}

func Test_sqliteRepo_GetCheckWindowsForEvent(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	windowRepo := NewRepo(testDB)

	eventRes, err := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id").
		Values("Sunday Service", "pc_evt_1").
		ExecContext(t.Context())
	require.NoError(t, err)
	eventID, _ := eventRes.LastInsertId()

	_, err = squirrel.Insert("event_check_windows").
		RunWith(testDB).
		Columns("event_id", "start_day_of_week", "start_time", "end_day_of_week", "end_time", "timezone").
		Values(eventID, 1, "09:00", 1, "12:00", "America/Los_Angeles").
		ExecContext(t.Context())
	require.NoError(t, err)

	_, err = squirrel.Insert("event_check_windows").
		RunWith(testDB).
		Columns("event_id", "start_day_of_week", "start_time", "end_day_of_week", "end_time", "timezone").
		Values(eventID, 7, "17:00", 7, "20:00", "America/Los_Angeles").
		ExecContext(t.Context())
	require.NoError(t, err)

	windows, err := windowRepo.GetCheckWindowsForEvent(t.Context(), eventID)
	require.NoError(t, err)
	assert.Len(t, windows, 2)
	assert.Equal(t, 1, windows[0].StartDayOfWeek)
	assert.Equal(t, 7, windows[1].StartDayOfWeek)

	t.Run("no windows for event", func(t *testing.T) {
		otherRes, err := squirrel.Insert("events").
			RunWith(testDB).
			Columns("name", "planning_center_id").
			Values("Other Service", "pc_evt_2").
			ExecContext(t.Context())
		require.NoError(t, err)
		otherEventID, _ := otherRes.LastInsertId()

		windows, err := windowRepo.GetCheckWindowsForEvent(t.Context(), otherEventID)
		require.NoError(t, err)
		assert.Len(t, windows, 0)
	})
}

func Test_sqliteRepo_ListCheckWindows(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	windowRepo := NewRepo(testDB)

	eventRes, err := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id").
		Values("Sunday Service", "pc_evt_1").
		ExecContext(t.Context())
	require.NoError(t, err)
	eventID, _ := eventRes.LastInsertId()

	_, err = squirrel.Insert("event_check_windows").
		RunWith(testDB).
		Columns("event_id", "start_day_of_week", "start_time", "end_day_of_week", "end_time", "timezone").
		Values(eventID, 1, "09:00", 1, "12:00", "America/Los_Angeles").
		ExecContext(t.Context())
	require.NoError(t, err)

	// A second window for the same event so a positive Limit can be observed
	// truncating the result set.
	_, err = squirrel.Insert("event_check_windows").
		RunWith(testDB).
		Columns("event_id", "start_day_of_week", "start_time", "end_day_of_week", "end_time", "timezone").
		Values(eventID, 3, "18:00", 3, "20:00", "America/Los_Angeles").
		ExecContext(t.Context())
	require.NoError(t, err)

	t.Run("list all", func(t *testing.T) {
		windows, err := windowRepo.ListCheckWindows(t.Context(), Filter{})
		require.NoError(t, err)
		assert.Len(t, windows, 2)
	})

	t.Run("filter by id", func(t *testing.T) {
		all, _ := windowRepo.ListCheckWindows(t.Context(), Filter{})
		windows, err := windowRepo.ListCheckWindows(t.Context(), Filter{ID: all[0].ID})
		require.NoError(t, err)
		assert.Len(t, windows, 1)
		assert.Equal(t, all[0].ID, windows[0].ID)
	})

	t.Run("filter by event id", func(t *testing.T) {
		windows, err := windowRepo.ListCheckWindows(t.Context(), Filter{EventID: eventID})
		require.NoError(t, err)
		assert.Len(t, windows, 2)
	})

	t.Run("filter by limit truncates", func(t *testing.T) {
		windows, err := windowRepo.ListCheckWindows(t.Context(), Filter{Limit: 1})
		require.NoError(t, err)
		assert.Len(t, windows, 1)
	})
}

func Test_sqliteRepo_CreateCheckWindow(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	windowRepo := NewRepo(testDB)

	eventRes, err := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id").
		Values("Sunday Service", "pc_evt_1").
		ExecContext(t.Context())
	require.NoError(t, err)
	eventID, _ := eventRes.LastInsertId()

	created, err := windowRepo.CreateCheckWindow(t.Context(), EventCheckWindow{
		EventID:        eventID,
		StartDayOfWeek: 1,
		StartTime:      "9:00",
		EndDayOfWeek:   1,
		EndTime:        "12:00",
		Timezone:       "America/Los_Angeles",
	})
	require.NoError(t, err)
	require.NotZero(t, created.ID)
	assert.Equal(t, "09:00", created.StartTime)
	assert.Equal(t, "12:00", created.EndTime)

	loaded, err := windowRepo.GetCheckWindowByID(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, loaded.ID)

	// The validation rules themselves are covered by Test_validateWindow; this
	// only pins that CreateCheckWindow routes through it before writing.
	t.Run("invalid input calls validate", func(t *testing.T) {
		_, err := windowRepo.CreateCheckWindow(t.Context(), EventCheckWindow{
			EventID:        eventID,
			StartDayOfWeek: 0,
			StartTime:      "09:00",
			EndDayOfWeek:   1,
			EndTime:        "12:00",
			Timezone:       "America/Los_Angeles",
		})
		require.ErrorContains(t, err, "start_day_of_week must be between 1 and 7")
	})
}

func Test_sqliteRepo_UpdateCheckWindow(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	windowRepo := NewRepo(testDB)

	eventRes, err := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id").
		Values("Sunday Service", "pc_evt_1").
		ExecContext(t.Context())
	require.NoError(t, err)
	eventID, _ := eventRes.LastInsertId()

	windowRes, err := squirrel.Insert("event_check_windows").
		RunWith(testDB).
		Columns("event_id", "start_day_of_week", "start_time", "end_day_of_week", "end_time", "timezone").
		Values(eventID, 1, "09:00", 1, "12:00", "America/Los_Angeles").
		ExecContext(t.Context())
	require.NoError(t, err)
	windowID, _ := windowRes.LastInsertId()

	err = windowRepo.UpdateCheckWindow(t.Context(), EventCheckWindow{
		ID:             windowID,
		EventID:        eventID,
		StartDayOfWeek: 1,
		StartTime:      "10:00",
		EndDayOfWeek:   1,
		EndTime:        "13:00",
		Timezone:       "America/Los_Angeles",
	})
	require.NoError(t, err)

	loaded, err := windowRepo.GetCheckWindowByID(t.Context(), windowID)
	require.NoError(t, err)
	assert.Equal(t, "10:00", loaded.StartTime)
	assert.Equal(t, "13:00", loaded.EndTime)

	t.Run("not found", func(t *testing.T) {
		err := windowRepo.UpdateCheckWindow(t.Context(), EventCheckWindow{
			ID:             9999,
			EventID:        eventID,
			StartDayOfWeek: 1,
			StartTime:      "10:00",
			EndDayOfWeek:   1,
			EndTime:        "13:00",
			Timezone:       "America/Los_Angeles",
		})
		assert.ErrorIs(t, err, repo.ErrNotFound)
	})

	// Pins that UpdateCheckWindow validates before writing; see Test_validateWindow
	// for the rules themselves.
	t.Run("invalid input calls validate", func(t *testing.T) {
		err := windowRepo.UpdateCheckWindow(t.Context(), EventCheckWindow{
			ID:             windowID,
			EventID:        eventID,
			StartDayOfWeek: 0,
			StartTime:      "10:00",
			EndDayOfWeek:   1,
			EndTime:        "13:00",
			Timezone:       "America/Los_Angeles",
		})
		require.ErrorContains(t, err, "start_day_of_week must be between 1 and 7")
	})
}

func Test_sqliteRepo_DeleteCheckWindow(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)

	windowRepo := NewRepo(testDB)

	eventRes, err := squirrel.Insert("events").
		RunWith(testDB).
		Columns("name", "planning_center_id").
		Values("Sunday Service", "pc_evt_1").
		ExecContext(t.Context())
	require.NoError(t, err)
	eventID, _ := eventRes.LastInsertId()

	windowRes, err := squirrel.Insert("event_check_windows").
		RunWith(testDB).
		Columns("event_id", "start_day_of_week", "start_time", "end_day_of_week", "end_time", "timezone").
		Values(eventID, 1, "09:00", 1, "12:00", "America/Los_Angeles").
		ExecContext(t.Context())
	require.NoError(t, err)
	windowID, _ := windowRes.LastInsertId()

	err = windowRepo.DeleteCheckWindow(t.Context(), windowID)
	require.NoError(t, err)

	_, err = windowRepo.GetCheckWindowByID(t.Context(), windowID)
	assert.ErrorIs(t, err, repo.ErrNotFound)

	t.Run("not found", func(t *testing.T) {
		err := windowRepo.DeleteCheckWindow(t.Context(), 9999)
		assert.ErrorIs(t, err, repo.ErrNotFound)
	})
}

func Test_validateWindow(t *testing.T) {
	t.Run("valid window", func(t *testing.T) {
		window := EventCheckWindow{
			StartDayOfWeek: 1,
			StartTime:      "09:00",
			EndDayOfWeek:   1,
			EndTime:        "12:00",
			Timezone:       "America/Los_Angeles",
		}
		err := validateWindow(&window)
		assert.NoError(t, err)
	})

	t.Run("invalid start_day_of_week too low", func(t *testing.T) {
		window := EventCheckWindow{
			StartDayOfWeek: 0,
			StartTime:      "09:00",
			EndDayOfWeek:   1,
			EndTime:        "12:00",
			Timezone:       "America/Los_Angeles",
		}
		err := validateWindow(&window)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "start_day_of_week must be between 1 and 7")
	})

	t.Run("invalid start_day_of_week too high", func(t *testing.T) {
		window := EventCheckWindow{
			StartDayOfWeek: 8,
			StartTime:      "09:00",
			EndDayOfWeek:   1,
			EndTime:        "12:00",
			Timezone:       "America/Los_Angeles",
		}
		err := validateWindow(&window)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "start_day_of_week must be between 1 and 7")
	})

	t.Run("invalid end_day_of_week too low", func(t *testing.T) {
		window := EventCheckWindow{
			StartDayOfWeek: 1,
			StartTime:      "09:00",
			EndDayOfWeek:   0,
			EndTime:        "12:00",
			Timezone:       "America/Los_Angeles",
		}
		err := validateWindow(&window)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "end_day_of_week must be between 1 and 7")
	})

	t.Run("invalid end_day_of_week too high", func(t *testing.T) {
		window := EventCheckWindow{
			StartDayOfWeek: 1,
			StartTime:      "09:00",
			EndDayOfWeek:   8,
			EndTime:        "12:00",
			Timezone:       "America/Los_Angeles",
		}
		err := validateWindow(&window)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "end_day_of_week must be between 1 and 7")
	})

	t.Run("invalid start_time format", func(t *testing.T) {
		window := EventCheckWindow{
			StartDayOfWeek: 1,
			StartTime:      "900",
			EndDayOfWeek:   1,
			EndTime:        "12:00",
			Timezone:       "America/Los_Angeles",
		}
		err := validateWindow(&window)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid start_time format")
	})

	t.Run("invalid end_time format", func(t *testing.T) {
		window := EventCheckWindow{
			StartDayOfWeek: 1,
			StartTime:      "09:00",
			EndDayOfWeek:   1,
			EndTime:        "1200",
			Timezone:       "America/Los_Angeles",
		}
		err := validateWindow(&window)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid end_time format")
	})

	t.Run("invalid timezone", func(t *testing.T) {
		window := EventCheckWindow{
			StartDayOfWeek: 1,
			StartTime:      "09:00",
			EndDayOfWeek:   1,
			EndTime:        "12:00",
			Timezone:       "Invalid/Timezone",
		}
		err := validateWindow(&window)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid timezone")
	})

	t.Run("valid different timezone", func(t *testing.T) {
		window := EventCheckWindow{
			StartDayOfWeek: 1,
			StartTime:      "09:00",
			EndDayOfWeek:   1,
			EndTime:        "12:00",
			Timezone:       "UTC",
		}
		err := validateWindow(&window)
		assert.NoError(t, err)
	})

	t.Run("valid edge case day 7", func(t *testing.T) {
		window := EventCheckWindow{
			StartDayOfWeek: 7,
			StartTime:      "23:59",
			EndDayOfWeek:   7,
			EndTime:        "23:59",
			Timezone:       "America/New_York",
		}
		err := validateWindow(&window)
		assert.NoError(t, err)
	})
}
