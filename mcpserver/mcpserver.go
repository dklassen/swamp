// Package mcpserver exposes swamp's agent hand-off operations as MCP
// tools -- the interface the `apply-to-posting` skill uses to reach
// Swamp data. The tool definitions here are the source of truth for each
// tool's arguments and behavior; the skill only says which tool to call
// when. It's a thin protocol adapter over the stage, documents and sync
// packages -- no business logic lives here, only translation between
// MCP's tools/call convention and their existing Go APIs.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dklassen/swamp/applicationstatus"
	"github.com/dklassen/swamp/cursor"
	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/scoring"
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
		Description: "List non-archived postings that still need a cover letter and/or resume drafted, or whose latest draft was flagged for revision: postings marked interested, plus started applications that never were.",
	}, listPostingsHandler(st))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_companies",
		Description: "List every company being tracked, by name: ID, display name, job board, and open posting count. Use it to match a company the user names, then pass its ID to search_postings.",
	}, listCompaniesHandler(st))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_postings",
		Description: "Find postings by what they are -- company, application and its status, interested, open or closed -- across every company being tracked, whether or not they have an application. Use it to resolve a posting or application the user names (get the company's ID from list_companies first), or to review the interested shortlist. Every filter is optional; by default it returns open, non-archived postings that their company's filters let through, as the user's company lists show them. Query looks postings up by words, each of which must appear in the title, company, department or location (\"platform dublin\"). Results come a page at a time: pass NextCursor back as Cursor, with the same filters, for the next page; it's null on the last page. Total is the number of matches when the page was read: if it's large, narrow the filters rather than paging through everything.",
		InputSchema: searchPostingsInputSchema(),
	}, searchPostingsHandler(st))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_posting",
		Description: "Read one posting in full: its summary, the description as text, the job URL, its company's description, and Version, the version of the content you read. Use it to judge a posting before scoring it; a score must name the Version it was given for as its PostingVersion. Refuses an unknown posting, or one at a company the user deleted.",
	}, readPostingHandler(st))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_pending_postings",
		Description: "List the postings still to score for fit, a page at a time, newest first (the all-postings view's default order): each one's ID, company, title, department, location, workplace type, published date, Version (which its score must name as PostingVersion) and why it's pending: new (never scored) or changed (its content changed since). Pass read_profile's Version as ProfileVersion; it's refused if the profile has changed since. Rescore lists postings with no score against this profile instead, which after a profile edit is all of them (reason rescore): use it only when the user asks for a rescore. Eligible postings are open, not archived, not interested, without an application, and let through by their company's filters. Pass NextCursor back as Cursor, with the same ProfileVersion and Rescore, for the next page; it's null on the last page. Total is how many are pending.",
	}, listPendingPostingsHandler(st))

	mcp.AddTool(server, &mcp.Tool{
		Name:         "record_scores",
		Description:  "Record how well postings fit the user, in one batch: each score is 1 (no fit) to 5 (apply now), with a one-line reason shown next to it and, as PostingVersion, the Version you read from list_pending_postings or read_posting. Pass your own model ID and read_profile's Version as ProfileVersion; a stale one records nothing. Batch many scores per call. Each entry is recorded or refused, with why: no_such_posting, score_out_of_range, no_reason, content_changed (the posting changed since you read it; it stays pending) or already_scored (a score on that content, against that profile, from that model exists).",
		OutputSchema: recordScoresOutputSchema(),
	}, recordScoresHandler(st))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "stage_prepare",
		Description: "Commit to drafting one posting's application: creates its application record if one doesn't exist yet, ensures its document directory exists, and returns the resolved cover letter/resume paths plus any existing review feedback. Idempotent -- safe to call again for the same posting.",
	}, stagePrepareHandler(st))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "write_document",
		Description: "Write drafted cover letter or resume content to the path stage_prepare resolved for an application, the same effect writing the file directly would have. Refuses, writing nothing, an application that doesn't exist or that the user deleted, or (given ReplacesVersion) a document that changed since it was read; the error says what to do instead.",
		InputSchema: documentInputSchema[writeDocumentInput](),
	}, writeDocumentHandler(st))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_document",
		Description: "Read an application's current cover letter or resume content, e.g. the existing draft to revise when its latest review was flagged. Returns a tool error if that document hasn't been written yet.",
		InputSchema: documentInputSchema[readDocumentInput](),
	}, readDocumentHandler(st))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_canonical_resume",
		Description: "Read the user's canonical resume: their maintained, best-version resume in markdown, the baseline to tailor for each posting instead of writing a resume from scratch. Read-only; the user maintains it.",
	}, readCanonicalHandler("read_canonical_resume", d.CanonicalResumePath()))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_profile",
		Description: "Read the user's profile reference: their real background, experience, skills and voice/style notes in markdown, the source every draft comes from. Read-only; the user maintains it.",
	}, readCanonicalHandler("read_profile", d.ProfilePath()))

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

