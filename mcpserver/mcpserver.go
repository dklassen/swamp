// Package mcpserver exposes swamp's agent hand-off operations as MCP
// tools -- the interface the `apply-to-posting` skill uses to reach
// Swamp data. The tool definitions here are the source of truth for each
// tool's arguments and behavior; the skill only says which tool to call
// when. It's a thin protocol adapter over the stage, documents and sync
// packages -- no business logic lives here, only translation between
// MCP's tools/call convention and their existing Go APIs. See
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
	"github.com/dklassen/swamp/sync"
)

// New builds an MCP server exposing st and d's operations as tools, plus
// add_company via syncer (which also checks new slugs against their board). The
// returned server has no active session yet -- connect it to a transport
// (Server.Run for a single stdio-style session, or mount a
// StreamableHTTPHandler for concurrent HTTP sessions) to start serving.
func New(st *stage.Stage, d *documents.Store, syncer *sync.Syncer) *mcp.Server {
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

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_document",
		Description: "Read an application's current cover letter or resume content, e.g. the existing draft to revise when its latest review was flagged. DocumentType must be \"cover_letter\" or \"resume\". Returns a tool error if that document hasn't been written yet.",
	}, readDocumentHandler(d))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_company",
		Description: "Add a company to track, by its job board (ashby, greenhouse or lever) and board slug, with a short description of who the company is. The slug is checked against the board first. Never restores a company the user deleted or renames an existing one; postings are fetched on the user's next sync, not now.",
	}, addCompanyHandler(syncer))

	return server
}

type listPostingsInput struct{}

// listPostingsOutput wraps the candidate list because the MCP spec requires
// a tool's structuredContent to be a JSON object -- a bare array is rejected
// by spec-conforming clients such as Claude Code.
type listPostingsOutput struct {
	Postings []stage.Candidate
}

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
		return nil, listPostingsOutput{Postings: candidates}, nil
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

		path, err := documentPath(paths, in.DocumentType)
		if err != nil {
			return nil, writeDocumentOutput{}, fmt.Errorf("write_document: %w", err)
		}

		if err := os.WriteFile(path, []byte(in.Content), 0o644); err != nil {
			return nil, writeDocumentOutput{}, fmt.Errorf("write_document: write %s: %w", path, err)
		}

		return nil, writeDocumentOutput{Path: path, BytesWritten: int64(len(in.Content))}, nil
	}
}

type readDocumentInput struct {
	ApplicationID int64  `json:"ApplicationID" jsonschema:"the application id, from stage_prepare's ApplicationID field"`
	DocumentType  string `json:"DocumentType" jsonschema:"either cover_letter or resume"`
}

type readDocumentOutput struct {
	Path    string `json:"Path"`
	Content string `json:"Content"`
}

// readDocumentHandler resolves paths via Status rather than EnsureDir: a
// read has no reason to create the application's document directory.
func readDocumentHandler(d *documents.Store) mcp.ToolHandlerFor[readDocumentInput, readDocumentOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in readDocumentInput) (*mcp.CallToolResult, readDocumentOutput, error) {
		status := d.Status(in.ApplicationID)
		paths := documents.Paths{CoverLetter: status.CoverLetter.Path, Resume: status.Resume.Path}
		path, err := documentPath(paths, in.DocumentType)
		if err != nil {
			return nil, readDocumentOutput{}, fmt.Errorf("read_document: %w", err)
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil, readDocumentOutput{}, fmt.Errorf("read_document: read %s: %w", path, err)
		}

		return nil, readDocumentOutput{Path: path, Content: string(content)}, nil
	}
}

// documentPath picks documentType's path out of paths. documentType is
// the DocumentType argument write_document and read_document accept.
func documentPath(paths documents.Paths, documentType string) (string, error) {
	switch documentType {
	case "cover_letter":
		return paths.CoverLetter, nil
	case "resume":
		return paths.Resume, nil
	default:
		return "", fmt.Errorf("DocumentType must be \"cover_letter\" or \"resume\", got %q", documentType)
	}
}

type addCompanyInput struct {
	Name        string `json:"Name" jsonschema:"the company's display name"`
	Source      string `json:"Source" jsonschema:"the job board: ashby, greenhouse or lever"`
	Slug        string `json:"Slug" jsonschema:"the company's slug on that board, e.g. acme in jobs.ashbyhq.com/acme"`
	Description string `json:"Description" jsonschema:"one or two sentences on who the company is: what it builds, stage, domain"`
}

// addCompanyOutcomes names each sync.AddCompanyOutcome for the tool's result.
var addCompanyOutcomes = map[sync.AddCompanyOutcome]string{
	sync.AddCompanyCreated:        "created",
	sync.AddCompanyAlreadyExists:  "already_exists",
	sync.AddCompanySkippedDeleted: "skipped_deleted",
}

type addCompanyOutput struct {
	Outcome   string `json:"Outcome"`
	CompanyID int64  `json:"CompanyID"`
	Name      string `json:"Name"`
	OpenJobs  int    `json:"OpenJobs"`
}

func addCompanyHandler(syncer *sync.Syncer) mcp.ToolHandlerFor[addCompanyInput, addCompanyOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in addCompanyInput) (*mcp.CallToolResult, addCompanyOutput, error) {
		res, err := syncer.AddCompany(ctx, in.Name, in.Source, in.Slug, in.Description)
		if err != nil {
			return nil, addCompanyOutput{}, fmt.Errorf("add_company: %w", err)
		}
		return nil, addCompanyOutput{
			Outcome:   addCompanyOutcomes[res.Outcome],
			CompanyID: res.Company.ID,
			Name:      res.Company.Name,
			OpenJobs:  res.OpenJobs,
		}, nil
	}
}
