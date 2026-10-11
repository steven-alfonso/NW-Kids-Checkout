package apiserver

import (
	"context"
	"errors"
	"log/slog"

	"kids-checkin/internal/controllers"

	"github.com/urfave/cli/v3"
)

func ServeCmd(ctx context.Context, cmd *cli.Command) error {
	port := cmd.Int("port")

	if port <= 0 {
		return errors.New("port must be greater than 0")
	}

	slog.Info("starting api server", slog.Int("port", port), slog.String("db_file", cmd.String("db-file")))

	err := controllers.StartServer(ctx, port, cmd.String("db-file"))
	if err != nil {
		slog.Error("api server failed", slog.String("error", err.Error()))
		return err
	}

	slog.Info("api server stopped")
	return nil
}
