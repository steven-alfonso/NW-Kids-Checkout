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

	"github.com/urfave/cli/v3"
)

// sessionTable is the table the session store uses. StartServer in
// internal/controllers/server.go configures sqlite3.Config without a Table,
// so the storage's default applies: fiber_storage. That same store also backs
// Planning Center pagination cursors (see planningcenterv1).
const sessionTable = "fiber_storage"

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

	dbPath, before, after, err := purgeSessions(ctx, cmd.String("db-file"))
	if err != nil {
		return fmt.Errorf("purge sessions: %w", err)
	}

	log.InfoContext(ctx, "purged all sessions",
		slog.String("db_file", dbPath),
		slog.Int("sessions_before", before),
		slog.Int("sessions_after", after),
	)

	return nil
}

// purgeSessions deletes every row from the session store's table, including
// unexpired ones, so every user must log in again. It returns the absolute
// database path and the row counts before and after the delete.
//
// A missing, empty, or misspelled --db-file must fail instead of SQLite
// creating a new database, which would turn the purge into a silent no-op. So
// the path is checked with os.Stat and the connection uses a read-write-only
// DSN (mode=rw), which refuses to create a missing file.
func purgeSessions(ctx context.Context, dbFile string) (dbPath string, before, after int, err error) {
	dbFile = strings.TrimSpace(dbFile)
	if dbFile == "" {
		return "", 0, 0, fmt.Errorf("missing database DSN: pass --db-file, or set $%s, or unset it to use the default %s", db.EnvDBFile, db.DefaultDBFile)
	}

	// Match db.InitDB: a "path?params" DSN names the file at path.
	filePath, params, hasParams := strings.Cut(dbFile, "?")
	dbPath, absErr := filepath.Abs(filePath)
	if absErr != nil {
		dbPath = filePath
	}

	if _, statErr := os.Stat(filePath); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return dbPath, 0, 0, fmt.Errorf("database file %s does not exist; check --db-file and the mounted volume", dbPath)
		}
		return dbPath, 0, 0, fmt.Errorf("stat database file %s: %w", dbPath, statErr)
	}

	// The sqlite3 driver is registered by the db package's blank import.
	dsn := "file:" + filePath + "?mode=rw"
	if hasParams && params != "" {
		dsn += "&" + params
	}
	database, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return dbPath, 0, 0, fmt.Errorf("open database %s: %w", dbPath, err)
	}
	defer database.Close()

	countQuery := "SELECT COUNT(*) FROM " + sessionTable

	if err := database.QueryRowContext(ctx, countQuery).Scan(&before); err != nil {
		return dbPath, 0, 0, fmt.Errorf("count sessions in %s: %w", sessionTable, err)
	}

	if _, err := database.ExecContext(ctx, "DELETE FROM "+sessionTable); err != nil {
		return dbPath, before, 0, fmt.Errorf("delete sessions from %s: %w", sessionTable, err)
	}

	if err := database.QueryRowContext(ctx, countQuery).Scan(&after); err != nil {
		return dbPath, before, 0, fmt.Errorf("count sessions in %s after delete: %w", sessionTable, err)
	}

	return dbPath, before, after, nil
}
