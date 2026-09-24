package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
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
	syncer := swampsync.New(s, map[string]swampsync.PostingFetcher{"ashby": fetcher})
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
	if got.CoverLetter.Path == "" {
		t.Error("CoverLetter.Path is empty, want a resolved path")
	}
	if got.CoverLetter.Exists {
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
		{"cover_letter", prepared.CoverLetter.Path},
		{"resume", prepared.Resume.Path},
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
		{"cover_letter", prepared.CoverLetter.Path},
		{"resume", prepared.Resume.Path},
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

			want := readDocumentOutput{Path: tc.wantPath, Content: content}
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
		"description": "either cover_letter or resume",
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
