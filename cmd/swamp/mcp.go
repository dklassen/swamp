package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/mcpserver"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
)

// mcpHandler serves Swamp's MCP tools over Streamable HTTP. See runMCPServe
// for why localhost protection is off.
func mcpHandler(s *store.Store, d *documents.Store, syncer *sync.Syncer) http.Handler {
	return streamableHandler(mcpserver.New(newStage(s, d), d, syncer))
}

// streamableHandler serves server over Streamable HTTP.
func streamableHandler(server *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{DisableLocalhostProtection: true})
}

// releaseTerminalOnPanic hands the terminal back before a panic in a
// request crashes the process: the SDK runs each request in a goroutine
// of its own, where Bubble Tea can't restore the terminal, which would
// leave it raw with the trace drawn over the alt screen. Still crashes.
func releaseTerminalOnPanic(release func()) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			defer func() {
				if r := recover(); r != nil {
					release()
					panic(r)
				}
			}()
			return next(ctx, method, req)
		}
	}
}

// startMCP serves the MCP tools on ln in this process (swamp --mcp)
// until the returned stop is called. It opens its own database handle: the
// TUI skips events with its own origin, so on a shared handle every change
// the agent makes would be invisible to it.
func startMCP(dbPath string, d *documents.Store, ln net.Listener, logger *slog.Logger, release func()) (stop func(context.Context) error, err error) {
	cfg := store.DefaultConfig()
	cfg.Origin = fmt.Sprintf("mcp:%d", os.Getpid())
	sqlDB, err := store.Open(dbPath, cfg)
	if err != nil {
		return nil, fmt.Errorf("open db for the MCP server: %w", err)
	}
	s := store.New(sqlDB)
	syncer := newSyncer(s)
	// Cancelled on stop: an agent's open stream never goes idle, so
	// Shutdown would otherwise wait out its whole deadline.
	base, cancelRequests := context.WithCancel(context.Background())
	server := mcpserver.New(newStage(s, d), d, syncer)
	server.AddReceivingMiddleware(releaseTerminalOnPanic(release))
	srv := &http.Server{
		Handler:     streamableHandler(server),
		ErrorLog:    slog.NewLogLogger(logger.Handler(), slog.LevelError),
		BaseContext: func(net.Listener) context.Context { return base },
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				release()
				panic(r)
			}
		}()
		if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve", "err", err)
		}
	}()
	logger.Info("listening", "addr", ln.Addr().String())

	return func(ctx context.Context) error {
		cancelRequests()
		err := srv.Shutdown(ctx)
		logger.Info("stopped", "err", err)
		return errors.Join(err, syncer.ReleaseHeldLeases(ctx), sqlDB.Close())
	}, nil
}

// mcpForTUI starts the MCP server on addr for the TUI about to run, and
// returns the status line to start the TUI with. If addr can't be bound,
// e.g. a standalone mcp-serve holds it, the TUI runs without the server
// and stop does nothing.
func mcpForTUI(addr, dbPath string, d *documents.Store, logger *slog.Logger, release func()) (status string, stop func(context.Context) error) {
	noop := func(context.Context) error { return nil }
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		stop, err = startMCP(dbPath, d, ln, logger, release)
		if err != nil {
			_ = ln.Close()
		}
	}
	if err != nil {
		logger.Error("not started", "addr", addr, "err", err)
		return fmt.Sprintf("MCP server not started on %s: %v", addr, err), noop
	}
	return "MCP server on " + addr, stop
}

// mcpAddr is where the MCP server listens: SWAMP_MCP_ADDR, by default
// loopback only (see runMCPServe).
func mcpAddr() string {
	if addr := os.Getenv("SWAMP_MCP_ADDR"); addr != "" {
		return addr
	}
	return "127.0.0.1:8787"
}
