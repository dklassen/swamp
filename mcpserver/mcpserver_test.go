package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pressly/goose/v3"

	"github.com/google/go-cmp/cmp"

	_ "modernc.org/sqlite"

	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/stage"
	"github.com/dklassen/swamp/store"
	swampsync "github.com/dklassen/swamp/sync"
)

// fakeFetcher stands in for a job board API: a slug with an entry in boards
// resolves to those postings, any other slug is rejected like a 404.
type fakeFetcher struct {
	boards map[string][]jobboard.Posting
}

func (f fakeFetcher) FetchPostings(_ context.Context, slug string) ([]jobboard.Posting, error) {
	postings, ok := f.boards[slug]
	if !ok {
		return nil, fmt.Errorf("board %q not found", slug)
	}
	return postings, nil
}

func newTestServer(t *testing.T) (*mcp.Server, *store.Store, *documents.Store) {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", "file:"+t.TempDir()+"/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	s := store.New(sqlDB)
	d := documents.NewStore(t.TempDir())
	fetcher := fakeFetcher{boards: map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}, {SourceID: "job-2", Title: "Designer"}},
	}}
	syncer := swampsync.New(s, map[string]swampsync.PostingFetcher{"ashby": fetcher}, swampsync.DefaultConfig())
	return New(stage.New(s, d), d, syncer), s, d
}

// connectClient wires an in-process MCP client to srv over an in-memory
// transport pair, running the server's side of the session in the
// background for the lifetime of the test.
func connectClient(t *testing.T, srv *mcp.Server) *mcp.ClientSession {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	go func() {
		if err := srv.Run(ctx, serverTransport); err != nil && ctx.Err() == nil {
			t.Errorf("server run: %v", err)
		}
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "mcpserver-test", Version: "test"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		if err := cs.Close(); err != nil {
			t.Errorf("close client session: %v", err)
		}
	})
	return cs
}

// callTool calls name with args, fails the test if the call itself errors
// or the tool reports IsError, and unmarshals the result's structured
// content into an Out value.
func callTool[Out any](t *testing.T, cs *mcp.ClientSession, name string, args any) Out {
	t.Helper()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool(%s) returned a tool error: %+v", name, res.Content)
	}

	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	// The MCP spec requires structuredContent to be a JSON object, and
	// real clients (e.g. Claude Code) reject anything else -- the SDK's
	// own client doesn't check, so this helper has to.
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("CallTool(%s) structured content is not a JSON object: %s", name, raw)
	}
	var out Out
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structured content into %T: %v", out, err)
	}
	return out
}

func mustCreateCompany(t *testing.T, s *store.Store, name string) store.Company {
	t.Helper()
	c, err := s.CreateCompany(context.Background(), name, "ashby", name)
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return c
}

