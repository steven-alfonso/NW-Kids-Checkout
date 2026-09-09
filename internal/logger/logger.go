package logger

import (
	"log/slog"
	"os"
	"time"
)

func Init(level slog.Leveler) {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:       level,
		AddSource:   false,
		ReplaceAttr: replaceAttr,
	})
	slog.SetDefault(slog.New(NewTraceHandler(handler)))
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		a.Value = slog.StringValue(a.Value.Time().Format(time.RFC3339))
	}
	return a
}

func LevelFromEnv() slog.Level {
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
