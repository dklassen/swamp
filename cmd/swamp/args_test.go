package main

import (
	"errors"
	"flag"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseArgs_Kind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args []string
		want string
	}{
		{args: nil, want: "tui"},
		{args: []string{"--mcp"}, want: "tui"},
		{args: []string{"-mcp"}, want: "tui"},
		{args: []string{"mcp-serve"}, want: "mcp"},
		{args: []string{"fetch"}, want: "fetch"},
		{args: []string{"stage", "list"}, want: "stage"},
		{args: []string{"export", "3"}, want: "export"},
		{args: []string{"import", "seed.yaml"}, want: "import"},
		{args: []string{"migrate", "up"}, want: "migrate"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.args), func(t *testing.T) {
			t.Parallel()

			f, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("parseArgs(%q): %v", tt.args, err)
			}
			if got := f.kind(); got != tt.want {
				t.Errorf("parseArgs(%q).kind() = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestParseArgs_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args []string
		want runFlags
	}{
		{args: nil, want: runFlags{command: cmdTUI}},
		{args: []string{"--mcp"}, want: runFlags{command: cmdTUI, mcp: true}},
		{args: []string{"migrate", "up"}, want: runFlags{command: cmdMigrate, migrate: migrateUp}},
		{args: []string{"migrate", "down"}, want: runFlags{command: cmdMigrate, migrate: migrateDown}},
		{args: []string{"fetch"}, want: runFlags{command: cmdFetch}},
		{args: []string{"stage", "list"}, want: runFlags{command: cmdStage, stage: stageList}},
		{args: []string{"stage", "prepare", "42"}, want: runFlags{command: cmdStage, stage: stagePrepare, postingID: 42}},
		{args: []string{"export", "3"}, want: runFlags{command: cmdExport, applicationID: 3}},
		{args: []string{"import", "seed.yaml"}, want: runFlags{command: cmdImport, seedFile: "seed.yaml"}},
		{args: []string{"mcp-serve"}, want: runFlags{command: cmdMCPServe}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.args), func(t *testing.T) {
			t.Parallel()

			got, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("parseArgs(%q): %v", tt.args, err)
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(runFlags{})); diff != "" {
				t.Errorf("parseArgs(%q) mismatch (-want +got):\n%s", tt.args, diff)
			}
		})
	}
}

func TestParseArgs_Invalid(t *testing.T) {
	t.Parallel()

	tests := [][]string{
		{"bogus"},
		{"--nope"},
		{"--mcp", "fetch"},
		{"migrate"},
		{"migrate", "sideways"},
		{"migrate", "up", "extra"},
		{"fetch", "extra"},
		{"stage"},
		{"stage", "sideways"},
		{"stage", "list", "extra"},
		{"stage", "prepare"},
		{"stage", "prepare", "x"},
		{"stage", "-x", "list"},
		{"export"},
		{"export", "x"},
		{"import"},
		{"mcp-serve", "extra"},
	}
	for _, args := range tests {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			t.Parallel()

			if got, err := parseArgs(args); err == nil {
				t.Errorf("parseArgs(%q) = %+v, nil; want an error", args, got)
			}
		})
	}
}

func TestParseArgs_Help(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"-h"}, {"--help"}, {"migrate", "-h"}} {
		if _, err := parseArgs(args); !errors.Is(err, flag.ErrHelp) {
			t.Errorf("parseArgs(%q) = %v, want flag.ErrHelp", args, err)
		}
	}
}
