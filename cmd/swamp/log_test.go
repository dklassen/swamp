package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestOpenLog_ComponentsShareOneFileAndStayIdentifiable: the TUI and the
// MCP server log from different goroutines into one file, each line naming
// its process and component.
func TestOpenLog_ComponentsShareOneFileAndStayIdentifiable(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "swamp.log")
	logger, closeLog, err := openLog(path)
	if err != nil {
		t.Fatalf("openLog: %v", err)
	}
	logger.With("component", "tui").Info("started")
	logger.With("component", "mcp").Info("listening", "addr", "127.0.0.1:8787")
	if err := closeLog(); err != nil {
		t.Fatalf("close: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("log has %d lines, want 2:\n%s", len(lines), raw)
	}
	pid := "pid=" + strconv.Itoa(os.Getpid())
	for i, component := range []string{"component=tui", "component=mcp"} {
		if !strings.Contains(lines[i], pid) || !strings.Contains(lines[i], component) {
			t.Errorf("line %d = %q, want it to carry %s and %s", i, lines[i], pid, component)
		}
	}
	if !strings.Contains(lines[1], "addr=127.0.0.1:8787") {
		t.Errorf("line 1 = %q, want the message's own attributes too", lines[1])
	}
}