func mustUpsertPosting(t *testing.T, s *store.Store, companyID int64, sourceID, title string) store.Posting {
	t.Helper()
	p, err := s.UpsertPosting(context.Background(), store.CreatePostingParams{
		CompanyID: companyID,
		Source:    "ashby",
		SourceID:  sourceID,
		IngestedFields: store.IngestedFields{
			Title:      title,
			RawPayload: `{"id":"` + sourceID + `"}`,
		},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	return p
}

func mustMarkInterested(t *testing.T, s *store.Store, postingID int64) {
	t.Helper()
	if _, err := s.SetPostingInterested(context.Background(), postingID); err != nil {
		t.Fatalf("SetPostingInterested: %v", err)
	}
}

func TestListPostings_ReturnsInterestedCandidates(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Senior Data Engineer")
	mustMarkInterested(t, s, posting.ID)

	cs := connectClient(t, srv)

	got := callTool[listPostingsOutput](t, cs, "list_postings", map[string]any{}).Postings

	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (%+v)", len(got), got)
	}
	if got[0].Posting.ID != posting.ID {
		t.Errorf("Posting.ID = %d, want %d", got[0].Posting.ID, posting.ID)
	}
	if diff := cmp.Diff("Acme", got[0].CompanyName); diff != "" {
		t.Errorf("CompanyName mismatch (-want +got):\n%s", diff)
	}
}

func TestStagePrepare_CreatesApplicationAndReturnsDocumentPaths(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Senior Data Engineer")
	mustMarkInterested(t, s, posting.ID)

	cs := connectClient(t, srv)

	got := callTool[stage.Prepared](t, cs, "stage_prepare", map[string]any{"PostingID": posting.ID})

	if got.ApplicationID == 0 {
		t.Fatal("ApplicationID = 0, want a created application id")
	}
	if diff := cmp.Diff("Acme", got.CompanyName); diff != "" {
		t.Errorf("CompanyName mismatch (-want +got):\n%s", diff)
	}
	if got.Documents[documents.CoverLetter].Path == "" {
		t.Error("CoverLetter.Path is empty, want a resolved path")
	}
	if got.Documents[documents.CoverLetter].Exists {
		t.Error("CoverLetter.Exists = true, want false (nothing drafted yet)")
	}

	application, err := s.GetApplication(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if application.ID != got.ApplicationID {
		t.Errorf("persisted application id = %d, want %d", application.ID, got.ApplicationID)
	}
}

func TestWriteDocument_WritesContentToTheResolvedPath(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Senior Data Engineer")
	mustMarkInterested(t, s, posting.ID)

	cs := connectClient(t, srv)
	prepared := callTool[stage.Prepared](t, cs, "stage_prepare", map[string]any{"PostingID": posting.ID})

	for _, tc := range []struct {
		documentType string
		wantPath     string
	}{
		{"cover_letter", prepared.Documents[documents.CoverLetter].Path},
		{"resume", prepared.Documents[documents.Resume].Path},
	} {
		t.Run(tc.documentType, func(t *testing.T) {
			content := "content for " + tc.documentType

			got := callTool[writeDocumentOutput](t, cs, "write_document", map[string]any{
				"ApplicationID": prepared.ApplicationID,
				"DocumentType":  tc.documentType,
				"Content":       content,
			})

			if diff := cmp.Diff(tc.wantPath, got.Path); diff != "" {
				t.Errorf("Path mismatch (-want +got):\n%s", diff)
			}
			if got.BytesWritten != int64(len(content)) {
				t.Errorf("BytesWritten = %d, want %d", got.BytesWritten, len(content))
			}

			onDisk, err := os.ReadFile(tc.wantPath)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", tc.wantPath, err)
			}
			if string(onDisk) != content {
				t.Errorf("file content = %q, want %q", onDisk, content)
			}
		})
	}
}

func TestWriteDocument_RejectsInvalidDocumentType(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Senior Data Engineer")
	mustMarkInterested(t, s, posting.ID)

	cs := connectClient(t, srv)
	prepared := callTool[stage.Prepared](t, cs, "stage_prepare", map[string]any{"PostingID": posting.ID})

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "write_document",
		Arguments: map[string]any{
			"ApplicationID": prepared.ApplicationID,
			"DocumentType":  "resumeee",
			"Content":       "whatever",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true for an invalid DocumentType")
	}
}

// callToolError calls name with args and returns the text of the tool
// error it reports, failing the test if the call succeeds instead.
func callToolError(t *testing.T, cs *mcp.ClientSession, name string, args any) string {
	t.Helper()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if !res.IsError {
		t.Fatalf("CallTool(%s): IsError = false, want a tool error (%+v)", name, res.StructuredContent)
	}
	text, _ := res.Content[0].(*mcp.TextContent)
	if text == nil {
		t.Fatalf("CallTool(%s): tool error content = %+v, want text", name, res.Content)
	}
	return text.Text
}

// TestWriteDocument_UnknownApplication_WritesNothing: an ID with no
// application row must not get a documents folder, or the application
// later given that ID inherits the draft.
func TestWriteDocument_UnknownApplication_WritesNothing(t *testing.T) {
	t.Parallel()

	srv, _, d := newTestServer(t)
	cs := connectClient(t, srv)

	got := callToolError(t, cs, "write_document", map[string]any{
		"ApplicationID": 999,
		"DocumentType":  "cover_letter",
		"Content":       "a draft",
	})

	want := "write_document: there is no application 999, so nothing was written; get the ApplicationID from stage_prepare for the posting you are drafting"
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("tool error mismatch (-want +got):\n%s", diff)
	}
	path, err := d.Path(999, documents.CoverLetter)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat(%s): err = %v, want the folder not created", filepath.Dir(path), err)
	}
}

