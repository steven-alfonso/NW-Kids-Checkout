package sessions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"kids-checkin/internal/db"
	"kids-checkin/internal/logger"
	"kids-checkin/internal/repo/session"

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

	ctx = logger.WithLogger(ctx, slog.With(slog.String("cmd", "sessions-purge")))
	log := logger.FromContext(ctx)

	log.InfoContext(ctx, "starting sessions purge", slog.String("db_file", strings.TrimSpace(cmd.String("db-file"))))

	dbPath, deleted, err := purgeSessions(ctx, cmd.String("db-file"))
	if err != nil {
		return fmt.Errorf("purge sessions: %w", err)
	}

	if deleted == 0 {
		log.WarnContext(ctx, "nothing to revoke; verify the database path",
			slog.String("db_file", dbPath),
			slog.Int64("sessions_deleted", deleted),
		)
		return nil
	}

	log.InfoContext(ctx, "purged all sessions",
		slog.String("db_file", dbPath),
		slog.Int64("sessions_deleted", deleted),
	)

	return nil
}

// purgeSessions deletes every row from the session store's table, including
// unexpired ones, so every user must log in again. It returns the absolute
// database path and how many rows were deleted.
//
// A missing, empty, or misspelled --db-file must fail instead of SQLite
// creating a new database, which would turn the purge into a silent no-op. So
// the path is checked with os.Stat and the connection uses a read-write-only
// DSN (mode=rw), which refuses to create a missing file.
func purgeSessions(ctx context.Context, dbFile string) (dbPath string, deleted int64, err error) {
	dbFile = strings.TrimSpace(dbFile)
	if dbFile == "" {
		return "", 0, fmt.Errorf("missing database DSN: pass --db-file, or set $%s, or unset it to use the default %s", db.EnvDBFile, db.DefaultDBFile)
	}

	// Match db.InitDB and db-init: a "path?params" DSN names the file at path,
	// so strip the query before inspecting the path. Only plain files work
	// here; an in-memory store or a file: URI has no row to purge on disk.
	filePath, params, hasParams := strings.Cut(dbFile, "?")
	if filePath == "" || filePath == ":memory:" || strings.HasPrefix(filePath, "file:") {
		return "", 0, fmt.Errorf("sessions purge needs a plain file path, got %q (pass a filesystem path, not a SQLite URI or in-memory database)", dbFile)
	}

	dbPath, absErr := filepath.Abs(filePath)
	if absErr != nil {
		dbPath = filePath
	}

	if _, statErr := os.Stat(filePath); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return dbPath, 0, fmt.Errorf("database file %s does not exist; check --db-file and the mounted volume", dbPath)
		}
		return dbPath, 0, fmt.Errorf("stat database file %s: %w", dbPath, statErr)
	}

	// The sqlite3 driver is registered by the db package's blank import.
	dsn := "file:" + filePath + "?mode=rw"
	if hasParams && params != "" {
		dsn += "&" + params
	}
	database, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return dbPath, 0, fmt.Errorf("open database %s: %w", dbPath, err)
	}
	defer database.Close()

	// sql.Open is lazy, so ping to surface open-class failures before the
	// destructive statement rather than after it.
	if err := database.PingContext(ctx); err != nil {
		return dbPath, 0, fmt.Errorf("open database %s: %w", dbPath, err)
	}

	deleted, err = session.NewRepo(database).Purge(ctx)
	if err != nil {
		return dbPath, 0, err
	}

	return dbPath, deleted, nil
}
