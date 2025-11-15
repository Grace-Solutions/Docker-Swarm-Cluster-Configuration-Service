package logging

import (
	"os"

	"go.uber.org/zap"
)

// logger is a process-wide structured logger used by the orchestrator.
//
// For now we use a reasonably strict production configuration with JSON
// output. The log level can be controlled via the CLUSTERCTL_LOG_LEVEL
// environment variable (e.g. "debug", "info", "warn", "error").
var logger *zap.SugaredLogger

// Init initialises the global logger. It is safe to call multiple times; the
// first successful call wins.
func Init() error {
	if logger != nil {
		return nil
	}

	level := os.Getenv("CLUSTERCTL_LOG_LEVEL")
	if level == "" {
		level = "info"
	}

	cfg := zap.NewProductionConfig()
	if err := cfg.Level.UnmarshalText([]byte(level)); err != nil {
		return err
	}

	lg, err := cfg.Build()
	if err != nil {
		return err
	}
	logger = lg.Sugar()
	return nil
}

// L returns the process-wide logger, initialising it on first use if needed.
func L() *zap.SugaredLogger {
	if logger == nil {
		_ = Init()
	}
	return logger
}

// Sync flushes any buffered logs to their destination.
func Sync() {
	if logger != nil {
		_ = logger.Sync()
	}
}

