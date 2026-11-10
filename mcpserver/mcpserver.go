// Package mcpserver exposes swamp's agent hand-off operations as MCP
// tools -- the interface the skills use to reach Swamp data. The tool
// definitions here are the source of truth for each tool's arguments and
// behavior; the skills only say which tool to call when. Rules live in
// the services; this package owns what's endpoint-only: the agent's JSON
// shapes, page sizes, cursors and the sort-order registry.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/sync"
)

type companyService interface {
	ListActiveWithOpenPostings(ctx context.Context) ([]company.Stats, error)
}

type postingService interface {
	GetWithCompany(ctx context.Context, postingID int64) (*posting.Posting, *company.Company, error)
	Search(ctx context.Context, search posting.Search) (*posting.SearchPage, error)
}

type documentService interface {
	Write(ctx context.Context, applicationID int64, documentType document.Type, content, version string, source document.Source) (string, error)
	Read(ctx context.Context, applicationID int64, documentType document.Type) (path, content string, err error)
	Profile() (*service.CanonicalFile, error)
	CanonicalResume() (*service.CanonicalFile, error)
}

type draftingService interface {
	Queue(ctx context.Context) ([]service.QueuedPosting, error)
	Prepare(ctx context.Context, postingID int64) (*service.Prepared, error)
}

type scoringService interface {
	Pending(ctx context.Context, profileSHA256 string, rescore bool, beforeID int64, limit int) (*score.PendingPostingsPage, error)
	Record(ctx context.Context, provenance score.Provenance, entries []score.Entry) (*score.RecordResult, error)
}

// New builds an MCP server exposing the services' operations as tools,
// plus add_company via syncer (which also checks new slugs against their
// board). The returned server has no active session yet -- connect it to
// a transport (Server.Run for a single stdio-style session, or mount a
// StreamableHTTPHandler for concurrent HTTP sessions) to start serving.
func New(companies companyService, postings postingService, docs documentService, scores scoringService, drafting draftingService, syncer *sync.Syncer) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "swamp", Version: "v0.1.0"}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_postings",
		Description: "List non-archived postings that still need a cover letter and/or resume drafted, or whose latest draft was flagged for revision: postings marked interested, plus started applications that never were.",
	}, listPostingsHandler(drafting))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_companies",
		Description: "List every company being tracked, by name: ID, display name, job board, and open posting count. Use it to match a company the user names, then pass its ID to search_postings.",
	}, listCompaniesHandler(companies))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_postings",
		Description: "Find postings by what they are -- company, application and its status, interested, open or closed -- across every company being tracked, whether or not they have an application. Use it to resolve a posting or application the user names (get the company's ID from list_companies first), or to review the interested shortlist. Every filter is optional; by default it returns open, non-archived postings that their company's filters let through, as the user's company lists show them. Query looks postings up by words, each of which must appear in the title, company, department or location (\"platform dublin\"). Results come a page at a time: pass NextCursor back as Cursor, with the same filters, for the next page; it's null on the last page. Total is the number of matches when the page was read: if it's large, narrow the filters rather than paging through everything.",
		InputSchema: searchPostingsInputSchema(),
	}, searchPostingsHandler(postings))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_posting",
		Description: "Read one posting in full: its summary, the description as text, the job URL, its company's description, and Version, the version of the content you read. Use it to judge a posting before scoring it; a score must name the Version it was given for as its PostingVersion. Refuses an unknown posting, or one at a company the user deleted.",
	}, readPostingHandler(postings))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_pending_postings",
		Description: "List the postings still to score for fit, a page at a time, newest first (the all-postings view's default order): each one's ID, company, title, department, location, workplace type, published date, Version (which its score must name as PostingVersion) and why it's pending: new (never scored) or changed (its content changed since). Pass read_profile's Version as ProfileVersion; it's refused if the profile has changed since. Rescore lists postings with no score against this profile instead, which after a profile edit is all of them (reason rescore): use it only when the user asks for a rescore. Eligible postings are open, not archived, not interested, without an application, and let through by their company's filters. Pass NextCursor back as Cursor, with the same ProfileVersion and Rescore, for the next page; it's null on the last page. Total is how many are pending.",
	}, listPendingPostingsHandler(scores))

	mcp.AddTool(server, &mcp.Tool{
		Name:         "record_scores",
		Description:  "Record how well postings fit the user, in one batch: each score is 1 (no fit) to 5 (apply now), with a one-line reason shown next to it and, as PostingVersion, the Version you read from list_pending_postings or read_posting. Pass your own model ID and read_profile's Version as ProfileVersion; a stale one records nothing. Batch many scores per call. Each entry is recorded or refused, with why: no_such_posting, score_out_of_range, no_reason, content_changed (the posting changed since you read it; it stays pending) or already_scored (a score on that content, against that profile, from that model exists).",
		OutputSchema: recordScoresOutputSchema(),
	}, recordScoresHandler(scores))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "stage_prepare",
		Description: "Commit to drafting one posting's application: creates its application record if one doesn't exist yet, ensures its document directory exists, and returns the resolved cover letter/resume paths plus any existing review feedback. Idempotent -- safe to call again for the same posting.",
	}, stagePrepareHandler(drafting))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "write_document",
		Description: "Write drafted cover letter or resume content to the path stage_prepare resolved for an application, the same effect writing the file directly would have. Refuses, writing nothing, an application that doesn't exist or that the user deleted, or a document that changed since ReplacesVersion was read; the error says what to do instead.",
		InputSchema: documentInputSchema[writeDocumentInput](),
	}, writeDocumentHandler(docs))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_document",
		Description: "Read an application's current cover letter or resume content, e.g. the existing draft to revise when its latest review was flagged. Returns a tool error if that document hasn't been written yet.",
		InputSchema: documentInputSchema[readDocumentInput](),
	}, readDocumentHandler(docs))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_canonical_resume",
		Description: "Read the user's canonical resume: their maintained, best-version resume in markdown, the baseline to tailor for each posting instead of writing a resume from scratch. Read-only; the user maintains it.",
	}, readCanonicalHandler("read_canonical_resume", docs.CanonicalResume))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_profile",
		Description: "Read the user's profile reference: their real background, experience, skills and voice/style notes in markdown, the source every draft comes from. Read-only; the user maintains it.",
	}, readCanonicalHandler("read_profile", docs.Profile))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_company",
		Description: "Add a company to track, by its job board (ashby, greenhouse or lever) and board slug, with a short description of who the company is. The slug is checked against the board first. Never restores a company the user deleted or renames an existing one; postings are fetched on the user's next sync, not now.",
	}, addCompanyHandler(syncer))

	return server
}

