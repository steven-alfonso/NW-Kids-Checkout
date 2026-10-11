package apiserver

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"kids-checkin/internal/controllers"

	"github.com/urfave/cli/v3"
)

func ServeCmd(ctx context.Context, cmd *cli.Command) error {
	port := cmd.Int("port")

	if port <= 0 {
		return errors.New("port must be greater than 0")
	}

	// Trimmed once here so this line names the same file StartServer opens
	// (which trims before both InitDB and the session store).
	dbFile := strings.TrimSpace(cmd.String("db-file"))
	slog.Info("starting api server", slog.Int("port", port), slog.String("db_file", dbFile))

	err := controllers.StartServer(ctx, port, dbFile)
	if err != nil {
		slog.Error("api server failed", slog.String("error", err.Error()))
		return err
	}

	slog.Info("api server stopped")
	return nil
}
