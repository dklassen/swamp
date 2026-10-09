package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// migratedDBPath is a fresh database at the latest schema.
func migratedDBPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	newExportTestStoreAt(t, path)
	return path
}

// TestStartMCP_TheAgentsWritesReachTheTUIAsAnotherProcesses: the TUI skips
// events with its own origin, so a server sharing its database handle would
// make every agent change invisible to it.
func TestStartMCP_TheAgentsWritesReachTheTUIAsAnotherProcesses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := migratedDBPath(t)
	tuiOrigin := fmt.Sprintf("tui:%d", os.Getpid())
	cfg := store.DefaultConfig()
	cfg.Origin = tuiOrigin
	tuiDB, err := store.Open(path, cfg)
	if err != nil {
		t.Fatalf("open the TUI's handle: %v", err)
	}
	t.Cleanup(func() { _ = tuiDB.Close() })
	tui := store.New(tuiDB)
	company, err := tui.CreateCompany(ctx, "Acme", "ashby", "acme")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	posting, err := tui.UpsertPosting(ctx, store.CreatePostingParams{CompanyID: company.ID, Source: "ashby", SourceID: "job-1", IngestedFields: store.IngestedFields{Title: "Engineer", RawPayload: "{}"}})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	feed, err := tui.NewChangeFeed(ctx)
	if err != nil {
		t.Fatalf("NewChangeFeed: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	stop, err := startMCP(path, documents.NewStore(t.TempDir()), ln, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("startMCP: %v", err)
	}
	t.Cleanup(func() { _ = stop(context.Background()) })

	client := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: "test"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://" + ln.Addr().String()}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "stage_prepare", Arguments: map[string]any{"PostingID": posting.ID}})
	if err != nil || res.IsError {
		t.Fatalf("stage_prepare: err %v, result %+v", err, res)
	}

	events, err := feed.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := fmt.Sprintf("mcp:%d", os.Getpid())
	var found bool
	for _, e := range events {
		if e.Table == "applications" && e.Op == "insert" {
			found = true
			if e.Origin != want {
				t.Errorf("the agent's application insert has origin %q, want %q: the TUI (%s) would skip it", e.Origin, want, tuiOrigin)
			}
		}
	}
	if !found {
		t.Errorf("no applications insert among %+v", events)
	}
}

// TestMCPForTUI_PortInUse_TUIRunsWithoutIt: a standalone mcp-serve, or a
// second TUI with the flag, may already hold the address; the TUI says so
// and runs on.
func TestMCPForTUI_PortInUse_TUIRunsWithoutIt(t *testing.T) {
	t.Parallel()

	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = taken.Close() })
	addr := taken.Addr().String()

	status, stop := mcpForTUI(addr, migratedDBPath(t), documents.NewStore(t.TempDir()), slog.New(slog.NewTextHandler(io.Discard, nil)))

	if !strings.Contains(status, "MCP server not started") || !strings.Contains(status, addr) {
		t.Errorf("status = %q, want it to say the MCP server didn't start on %s", status, addr)
	}
	if err := stop(context.Background()); err != nil {
		t.Errorf("stop with no server = %v, want nil", err)
	}
}