// TestWriteDocument_DeletedApplication_WritesNothing: the user deleted the
// application in the TUI while an agent still held its ID. The error
// names the posting, since stage_prepare is how the agent would start
// again -- if the user wants that.
func TestWriteDocument_DeletedApplication_WritesNothing(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Senior Data Engineer")
	mustMarkInterested(t, s, posting.ID)
	cs := connectClient(t, srv)
	prepared := callTool[stage.Prepared](t, cs, "stage_prepare", map[string]any{"PostingID": posting.ID})
	if err := s.DeleteApplication(context.Background(), prepared.ApplicationID); err != nil {
		t.Fatalf("DeleteApplication: %v", err)
	}

	got := callToolError(t, cs, "write_document", map[string]any{
		"ApplicationID": prepared.ApplicationID,
		"DocumentType":  "cover_letter",
		"Content":       "a draft",
	})

	want := fmt.Sprintf("write_document: application %d was deleted by the user, so nothing was written; ask the user whether to start again before calling stage_prepare with PostingID %d, which starts a fresh application", prepared.ApplicationID, posting.ID)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("tool error mismatch (-want +got):\n%s", diff)
	}
	path := prepared.Documents[documents.CoverLetter].Path
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat(%s): err = %v, want no draft written", path, err)
	}
}

func TestAddCompany_NewBoard_CreatesCompanyWithDescription(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	cs := connectClient(t, srv)

	const description = "Acme builds rockets for roadrunner enthusiasts."
	got := callTool[addCompanyOutput](t, cs, "add_company", map[string]any{
		"Name":        "Acme",
		"Source":      "ashby",
		"Slug":        "acme",
		"Description": description,
	})

	want := addCompanyOutput{Outcome: "created", CompanyID: got.CompanyID, Name: "Acme", OpenJobs: 2}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("add_company result mismatch (-want +got):\n%s", diff)
	}

	stored, err := s.GetCompany(context.Background(), got.CompanyID)
	if err != nil {
		t.Fatalf("GetCompany(%d): %v", got.CompanyID, err)
	}
	if stored.Description != description {
		t.Errorf("stored Description = %q, want %q", stored.Description, description)
	}
}

func TestAddCompany_ExistingCompany_ReportsOutcome(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		softDelete bool
		want       string
	}{
		{"active company", false, "already_exists"},
		{"company the user deleted", true, "skipped_deleted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv, s, _ := newTestServer(t)
			acme, err := s.CreateCompany(context.Background(), "Acme", "ashby", "acme")
			if err != nil {
				t.Fatalf("CreateCompany: %v", err)
			}
			if tc.softDelete {
				if err := s.SoftDeleteCompany(context.Background(), acme.ID); err != nil {
					t.Fatalf("SoftDeleteCompany: %v", err)
				}
			}
			cs := connectClient(t, srv)

			got := callTool[addCompanyOutput](t, cs, "add_company", map[string]any{
				"Name": "Acme", "Source": "ashby", "Slug": "acme", "Description": "whatever",
			})

			if got.Outcome != tc.want {
				t.Errorf("Outcome = %q, want %q", got.Outcome, tc.want)
			}
			if got.CompanyID != acme.ID {
				t.Errorf("CompanyID = %d, want existing company's id %d", got.CompanyID, acme.ID)
			}
		})
	}
}

func TestAddCompany_BoardRejectsSlug_ReturnsToolErrorAndCreatesNothing(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	cs := connectClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "add_company",
		Arguments: map[string]any{
			"Name": "Nope", "Source": "ashby", "Slug": "nope", "Description": "whatever",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true for a slug the board rejects")
	}

	companies, err := s.ListActiveCompanies(context.Background())
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	if len(companies) != 0 {
		t.Fatalf("got %d companies, want 0", len(companies))
	}
}

