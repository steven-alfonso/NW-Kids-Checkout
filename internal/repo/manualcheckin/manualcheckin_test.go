package manualcheckin

import (
	"database/sql"
	"log"
	"os"
	"testing"
	"time"

	"kids-checkin/internal/db"

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

func Test_sqliteRepo_ListManualCheckins(t *testing.T) {
	_, err := squirrel.Delete("manual_checkins").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)

	s := NewRepo(testDB)

	time1 := time.Date(2022, 1, 1, 12, 18, 32, 0, time.UTC)
	time2 := time1.Add(time.Hour * 24)
	confirmed1 := time1.Add(30 * time.Minute)
	confirmed2 := time1.Add(45 * time.Minute)
	confirmed3 := time2.Add(15 * time.Minute)

	checkin1, err := s.CreateManualCheckin(t.Context(), ManualCheckin{
		PublicID:              "public-1",
		FirstName:             "sam",
		LastName:              "alpha",
		CheckedOutAt:          time1,
		CheckedOutConfirmedAt: confirmed1,
	})
	require.NoError(t, err)

	checkin2, err := s.CreateManualCheckin(t.Context(), ManualCheckin{
		PublicID:              "public-2",
		FirstName:             "sam",
		LastName:              "bravo",
		CheckedOutAt:          time1,
		CheckedOutConfirmedAt: confirmed2,
	})
	require.NoError(t, err)

	checkin3, err := s.CreateManualCheckin(t.Context(), ManualCheckin{
		PublicID:              "public-3",
		FirstName:             "alex",
		LastName:              "alpha",
		CheckedOutAt:          time2,
		CheckedOutConfirmedAt: confirmed3,
	})
	require.NoError(t, err)

	checkin4, err := s.CreateManualCheckin(t.Context(), ManualCheckin{
		PublicID:  "public-4",
		FirstName: "taylor",
		LastName:  "delta",
	})
	require.NoError(t, err)

	t.Run("no filter", func(t *testing.T) {
		c, err := s.ListManualCheckins(t.Context(), Filter{})
		require.NoError(t, err)
		assert.Lenf(t, c, 4, "expected 4 manual checkins, got %d", len(c))
	})

	t.Run("filter by ID", func(t *testing.T) {
		c, err := s.ListManualCheckins(t.Context(), Filter{ID: checkin2.ID})
		require.NoError(t, err)
		require.Lenf(t, c, 1, "expected 1 manual checkins, got %d", len(c))
		assert.Equal(t, checkin2.ID, c[0].ID)
		assert.Equal(t, checkin2.PublicID, c[0].PublicID)
		assert.Equal(t, confirmed2, c[0].CheckedOutConfirmedAt)
	})

	t.Run("filter by public ID", func(t *testing.T) {
		c, err := s.ListManualCheckins(t.Context(), Filter{PublicID: "public-3"})
		require.NoError(t, err)
		require.Lenf(t, c, 1, "expected 1 manual checkins, got %d", len(c))
		assert.Equal(t, checkin3.ID, c[0].ID)
		assert.Equal(t, "public-3", c[0].PublicID)
	})

	t.Run("filter by first name", func(t *testing.T) {
		c, err := s.ListManualCheckins(t.Context(), Filter{FirstName: "sam"})
		require.NoError(t, err)
		require.Len(t, c, 2)
	})

	t.Run("filter by last name", func(t *testing.T) {
		c, err := s.ListManualCheckins(t.Context(), Filter{LastName: "alpha"})
		require.NoError(t, err)
		require.Len(t, c, 2)
	})

	t.Run("filter by checked out before", func(t *testing.T) {
		c, err := s.ListManualCheckins(t.Context(), Filter{CheckedOutAtBefore: time2})
		require.NoError(t, err)
		require.Len(t, c, 2)
	})

	t.Run("filter by checked out after", func(t *testing.T) {
		c, err := s.ListManualCheckins(t.Context(), Filter{CheckedOutAtAfter: time1})
		require.NoError(t, err)
		require.Len(t, c, 1)
		assert.Equal(t, checkin3.ID, c[0].ID)
	})

	t.Run("filter by recent", func(t *testing.T) {
		c, err := s.ListManualCheckins(t.Context(), Filter{Recent: true})
		require.NoError(t, err)
		require.Len(t, c, 4)
		assert.Equal(t, checkin3.ID, c[0].ID)
		assert.ElementsMatch(t, []int64{checkin1.ID, checkin2.ID, checkin4.ID}, []int64{c[1].ID, c[2].ID, c[3].ID})
	})

	t.Run("filter by limit", func(t *testing.T) {
		c, err := s.ListManualCheckins(t.Context(), Filter{Limit: 1, Recent: true})
		require.NoError(t, err)
		require.Len(t, c, 1)
		assert.Equal(t, checkin3.ID, c[0].ID)
	})

	t.Run("filter by checked out after with include unchecked", func(t *testing.T) {
		c, err := s.ListManualCheckins(t.Context(), Filter{CheckedOutAtAfter: time1, IncludeUnchecked: true})
		require.NoError(t, err)
		require.Len(t, c, 2)
		assert.ElementsMatch(t, []int64{checkin3.ID, checkin4.ID}, []int64{c[0].ID, c[1].ID})
	})

	_ = checkin1
}

