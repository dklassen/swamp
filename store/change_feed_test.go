package store

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dklassen/swamp/documents"
)

func mustNewChangeFeed(t *testing.T, s *Store) *ChangeFeed {
	t.Helper()
	f, err := s.NewChangeFeed(context.Background())
	if err != nil {
		t.Fatalf("NewChangeFeed: %v", err)
	}
	return f
}

func mustNext(t *testing.T, f *ChangeFeed) []ChangeEvent {
	t.Helper()
	events, err := f.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	return events
}

// TestChangeFeed_DeliversAnotherProcessesChange: what the feed is for. The
// application created before the feed existed isn't delivered; the TUI
// already loaded it.
func TestChangeFeed_DeliversAnotherProcessesChange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := t.TempDir() + "/test.db"
	newTestStoreAt(t, path)
	tui := openWithOrigin(t, path, "tui:1")
	agent := openWithOrigin(t, path, "mcp:7")
	acme := mustCreateCompany(t, agent, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, agent, acme.ID, "job-1", "Engineer")
	application, err := agent.CreateApplication(ctx, posting.ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	feed := mustNewChangeFeed(t, tui)

	if _, err := agent.UpdateApplicationStatus(ctx, posting.ID, ApplicationStatusInterviewing); err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}

	want := []ChangeEvent{{
		Table:  "applications",
		RowID:  application.ID,
		Op:     "update",
		Old:    `{"posting_id":1,"status":"application_started","notes":"","deleted_at":null}`,
		New:    `{"posting_id":1,"status":"interviewing","notes":"","deleted_at":null}`,
		Origin: "mcp:7",
	}}
	if diff := cmp.Diff(want, mustNext(t, feed), cmpopts.IgnoreFields(ChangeEvent{}, "ID", "At")); diff != "" {
		t.Errorf("Next (-want +got):\n%s", diff)
	}
}

// TestChangeFeed_CatchesUpAfterAGap: the log is kept, so a reader that
// looks late still gets every change, once.
func TestChangeFeed_CatchesUpAfterAGap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	feed := mustNewChangeFeed(t, s)

	if _, err := s.CreateApplication(ctx, posting.ID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	for _, status := range []ApplicationStatus{ApplicationStatusSubmitted, ApplicationStatusInterviewing} {
		if _, err := s.UpdateApplicationStatus(ctx, posting.ID, status); err != nil {
			t.Fatalf("UpdateApplicationStatus: %v", err)
		}
	}

	var ops []string
	for _, e := range mustNext(t, feed) {
		ops = append(ops, e.Op)
	}
	if diff := cmp.Diff([]string{"insert", "update", "update"}, ops); diff != "" {
		t.Errorf("events after the gap (-want +got):\n%s", diff)
	}
	if again := mustNext(t, feed); len(again) != 0 {
		t.Errorf("Next again returned %d events, want none: each is delivered once", len(again))
	}
}

// TestChangeFeed_ACheckpointIsNotAChange: resetting the WAL rewrites the
// database files but commits nothing, so it mustn't trigger a reload.
func TestChangeFeed_ACheckpointIsNotAChange(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/test.db"
	tui := newTestStoreAt(t, path)
	other := newTestStoreAt(t, path)
	acme := mustCreateCompany(t, other, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, other, acme.ID, "job-1", "Engineer")
	if _, err := other.CreateApplication(context.Background(), posting.ID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	feed := mustNewChangeFeed(t, tui)

	if _, err := other.sqlDB.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}

	if events := mustNext(t, feed); len(events) != 0 {
		t.Errorf("Next after a checkpoint returned %d events, want none", len(events))
	}
}

