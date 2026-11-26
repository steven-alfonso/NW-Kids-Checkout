package sessions

import (
	"context"
	"fmt"
	"log/slog"

	"kids-checkin/internal/db"
	"kids-checkin/internal/logger"

	"github.com/gofiber/storage/sqlite3"
	"github.com/urfave/cli/v3"
)

var Commands = []*cli.Command{
	{
		Name:  "purge",
		Usage: "Deletes every session, logging all users out",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "force",
				Usage: "Required to confirm deleting every session",
			},
			db.DBFileFlag(),
		},
		Action: purgeCmd,
	},
}

func purgeCmd(ctx context.Context, cmd *cli.Command) error {
	if !cmd.Bool("force") {
		return fmt.Errorf("refusing to delete every session without --force")
	}

	dbFile := cmd.String("db-file")

	ctx = logger.WithLogger(ctx, slog.With(slog.String("cmd", "sessions-purge")))
	log := logger.FromContext(ctx)

	if err := purgeSessions(dbFile); err != nil {
		return fmt.Errorf("purge sessions: %w", err)
	}

	log.InfoContext(ctx, "purged all sessions", slog.String("db_file", dbFile))

	return nil
}

// purgeSessions deletes every row from the session store's table, including
// unexpired ones, so every user must log in again.
func purgeSessions(dbFile string) error {
	store := sqlite3.New(sqlite3.Config{
		Database: dbFile,
		Reset:    false, // We reset explicitly below; New's Reset drops the table.
	})
	defer store.Close()

	if err := store.Reset(); err != nil {
		return fmt.Errorf("reset session store: %w", err)
	}

	return nil
}
