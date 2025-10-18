package util

import (
	"log/slog"
	"os"
	"strings"
)

// InitSlog configures slog based on LOG_LEVEL environment variable.
// Supported levels: debug, info, warn, error
func InitSlog() {
	if logLevel, ok := os.LookupEnv("LOG_LEVEL"); ok {
		var level slog.Level

		switch strings.ToLower(logLevel) {
		case "debug":
			level = slog.LevelDebug
		case "info":
			level = slog.LevelInfo
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		default:
			level = slog.LevelInfo
		}

		opts := &slog.HandlerOptions{
			Level: level,
		}
		handler := slog.NewTextHandler(os.Stderr, opts)
		slog.SetDefault(slog.New(handler))
	}
}