func Test_sqliteRepo_CreateManualCheckin(t *testing.T) {
	s := NewRepo(testDB)
	_, err := squirrel.Delete("manual_checkins").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)

	tests := []struct {
		name      string
		arg       ManualCheckin
		expected  ManualCheckin
		expectErr bool
	}{
		{
			name: "create manual checkin",
			arg: ManualCheckin{
				PublicID:              "public-1",
				FirstName:             "somefirstname",
				LastName:              "somelastname",
				CheckedOutAt:          time.Date(2022, 1, 1, 12, 0, 0, 0, time.UTC),
				CheckedOutConfirmedAt: time.Date(2022, 1, 1, 12, 30, 0, 0, time.UTC),
			},
		},
		{
			name: "create manual checkin 2",
			arg: ManualCheckin{
				FirstName:             "somefirstname",
				LastName:              "somelastname",
				CheckedOutAt:          time.Time{},
				CheckedOutConfirmedAt: time.Time{},
			},
		},
		{
			name: "create manual checkin with child id",
			arg: ManualCheckin{
				FirstName: "somefirstname",
				LastName:  "somelastname",
				ChildID:   1,
			},
		},
		{
			name: "reject missing first name",
			arg: ManualCheckin{
				LastName: "somelastname",
			},
			expectErr: true,
		},
		{
			name: "reject missing last name",
			arg: ManualCheckin{
				FirstName: "somefirstname",
			},
			expectErr: true,
		},
		{
			name: "reject whitespace first name",
			arg: ManualCheckin{
				FirstName: "   ",
				LastName:  "somelastname",
			},
			expectErr: true,
		},
		{
			name: "reject missing names and child id",
			arg: ManualCheckin{
				ChildID: 0,
			},
			expectErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := s.CreateManualCheckin(t.Context(), tt.arg)
			if tt.expectErr {
				assert.ErrorIs(t, err, ErrInvalidManualCheckin)
				return
			}
			require.NoError(t, err)
			assert.NotZero(t, actual.ID)
			assert.WithinDuration(t, time.Now().UTC(), actual.CreatedAt, 5*time.Second, "CreatedAt should be set to current time")
			if tt.arg.PublicID == "" {
				assert.NotEmpty(t, actual.PublicID)
				_, parseErr := uuid.Parse(actual.PublicID)
				require.NoError(t, parseErr)
			} else {
				assert.Equal(t, tt.arg.PublicID, actual.PublicID)
			}
			assert.Equal(t, tt.arg.FirstName, actual.FirstName)
			assert.Equal(t, tt.arg.LastName, actual.LastName)
			assert.Equal(t, tt.arg.CheckedOutAt, actual.CheckedOutAt)
			assert.Equal(t, tt.arg.CheckedOutConfirmedAt, actual.CheckedOutConfirmedAt)

			manualCheckins, err := s.ListManualCheckins(t.Context(), Filter{
				ID: actual.ID,
			})
			require.NoError(t, err)
			require.Len(t, manualCheckins, 1)

			assert.Equal(t, actual.ID, manualCheckins[0].ID)
			assert.Equal(t, actual.CreatedAt, manualCheckins[0].CreatedAt)
			assert.Equal(t, actual.PublicID, manualCheckins[0].PublicID)
			assert.Equal(t, actual.FirstName, manualCheckins[0].FirstName)
			assert.Equal(t, actual.LastName, manualCheckins[0].LastName)
			assert.Equal(t, actual.CheckedOutAt, manualCheckins[0].CheckedOutAt)
			assert.Equal(t, actual.CheckedOutConfirmedAt, manualCheckins[0].CheckedOutConfirmedAt)
		})
	}
}

