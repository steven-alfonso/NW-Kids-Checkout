package checkins

import (
	"context"
	"log/slog"
	"time"

	"kids-checkin/internal/db"
	"kids-checkin/internal/logger"
	"kids-checkin/internal/repo/checkin"
	"kids-checkin/internal/repo/event"
	"kids-checkin/internal/repo/location"
	"kids-checkin/internal/repo/manualcheckin"

	"github.com/urfave/cli/v3"
)

func seedPreviewCmd(ctx context.Context, cmd *cli.Command) error {
	if !cmd.Bool("force") {
		return cli.Exit("must pass --force to seed preview data (destructive operation)", 1)
	}

	dbFile := cmd.String("db-file")
	database, err := db.InitDB(dbFile)
	if err != nil {
		return cli.Exit(err.Error(), 1)
	}
	defer database.Close()

	ctx = logger.WithLogger(ctx, slog.With(slog.String("cmd", "checkins-seed-preview")))
	log := logger.FromContext(ctx)

	log.InfoContext(ctx, "starting checkins seed-preview", slog.String("db_file", dbFile))

	checkinRepo := checkin.NewRepo(database)
	manualRepo := manualcheckin.NewRepo(database)
	locationRepo := location.NewRepo(database)
	eventRepo := event.NewRepo(database)

	deletedCheckins, err := checkinRepo.DeleteAllCheckins(ctx)
	if err != nil {
		return cli.Exit(err.Error(), 1)
	}
	log.InfoContext(ctx, "deleted checkins", slog.Int64("deleted_count", deletedCheckins))

	deletedManual, err := manualRepo.DeleteAllManualCheckins(ctx)
	if err != nil {
		return cli.Exit(err.Error(), 1)
	}
	log.InfoContext(ctx, "deleted manual checkins", slog.Int64("deleted_count", deletedManual))

	// Ensure we have a location to attach checkins to (checkins.location_id is NOT NULL).
	locationID, err := ensurePreviewLocation(ctx, locationRepo, eventRepo)
	if err != nil {
		return cli.Exit(err.Error(), 1)
	}

	// Helper to compute checked_out_at offsets matching preview.js ago().
	ago := func(minutes float64) time.Time {
		return time.Now().UTC().Add(-time.Duration(minutes * float64(time.Minute)))
	}
	now := time.Now().UTC()
	fetchedAt := now

	// Seed data mirroring internal/web/dev-assets/preview.js
	// Planning Center checkins (5 rows)
	pcSeeds := []checkin.Checkin{
		{FirstName: "Fresh", LastName: "Kid", SecurityCode: "1111", PlanningCenterID: "demo-0", LocationID: locationID, CheckedOutAt: ago(0), FetchedAt: fetchedAt},
		{FirstName: "Edge", LastName: "Kid", SecurityCode: "2222", PlanningCenterID: "demo-4", LocationID: locationID, CheckedOutAt: ago(3.9), FetchedAt: fetchedAt},
		{FirstName: "Overdue", LastName: "Kid", SecurityCode: "5555", PlanningCenterID: "demo-5", LocationID: locationID, CheckedOutAt: ago(6), FetchedAt: fetchedAt},
		{FirstName: "Late", LastName: "Kid", SecurityCode: "3333", PlanningCenterID: "demo-8", LocationID: locationID, CheckedOutAt: ago(7.9), FetchedAt: fetchedAt},
		{FirstName: "Done", LastName: "Kid", SecurityCode: "4444", PlanningCenterID: "demo-c", LocationID: locationID, CheckedOutAt: ago(2), CheckedOutConfirmedAt: now, FetchedAt: fetchedAt},
	}

	for _, c := range pcSeeds {
		_, err := checkinRepo.CreateCheckin(ctx, c)
		if err != nil {
			return cli.Exit(err.Error(), 1)
		}
	}
	log.InfoContext(ctx, "seeded planning center checkins", slog.Int("count", len(pcSeeds)))

	// Manual checkins (5 rows)
	manualSeeds := []manualcheckin.ManualCheckin{
		{PublicID: "demo-m0", FirstName: "Manu", LastName: "Checkin", CheckedOutAt: ago(0)},
		{PublicID: "demo-m4", FirstName: "Manual", LastName: "Edge", CheckedOutAt: ago(3.9)},
		{PublicID: "demo-m5", FirstName: "Manual", LastName: "Overdue", CheckedOutAt: ago(6)},
		{PublicID: "demo-m8", FirstName: "Manual", LastName: "Late", CheckedOutAt: ago(7.9)},
		{PublicID: "demo-mc", FirstName: "Manual", LastName: "Done", CheckedOutAt: ago(2), CheckedOutConfirmedAt: now},
	}

	for _, m := range manualSeeds {
		_, err := manualRepo.CreateManualCheckin(ctx, m)
		if err != nil {
			return cli.Exit(err.Error(), 1)
		}
	}
	log.InfoContext(ctx, "seeded manual checkins", slog.Int("count", len(manualSeeds)))

	log.InfoContext(ctx, "seed-preview complete", slog.Int("checkins", len(pcSeeds)), slog.Int("manual_checkins", len(manualSeeds)))
	return nil
}

func ensurePreviewLocation(ctx context.Context, locationRepo location.Repo, eventRepo event.Repo) (int64, error) {
	// Try to reuse any existing location.
	locations, err := locationRepo.ListLocations(ctx, location.LocationFilter{})
	if err != nil {
		return 0, err
	}
	if len(locations) > 0 {
		return locations[0].ID, nil
	}

	// No locations exist; create minimal preview infrastructure via Repos.
	ev, err := eventRepo.CreateEvent(ctx, event.Event{
		Name:             "Preview Event",
		PlanningCenterID: "preview-event",
	})
	if err != nil {
		// If event already exists (unique constraint), try to fetch it.
		ev, err = eventRepo.GetEventByPlanningCenterID(ctx, "preview-event")
		if err != nil {
			return 0, err
		}
	}

	loc, err := locationRepo.CreateLocation(ctx, location.Location{
		PlanningCenterID: "preview-loc",
		EventID:          ev.ID,
		Name:             "Preview Location",
	})
	if err != nil {
		return 0, err
	}

	return loc.ID, nil
}
