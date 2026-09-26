package location

import (
	"testing"
	"time"

	"kids-checkin/internal/db"
	"kids-checkin/internal/repo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_sqliteRepo_ListLocations(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)
	s := &sqliteRepo{db: testDB}

	t.Run("empty table", func(t *testing.T) {
		locations, err := s.ListLocations(t.Context(), LocationFilter{})
		require.NoError(t, err)
		assert.Empty(t, locations)
	})

	t.Run("some filtered locations", func(t *testing.T) {
		group, err := s.CreateLocationGroup(t.Context(), LocationGroup{Name: "group-a"})
		require.NoError(t, err)
		parentID := "parent-1"
		lastCheckedOut := time.Date(2022, 1, 1, 10, 0, 0, 0, time.UTC)
		location, err := s.CreateLocation(t.Context(), Location{
			PlanningCenterID:       "pcloc_1234",
			PlanningCenterParentID: &parentID,
			EventID:                2,
			LocationGroupID:        &group.ID,
			Name:                   "Cool location",
			LastCheckedOutTime:     lastCheckedOut,
		})
		require.NoError(t, err)

		_, err = s.CreateLocation(t.Context(), Location{
			PlanningCenterID: "pcloc_5678",
			EventID:          3,
			Name:             "Other location",
		})
		require.NoError(t, err)

		locations, err := s.ListLocations(t.Context(), LocationFilter{IDs: []int64{location.ID}})
		require.NoError(t, err)
		require.Len(t, locations, 1)
		assert.Equal(t, location.ID, locations[0].ID)
		assert.Equal(t, "pcloc_1234", locations[0].PlanningCenterID)
		assert.NotNil(t, locations[0].PlanningCenterParentID)
		assert.Equal(t, parentID, *locations[0].PlanningCenterParentID)
		assert.NotNil(t, locations[0].LocationGroupID)
		assert.Equal(t, group.ID, *locations[0].LocationGroupID)
		assert.Equal(t, "Cool location", locations[0].Name)
		assert.Equal(t, lastCheckedOut, locations[0].LastCheckedOutTime)

		locations, err = s.ListLocations(t.Context(), LocationFilter{PlanningCenterID: "pcloc_1234"})
		require.NoError(t, err)
		assert.Len(t, locations, 1)

		locations, err = s.ListLocations(t.Context(), LocationFilter{Name: "Cool location"})
		require.NoError(t, err)
		assert.Len(t, locations, 1)

		locations, err = s.ListLocations(t.Context(), LocationFilter{LocationGroupID: group.ID})
		require.NoError(t, err)
		assert.Len(t, locations, 1)

		locations, err = s.ListLocations(t.Context(), LocationFilter{EventID: 2})
		require.NoError(t, err)
		assert.Len(t, locations, 1)
	})
}

func Test_sqliteRepo_CreateLocation(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)
	s := &sqliteRepo{db: testDB}

	t.Run("location created successfully", func(t *testing.T) {
		actual, err := s.CreateLocation(t.Context(), Location{
			PlanningCenterID: "pcloc_1234",
			Name:             "Cool location",
		})
		require.NoError(t, err)
		assert.NotZero(t, actual.ID)
		assert.Equal(t, "pcloc_1234", actual.PlanningCenterID)
		assert.Equal(t, "Cool location", actual.Name)

		locations, err := s.ListLocations(t.Context(), LocationFilter{
			PlanningCenterID: "pcloc_1234",
		})
		require.NoError(t, err)
		require.Len(t, locations, 1)
		assert.Equal(t, actual.ID, locations[0].ID)
		assert.Equal(t, actual.PlanningCenterID, locations[0].PlanningCenterID)
		assert.Equal(t, actual.Name, locations[0].Name)
	})

	t.Run("duplicate location should not be created", func(t *testing.T) {
		first, err := s.CreateLocation(t.Context(), Location{
			PlanningCenterID: "pcloc_1235",
			Name:             "another location",
		})
		require.NoError(t, err)

		second, err := s.CreateLocation(t.Context(), Location{
			PlanningCenterID: "pcloc_1235",
			Name:             "new location name",
		})
		require.NoError(t, err)

		assert.Equal(t, first.ID, second.ID, "duplicate location should not be created")
		assert.NotEqual(t, first.Name, second.Name, "duplicate location should have updated name")

		locations, err := s.ListLocations(t.Context(), LocationFilter{
			PlanningCenterID: "pcloc_1235",
		})
		require.NoError(t, err)
		require.Len(t, locations, 1)
		assert.Equal(t, second.ID, locations[0].ID)
		assert.Equal(t, second.Name, locations[0].Name)
		assert.Equal(t, second.PlanningCenterID, locations[0].PlanningCenterID)
	})

	t.Run("optional fields set", func(t *testing.T) {
		group, err := s.CreateLocationGroup(t.Context(), LocationGroup{Name: "group-b"})
		require.NoError(t, err)
		parentID := "parent-2"
		lastCheckedOut := time.Date(2023, 2, 2, 11, 0, 0, 0, time.UTC)
		location, err := s.CreateLocation(t.Context(), Location{
			PlanningCenterID:       "pcloc_9999",
			PlanningCenterParentID: &parentID,
			EventID:                3,
			LocationGroupID:        &group.ID,
			Name:                   "Optional location",
			LastCheckedOutTime:     lastCheckedOut,
		})
		require.NoError(t, err)

		locations, err := s.ListLocations(t.Context(), LocationFilter{IDs: []int64{location.ID}})
		require.NoError(t, err)
		require.Len(t, locations, 1)
		assert.NotNil(t, locations[0].PlanningCenterParentID)
		assert.Equal(t, parentID, *locations[0].PlanningCenterParentID)
		assert.NotNil(t, locations[0].LocationGroupID)
		assert.Equal(t, group.ID, *locations[0].LocationGroupID)
		assert.Equal(t, lastCheckedOut, locations[0].LastCheckedOutTime)
	})
}

