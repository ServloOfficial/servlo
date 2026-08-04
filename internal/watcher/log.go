package watcher

import (
	"log/slog"
	"os"
)

// logger is the package-level structured logger. Defaults to WARN level on
// stderr so watcher noise is silent in normal use. Call SetLogger to override
// (e.g. with a DEBUG-level handler when SERVLO_DEBUG is set).
var logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
	Level: slog.LevelWarn,
}))

// SetLogger replaces the watcher logger. Call before starting any watchers.
func SetLogger(l *slog.Logger) {
	logger = l
}
