package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"kids-checkin/internal/cmd"
	"kids-checkin/internal/logger"
)

func main() {
	logger.Init(logger.LevelFromEnv())

	slog.Info("starting kids-checkin", slog.String("args", os.Args[0]))

	// Signal-aware root context, shared by every command. Ctrl-C/Ctrl-Break
	// cancels long-running work (seeding, fetcher polls, in-flight queries)
	// instead of leaving it unresponsive.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := (cmd.NewCommand()).Run(ctx, os.Args)
	if err != nil {
		slog.Error("execution failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	slog.Info("shutdown complete")
}