// TestChangeFeed_EachLoggedTable: a change made through the store's own
// methods to each logged table arrives as an event from its writer.
func TestChangeFeed_EachLoggedTable(t *testing.T) {
	t.Parallel()

	type fixture struct {
		s           *Store
		company     Company
		posting     Posting
		application Application
	}
	tests := []struct {
		name   string
		change func(context.Context, fixture) error
		table  string
		op     string
	}{
		{"company created", func(ctx context.Context, f fixture) error {
			_, err := f.s.CreateCompany(ctx, "Globex", "lever", "globex")
			return err
		}, "companies", "insert"},
		{"company renamed", func(ctx context.Context, f fixture) error {
			_, err := f.s.UpdateCompanyName(ctx, f.company.ID, "Acme Corp")
			return err
		}, "companies", "update"},
		{"company deleted", func(ctx context.Context, f fixture) error {
			return f.s.SoftDeleteCompany(ctx, f.company.ID)
		}, "companies", "update"},
		{"posting found", func(ctx context.Context, f fixture) error {
			_, err := f.s.UpsertPosting(ctx, CreatePostingParams{CompanyID: f.company.ID, Source: "ashby", SourceID: "job-2", IngestedFields: IngestedFields{Title: "Designer", RawPayload: `{}`}})
			return err
		}, "postings", "insert"},
		{"posting closed", func(ctx context.Context, f fixture) error {
			_, err := f.s.ClosePosting(ctx, f.posting.ID, nil)
			return err
		}, "postings", "update"},
		{"document written", func(ctx context.Context, f fixture) error {
			return f.s.RecordDocumentWrite(ctx, f.application.ID, documents.CoverLetter, "a draft", DocumentWriteSourceWriteDocument)
		}, "document_writes", "insert"},
		{"document reviewed", func(ctx context.Context, f fixture) error {
			_, err := f.s.CreateDocumentReview(ctx, f.application.ID, documents.CoverLetter, "a draft", ReviewOutcomeFlagged, "tighten it")
			return err
		}, "document_reviews", "insert"},
		{"document exported", func(ctx context.Context, f fixture) error {
			return f.s.RecordDocumentExport(ctx, f.application.ID, documents.CoverLetter, "a draft", "/tmp/cover.pdf")
		}, "document_exports", "insert"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			path := t.TempDir() + "/test.db"
			newTestStoreAt(t, path)
			s := openWithOrigin(t, path, "mcp:7")
			f := fixture{s: s, company: mustCreateCompany(t, s, "Acme", "ashby", "acme")}
			f.posting = mustUpsertPosting(t, s, f.company.ID, "job-1", "Engineer")
			application, err := s.CreateApplication(ctx, f.posting.ID)
			if err != nil {
				t.Fatalf("CreateApplication: %v", err)
			}
			f.application = application
			feed := mustNewChangeFeed(t, s)

			if err := tt.change(ctx, f); err != nil {
				t.Fatalf("change: %v", err)
			}

			var got []string
			for _, e := range mustNext(t, feed) {
				if e.Table == tt.table {
					got = append(got, e.Op+" by "+e.Origin)
				}
			}
			if diff := cmp.Diff([]string{tt.op + " by mcp:7"}, got); diff != "" {
				t.Errorf("%s events (-want +got):\n%s", tt.table, diff)
			}
		})
	}
}

// TestChangeFeed_AnApplicationEventNamesItsPosting: a screen showing a
// posting with no application yet can only tell an application was started
// on it from the event.
func TestChangeFeed_AnApplicationEventNamesItsPosting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := t.TempDir() + "/test.db"
	newTestStoreAt(t, path)
	s := openWithOrigin(t, path, "mcp:7")
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	feed := mustNewChangeFeed(t, s)

	if _, err := s.CreateApplication(ctx, posting.ID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}

	events := mustNext(t, feed)
	if len(events) != 1 {
		t.Fatalf("events = %+v, want one", events)
	}
	var row struct {
		PostingID int64 `json:"posting_id"`
	}
	if err := json.Unmarshal([]byte(events[0].New), &row); err != nil {
		t.Fatalf("event values %q: %v", events[0].New, err)
	}
	if row.PostingID != posting.ID {
		t.Errorf("the application event's posting_id = %d, want %d (event values %s)", row.PostingID, posting.ID, events[0].New)
	}
}

// TestChangeFeed_ASyncThatChangesNothingLogsNothing: every fetch sees each
// listed posting again (thousands of them), so only real changes may log.
func TestChangeFeed_ASyncThatChangesNothingLogsNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	params := CreatePostingParams{CompanyID: acme.ID, Source: "ashby", SourceID: "job-1", IngestedFields: IngestedFields{Title: "Engineer", RawPayload: `{"id":"job-1"}`}}
	posting, err := s.UpsertPosting(ctx, params)
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	// A day old, so this sync really changes last_seen_at: CURRENT_TIMESTAMP
	// has one-second resolution, and the same second would change nothing.
	if _, err := s.sqlDB.Exec(`UPDATE postings SET last_seen_at = datetime('now', '-1 day'), updated_at = datetime('now', '-1 day') WHERE id = ?`, posting.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	feed := mustNewChangeFeed(t, s)

	if err := s.MarkPostingsSeen(ctx, []int64{posting.ID}); err != nil {
		t.Fatalf("MarkPostingsSeen: %v", err)
	}
	if _, err := s.IngestPosting(ctx, params); err != nil {
		t.Fatalf("IngestPosting: %v", err)
	}

	if events := mustNext(t, feed); len(events) != 0 {
		t.Errorf("a sync that changed nothing logged %d events: %+v", len(events), events)
	}
}
