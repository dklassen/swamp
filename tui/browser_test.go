package tui

import "testing"

func TestBrowserCommand_Darwin_UsesOpen(t *testing.T) {
	cmd, args, err := browserCommand("darwin", "https://example.com")
	if err != nil {
		t.Fatalf("browserCommand: %v", err)
	}
	if cmd != "open" {
		t.Fatalf("cmd = %q, want %q", cmd, "open")
	}
	if len(args) != 1 || args[0] != "https://example.com" {
		t.Fatalf("args = %v, want [https://example.com]", args)
	}
}

func TestBrowserCommand_Linux_UsesXdgOpen(t *testing.T) {
	cmd, args, err := browserCommand("linux", "https://example.com")
	if err != nil {
		t.Fatalf("browserCommand: %v", err)
	}
	if cmd != "xdg-open" {
		t.Fatalf("cmd = %q, want %q", cmd, "xdg-open")
	}
	if len(args) != 1 || args[0] != "https://example.com" {
		t.Fatalf("args = %v, want [https://example.com]", args)
	}
}

func TestBrowserCommand_Windows_UsesRundll32(t *testing.T) {
	cmd, args, err := browserCommand("windows", "https://example.com")
	if err != nil {
		t.Fatalf("browserCommand: %v", err)
	}
	if cmd != "rundll32" {
		t.Fatalf("cmd = %q, want %q", cmd, "rundll32")
	}
	if len(args) != 2 || args[0] != "url.dll,FileProtocolHandler" || args[1] != "https://example.com" {
		t.Fatalf("args = %v, want [url.dll,FileProtocolHandler https://example.com]", args)
	}
}

func TestBrowserCommand_UnsupportedOS_ReturnsError(t *testing.T) {
	_, _, err := browserCommand("plan9", "https://example.com")
	if err == nil {
		t.Fatal("browserCommand: expected error for unsupported OS, got nil")
	}
}
