package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	stop, err := startMCP(path, documents.NewStore(t.TempDir()), ln, slog.New(slog.NewTextHandler(io.Discard, nil)), func() {})
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

	status, stop := mcpForTUI(addr, migratedDBPath(t), documents.NewStore(t.TempDir()), slog.New(slog.NewTextHandler(io.Discard, nil)), func() {})

	if !strings.Contains(status, "MCP server not started") || !strings.Contains(status, addr) {
		t.Errorf("status = %q, want it to say the MCP server didn't start on %s", status, addr)
	}
	if err := stop(context.Background()); err != nil {
		t.Errorf("stop with no server = %v, want nil", err)
	}
}

// TestStartMCP_StopWithAnAgentConnected: a connected agent holds a stream
// open that never goes idle, so waiting for it would spend stop's whole
// deadline and leave nothing for releasing leases.
func TestStartMCP_StopWithAnAgentConnected(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	stop, err := startMCP(migratedDBPath(t), documents.NewStore(t.TempDir()), ln, slog.New(slog.NewTextHandler(io.Discard, nil)), func() {})
	if err != nil {
		t.Fatalf("startMCP: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: "test"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://" + ln.Addr().String()}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := stop(stopCtx); err != nil {
		t.Errorf("stop with an agent connected = %v, want nil", err)
	}
	if stopCtx.Err() != nil {
		t.Error("stop spent its whole deadline")
	}
}

// panickingTool stands in for a buggy tool handler; the crash's trace
// must still name it.
func panickingTool(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
	panic("boom from a tool")
}

// TestReleaseTerminalOnPanic_ToolPanicCrashesAfterReleasing: a tool call
// runs in a goroutine of the SDK's, out of Bubble Tea's reach, so without
// the release the trace lands on the alt screen of a terminal left raw.
func TestReleaseTerminalOnPanic_ToolPanicCrashesAfterReleasing(t *testing.T) {
	t.Parallel()

	if os.Getenv("SWAMP_TEST_TOOL_PANIC") == "1" {
		callPanickingTool(t)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestReleaseTerminalOnPanic_ToolPanicCrashesAfterReleasing$")
	cmd.Env = append(os.Environ(), "SWAMP_TEST_TOOL_PANIC=1")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("the process survived a tool panic (err %v), want a crash; stderr:\n%s", err, stderr.String())
	}
	out := stderr.String()
	released := strings.Index(out, "terminal released")
	panicked := strings.Index(out, "panic: boom from a tool")
	if released < 0 || panicked < released {
		t.Errorf("want the terminal released before the panic is printed; stderr:\n%s", out)
	}
	if !strings.Contains(out, "panickingTool") {
		t.Errorf("the trace lost the panicking frame; stderr:\n%s", out)
	}
}

// callPanickingTool is the crashing half of
// TestReleaseTerminalOnPanic_ToolPanicCrashesAfterReleasing.
func callPanickingTool(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "swamp", Version: "test"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "boom"}, panickingTool)
	server.AddReceivingMiddleware(releaseTerminalOnPanic(func() { fmt.Fprintln(os.Stderr, "terminal released") }))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = http.Serve(ln, streamableHandler(server)) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: "test"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://" + ln.Addr().String()}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	_, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "boom"})
}

// panickingListener makes the server's own goroutine, the one running
// Serve, panic.
type panickingListener struct{ net.Listener }

func (panickingListener) Accept() (net.Conn, error) { panic("boom from accept") }

// TestStartMCP_ServePanicCrashesAfterReleasing: the goroutine startMCP
// runs Serve in is as far from Bubble Tea's recovery as a tool call's.
func TestStartMCP_ServePanicCrashesAfterReleasing(t *testing.T) {
	t.Parallel()

	if os.Getenv("SWAMP_TEST_SERVE_PANIC") == "1" {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		release := func() { fmt.Fprintln(os.Stderr, "terminal released") }
		if _, err := startMCP(migratedDBPath(t), documents.NewStore(t.TempDir()), panickingListener{ln}, slog.New(slog.NewTextHandler(io.Discard, nil)), release); err != nil {
			t.Fatalf("startMCP: %v", err)
		}
		time.Sleep(5 * time.Second)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestStartMCP_ServePanicCrashesAfterReleasing$")
	cmd.Env = append(os.Environ(), "SWAMP_TEST_SERVE_PANIC=1")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("the process survived a panic in Serve (err %v), want a crash; stderr:\n%s", err, stderr.String())
	}
	out := stderr.String()
	released := strings.Index(out, "terminal released")
	panicked := strings.Index(out, "panic: boom from accept")
	if released < 0 || panicked < released {
		t.Errorf("want the terminal released before the panic is printed; stderr:\n%s", out)
	}
}
