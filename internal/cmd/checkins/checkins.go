package checkins

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"kids-checkin/internal/db"
	"kids-checkin/internal/logger"
	"kids-checkin/internal/repo/checkin"
	"kids-checkin/internal/repo/manualcheckin"

	"github.com/urfave/cli/v3"
)

var Commands = []*cli.Command{
	{
		Name:  "delete-old",
		Usage: "Deletes old checkins older than the specified age",
		Flags: []cli.Flag{
			&cli.DurationFlag{
				Name:    "age",
				Value:   -7 * 24 * time.Hour, // 7 days ago
				Sources: cli.NewValueSourceChain(cli.EnvVar("CHECKINS_DELETE_OLDER_THAN_AGE")),
			},
			db.DBFileFlag(),
		},
		Action: deleteOlderThanCmd,
	},
	{
		Name:  "seed-preview",
		Usage: "Deletes all checkins/manual_checkins and seeds preview data mirroring preview.js",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Required to confirm destructive delete",
			},
			db.DBFileFlag(),
		},
		Action: seedPreviewCmd,
	},
}

func deleteOlderThanCmd(ctx context.Context, cmd *cli.Command) error {
	olderThan := cmd.Duration("age")
	if olderThan > 0 {
		return fmt.Errorf("age in the future is not allowed: use a negative value")
	}

	dbFile := cmd.String("db-file")
	database, err := db.InitDB(dbFile)
	if err != nil {
		return fmt.Errorf("init db: %w", err)
	}

	defer database.Close()

	ctx = logger.WithLogger(ctx, slog.With(slog.String("cmd", "checkins-delete-old")))
	log := logger.FromContext(ctx)

	log.InfoContext(ctx, "starting checkins delete-old", slog.Duration("age", olderThan), slog.String("db_file", dbFile))

	checkinRepo := checkin.NewRepo(database)
	deletedCount, err := checkinRepo.RemoveOldCheckins(ctx, time.Now().Add(olderThan))
	if err != nil {
		return fmt.Errorf("remove old checkins: %w", err)
	}

	log.InfoContext(ctx, "deleted old checkins", slog.Int64("deleted_count", deletedCount), slog.Duration("older_than", olderThan))

	manualCheckinRepo := manualcheckin.NewRepo(database)
	manualDeletedCount, err := manualCheckinRepo.RemoveOldManualCheckins(ctx, time.Now().Add(olderThan))
	if err != nil {
		// The checkins above are already committed (two non-transactional
		// deletes), so include their count in the error path.
		return fmt.Errorf("remove old manual checkins: %w (deleted %d checkins before failing)", err, deletedCount)
	}

	log.InfoContext(ctx, "deleted old manual checkins", slog.Int64("deleted_count", manualDeletedCount), slog.Duration("older_than", olderThan))

	return nil
}