func TestReadDocument_ReturnsWrittenContent(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Senior Data Engineer")
	mustMarkInterested(t, s, posting.ID)

	cs := connectClient(t, srv)
	prepared := callTool[stage.Prepared](t, cs, "stage_prepare", map[string]any{"PostingID": posting.ID})

	for _, tc := range []struct {
		documentType string
		wantPath     string
	}{
		{"cover_letter", prepared.Documents[documents.CoverLetter].Path},
		{"resume", prepared.Documents[documents.Resume].Path},
	} {
		t.Run(tc.documentType, func(t *testing.T) {
			content := "draft of " + tc.documentType
			callTool[writeDocumentOutput](t, cs, "write_document", map[string]any{
				"ApplicationID": prepared.ApplicationID,
				"DocumentType":  tc.documentType,
				"Content":       content,
			})

			got := callTool[readDocumentOutput](t, cs, "read_document", map[string]any{
				"ApplicationID": prepared.ApplicationID,
				"DocumentType":  tc.documentType,
			})

			want := readDocumentOutput{Path: tc.wantPath, Content: content, SHA256: documents.ContentSHA256(content)}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("read_document result mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReadDocument_ReturnsToolError(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		documentType string
	}{
		{"document not written yet", "cover_letter"},
		{"invalid DocumentType", "resumeee"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv, s, _ := newTestServer(t)
			company := mustCreateCompany(t, s, "Acme")
			posting := mustUpsertPosting(t, s, company.ID, "job-1", "Senior Data Engineer")
			mustMarkInterested(t, s, posting.ID)

			cs := connectClient(t, srv)
			prepared := callTool[stage.Prepared](t, cs, "stage_prepare", map[string]any{"PostingID": posting.ID})

			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "read_document",
				Arguments: map[string]any{
					"ApplicationID": prepared.ApplicationID,
					"DocumentType":  tc.documentType,
				},
			})
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			if !res.IsError {
				t.Fatalf("IsError = false, want true (%+v)", res.StructuredContent)
			}
		})
	}
}

func TestReadDocument_UnknownApplication_SaysSo(t *testing.T) {
	t.Parallel()

	srv, _, _ := newTestServer(t)
	cs := connectClient(t, srv)

	got := callToolError(t, cs, "read_document", map[string]any{
		"ApplicationID": 999,
		"DocumentType":  "cover_letter",
	})

	want := "read_document: there is no application 999; get the ApplicationID from stage_prepare for the posting you are drafting"
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("tool error mismatch (-want +got):\n%s", diff)
	}
}

func TestReadDocument_DeletedApplication_SaysSo(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Senior Data Engineer")
	mustMarkInterested(t, s, posting.ID)
	cs := connectClient(t, srv)
	prepared := callTool[stage.Prepared](t, cs, "stage_prepare", map[string]any{"PostingID": posting.ID})
	callTool[writeDocumentOutput](t, cs, "write_document", map[string]any{
		"ApplicationID": prepared.ApplicationID,
		"DocumentType":  "cover_letter",
		"Content":       "a draft",
	})
	if err := s.DeleteApplication(context.Background(), prepared.ApplicationID); err != nil {
		t.Fatalf("DeleteApplication: %v", err)
	}

	got := callToolError(t, cs, "read_document", map[string]any{
		"ApplicationID": prepared.ApplicationID,
		"DocumentType":  "cover_letter",
	})

	want := fmt.Sprintf("read_document: application %d was deleted by the user; ask the user whether to start again before calling stage_prepare with PostingID %d, which starts a fresh application", prepared.ApplicationID, posting.ID)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("tool error mismatch (-want +got):\n%s", diff)
	}
}

func TestReadDocument_NotWrittenYet_SaysSo(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Senior Data Engineer")
	mustMarkInterested(t, s, posting.ID)
	cs := connectClient(t, srv)
	prepared := callTool[stage.Prepared](t, cs, "stage_prepare", map[string]any{"PostingID": posting.ID})

	got := callToolError(t, cs, "read_document", map[string]any{
		"ApplicationID": prepared.ApplicationID,
		"DocumentType":  "cover_letter",
	})

	want := fmt.Sprintf("read_document: application %d has no cover_letter yet; draft one and save it with write_document", prepared.ApplicationID)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("tool error mismatch (-want +got):\n%s", diff)
	}
}