type writeDocumentInput struct {
	ApplicationID   int64         `json:"ApplicationID" jsonschema:"the application id, from stage_prepare's ApplicationID field"`
	DocumentType    document.Type `json:"DocumentType" jsonschema:"the document type: one of the keys of stage_prepare's Documents"`
	Content         string        `json:"Content" jsonschema:"the full document content to write, replacing whatever is there"`
	ReplacesVersion string        `json:"ReplacesVersion" jsonschema:"the Version this write replaces: from the read_document or stage_prepare you drafted from, which is no_document_yet when the document doesn't exist yet; the write is refused if the document changed since. To overwrite on the user's say-so, read_document first and pass its Version"`
}

type writeDocumentOutput struct {
	Path         string `json:"Path"`
	BytesWritten int64  `json:"BytesWritten"`
}

func writeDocumentHandler(docs documentService) mcp.ToolHandlerFor[writeDocumentInput, writeDocumentOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in writeDocumentInput) (*mcp.CallToolResult, writeDocumentOutput, error) {
		path, err := docs.Write(ctx, in.ApplicationID, in.DocumentType, in.Content, in.ReplacesVersion, document.SourceWriteDocument)
		if errors.Is(err, documents.ErrChanged) {
			if in.ReplacesVersion == service.NoDocumentYet {
				return nil, writeDocumentOutput{}, fmt.Errorf("write_document: application %d already has a %s, written since you saw it missing, so nothing was written; read it with read_document and ask the user before replacing it", in.ApplicationID, in.DocumentType)
			}
			return nil, writeDocumentOutput{}, fmt.Errorf("write_document: application %d's %s changed since you read it, so nothing was written; read it again with read_document, apply your changes to what's there now, and pass its Version as ReplacesVersion", in.ApplicationID, in.DocumentType)
		}
		if err != nil {
			return nil, writeDocumentOutput{}, documentToolError("write_document", in.ApplicationID, in.DocumentType, ", so nothing was written", err)
		}
		return nil, writeDocumentOutput{Path: path, BytesWritten: int64(len(in.Content))}, nil
	}
}