func Test_sqliteRepo_UpdateLocation(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)
	s := &sqliteRepo{db: testDB}

	group, err := s.CreateLocationGroup(t.Context(), LocationGroup{Name: "group-update"})
	require.NoError(t, err)
	parentID := "parent-update"
	location, err := s.CreateLocation(t.Context(), Location{
		PlanningCenterID:       "pcloc_update",
		PlanningCenterParentID: &parentID,
		EventID:                4,
		LocationGroupID:        &group.ID,
		Name:                   "Original location",
	})
	require.NoError(t, err)

	updatedGroup, err := s.CreateLocationGroup(t.Context(), LocationGroup{Name: "group-updated"})
	require.NoError(t, err)
	updatedParent := "parent-updated"
	updatedTime := time.Date(2024, 3, 3, 12, 0, 0, 0, time.UTC)

	err = s.UpdateLocation(t.Context(), Location{
		ID:                     location.ID,
		PlanningCenterID:       "pcloc_update",
		PlanningCenterParentID: &updatedParent,
		EventID:                5,
		LocationGroupID:        &updatedGroup.ID,
		Name:                   "Updated location",
		LastCheckedOutTime:     updatedTime,
	})
	require.NoError(t, err)

	locations, err := s.ListLocations(t.Context(), LocationFilter{IDs: []int64{location.ID}})
	require.NoError(t, err)
	require.Len(t, locations, 1)
	assert.Equal(t, "Updated location", locations[0].Name)
	assert.Equal(t, updatedTime, locations[0].LastCheckedOutTime)
	assert.NotNil(t, locations[0].PlanningCenterParentID)
	assert.Equal(t, updatedParent, *locations[0].PlanningCenterParentID)
	assert.NotNil(t, locations[0].LocationGroupID)
	assert.Equal(t, updatedGroup.ID, *locations[0].LocationGroupID)
}

func Test_sqliteRepo_UpdateLocation_NotFound(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)
	s := &sqliteRepo{db: testDB}

	err = s.UpdateLocation(t.Context(), Location{
		ID:               999,
		PlanningCenterID: "missing",
		EventID:          1,
		Name:             "Missing",
	})
	assert.ErrorIs(t, err, repo.ErrNotFound)
}

func Test_sqliteRepo_DeleteLocation(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)
	s := &sqliteRepo{db: testDB}

	location, err := s.CreateLocation(t.Context(), Location{
		PlanningCenterID: "pcloc_delete",
		EventID:          1,
		Name:             "Delete location",
	})
	require.NoError(t, err)

	err = s.DeleteLocation(t.Context(), location.ID)
	require.NoError(t, err)

	locations, err := s.ListLocations(t.Context(), LocationFilter{IDs: []int64{location.ID}})
	require.NoError(t, err)
	assert.Empty(t, locations)

	err = s.DeleteLocation(t.Context(), location.ID)
	assert.ErrorIs(t, err, repo.ErrNotFound)
}

func Test_sqliteRepo_ListLocationGroups(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)
	s := &sqliteRepo{db: testDB}

	groupA, err := s.CreateLocationGroup(t.Context(), LocationGroup{Name: "group-a"})
	require.NoError(t, err)
	groupB, err := s.CreateLocationGroup(t.Context(), LocationGroup{Name: "group-b"})
	require.NoError(t, err)

	groups, err := s.ListLocationGroups(t.Context(), LocationGroupFilter{})
	require.NoError(t, err)
	assert.Len(t, groups, 2)

	groups, err = s.ListLocationGroups(t.Context(), LocationGroupFilter{ID: groupA.ID})
	require.NoError(t, err)
	assert.Len(t, groups, 1)
	assert.Equal(t, groupA.ID, groups[0].ID)

	groups, err = s.ListLocationGroups(t.Context(), LocationGroupFilter{Name: "group-b"})
	require.NoError(t, err)
	assert.Len(t, groups, 1)
	assert.Equal(t, groupB.ID, groups[0].ID)
}

