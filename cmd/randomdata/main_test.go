package main

import (
	"testing"

	"kids-checkin/internal/db"
	"kids-checkin/internal/repo/event"
	"kids-checkin/internal/repo/location"

	"github.com/stretchr/testify/require"
)

// Test_seedCheckins_propagatesListEventsError pins that a failing event lookup
// aborts seeding instead of being silently ignored and producing checkins
// with EventID 0 (FK violation or orphan).
func Test_seedCheckins_propagatesListEventsError(t *testing.T) {
	testDB, cleanup, err := db.PrepareTestDB()
	require.NoError(t, err)
	t.Cleanup(cleanup)
	ctx := t.Context()

	// Seed one valid event+location through the repos so the location
	// prerequisite is met, then drop the events table so ListEvents fails
	// while ListLocations still succeeds.
	eventRepo := event.NewRepo(testDB)
	createdEvent, err := eventRepo.CreateEvent(ctx, event.Event{Name: "E", PlanningCenterID: "pc-e-1"})
	require.NoError(t, err)
	locationRepo := location.NewRepo(testDB)
	_, err = locationRepo.CreateLocation(ctx, location.Location{
		PlanningCenterID: "pc-l-1",
		EventID:          createdEvent.ID,
		Name:             "L",
	})
	require.NoError(t, err)
	_, err = testDB.Exec(`DROP TABLE events`)
	require.NoError(t, err)

	err = seedCheckins(ctx, testDB, 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "listing events")
}