// documentToolError rewords the errors for a document the agent can't
// use into what to do next; consequence says what the call didn't do, if
// anything. An agent may hold an ID from long ago, so "deleted" and
// "never existed" get different advice.
func documentToolError(tool string, applicationID int64, documentType document.Type, consequence string, err error) error {
	var deleted *service.ApplicationDeletedError
	switch {
	case errors.As(err, &deleted):
		return fmt.Errorf("%s: application %d was deleted by the user%s; ask the user whether to start again before calling stage_prepare with PostingID %d, which starts a fresh application", tool, applicationID, consequence, deleted.PostingID)
	case errors.Is(err, service.ErrApplicationNotFound):
		return fmt.Errorf("%s: there is no application %d%s; get the ApplicationID from stage_prepare for the posting you are drafting", tool, applicationID, consequence)
	case errors.Is(err, service.ErrDocumentNotWritten):
		return fmt.Errorf("%s: application %d has no %s yet; draft one and save it with write_document", tool, applicationID, documentType)
	case errors.Is(err, service.ErrNoVersion):
		return fmt.Errorf("%s: ReplacesVersion is empty%s; pass %q if you expect no document yet, or the Version stage_prepare or read_document gave you", tool, consequence, service.NoDocumentYet)
	default:
		return fmt.Errorf("%s: %w", tool, err)
	}
}

type readDocumentInput struct {
	ApplicationID int64         `json:"ApplicationID" jsonschema:"the application id, from stage_prepare's ApplicationID field"`
	DocumentType  document.Type `json:"DocumentType" jsonschema:"the document type: one of the keys of stage_prepare's Documents"`
}

type readDocumentOutput struct {
	Path    string `json:"Path"`
	Content string `json:"Content"`
	Version string `json:"Version" jsonschema:"pass this to write_document as ReplacesVersion when saving a revision of this content"`
}

func readDocumentHandler(docs documentService) mcp.ToolHandlerFor[readDocumentInput, readDocumentOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in readDocumentInput) (*mcp.CallToolResult, readDocumentOutput, error) {
		path, content, err := docs.Read(ctx, in.ApplicationID, in.DocumentType)
		if err != nil {
			return nil, readDocumentOutput{}, documentToolError("read_document", in.ApplicationID, in.DocumentType, "", err)
		}
		return nil, readDocumentOutput{Path: path, Content: content, Version: document.ContentSHA256(content)}, nil
	}
}

type readCanonicalInput struct{}

type readCanonicalOutput struct {
	Path    string `json:"Path"`
	Exists  bool   `json:"Exists"`
	Content string `json:"Content"`
	// Version is Content's document.ContentSHA256, which the scoring tools
	// take back as ProfileVersion.
	Version string `json:"Version"`
}

// readCanonicalHandler serves the user-maintained file read returns,
// read-only. A missing file is Exists: false rather than a tool error; the
// skill decides what to do.
func readCanonicalHandler(tool string, read func() (*service.CanonicalFile, error)) mcp.ToolHandlerFor[readCanonicalInput, readCanonicalOutput] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ readCanonicalInput) (*mcp.CallToolResult, readCanonicalOutput, error) {
		file, err := read()
		if err != nil {
			return nil, readCanonicalOutput{}, fmt.Errorf("%s: %w", tool, err)
		}
		return nil, readCanonicalOutput{Path: file.Path, Exists: file.Exists, Content: file.Content, Version: file.Version}, nil
	}
}

// documentInputSchema infers In's input schema, advertising any
// document.Type field as a string enum of document.Types()'
// names. Without the override, inference would see DocumentType's
// underlying int and advertise an integer; the SDK validates arguments
// against this schema before the handler runs, so an unknown value is a
// tool error there. Decoding into document.Type goes through its
// UnmarshalText. Panics like mcp.AddTool does on an uninferrable schema,
// since that's a programming error caught at startup.
func documentInputSchema[In any]() *jsonschema.Schema {
	types := document.Types()
	names := make([]any, len(types))
	for i, documentType := range types {
		names[i] = documentType.String()
	}
	schema, err := jsonschema.For[In](&jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{
			reflect.TypeFor[document.Type](): {Type: "string", Enum: names},
		},
	})
	if err != nil {
		panic(fmt.Sprintf("mcpserver: infer %T input schema: %v", *new(In), err))
	}
	return schema
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
