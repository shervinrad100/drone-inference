package platform

import (
	"log/slog"
	"os"
)

// The function reads the env variable LOG_LEVEL and creates a new JSON Logger on stdout
// Log level comes from env var and defaults to INFO
func NewLogger() *slog.Logger {

	var logLevel slog.Level
	envLogLevel := os.Getenv("LOG_LEVEL")

	err := logLevel.UnmarshalText([]byte(envLogLevel))

	opts := &slog.HandlerOptions{
		Level: logLevel,
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, opts))

	if envLogLevel != "" && err != nil {
		logger.Warn("LOG_LEVEL variable was not accepted. Defaulting to INFO...", "err", err)
	}

	return logger
}
