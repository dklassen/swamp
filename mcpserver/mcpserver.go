// Package mcpserver exposes the same agent hand-off operations the
// `apply-to-posting` skill already drives via the `swamp stage` CLI
// subcommand, as MCP tools instead. It's a thin protocol adapter over the
// stage package -- no business logic lives here, only translation between
// MCP's tools/call convention and stage.Stage's existing Go API. See
// decisions.log for why MCP (and specifically its Streamable HTTP
// transport) is required here rather than gRPC or a plain REST API.
package mcpserver

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/stage"
)

// New builds an MCP server exposing st and d's operations as tools. The
// returned server has no active session yet -- connect it to a transport
// (Server.Run for a single stdio-style session, or mount a
// StreamableHTTPHandler for concurrent HTTP sessions) to start serving.
func New(st *stage.Stage, d *documents.Store) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "swamp", Version: "v0.1.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_postings",
		Description: "List interested, non-archived postings that still need a cover letter and/or resume drafted, or whose latest draft was flagged for revision.",
	}, listPostingsHandler(st))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "stage_prepare",
		Description: "Commit to drafting one posting's application: creates its application record if one doesn't exist yet, ensures its document directory exists, and returns the resolved cover letter/resume paths plus any existing review feedback. Idempotent -- safe to call again for the same posting.",
	}, stagePrepareHandler(st))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "write_document",
		Description: "Write drafted cover letter or resume content to the path stage_prepare resolved for an application, the same effect writing the file directly would have. DocumentType must be \"cover_letter\" or \"resume\".",
	}, writeDocumentHandler(d))

	return server
}

type listPostingsInput struct{}

// The output type parameter is 'any' rather than []stage.Candidate: the
// SDK's automatic JSON Schema inference panics on store.Posting's shape
// (it embeds IngestedFields, which has an OptionalTime field whose custom
// MarshalJSON returns a JSON string -- the inference library requires
// embedded-struct fields with custom marshalers to marshal to an object).
// 'any' skips schema inference entirely; the concrete value returned here
// still populates CallToolResult.StructuredContent normally.
func listPostingsHandler(st *stage.Stage) mcp.ToolHandlerFor[listPostingsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ listPostingsInput) (*mcp.CallToolResult, any, error) {
		candidates, err := st.List(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("list_postings: %w", err)
		}
		return nil, candidates, nil
	}
}

type stagePrepareInput struct {
	PostingID int64 `json:"PostingID" jsonschema:"the posting's id, from list_postings' Posting.ID field"`
}

// Out is 'any' for the same reason as listPostingsHandler: stage.Prepared
// embeds store.Posting, which trips the SDK's schema-inference panic on
// OptionalTime's custom string marshaling.
func stagePrepareHandler(st *stage.Stage) mcp.ToolHandlerFor[stagePrepareInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in stagePrepareInput) (*mcp.CallToolResult, any, error) {
		prepared, err := st.Prepare(ctx, in.PostingID)
		if err != nil {
			return nil, nil, fmt.Errorf("stage_prepare: %w", err)
		}
		return nil, prepared, nil
	}
}

type writeDocumentInput struct {
	ApplicationID int64  `json:"ApplicationID" jsonschema:"the application id, from stage_prepare's ApplicationID field"`
	DocumentType  string `json:"DocumentType" jsonschema:"either cover_letter or resume"`
	Content       string `json:"Content" jsonschema:"the full document content to write, replacing whatever is there"`
}

type writeDocumentOutput struct {
	Path         string `json:"Path"`
	BytesWritten int64  `json:"BytesWritten"`
}

func writeDocumentHandler(d *documents.Store) mcp.ToolHandlerFor[writeDocumentInput, writeDocumentOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in writeDocumentInput) (*mcp.CallToolResult, writeDocumentOutput, error) {
		paths, err := d.EnsureDir(in.ApplicationID)
		if err != nil {
			return nil, writeDocumentOutput{}, fmt.Errorf("write_document: ensure document dir: %w", err)
		}

		var path string
		switch in.DocumentType {
		case "cover_letter":
			path = paths.CoverLetter
		case "resume":
			path = paths.Resume
		default:
			return nil, writeDocumentOutput{}, fmt.Errorf("write_document: DocumentType must be \"cover_letter\" or \"resume\", got %q", in.DocumentType)
		}

		if err := os.WriteFile(path, []byte(in.Content), 0o644); err != nil {
			return nil, writeDocumentOutput{}, fmt.Errorf("write_document: write %s: %w", path, err)
		}

		return nil, writeDocumentOutput{Path: path, BytesWritten: int64(len(in.Content))}, nil
	}
}