type listCompaniesInput struct{}

type listCompaniesOutput struct {
	Companies []stage.CompanySummary
}

func listCompaniesHandler(st *stage.Stage) mcp.ToolHandlerFor[listCompaniesInput, listCompaniesOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ listCompaniesInput) (*mcp.CallToolResult, listCompaniesOutput, error) {
		companies, err := st.Companies(ctx)
		if err != nil {
			return nil, listCompaniesOutput{}, fmt.Errorf("list_companies: %w", err)
		}
		return nil, listCompaniesOutput{Companies: companies}, nil
	}
}

type searchPostingsInput struct {
	CompanyID           int64    `json:"CompanyID,omitempty" jsonschema:"one company's ID, from list_companies"`
	HasApplication      *bool    `json:"HasApplication,omitempty" jsonschema:"true: only postings with an application; false: only postings without one"`
	ApplicationStatuses []string `json:"ApplicationStatuses,omitempty" jsonschema:"only applications at one of these statuses"`
	Interested          *bool    `json:"Interested,omitempty" jsonschema:"true: only postings marked interested; false: only postings not marked"`
	ListingStatus       string   `json:"ListingStatus,omitempty" jsonschema:"open (the default), closed or any"`
	IncludeArchived     bool     `json:"IncludeArchived,omitempty" jsonschema:"include postings the user archived"`
	Sort                string   `json:"Sort,omitempty"` // enum and description from stage.SortOrders(), in searchPostingsInputSchema
	Cursor              string   `json:"Cursor,omitempty" jsonschema:"the previous page's NextCursor, unchanged; omit for the first page"`
	Query               string   `json:"Query,omitempty" jsonschema:"space-separated words, each of which must appear in the title, company, department or location, ignoring case"`
	Limit               int      `json:"Limit,omitempty" jsonschema:"postings per page: 50 by default, at most 100"`
}

// searchPostingsInputSchema infers search_postings' input schema and
// advertises its controlled values as enums, so the agent learns them
// from the schema instead of guessing (RFC 0006). The SDK validates
// arguments against it before the handler runs. Panics like mcp.AddTool
// on an uninferrable schema: a programming error caught at startup.
func searchPostingsInputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[searchPostingsInput](nil)
	if err != nil {
		panic(fmt.Sprintf("mcpserver: infer search_postings input schema: %v", err))
	}
	var statuses []any
	for _, status := range applicationstatus.All() {
		statuses = append(statuses, status.String())
	}
	schema.Properties["ApplicationStatuses"].Items.Enum = statuses
	schema.Properties["ListingStatus"].Enum = []any{"open", "closed", "any"}
	sort := schema.Properties["Sort"]
	sort.Enum = nil
	description := []string{"the order to page in, one of:"}
	for _, order := range stage.SortOrders() {
		sort.Enum = append(sort.Enum, order.Name())
		description = append(description, order.Name()+": "+order.Description)
	}
	sort.Description = strings.Join(description, "\n")
	return schema
}

// Out is 'any': stage.SearchMatch carries applicationstatus.Status, whose
// MarshalJSON writes a string where schema inference would see an
// integer (the same problem listPostingsHandler works around).
func searchPostingsHandler(st *stage.Stage) mcp.ToolHandlerFor[searchPostingsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in searchPostingsInput) (*mcp.CallToolResult, any, error) {
		statuses := make([]applicationstatus.Status, len(in.ApplicationStatuses))
		for i, name := range in.ApplicationStatuses {
			status, err := applicationstatus.Parse(name)
			if err != nil {
				return nil, nil, fmt.Errorf("search_postings: %w", err)
			}
			statuses[i] = status
		}
		result, err := st.Search(ctx, stage.SearchOptions{
			CompanyID:           in.CompanyID,
			HasApplication:      in.HasApplication,
			ApplicationStatuses: statuses,
			Interested:          in.Interested,
			ListingStatus:       in.ListingStatus,
			IncludeArchived:     in.IncludeArchived,
			Query:               in.Query,
			Sort:                in.Sort,
			Cursor:              in.Cursor,
			Limit:               in.Limit,
		})
		switch {
		case errors.Is(err, cursor.ErrMismatch):
			return nil, nil, errors.New("search_postings: this cursor belongs to a different search (other filters or sort); start again without a cursor")
		case errors.Is(err, cursor.ErrInvalid):
			return nil, nil, errors.New("search_postings: that isn't a cursor from search_postings; start again without a cursor")
		case err != nil:
			return nil, nil, fmt.Errorf("search_postings: %w", err)
		}
		return nil, result, nil
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

type readPostingInput struct {
	PostingID int64 `json:"PostingID" jsonschema:"the posting's id, from search_postings' Posting.ID field"`
}

func readPostingHandler(st *stage.Stage) mcp.ToolHandlerFor[readPostingInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in readPostingInput) (*mcp.CallToolResult, any, error) {
		posting, err := st.ReadPosting(ctx, in.PostingID)
		if err != nil {
			return nil, nil, fmt.Errorf("read_posting: %w", err)
		}
		return nil, posting, nil
	}
}

