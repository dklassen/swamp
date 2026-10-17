package service_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

// After returns what another process changed since Latest, stamped with
// its origin, and nothing from before.
func TestChangeLog_After_ReturnsAnotherProcessesChangeSinceLatest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	other := testutil.OpenStore(t, s.Path, "mcp:7")
	acme, err := service.NewCompanyService(other).Add(ctx, "Acme", jobboard.Ashby, "acme")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	ingested, err := service.NewPostingService(other).Ingest(ctx, acme.ID, jobboard.Ashby, "job-1", posting.IngestedFields{Title: "Engineer", RawPayload: "{}"})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	app, err := service.NewPostingService(other).ApplyTo(ctx, ingested.Posting.ID)
	if err != nil {
		t.Fatalf("ApplyTo: %v", err)
	}
	changes := s.Changes
	since, err := changes.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}

	if _, err := service.NewApplicationService(other).SetStatus(ctx, app.ID, application.Interviewing); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	got, err := changes.After(ctx, since)
	if err != nil {
		t.Fatalf("After: %v", err)
	}
	want := []change.Event{{
		Table:  "applications",
		RowID:  app.ID,
		Op:     "update",
		Old:    `{"posting_id":1,"status":"application_started","notes":"","deleted_at":null}`,
		New:    `{"posting_id":1,"status":"interviewing","notes":"","deleted_at":null}`,
		Origin: "mcp:7",
	}}
	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(change.Event{}, "ID", "At")); diff != "" {
		t.Errorf("After (-want +got):\n%s", diff)
	}
}

// The log keeps no reader's place: After reads from any point, oldest
// first, and Latest is the newest event.
func TestChangeLog_After_ReadsFromAnyPoint(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	changes := s.Changes
	start, err := changes.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	acme := testutil.MustAddCompany(t, s, "Acme")
	if _, err := s.Companies.SetName(ctx, acme.ID, "Acme Corp"); err != nil {
		t.Fatalf("SetName: %v", err)
	}

	all, err := changes.After(ctx, start)
	if err != nil {
		t.Fatalf("After(start): %v", err)
	}
	if len(all) != 2 || all[0].Op != "insert" || all[1].Op != "update" {
		t.Fatalf("After(start) = %+v, want the company's insert then its update", all)
	}
	later, err := changes.After(ctx, all[0].ID)
	if err != nil {
		t.Fatalf("After(insert): %v", err)
	}
	if len(later) != 1 || later[0].ID != all[1].ID {
		t.Errorf("After(insert) = %+v, want only the update", later)
	}
	latest, err := changes.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest != all[1].ID {
		t.Errorf("Latest = %d, want the update's %d", latest, all[1].ID)
	}
}

// A change through the services to each logged table is an event from its
// writer.
func TestChangeLog_EachLoggedTable(t *testing.T) {
	t.Parallel()

	type fixture struct {
		db                                  store.Store
		companyID, postingID, applicationID int64
	}
	tests := []struct {
		name   string
		change func(context.Context, fixture) error
		table  string
		op     string
	}{
		{"company added", func(ctx context.Context, f fixture) error {
			_, err := service.NewCompanyService(f.db).Add(ctx, "Globex", jobboard.Lever, "globex")
			return err
		}, "companies", "insert"},
		{"company renamed", func(ctx context.Context, f fixture) error {
			_, err := service.NewCompanyService(f.db).SetName(ctx, f.companyID, "Acme Corp")
			return err
		}, "companies", "update"},
		{"company deleted", func(ctx context.Context, f fixture) error {
			return service.NewCompanyService(f.db).Delete(ctx, f.companyID)
		}, "companies", "update"},
		{"posting found", func(ctx context.Context, f fixture) error {
			_, err := service.NewPostingService(f.db).Ingest(ctx, f.companyID, jobboard.Ashby, "job-2", posting.IngestedFields{Title: "Designer", RawPayload: "{}"})
			return err
		}, "postings", "insert"},
		{"posting closed", func(ctx context.Context, f fixture) error {
			_, _, err := service.NewPostingService(f.db).Close(ctx, f.postingID, nil)
			return err
		}, "postings", "update"},
		{"document written", func(ctx context.Context, f fixture) error {
			return service.NewDocumentService(f.db).RecordWrite(ctx, f.applicationID, document.CoverLetter, "draft", document.SourceEditor)
		}, "document_writes", "insert"},
		{"document reviewed", func(ctx context.Context, f fixture) error {
			_, err := service.NewDocumentService(f.db).Review(ctx, f.applicationID, document.CoverLetter, "draft", document.Passed, "")
			return err
		}, "document_reviews", "insert"},
		{"document exported", func(ctx context.Context, f fixture) error {
			return service.NewDocumentService(f.db).RecordExport(ctx, f.applicationID, document.CoverLetter, "draft", "/out/cover_letter.pdf")
		}, "document_exports", "insert"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			s := testutil.NewTestApp(t)
			writer := testutil.OpenStore(t, s.Path, "mcp:7")
			p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
			app := testutil.MustApplyTo(t, s, p.ID)
			changes := s.Changes
			since, err := changes.Latest(ctx)
			if err != nil {
				t.Fatalf("Latest: %v", err)
			}

			if err := tt.change(ctx, fixture{db: writer, companyID: p.CompanyID, postingID: p.ID, applicationID: app.ID}); err != nil {
				t.Fatalf("change: %v", err)
			}

			events, err := changes.After(ctx, since)
			if err != nil {
				t.Fatalf("After: %v", err)
			}
			var got []string
			for _, e := range events {
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

// An application's event carries its posting_id, so a reader can tell
// which posting gained an application.
func TestChangeLog_AnApplicationEventNamesItsPosting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	p := testutil.MustIngestPosting(t, s, "job-1", "Engineer")
	changes := s.Changes
	since, err := changes.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}

	testutil.MustApplyTo(t, s, p.ID)

	events, err := changes.After(ctx, since)
	if err != nil {
		t.Fatalf("After: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %+v, want one", events)
	}
	var row struct {
		PostingID int64 `json:"posting_id"`
	}
	if err := json.Unmarshal([]byte(events[0].New), &row); err != nil {
		t.Fatalf("event values %q: %v", events[0].New, err)
	}
	if row.PostingID != p.ID {
		t.Errorf("the application event's posting_id = %d, want %d (event values %s)", row.PostingID, p.ID, events[0].New)
	}
}