func TestDocumentTools_AdvertiseDocumentTypeAsStringEnum(t *testing.T) {
	t.Parallel()

	srv, _, _ := newTestServer(t)
	cs := connectClient(t, srv)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	schemas := map[string]any{}
	for _, tool := range res.Tools {
		schemas[tool.Name] = tool.InputSchema
	}

	want := map[string]any{
		"type":        "string",
		"enum":        []any{"cover_letter", "resume"},
		"description": "the document type: one of the keys of stage_prepare's Documents",
	}
	for _, name := range []string{"write_document", "read_document"} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(schemas[name])
			if err != nil {
				t.Fatalf("marshal %s input schema: %v", name, err)
			}
			var schema struct {
				Properties map[string]map[string]any `json:"properties"`
			}
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatalf("unmarshal %s input schema: %v", name, err)
			}
			if diff := cmp.Diff(want, schema.Properties["DocumentType"]); diff != "" {
				t.Errorf("DocumentType schema mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// canonicalTools are the read-only tools serving a user-maintained file,
// each with where that file lives.
var canonicalTools = []struct {
	tool string
	path func(*documents.Store) string
}{
	{"read_canonical_resume", (*documents.Store).CanonicalResumePath},
	{"read_profile", (*documents.Store).ProfilePath},
}

func TestReadCanonical_ReturnsContentWhenPresent(t *testing.T) {
	t.Parallel()

	for _, tc := range canonicalTools {
		t.Run(tc.tool, func(t *testing.T) {
			t.Parallel()

			srv, _, d := newTestServer(t)
			path := tc.path(d)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(path, []byte("# "+tc.tool), 0o644); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}

			cs := connectClient(t, srv)
			got := callTool[readCanonicalOutput](t, cs, tc.tool, map[string]any{})

			want := readCanonicalOutput{Path: path, Exists: true, Content: "# " + tc.tool}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("%s result mismatch (-want +got):\n%s", tc.tool, diff)
			}
		})
	}
}

func TestReadCanonical_ReportsMissingFile(t *testing.T) {
	t.Parallel()

	for _, tc := range canonicalTools {
		t.Run(tc.tool, func(t *testing.T) {
			t.Parallel()

			srv, _, d := newTestServer(t)
			cs := connectClient(t, srv)
			got := callTool[readCanonicalOutput](t, cs, tc.tool, map[string]any{})

			want := readCanonicalOutput{Path: tc.path(d), Exists: false}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("%s result mismatch (-want +got):\n%s", tc.tool, diff)
			}
		})
	}
}

func TestStagePrepare_ClosedPosting_ReturnsToolErrorSayingSo(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Senior Data Engineer")
	if err := s.MarkPostingClosed(context.Background(), posting.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}
	cs := connectClient(t, srv)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "stage_prepare",
		Arguments: map[string]any{"PostingID": posting.ID},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true for a closed posting")
	}
	text, _ := res.Content[0].(*mcp.TextContent)
	if text == nil || !strings.Contains(text.Text, "posting is closed") {
		t.Errorf("tool error = %+v, want it to say the posting is closed", res.Content)
	}
}

type listCompaniesResult struct {
	Companies []struct {
		ID           int64
		Name         string
		Source       string
		OpenPostings int
	}
}

// TestListCompanies_LiveCompaniesByNameWithOpenCounts: the agent's company
// vocabulary for search_postings (#220, RFC 0006). Deleted companies are
// left out; each company's count is its open, unarchived postings.
func TestListCompanies_LiveCompaniesByNameWithOpenCounts(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	ctx := context.Background()
	beta := mustCreateCompany(t, s, "Beta")
	acme := mustCreateCompany(t, s, "Acme")
	gone := mustCreateCompany(t, s, "Gone")
	mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustUpsertPosting(t, s, acme.ID, "job-2", "Designer")
	closed := mustUpsertPosting(t, s, acme.ID, "job-3", "Writer")
	if err := s.MarkPostingClosed(ctx, closed.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}
	if err := s.SoftDeleteCompany(ctx, gone.ID); err != nil {
		t.Fatalf("SoftDeleteCompany: %v", err)
	}
	cs := connectClient(t, srv)

	got := callTool[listCompaniesResult](t, cs, "list_companies", map[string]any{}).Companies

	type company struct {
		ID           int64
		Name, Source string
		OpenPostings int
	}
	gotCompanies := make([]company, len(got))
	for i, c := range got {
		gotCompanies[i] = company{c.ID, c.Name, c.Source, c.OpenPostings}
	}
	want := []company{
		{acme.ID, "Acme", "ashby", 2},
		{beta.ID, "Beta", "ashby", 0},
	}
	if diff := cmp.Diff(want, gotCompanies); diff != "" {
		t.Errorf("list_companies mismatch (-want +got):\n%s", diff)
	}
}