type listPendingPostingsInput struct {
	ProfileVersion string `json:"ProfileVersion" jsonschema:"read_profile's Version"`
	Rescore        bool   `json:"Rescore,omitempty" jsonschema:"list postings with no score against this profile, not just none at all; only when the user asks for a rescore"`
	Cursor         string `json:"Cursor,omitempty" jsonschema:"the previous page's NextCursor, unchanged; omit for the first page"`
	Limit          int    `json:"Limit,omitempty" jsonschema:"postings per page: 50 by default, at most 100"`
}

func listPendingPostingsHandler(st *stage.Stage) mcp.ToolHandlerFor[listPendingPostingsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listPendingPostingsInput) (*mcp.CallToolResult, any, error) {
		result, err := st.PendingPostings(ctx, stage.PendingOptions{ProfileVersion: in.ProfileVersion, Rescore: in.Rescore, Cursor: in.Cursor, Limit: in.Limit})
		if err != nil {
			return nil, nil, fmt.Errorf("list_pending_postings: %w", err)
		}
		return nil, result, nil
	}
}

type recordScoresInput struct {
	Model          string             `json:"Model" jsonschema:"your model ID, e.g. claude-opus-5-5"`
	ProfileVersion string             `json:"ProfileVersion" jsonschema:"read_profile's Version"`
	Scores         []stage.ScoreInput `json:"Scores" jsonschema:"the scores, each with PostingID, Score (1-5), Reason (one line) and, as PostingVersion, the posting's Version you read"`
}

// recordScoresOutputSchema advertises a refusal's Why as its name, one of
// scoring's refusals: inferred, it would be scoring.Refusal's int.
func recordScoresOutputSchema() *jsonschema.Schema {
	schema, err := jsonschema.For[stage.RecordResult](nil)
	if err != nil {
		panic(fmt.Sprintf("mcpserver: infer record_scores output schema: %v", err))
	}
	var names []any
	for _, refusal := range scoring.Refusals() {
		names = append(names, refusal.String())
	}
	schema.Properties["Refused"].Items.Properties["Why"] = &jsonschema.Schema{Type: "string", Enum: names}
	return schema
}

func recordScoresHandler(st *stage.Stage) mcp.ToolHandlerFor[recordScoresInput, stage.RecordResult] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in recordScoresInput) (*mcp.CallToolResult, stage.RecordResult, error) {
		opts := stage.RecordOptions{Model: in.Model, ProfileVersion: in.ProfileVersion, Scores: in.Scores}
		// Client info is optional in MCP; scores without it record none.
		if params := req.Session.InitializeParams(); params != nil && params.ClientInfo != nil {
			opts.ClientName, opts.ClientVersion = params.ClientInfo.Name, params.ClientInfo.Version
		}
		result, err := st.RecordScores(ctx, opts)
		if err != nil {
			return nil, stage.RecordResult{}, fmt.Errorf("record_scores: %w", err)
		}
		return nil, result, nil
	}
}

type writeDocumentInput struct {
	ApplicationID int64          `json:"ApplicationID" jsonschema:"the application id, from stage_prepare's ApplicationID field"`
	DocumentType  documents.Type `json:"DocumentType" jsonschema:"the document type: one of the keys of stage_prepare's Documents"`
	Content       string         `json:"Content" jsonschema:"the full document content to write, replacing whatever is there"`
	// A pointer so an empty value is refused rather than taken for omitted,
	// which writes unconditionally.
	ReplacesVersion *string `json:"ReplacesVersion,omitempty" jsonschema:"the Version this write replaces: from the read_document or stage_prepare you drafted from, which is no_document_yet when the document doesn't exist yet; the write is refused if the document changed since. Omit only when the user asked to overwrite regardless"`
}

type writeDocumentOutput struct {
	Path         string `json:"Path"`
	BytesWritten int64  `json:"BytesWritten"`
}

