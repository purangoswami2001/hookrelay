package observability

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// NewLogger creates structured logger writing to stdout and optional log file
func NewLogger(level string, filePath string) (*slog.Logger, func() error, error) {
	logLevel, err := parseLogLevel(level)
	if err != nil {
		return nil, nil, err
	}

	var writers []io.Writer
	writers = append(writers, os.Stdout)

	var logFile *os.File

	if strings.TrimSpace(filePath) != "" {
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, nil, fmt.Errorf("create log directory: %w", err)
		}

		logFile, err = os.OpenFile(
			filePath,
			os.O_CREATE|os.O_APPEND|os.O_WRONLY,
			0644,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("open log file: %w", err)
		}

		writers = append(writers, logFile)
	}

	// pretty text handler for dev:
	// handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})

	handler := slog.NewJSONHandler(
		io.MultiWriter(writers...),
		&slog.HandlerOptions{
			Level:     logLevel,
			AddSource: true,
		},
	)

	logger := slog.New(handler)

	closeLogger := func() error {
		if logFile != nil {
			return logFile.Close()
		}
		return nil
	}

	return logger, closeLogger, nil
}

func parseLogLevel(level string) (slog.Level, error) {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "", "INFO":
		return slog.LevelInfo, nil
	case "DEBUG":
		return slog.LevelDebug, nil
	case "WARN", "WARNING":
		return slog.LevelWarn, nil
	case "ERROR":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q", level)
	}
}