func Test_sqliteRepo_ListLocations_filter_by_multiple_IDs(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)
	s := &sqliteRepo{db: testDB}

	loc1, err := s.CreateLocation(t.Context(), Location{
		PlanningCenterID: "pcloc_multi_1",
		EventID:          1,
		Name:             "Multi Location 1",
	})
	require.NoError(t, err)

	loc2, err := s.CreateLocation(t.Context(), Location{
		PlanningCenterID: "pcloc_multi_2",
		EventID:          1,
		Name:             "Multi Location 2",
	})
	require.NoError(t, err)

	// A third location so the multi-id and empty-id filters have something to
	// discriminate against.
	_, err = s.CreateLocation(t.Context(), Location{
		PlanningCenterID: "pcloc_multi_3",
		EventID:          1,
		Name:             "Multi Location 3",
	})
	require.NoError(t, err)

	t.Run("filter by multiple ids", func(t *testing.T) {
		locations, err := s.ListLocations(t.Context(), LocationFilter{IDs: []int64{loc1.ID, loc2.ID}})
		require.NoError(t, err)
		require.Len(t, locations, 2)

		idMap := map[int64]bool{
			locations[0].ID: true,
			locations[1].ID: true,
		}
		assert.True(t, idMap[loc1.ID])
		assert.True(t, idMap[loc2.ID])
	})

	t.Run("filter by empty ids returns all", func(t *testing.T) {
		locations, err := s.ListLocations(t.Context(), LocationFilter{IDs: []int64{}})
		require.NoError(t, err)
		assert.Len(t, locations, 3)
	})

	t.Run("filter by non-existent ids returns empty", func(t *testing.T) {
		locations, err := s.ListLocations(t.Context(), LocationFilter{IDs: []int64{9999, 9998}})
		require.NoError(t, err)
		assert.Len(t, locations, 0)
	})
}

func Test_sqliteRepo_CreateLocationGroup(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)
	s := &sqliteRepo{db: testDB}

	first, err := s.CreateLocationGroup(t.Context(), LocationGroup{Name: "group-unique"})
	require.NoError(t, err)
	_, err = s.CreateLocationGroup(t.Context(), LocationGroup{Name: "group-unique"})
	require.NoError(t, err)

	groups, err := s.ListLocationGroups(t.Context(), LocationGroupFilter{Name: "group-unique"})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, first.ID, groups[0].ID)
}

func Test_sqliteRepo_UpdateLocationGroup(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)
	s := &sqliteRepo{db: testDB}

	created, err := s.CreateLocationGroup(t.Context(), LocationGroup{Name: "group-rename"})
	require.NoError(t, err)

	err = s.UpdateLocationGroup(t.Context(), LocationGroup{ID: created.ID, Name: "group-renamed"})
	require.NoError(t, err)

	groups, err := s.ListLocationGroups(t.Context(), LocationGroupFilter{})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, "group-renamed", groups[0].Name)
}

func Test_sqliteRepo_UpdateLocationGroup_not_found(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)
	s := &sqliteRepo{db: testDB}

	err = s.UpdateLocationGroup(t.Context(), LocationGroup{ID: 999999, Name: "missing"})
	assert.ErrorIs(t, err, repo.ErrNotFound)
}

func Test_sqliteRepo_DeleteLocationGroup_in_use(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)
	s := &sqliteRepo{db: testDB}

	group, err := s.CreateLocationGroup(t.Context(), LocationGroup{Name: "group-in-use"})
	require.NoError(t, err)

	_, err = s.CreateLocation(t.Context(), Location{
		PlanningCenterID: "pcloc_in_use",
		EventID:          1,
		LocationGroupID:  &group.ID,
		Name:             "In Use Room",
	})
	require.NoError(t, err)

	err = s.DeleteLocationGroup(t.Context(), group.ID)
	assert.ErrorIs(t, err, ErrLocationGroupInUse)
}

func Test_sqliteRepo_DeleteLocationGroup_unused(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err, "Failed to prepare test DB")
	t.Cleanup(cleanup)
	s := &sqliteRepo{db: testDB}

	group, err := s.CreateLocationGroup(t.Context(), LocationGroup{Name: "group-unused"})
	require.NoError(t, err)

	err = s.DeleteLocationGroup(t.Context(), group.ID)
	require.NoError(t, err)

	err = s.DeleteLocationGroup(t.Context(), group.ID)
	assert.ErrorIs(t, err, repo.ErrNotFound)
}