func writeDocumentHandler(st *stage.Stage) mcp.ToolHandlerFor[writeDocumentInput, writeDocumentOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in writeDocumentInput) (*mcp.CallToolResult, writeDocumentOutput, error) {
		path, err := st.WriteDocument(ctx, in.ApplicationID, in.DocumentType, in.Content, in.ReplacesVersion)
		if errors.Is(err, documents.ErrChanged) {
			if *in.ReplacesVersion == stage.NoDocumentYet {
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

// documentToolError rewords stage's errors for a document the agent can't
// use into what to do next; consequence says what the call didn't do, if
// anything. An agent may hold an ID from long ago, so "deleted" and
// "never existed" get different advice.
func documentToolError(tool string, applicationID int64, documentType documents.Type, consequence string, err error) error {
	var deleted *stage.ApplicationDeletedError
	switch {
	case errors.As(err, &deleted):
		return fmt.Errorf("%s: application %d was deleted by the user%s; ask the user whether to start again before calling stage_prepare with PostingID %d, which starts a fresh application", tool, applicationID, consequence, deleted.PostingID)
	case errors.Is(err, stage.ErrApplicationNotFound):
		return fmt.Errorf("%s: there is no application %d%s; get the ApplicationID from stage_prepare for the posting you are drafting", tool, applicationID, consequence)
	case errors.Is(err, stage.ErrDocumentNotWritten):
		return fmt.Errorf("%s: application %d has no %s yet; draft one and save it with write_document", tool, applicationID, documentType)
	case errors.Is(err, stage.ErrEmptyReplacesVersion):
		return fmt.Errorf("%s: ReplacesVersion is empty%s; pass %q if you expect no document yet, or the Version stage_prepare or read_document gave you", tool, consequence, stage.NoDocumentYet)
	default:
		return fmt.Errorf("%s: %w", tool, err)
	}
}

type readDocumentInput struct {
	ApplicationID int64          `json:"ApplicationID" jsonschema:"the application id, from stage_prepare's ApplicationID field"`
	DocumentType  documents.Type `json:"DocumentType" jsonschema:"the document type: one of the keys of stage_prepare's Documents"`
}

type readDocumentOutput struct {
	Path    string `json:"Path"`
	Content string `json:"Content"`
	Version string `json:"Version" jsonschema:"pass this to write_document as ReplacesVersion when saving a revision of this content"`
}

func readDocumentHandler(st *stage.Stage) mcp.ToolHandlerFor[readDocumentInput, readDocumentOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in readDocumentInput) (*mcp.CallToolResult, readDocumentOutput, error) {
		path, content, err := st.ReadDocument(ctx, in.ApplicationID, in.DocumentType)
		if err != nil {
			return nil, readDocumentOutput{}, documentToolError("read_document", in.ApplicationID, in.DocumentType, "", err)
		}
		return nil, readDocumentOutput{Path: path, Content: content, Version: documents.ContentSHA256(content)}, nil
	}
}

type readCanonicalInput struct{}

type readCanonicalOutput struct {
	Path    string `json:"Path"`
	Exists  bool   `json:"Exists"`
	Content string `json:"Content"`
	// Version is Content's documents.ContentSHA256, which the scoring tools
	// take back as ProfileVersion.
	Version string `json:"Version"`
}

// readCanonicalHandler serves the user-maintained file at path, read-only.
// A missing file is an expected state (the user hasn't set one up), so it's
// Exists: false rather than a tool error; the skill decides what to do.
func readCanonicalHandler(tool, path string) mcp.ToolHandlerFor[readCanonicalInput, readCanonicalOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ readCanonicalInput) (*mcp.CallToolResult, readCanonicalOutput, error) {
		content, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, readCanonicalOutput{Path: path}, nil
		}
		if err != nil {
			return nil, readCanonicalOutput{}, fmt.Errorf("%s: read %s: %w", tool, path, err)
		}
		return nil, readCanonicalOutput{Path: path, Exists: true, Content: string(content), Version: documents.ContentSHA256(string(content))}, nil
	}
}

// documentInputSchema infers In's input schema, advertising any
// documents.Type field as a string enum of documents.Types()'
// names. Without the override, inference would see DocumentType's
// underlying int and advertise an integer; the SDK validates arguments
// against this schema before the handler runs, so an unknown value is a
// tool error there. Decoding into documents.Type goes through its
// UnmarshalText. Panics like mcp.AddTool does on an uninferrable schema,
// since that's a programming error caught at startup.
func documentInputSchema[In any]() *jsonschema.Schema {
	var names []any
	for _, documentType := range documents.Types() {
		names = append(names, documentType.String())
	}
	schema, err := jsonschema.For[In](&jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{
			reflect.TypeFor[documents.Type](): {Type: "string", Enum: names},
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
