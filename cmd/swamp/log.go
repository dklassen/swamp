package main

import (
	"log/slog"
	"os"
)

// openLog opens the log every part of this process writes to, appending
// so the TUI's and an earlier run's lines stay. Each line carries the pid;
// callers add a component (logger.With("component", "mcp")) so lines from
// different goroutines stay identifiable.
func openLog(path string) (*slog.Logger, func() error, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, err
	}
	logger := slog.New(slog.NewTextHandler(f, nil)).With("pid", os.Getpid())
	return logger, f.Close, nil
}