// searchPostingsResult is search_postings' output, decoded with plain
// strings for the enums.
type searchPostingsResult struct {
	Postings []struct {
		Posting           struct{ ID int64 }
		CompanyName       string
		ApplicationID     *int64
		ApplicationStatus *string
	}
	NextCursor *string
	Total      int
}

func searchIDs(r searchPostingsResult) []int64 {
	ids := make([]int64, len(r.Postings))
	for i, p := range r.Postings {
		ids[i] = p.Posting.ID
	}
	return ids
}

// TestSearchPostings_FindsADraftedApplicationByCompany is #215's case: an
// application with both documents drafted and none flagged isn't in
// list_postings, so the agent couldn't find it when the user named it.
// search_postings finds it by company and "has an application" (#221).
func TestSearchPostings_FindsADraftedApplicationByCompany(t *testing.T) {
	t.Parallel()

	srv, s, d := newTestServer(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme")
	drafted := mustUpsertPosting(t, s, acme.ID, "job-1", "Staff Software Developer, Product")
	mustUpsertPosting(t, s, acme.ID, "job-2", "Staff Software Developer, Risk")
	mustMarkInterested(t, s, drafted.ID)
	application, err := s.CreateApplication(ctx, drafted.ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if _, err := d.EnsureDir(application.ID); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	for _, documentType := range documents.Types() {
		path, err := d.Path(application.ID, documentType)
		if err != nil {
			t.Fatalf("Path: %v", err)
		}
		if err := os.WriteFile(path, []byte("drafted"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	cs := connectClient(t, srv)

	if queue := callTool[listPostingsOutput](t, cs, "list_postings", map[string]any{}).Postings; len(queue) != 0 {
		t.Fatalf("list_postings = %d postings, want 0: the drafted application isn't drafting work", len(queue))
	}
	got := callTool[searchPostingsResult](t, cs, "search_postings", map[string]any{"CompanyID": acme.ID, "HasApplication": true})
	if diff := cmp.Diff([]int64{drafted.ID}, searchIDs(got)); diff != "" {
		t.Fatalf("search_postings IDs mismatch (-want +got):\n%s", diff)
	}
	match := got.Postings[0]
	if match.CompanyName != "Acme" || match.ApplicationID == nil || *match.ApplicationID != application.ID ||
		match.ApplicationStatus == nil || *match.ApplicationStatus != "application_started" {
		t.Errorf("match = %+v, want Acme with application %d, application_started", match, application.ID)
	}
}

// TestSearchPostings_InterestedShortlist: postings marked interested that
// have no application yet -- "what have I shortlisted but not started?"
func TestSearchPostings_InterestedShortlist(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	acme := mustCreateCompany(t, s, "Acme")
	shortlisted := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	started := mustUpsertPosting(t, s, acme.ID, "job-2", "Designer")
	mustUpsertPosting(t, s, acme.ID, "job-3", "Writer")
	mustMarkInterested(t, s, shortlisted.ID)
	mustMarkInterested(t, s, started.ID)
	if _, err := s.CreateApplication(context.Background(), started.ID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	cs := connectClient(t, srv)

	got := callTool[searchPostingsResult](t, cs, "search_postings", map[string]any{"Interested": true, "HasApplication": false})
	if diff := cmp.Diff([]int64{shortlisted.ID}, searchIDs(got)); diff != "" {
		t.Errorf("shortlist mismatch (-want +got):\n%s", diff)
	}
}

// TestSearchPostings_PagesThroughACompany: three pages of two concatenate
// to the company's five postings; a cursor passed with other filters is
// a tool error, not a page.
func TestSearchPostings_PagesThroughACompany(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	acme := mustCreateCompany(t, s, "Acme")
	other := mustCreateCompany(t, s, "Beta")
	var want []int64
	for _, id := range []string{"1", "2", "3", "4", "5"} {
		want = append(want, mustUpsertPosting(t, s, acme.ID, "job-"+id, "Engineer "+id).ID)
	}
	mustUpsertPosting(t, s, other.ID, "job-6", "Engineer")
	cs := connectClient(t, srv)

	var got []int64
	args := map[string]any{"CompanyID": acme.ID, "Limit": 2}
	var firstCursor string
	for pages := 1; ; pages++ {
		page := callTool[searchPostingsResult](t, cs, "search_postings", args)
		if page.Total != 5 {
			t.Errorf("page %d Total = %d, want 5", pages, page.Total)
		}
		got = append(got, searchIDs(page)...)
		if page.NextCursor == nil {
			if pages != 3 {
				t.Errorf("%d pages, want 3", pages)
			}
			break
		}
		if pages == 1 {
			firstCursor = *page.NextCursor
		}
		args["Cursor"] = *page.NextCursor
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("pages don't concatenate to the company's postings (-want +got):\n%s", diff)
	}

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_postings",
		Arguments: map[string]any{"CompanyID": other.ID, "Cursor": firstCursor},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("cursor with other filters: IsError = false, want a tool error")
	}
	if text, _ := res.Content[0].(*mcp.TextContent); text == nil || !strings.Contains(text.Text, "start again without a cursor") {
		t.Errorf("tool error = %+v, want it to say to start again without a cursor", res.Content)
	}
}

// TestSearchPostings_AdvertisesEnums: the agent learns the controlled
// values from the tool's schema instead of guessing them.
func TestSearchPostings_AdvertisesEnums(t *testing.T) {
	t.Parallel()

	srv, _, _ := newTestServer(t)
	cs := connectClient(t, srv)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var raw []byte
	for _, tool := range res.Tools {
		if tool.Name == "search_postings" {
			if raw, err = json.Marshal(tool.InputSchema); err != nil {
				t.Fatalf("marshal schema: %v", err)
			}
		}
	}
	var schema struct {
		Properties struct {
			ApplicationStatuses struct {
				Items struct{ Enum []string } `json:"items"`
			}
			ListingStatus struct{ Enum []string }
			Sort          struct {
				Enum        []string
				Description string `json:"description"`
			}
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v\n%s", err, raw)
	}
	var statuses []string
	for _, status := range store.ApplicationStatuses() {
		statuses = append(statuses, status.String())
	}
	if diff := cmp.Diff(statuses, schema.Properties.ApplicationStatuses.Items.Enum); diff != "" {
		t.Errorf("ApplicationStatuses enum mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"open", "closed", "any"}, schema.Properties.ListingStatus.Enum); diff != "" {
		t.Errorf("ListingStatus enum mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"id_asc", "id_desc", "published_desc"}, schema.Properties.Sort.Enum); diff != "" {
		t.Errorf("Sort enum mismatch (-want +got):\n%s", diff)
	}
	for _, order := range stage.SortOrders() {
		if !strings.Contains(schema.Properties.Sort.Description, order.Name()+": "+order.Description) {
			t.Errorf("Sort description doesn't explain %s:\n%s", order.Name(), schema.Properties.Sort.Description)
		}
	}
}

// TestWriteDocument_ExpectedSHA256: an agent passes the hash of what it
// read, so a draft the user edited since isn't silently replaced (RFC
// 0007, H2). Omitting it still writes unconditionally.
func TestWriteDocument_ExpectedSHA256(t *testing.T) {
	t.Parallel()

	const edited = "edited by the user"
	tests := []struct {
		name      string
		onDisk    string // empty: no document yet
		expected  any    // nil: argument omitted
		wantError string // empty: the write succeeds
	}{
		{name: "omitted", onDisk: edited, expected: nil},
		{name: "matches", onDisk: edited, expected: documents.ContentSHA256(edited)},
		{name: "changed since read", onDisk: edited, expected: documents.ContentSHA256("what the agent read"), wantError: "changed since you read it"},
		{name: "expected absent, still absent", expected: ""},
		{name: "expected absent, one appeared", onDisk: edited, expected: "", wantError: "already has a cover_letter"},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv, s, _ := newTestServer(t)
			company := mustCreateCompany(t, s, "Acme")
			posting := mustUpsertPosting(t, s, company.ID, fmt.Sprintf("job-%d", i), "Senior Data Engineer")
			mustMarkInterested(t, s, posting.ID)
			cs := connectClient(t, srv)
			prepared := callTool[stage.Prepared](t, cs, "stage_prepare", map[string]any{"PostingID": posting.ID})
			path := prepared.Documents[documents.CoverLetter].Path
			if tt.onDisk != "" {
				if err := os.WriteFile(path, []byte(tt.onDisk), 0o644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			}

			args := map[string]any{
				"ApplicationID": prepared.ApplicationID,
				"DocumentType":  "cover_letter",
				"Content":       "the agent's draft",
			}
			if tt.expected != nil {
				args["ExpectedSHA256"] = tt.expected
			}

			want := "the agent's draft"
			if tt.wantError != "" {
				msg := callToolError(t, cs, "write_document", args)
				for _, part := range []string{tt.wantError, "nothing was written", "read_document"} {
					if !strings.Contains(msg, part) {
						t.Errorf("error %q doesn't contain %q", msg, part)
					}
				}
				want = tt.onDisk
			} else {
				callTool[writeDocumentOutput](t, cs, "write_document", args)
			}

			onDisk, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile: %v", err)
			}
			if string(onDisk) != want {
				t.Errorf("file content = %q, want %q", onDisk, want)
			}
		})
	}
}

// TestStagePrepare_DocumentsCarrySHA256: what an agent passes to
// write_document as ExpectedSHA256 when it drafts from stage_prepare,
// empty for a document that doesn't exist yet.
func TestStagePrepare_DocumentsCarrySHA256(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Senior Data Engineer")
	mustMarkInterested(t, s, posting.ID)
	cs := connectClient(t, srv)
	prepared := callTool[stage.Prepared](t, cs, "stage_prepare", map[string]any{"PostingID": posting.ID})
	callTool[writeDocumentOutput](t, cs, "write_document", map[string]any{
		"ApplicationID": prepared.ApplicationID,
		"DocumentType":  "cover_letter",
		"Content":       "a draft",
	})

	got := callTool[stage.Prepared](t, cs, "stage_prepare", map[string]any{"PostingID": posting.ID})

	if want := documents.ContentSHA256("a draft"); got.Documents[documents.CoverLetter].SHA256 != want {
		t.Errorf("cover letter SHA256 = %q, want %q", got.Documents[documents.CoverLetter].SHA256, want)
	}
	if got := got.Documents[documents.Resume].SHA256; got != "" {
		t.Errorf("resume SHA256 = %q, want empty (never written)", got)
	}
}

// TestWriteDocument_IsRecorded: a file write is invisible to the database,
// so write_document records it, saying the agent made it.
func TestWriteDocument_IsRecorded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	srv, s, _ := newTestServer(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Senior Data Engineer")
	mustMarkInterested(t, s, posting.ID)
	cs := connectClient(t, srv)
	prepared := callTool[stage.Prepared](t, cs, "stage_prepare", map[string]any{"PostingID": posting.ID})
	callTool[writeDocumentOutput](t, cs, "write_document", map[string]any{
		"ApplicationID": prepared.ApplicationID,
		"DocumentType":  "cover_letter",
		"Content":       "a draft",
	})

	write, ok, err := s.LatestDocumentWrite(ctx, prepared.ApplicationID, documents.CoverLetter)
	if err != nil || !ok {
		t.Fatalf("LatestDocumentWrite = ok %v, err %v; want the write", ok, err)
	}
	if write.Source != store.DocumentWriteSourceWriteDocument || write.ContentSHA256 != documents.ContentSHA256("a draft") {
		t.Errorf("recorded write = source %v, hash %q; want write_document and the draft's hash", write.Source, write.ContentSHA256)
	}
}
