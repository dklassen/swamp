package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/stage"
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

func newTestServer(t *testing.T) (*mcp.Server, testutil.TestApp, *documents.Store) {
	t.Helper()
	s := testutil.NewTestApp(t)
	d := documents.NewStore(t.TempDir())
	fetcher := fakeFetcher{boards: map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer"}, {SourceID: "job-2", Title: "Designer"}},
	}}
	syncer := swampsync.New(s.Companies, s.Postings, map[string]swampsync.PostingFetcher{jobboard.Ashby: fetcher}, swampsync.DefaultConfig())
	return New(stage.New(s.Companies, s.Postings, s.Applications, s.Documents, s.Scores, d), d, syncer), s, d
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

func mustCreateCompany(t *testing.T, s testutil.TestApp, name string) company.Company {
	t.Helper()
	c, err := s.Companies.Add(context.Background(), name, jobboard.Ashby, name)
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}
	return c
}

func mustUpsertPosting(t *testing.T, s testutil.TestApp, companyID int64, sourceID, title string) posting.Posting {
	t.Helper()
	ingested, err := s.Postings.Ingest(context.Background(), companyID, jobboard.Ashby, sourceID, posting.IngestedFields{
		Title:      title,
		RawPayload: `{"id":"` + sourceID + `"}`,
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return ingested.Posting
}

func mustMarkInterested(t *testing.T, s testutil.TestApp, postingID int64) {
	t.Helper()
	if _, err := s.Postings.SetInterested(context.Background(), postingID); err != nil {
		t.Fatalf("postingmarkup.SetInterested: %v", err)
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
	if got.Documents[document.CoverLetter].Path == "" {
		t.Error("CoverLetter.Path is empty, want a resolved path")
	}
	if got.Documents[document.CoverLetter].Exists {
		t.Error("CoverLetter.Exists = true, want false (nothing drafted yet)")
	}

	app, err := s.Postings.Application(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("application.Get: %v", err)
	}
	if app.ID != got.ApplicationID {
		t.Errorf("persisted application id = %d, want %d", app.ID, got.ApplicationID)
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
		{"cover_letter", prepared.Documents[document.CoverLetter].Path},
		{"resume", prepared.Documents[document.Resume].Path},
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
	path, err := d.Path(999, document.CoverLetter)
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
	if err := s.Applications.Delete(context.Background(), prepared.ApplicationID); err != nil {
		t.Fatalf("application.Delete: %v", err)
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
	path := prepared.Documents[document.CoverLetter].Path
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
		"Source":      jobboard.Ashby,
		"Slug":        "acme",
		"Description": description,
	})

	want := addCompanyOutput{Outcome: "created", CompanyID: got.CompanyID, Name: "Acme", OpenJobs: 2}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("add_company result mismatch (-want +got):\n%s", diff)
	}

	stored, err := s.Companies.Get(context.Background(), got.CompanyID)
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
			acme, err := s.Companies.Add(context.Background(), "Acme", jobboard.Ashby, "acme")
			if err != nil {
				t.Fatalf("CreateCompany: %v", err)
			}
			if tc.softDelete {
				if err := s.Companies.Delete(context.Background(), acme.ID); err != nil {
					t.Fatalf("SoftDeleteCompany: %v", err)
				}
			}
			cs := connectClient(t, srv)

			got := callTool[addCompanyOutput](t, cs, "add_company", map[string]any{
				"Name": "Acme", "Source": jobboard.Ashby, "Slug": "acme", "Description": "whatever",
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
			"Name": "Nope", "Source": jobboard.Ashby, "Slug": "nope", "Description": "whatever",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true for a slug the board rejects")
	}

	companies, err := s.Companies.ListActive(context.Background())
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
		{"cover_letter", prepared.Documents[document.CoverLetter].Path},
		{"resume", prepared.Documents[document.Resume].Path},
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

			want := readDocumentOutput{Path: tc.wantPath, Content: content, Version: documents.ContentSHA256(content)}
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
	if err := s.Applications.Delete(context.Background(), prepared.ApplicationID); err != nil {
		t.Fatalf("application.Delete: %v", err)
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

			want := readCanonicalOutput{Path: path, Exists: true, Content: "# " + tc.tool, Version: documents.ContentSHA256("# " + tc.tool)}
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
	if _, _, err := s.Postings.Close(context.Background(), posting.ID, nil); err != nil {
		t.Fatalf("Close: %v", err)
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
	if _, _, err := s.Postings.Close(ctx, closed.ID, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Companies.Delete(ctx, gone.ID); err != nil {
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
		{acme.ID, "Acme", jobboard.Ashby, 2},
		{beta.ID, "Beta", jobboard.Ashby, 0},
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
// search_postings finds it by company and "has an application".
func TestSearchPostings_FindsADraftedApplicationByCompany(t *testing.T) {
	t.Parallel()

	srv, s, d := newTestServer(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme")
	drafted := mustUpsertPosting(t, s, acme.ID, "job-1", "Staff Software Developer, Product")
	mustUpsertPosting(t, s, acme.ID, "job-2", "Staff Software Developer, Risk")
	mustMarkInterested(t, s, drafted.ID)
	application, err := createApplication(ctx, s, drafted.ID)
	if err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	if _, err := d.EnsureDir(application.ID); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	for _, documentType := range document.Types() {
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
	if _, err := createApplication(context.Background(), s, started.ID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	cs := connectClient(t, srv)

	got := callTool[searchPostingsResult](t, cs, "search_postings", map[string]any{"Interested": true, "HasApplication": false})
	if diff := cmp.Diff([]int64{shortlisted.ID}, searchIDs(got)); diff != "" {
		t.Errorf("shortlist mismatch (-want +got):\n%s", diff)
	}
}

func TestSearchPostings_QueryNarrowsByText(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	acme := mustCreateCompany(t, s, "Acme")
	globex := mustCreateCompany(t, s, "Globex")
	platform := mustUpsertPosting(t, s, acme.ID, "job-1", "Platform Engineer")
	mustUpsertPosting(t, s, acme.ID, "job-2", "Designer")
	globexPlatform := mustUpsertPosting(t, s, globex.ID, "job-3", "Platform Lead")
	cs := connectClient(t, srv)

	got := callTool[searchPostingsResult](t, cs, "search_postings", map[string]any{"Query": "PLATFORM acme"})
	if diff := cmp.Diff([]int64{platform.ID}, searchIDs(got)); diff != "" {
		t.Errorf("Query \"PLATFORM acme\" (-want +got):\n%s", diff)
	}
	got = callTool[searchPostingsResult](t, cs, "search_postings", map[string]any{"Query": "platform"})
	if diff := cmp.Diff([]int64{platform.ID, globexPlatform.ID}, searchIDs(got)); diff != "" {
		t.Errorf("Query \"platform\" (-want +got):\n%s", diff)
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
	for _, status := range application.All() {
		statuses = append(statuses, status.String())
	}
	if diff := cmp.Diff(statuses, schema.Properties.ApplicationStatuses.Items.Enum); diff != "" {
		t.Errorf("ApplicationStatuses enum mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"open", "closed", "any"}, schema.Properties.ListingStatus.Enum); diff != "" {
		t.Errorf("ListingStatus enum mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"id_asc", "id_desc", "published_desc", "score_desc"}, schema.Properties.Sort.Enum); diff != "" {
		t.Errorf("Sort enum mismatch (-want +got):\n%s", diff)
	}
	for _, order := range stage.SortOrders() {
		if !strings.Contains(schema.Properties.Sort.Description, order.Name()+": "+order.Description) {
			t.Errorf("Sort description doesn't explain %s:\n%s", order.Name(), schema.Properties.Sort.Description)
		}
	}
}

// TestWriteDocument_ReplacesVersion: an agent passes the Version of what it
// read, so a draft the user edited since isn't silently replaced (RFC
// 0007, H2). Omitting it still writes unconditionally.
func TestWriteDocument_ReplacesVersion(t *testing.T) {
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
		{name: "no document yet, still absent", expected: "no_document_yet"},
		{name: "no document yet, one appeared", onDisk: edited, expected: "no_document_yet", wantError: "already has a cover_letter"},
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
			path := prepared.Documents[document.CoverLetter].Path
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
				args["ReplacesVersion"] = tt.expected
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

// An empty ReplacesVersion is easy to mistake for an omitted one, which writes
// unconditionally, so it's refused: "no_document_yet" says so instead.
func TestWriteDocument_EmptyReplacesVersion_IsRefused(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Senior Data Engineer")
	mustMarkInterested(t, s, posting.ID)
	cs := connectClient(t, srv)
	prepared := callTool[stage.Prepared](t, cs, "stage_prepare", map[string]any{"PostingID": posting.ID})

	msg := callToolError(t, cs, "write_document", map[string]any{
		"ApplicationID":   prepared.ApplicationID,
		"DocumentType":    "cover_letter",
		"Content":         "the agent's draft",
		"ReplacesVersion": "",
	})

	for _, part := range []string{"nothing was written", `"no_document_yet"`} {
		if !strings.Contains(msg, part) {
			t.Errorf("error %q doesn't contain %q", msg, part)
		}
	}
	if _, err := os.Stat(prepared.Documents[document.CoverLetter].Path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cover letter on disk after a refused write: Stat error %v, want not exist", err)
	}
}

// TestStagePrepare_DocumentsCarryVersion: what an agent passes to
// write_document as ReplacesVersion when it drafts from stage_prepare,
// stage.NoDocumentYet for a document that doesn't exist yet.
func TestStagePrepare_DocumentsCarryVersion(t *testing.T) {
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

	if want := documents.ContentSHA256("a draft"); got.Documents[document.CoverLetter].Version != want {
		t.Errorf("cover letter Version = %q, want %q", got.Documents[document.CoverLetter].Version, want)
	}
	if got := got.Documents[document.Resume].Version; got != stage.NoDocumentYet {
		t.Errorf("resume Version = %q, want %q (never written)", got, stage.NoDocumentYet)
	}
}

// TestWriteDocument_IsRecordedAndReachesTheChangeLog: a file write is
// invisible to the database, so write_document records it; that record is
// how a TUI watching the change log learns the agent revised a draft.
func TestWriteDocument_IsRecordedAndReachesTheChangeLog(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	srv, s, _ := newTestServer(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Senior Data Engineer")
	mustMarkInterested(t, s, posting.ID)
	cs := connectClient(t, srv)
	prepared := callTool[stage.Prepared](t, cs, "stage_prepare", map[string]any{"PostingID": posting.ID})
	since, err := s.Changes.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	callTool[writeDocumentOutput](t, cs, "write_document", map[string]any{
		"ApplicationID": prepared.ApplicationID,
		"DocumentType":  "cover_letter",
		"Content":       "a draft",
	})

	write, ok, err := s.Documents.LatestWrite(ctx, prepared.ApplicationID, document.CoverLetter)
	if err != nil || !ok {
		t.Fatalf("LatestWrite = ok %v, err %v; want the write", ok, err)
	}
	if write.Source != document.SourceWriteDocument || write.ContentSHA256 != documents.ContentSHA256("a draft") {
		t.Errorf("recorded write = source %v, hash %q; want write_document and the draft's hash", write.Source, write.ContentSHA256)
	}

	events, err := s.Changes.After(ctx, since)
	if err != nil {
		t.Fatalf("After: %v", err)
	}
	if len(events) != 1 || events[0].Table != "document_writes" || events[0].RowID != write.ID {
		t.Errorf("change events after write_document = %+v, want one document_writes insert for write %d", events, write.ID)
	}
}

func TestReadPosting_ReturnsThePostingInFull(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme")
	if _, err := s.Companies.SetDescription(ctx, acme.ID, "Builds rockets."); err != nil {
		t.Fatalf("UpdateCompanyDescription: %v", err)
	}
	ingested, err := s.Postings.Ingest(ctx, acme.ID, jobboard.Ashby, "job-1", posting.IngestedFields{
		Title: "Platform Engineer", Department: "Infra", Location: "Dublin", WorkplaceType: "Hybrid",
		DescriptionText: "Run the platform.", DescriptionHTML: "<p>Run the platform.</p>",
		JobURL: "https://jobs.example.com/acme/1", ApplicationURL: "https://jobs.example.com/acme/1/apply", RawPayload: "{}",
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	posting := ingested.Posting
	cs := connectClient(t, srv)

	got := callTool[stage.FullPosting](t, cs, "read_posting", map[string]any{"PostingID": posting.ID})

	want := stage.FullPosting{
		ID: posting.ID, Title: "Platform Engineer", CompanyName: "Acme", CompanyDescription: "Builds rockets.",
		Department: "Infra", Location: "Dublin", WorkplaceType: "Hybrid", ListingStatus: "open",
		DescriptionText: "Run the platform.", JobURL: "https://jobs.example.com/acme/1", ApplicationURL: "https://jobs.example.com/acme/1/apply",
		FirstSeenAt: posting.FirstSeenAt, Version: posting.ContentSHA256,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("read_posting (-want +got):\n%s", diff)
	}
	if got.Version == "" {
		t.Error("Version is empty")
	}
}

func TestReadPosting_RefusesWhatItCantShow(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	gone := mustCreateCompany(t, s, "Gone")
	atGone := mustUpsertPosting(t, s, gone.ID, "job-1", "Engineer")
	if err := s.Companies.Delete(context.Background(), gone.ID); err != nil {
		t.Fatalf("SoftDeleteCompany: %v", err)
	}
	cs := connectClient(t, srv)

	for _, tt := range []struct {
		name      string
		postingID int64
		want      string
	}{
		{"unknown posting", 999, "no posting 999"},
		{"deleted company", atGone.ID, "Gone, which was deleted"},
	} {
		if text := callToolError(t, cs, "read_posting", map[string]any{"PostingID": tt.postingID}); !strings.Contains(text, tt.want) {
			t.Errorf("%s: tool error %q, want it to say %q", tt.name, text, tt.want)
		}
	}
}

// Scores carry their own provenance, so there are no runs to start
// or list.
func TestServer_HasNoScoringRunTools(t *testing.T) {
	t.Parallel()

	srv, _, _ := newTestServer(t)
	cs := connectClient(t, srv)
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "start_scoring_run" || tool.Name == "list_scoring_runs" {
			t.Errorf("the server still offers %s", tool.Name)
		}
	}
}

// mustWriteProfile writes the user's profile and returns its hash.
func mustWriteProfile(t *testing.T, d *documents.Store, content string) string {
	t.Helper()
	path := d.ProfilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	return documents.ContentSHA256(content)
}

func pendingIDs(r stage.PendingResult) []int64 {
	var ids []int64
	for _, p := range r.Postings {
		ids = append(ids, p.ID)
	}
	return ids
}

func TestListPendingPostings_PagesToTheLastPage(t *testing.T) {
	t.Parallel()

	srv, s, d := newTestServer(t)
	acme := mustCreateCompany(t, s, "Acme")
	var ids []int64
	for i := 1; i <= 3; i++ {
		ids = append(ids, mustUpsertPosting(t, s, acme.ID, fmt.Sprintf("job-%d", i), fmt.Sprintf("Role %d", i)).ID)
	}
	profile := mustWriteProfile(t, d, "# Me")
	cs := connectClient(t, srv)

	first := callTool[stage.PendingResult](t, cs, "list_pending_postings", map[string]any{"ProfileVersion": profile, "Limit": 2})
	if first.NextCursor == nil || first.Total != 3 {
		t.Fatalf("first page: NextCursor %v, Total %d; want a cursor and 3", first.NextCursor, first.Total)
	}
	second := callTool[stage.PendingResult](t, cs, "list_pending_postings", map[string]any{"ProfileVersion": profile, "Limit": 2, "Cursor": *first.NextCursor})

	if diff := cmp.Diff([]int64{ids[2], ids[1], ids[0]}, append(pendingIDs(first), pendingIDs(second)...)); diff != "" {
		t.Errorf("pending across both pages, newest first (-want +got):\n%s", diff)
	}
	if second.NextCursor != nil {
		t.Errorf("last page's NextCursor = %q, want null", *second.NextCursor)
	}
	if first.Postings[0].Reason != score.PendingNew || first.Postings[0].Version == "" {
		t.Errorf("first posting = %+v, want new, with its content hash", first.Postings[0])
	}
}

func TestListPendingPostings_SaysWhyEachIsPending(t *testing.T) {
	t.Parallel()

	srv, s, d := newTestServer(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme")
	fresh := mustUpsertPosting(t, s, acme.ID, "job-1", "Fresh")
	scored := mustUpsertPosting(t, s, acme.ID, "job-2", "Scored before the profile changed")
	oldProfile := documents.ContentSHA256("# Me")
	if _, err := recordScores(ctx, s, score.Provenance{Model: "claude-opus-5-5", ProfileSHA256: oldProfile}, []score.Entry{
		{PostingID: scored.ID, Score: 3, Reason: "ok", ContentSHA256: scored.ContentSHA256},
	}); err != nil {
		t.Fatalf("recordScores: %v", err)
	}
	profile := mustWriteProfile(t, d, "# Me, edited")
	cs := connectClient(t, srv)

	reasons := func(args map[string]any) map[int64]score.PendingReason {
		got := map[int64]score.PendingReason{}
		for _, p := range callTool[stage.PendingResult](t, cs, "list_pending_postings", args).Postings {
			got[p.ID] = p.Reason
		}
		return got
	}
	if diff := cmp.Diff(map[int64]score.PendingReason{fresh.ID: score.PendingNew}, reasons(map[string]any{"ProfileVersion": profile})); diff != "" {
		t.Errorf("by default, an edited profile queues nothing (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(map[int64]score.PendingReason{fresh.ID: score.PendingNew, scored.ID: score.PendingRescore}, reasons(map[string]any{"ProfileVersion": profile, "Rescore": true})); diff != "" {
		t.Errorf("rescore (-want +got):\n%s", diff)
	}
}

func TestListPendingPostings_RefusesAProfileItDidntRead(t *testing.T) {
	t.Parallel()

	srv, _, d := newTestServer(t)
	mustWriteProfile(t, d, "# Me, edited since")
	cs := connectClient(t, srv)

	text := callToolError(t, cs, "list_pending_postings", map[string]any{"ProfileVersion": documents.ContentSHA256("# Me")})

	if !strings.Contains(text, "read_profile") {
		t.Errorf("tool error %q, want it to say to call read_profile again", text)
	}
}

func TestListPendingPostings_RefusesACursorFromAnotherListing(t *testing.T) {
	t.Parallel()

	srv, s, d := newTestServer(t)
	acme := mustCreateCompany(t, s, "Acme")
	for i := 1; i <= 3; i++ {
		mustUpsertPosting(t, s, acme.ID, fmt.Sprintf("job-%d", i), "Role")
	}
	profile := mustWriteProfile(t, d, "# Me")
	cs := connectClient(t, srv)
	first := callTool[stage.PendingResult](t, cs, "list_pending_postings", map[string]any{"ProfileVersion": profile, "Limit": 1})
	if first.NextCursor == nil {
		t.Fatal("first page has no NextCursor")
	}

	text := callToolError(t, cs, "list_pending_postings", map[string]any{"ProfileVersion": profile, "Rescore": true, "Cursor": *first.NextCursor})

	if !strings.Contains(text, "cursor") {
		t.Errorf("tool error %q, want it to say the cursor doesn't fit", text)
	}
}

func TestRecordScores_StoresValidEntriesAndReportsTheRest(t *testing.T) {
	t.Parallel()

	srv, s, d := newTestServer(t)
	acme := mustCreateCompany(t, s, "Acme")
	good := mustUpsertPosting(t, s, acme.ID, "job-1", "Platform Engineer")
	other := mustUpsertPosting(t, s, acme.ID, "job-2", "Designer")
	profile := mustWriteProfile(t, d, "# Me")
	cs := connectClient(t, srv)

	got := callTool[stage.RecordResult](t, cs, "record_scores", map[string]any{
		"Model":          "claude-opus-5-5",
		"ProfileVersion": profile,
		"Scores": []map[string]any{
			{"PostingID": good.ID, "Score": 4, "Reason": "Go, remote", "PostingVersion": good.ContentSHA256},
			{"PostingID": other.ID, "Score": 9, "Reason": "off the scale", "PostingVersion": other.ContentSHA256},
			{"PostingID": 999, "Score": 3, "Reason": "no such posting", "PostingVersion": "x"},
		},
	})

	want := stage.RecordResult{
		Recorded: []int64{good.ID},
		Refused:  []stage.RefusedEntry{{PostingID: other.ID, Why: score.OutOfRange}, {PostingID: 999, Why: score.NoSuchPosting}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("record_scores (-want +got):\n%s", diff)
	}
	scores, err := s.Scores.ForPosting(context.Background(), good.ID)
	if err != nil {
		t.Fatalf("scoring.ForPosting: %v", err)
	}
	wantProvenance := score.Provenance{Model: "claude-opus-5-5", ClientName: "mcpserver-test", ClientVersion: "test", ProfileSHA256: profile}
	if len(scores) != 1 || scores[0].Score != 4 || scores[0].Provenance != wantProvenance {
		t.Errorf("stored scores = %+v, want one 4 with %+v", scores, wantProvenance)
	}
}

func TestRecordScores_RefusesAProfileItDidntReadAndRecordsNothing(t *testing.T) {
	t.Parallel()

	srv, s, d := newTestServer(t)
	acme := mustCreateCompany(t, s, "Acme")
	p := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustWriteProfile(t, d, "# Me, edited since")
	cs := connectClient(t, srv)

	text := callToolError(t, cs, "record_scores", map[string]any{
		"Model":          "claude-opus-5-5",
		"ProfileVersion": documents.ContentSHA256("# Me"),
		"Scores":         []map[string]any{{"PostingID": p.ID, "Score": 4, "Reason": "ok", "PostingVersion": p.ContentSHA256}},
	})

	if !strings.Contains(text, "read_profile") {
		t.Errorf("tool error %q, want it to say to call read_profile again", text)
	}
	if scores, err := s.Scores.ForPosting(context.Background(), p.ID); err != nil || len(scores) != 0 {
		t.Errorf("scores after a refused call = %+v, %v; want none", scores, err)
	}
}

// A refusal's Why is advertised as one of the store's names, so the agent
// learns the vocabulary from the schema.
func TestRecordScores_AdvertisesTheRefusalNames(t *testing.T) {
	t.Parallel()

	srv, _, _ := newTestServer(t)
	cs := connectClient(t, srv)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var raw []byte
	for _, tool := range res.Tools {
		if tool.Name == "record_scores" {
			if raw, err = json.Marshal(tool.OutputSchema); err != nil {
				t.Fatalf("marshal schema: %v", err)
			}
		}
	}
	var schema struct {
		Properties struct {
			Refused struct {
				Items struct {
					Properties struct {
						Why struct {
							Type string
							Enum []string
						}
					}
				}
			}
		}
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal schema %s: %v", raw, err)
	}
	var want []string
	for _, r := range score.Refusals() {
		want = append(want, r.String())
	}
	why := schema.Properties.Refused.Items.Properties.Why
	if why.Type != "string" {
		t.Errorf("Why's type = %q, want string", why.Type)
	}
	if diff := cmp.Diff(want, why.Enum); diff != "" {
		t.Errorf("Why's names (-want +got):\n%s", diff)
	}
}

// Arguments named for the hash rather than its purpose are refused, not
// ignored: a write whose ExpectedSHA256 was dropped would replace whatever
// is there, and a score whose ProfileSHA256 was dropped would be refused
// only for the wrong reason.
func TestOldVersionArgumentNames_AreRefused(t *testing.T) {
	t.Parallel()

	srv, s, d := newTestServer(t)
	company := mustCreateCompany(t, s, "Acme")
	posting := mustUpsertPosting(t, s, company.ID, "job-1", "Senior Data Engineer")
	mustMarkInterested(t, s, posting.ID)
	profile := mustWriteProfile(t, d, "# Me")
	cs := connectClient(t, srv)
	prepared := callTool[stage.Prepared](t, cs, "stage_prepare", map[string]any{"PostingID": posting.ID})

	calls := []struct {
		tool string
		args map[string]any
	}{
		{"write_document", map[string]any{"ApplicationID": prepared.ApplicationID, "DocumentType": "cover_letter", "Content": "a draft", "ExpectedSHA256": "no_document_yet"}},
		{"list_pending_postings", map[string]any{"ProfileSHA256": profile}},
		{"record_scores", map[string]any{"Model": "claude-opus-5-5", "ProfileSHA256": profile, "Scores": []any{}}},
	}
	for _, c := range calls {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: c.tool, Arguments: c.args})
		if err == nil && !res.IsError {
			t.Errorf("%s with an old argument name succeeded, want it refused", c.tool)
		}
	}
	if _, err := os.Stat(prepared.Documents[document.CoverLetter].Path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cover letter on disk after a refused write: Stat error %v, want not exist", err)
	}
}

func recordScores(ctx context.Context, s testutil.TestApp, provenance score.Provenance, entries []score.Entry) (score.RecordResult, error) {
	return s.Scores.Record(ctx, provenance, entries)
}

func createApplication(ctx context.Context, s testutil.TestApp, postingID int64) (application.Application, error) {
	return s.Postings.ApplyTo(ctx, postingID)
}
