package main

import "testing"

// TestProcessKind: the kind half of each process's change-log origin
// ("tui:4120"), so a TUI can tell the agent's changes (mcp) from a sync's
// (fetch) and from its own (RFC 0008).
func TestProcessKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"swamp"}, want: "tui"},
		{args: []string{"swamp", "mcp-serve"}, want: "mcp"},
		{args: []string{"swamp", "fetch"}, want: "fetch"},
		{args: []string{"swamp", "stage", "list"}, want: "stage"},
		{args: []string{"swamp", "export", "3"}, want: "export"},
		{args: []string{"swamp", "import", "seed.yaml"}, want: "import"},
		{args: []string{"swamp", "migrate"}, want: "migrate"},
	}
	for _, tt := range tests {
		if got := processKind(tt.args); got != tt.want {
			t.Errorf("processKind(%q) = %q, want %q", tt.args, got, tt.want)
		}
	}
}
