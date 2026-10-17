package main

import (
	"log/slog"
	"os"

	"github.com/purangoswami2001/hookrelay/internal/observability"
)

// Quick manual run to verify logger output locally
func main() {
	logger, closeLogger, err := observability.NewLogger(
		"DEBUG",
		"logs/hookrelay.log",
	)
	if err != nil {
		slog.Error("logger init failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := closeLogger(); err != nil {
			slog.Error("failed to close logger", "error", err)
		}
	}()

	logger.Info("HookRelay logger initialized", "service", "hookrelay", "env", "dev")
	logger.Debug("debug logging enabled")
	logger.Warn("sample warning")
	logger.Error("sample error", "error", "test error")

	// fi, _ := os.Stat("logs/hookrelay.log")
	// logger.Debug("logfile size", "bytes", fi.Size())
}
