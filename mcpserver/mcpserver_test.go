package mcpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pressly/goose/v3"

	"github.com/google/go-cmp/cmp"

	_ "modernc.org/sqlite"

	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/stage"
	"github.com/dklassen/swamp/store"
)

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
	return New(stage.New(s, d), d), s, d
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

	got := callTool[[]stage.Candidate](t, cs, "list_postings", map[string]any{})

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