func Test_sqliteRepo_RemoveOldManualCheckins(t *testing.T) {
	s := NewRepo(testDB)

	tests := []struct {
		name      string
		olderThan time.Time
		expected  int64
		expectErr bool
		beforeFn  func(t *testing.T)
		checkFn   func(t *testing.T)
	}{
		{
			name:      "remove older than 1 day",
			olderThan: time.Now().Add(-24 * time.Hour),
			expected:  1,
			expectErr: false,
			beforeFn: func(t *testing.T) {
				_, err := squirrel.Delete("manual_checkins").RunWith(testDB).ExecContext(t.Context())
				require.NoError(t, err)
				_, err = s.CreateManualCheckin(t.Context(), ManualCheckin{
					FirstName:    "somefirstname1",
					LastName:     "somelastname1",
					CheckedOutAt: time.Now().Add(-24 * time.Hour).Add(-1 * time.Millisecond),
				})
				require.NoError(t, err)
				_, err = s.CreateManualCheckin(t.Context(), ManualCheckin{
					FirstName:    "somefirstname2",
					LastName:     "somelastname2",
					CheckedOutAt: time.Now().Add(-2 * time.Hour),
				})
				require.NoError(t, err)
			},
			checkFn: func(t *testing.T) {
				manualCheckins, err := s.ListManualCheckins(t.Context(), Filter{})
				require.NoError(t, err)
				require.Len(t, manualCheckins, 1, "unexpected number of manual checkins")
				assert.Equal(t, "somefirstname2", manualCheckins[0].FirstName, "unexpected manual checkin")
			},
		},
		{
			name:      "older than in the future",
			olderThan: time.Now().Add(2 * time.Hour),
			expected:  0,
			expectErr: false,
			beforeFn: func(t *testing.T) {
				_, err := squirrel.Delete("manual_checkins").RunWith(testDB).ExecContext(t.Context())
				require.NoError(t, err)
				_, err = s.CreateManualCheckin(t.Context(), ManualCheckin{
					FirstName:    "future",
					LastName:     "checkin",
					CheckedOutAt: time.Now().Add(-10 * time.Minute),
				})
				require.NoError(t, err)
			},
			checkFn: func(t *testing.T) {
				manualCheckins, err := s.ListManualCheckins(t.Context(), Filter{})
				require.NoError(t, err)
				require.Len(t, manualCheckins, 1)
				assert.Equal(t, "future", manualCheckins[0].FirstName)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.beforeFn != nil {
				tt.beforeFn(t)
			}

			actual, err := s.RemoveOldManualCheckins(t.Context(), tt.olderThan)
			if tt.expectErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, actual)
			if tt.checkFn != nil {
				tt.checkFn(t)
			}
		})
	}
}

func Test_sqliteRepo_SetManualCheckedOutAt(t *testing.T) {
	s := NewRepo(testDB)
	_, err := squirrel.Delete("manual_checkins").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)

	confirmed := time.Now().UTC().Add(-15 * time.Minute)
	created, err := s.CreateManualCheckin(t.Context(), ManualCheckin{
		FirstName:             "casey",
		LastName:              "quinn",
		CheckedOutAt:          time.Now().UTC().Add(-30 * time.Minute),
		CheckedOutConfirmedAt: confirmed,
	})
	require.NoError(t, err)

	t.Run("clear checked out and confirmation", func(t *testing.T) {
		updated, err := s.SetManualCheckedOutAt(t.Context(), created.ID, false)
		require.NoError(t, err)
		assert.True(t, updated.CheckedOutAt.IsZero())
		assert.True(t, updated.CheckedOutConfirmedAt.IsZero())

		rows, err := s.ListManualCheckins(t.Context(), Filter{ID: created.ID, Limit: 1})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.True(t, rows[0].CheckedOutAt.IsZero())
		assert.True(t, rows[0].CheckedOutConfirmedAt.IsZero())
	})

	t.Run("set checked out timestamp", func(t *testing.T) {
		updated, err := s.SetManualCheckedOutAt(t.Context(), created.ID, true)
		require.NoError(t, err)
		assert.WithinDuration(t, time.Now().UTC(), updated.CheckedOutAt, 2*time.Second)
	})
}

func Test_sqliteRepo_SetManualCheckedOutConfirmedAt(t *testing.T) {
	s := NewRepo(testDB)
	_, err := squirrel.Delete("manual_checkins").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)

	created, err := s.CreateManualCheckin(t.Context(), ManualCheckin{
		PublicID:     "public-1",
		FirstName:    "first",
		LastName:     "last",
		CheckedOutAt: time.Date(2022, 1, 1, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	before := time.Now().UTC()
	updated, err := s.SetManualCheckedOutConfirmedAt(t.Context(), created.ID, true)
	require.NoError(t, err)
	assert.Equal(t, created.ID, updated.ID)
	assert.WithinDuration(t, time.Now().UTC(), updated.CheckedOutConfirmedAt, 2*time.Second)
	assert.True(t, updated.CheckedOutConfirmedAt.After(before) || updated.CheckedOutConfirmedAt.Equal(before))

	updated, err = s.SetManualCheckedOutConfirmedAt(t.Context(), created.ID, false)
	require.NoError(t, err)
	assert.True(t, updated.CheckedOutConfirmedAt.IsZero())
}

func Test_sqliteRepo_CreateManualCheckinWithChildID(t *testing.T) {
	_, err := squirrel.Delete("manual_checkins").RunWith(testDB).ExecContext(t.Context())
	require.NoError(t, err)

	s := NewRepo(testDB)
	created, err := s.CreateManualCheckin(t.Context(), ManualCheckin{
		ChildID:   42,
		FirstName: "timmy",
		LastName:  "smith",
	})
	require.NoError(t, err)

	rows, err := s.ListManualCheckins(t.Context(), Filter{ID: created.ID, Limit: 1})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(42), rows[0].ChildID)
}
