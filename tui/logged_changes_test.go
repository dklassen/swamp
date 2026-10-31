package tui

import (
	"context"
	"maps"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dklassen/swamp/db/migrations"
	"github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/tui/message"
)

var mcp = message.Source{Process: "mcp:7"}

// Another process's write to each logged table is the Changed for that
// table, from that process, when the log recorded it, with After as the
// row is now.
func TestChangeReader_AnotherProcessesWrite_IsTheChangedForItsTable(t *testing.T) {
	t.Parallel()

	type fixture struct {
		s                                   testutil.TestApp
		writer                              store.Store
		companyID, postingID, applicationID int64
	}
	tests := []struct {
		name  string
		table string
		write func(context.Context, fixture) error
		// want is the event row records, read back through s.
		want func(ctx context.Context, f fixture, row change.Event) (tea.Msg, error)
	}{
		{"company renamed", "companies", func(ctx context.Context, f fixture) error {
			_, err := service.NewCompanyService(f.writer).SetName(ctx, f.companyID, "Acme Corp")
			return err
		}, func(ctx context.Context, f fixture, row change.Event) (tea.Msg, error) {
			c, err := f.s.Companies.Get(ctx, f.companyID)
			if err != nil {
				return nil, err
			}
			return message.Logged(row.ID, mcp, message.Subject{Type: message.TypeCompany, ID: c.ID}, message.OpUpdated, row.At, companySnapshot(*c)), nil
		}},
		{"application deleted", "applications", func(ctx context.Context, f fixture) error {
			_, err := service.NewApplicationService(f.writer).Delete(ctx, f.applicationID)
			return err
		}, func(ctx context.Context, f fixture, row change.Event) (tea.Msg, error) {
			a, err := f.s.Applications.GetIncludingDeleted(ctx, f.applicationID)
			if err != nil {
				return nil, err
			}
			// A soft delete is logged as an update. Before is the row as it's
			// left, since what it was is gone.
			e := message.Logged(row.ID, mcp, message.Subject{Type: message.TypeApplication, ID: a.ID}, message.OpDeleted, row.At, message.Application{})
			e.Payload.Before = applicationSnapshot(*a)
			return e, nil
		}},
		{"posting closed", "postings", func(ctx context.Context, f fixture) error {
			return service.NewPostingService(f.writer).Close(ctx, f.postingID, nil)
		}, func(ctx context.Context, f fixture, row change.Event) (tea.Msg, error) {
			p, err := f.s.Postings.Get(ctx, f.postingID)
			if err != nil {
				return nil, err
			}
			return message.Logged(row.ID, mcp, message.Subject{Type: message.TypePosting, ID: p.ID}, message.OpUpdated, row.At, postingSnapshot(*p)), nil
		}},
		{"posting marked interested", "posting_markup", func(ctx context.Context, f fixture) error {
			_, err := service.NewPostingService(f.writer).SetInterested(ctx, f.postingID)
			return err
		}, func(ctx context.Context, f fixture, row change.Event) (tea.Msg, error) {
			m, err := f.s.Postings.Markup(ctx, f.postingID)
			if err != nil {
				return nil, err
			}
			return message.Logged(row.ID, mcp, message.Subject{Type: message.TypePosting, ID: m.PostingID}, message.OpUpdated, row.At, markupSnapshot(*m)), nil
		}},
		{"posting scored", "posting_scores", func(ctx context.Context, f fixture) error {
			p, err := f.s.Postings.Get(ctx, f.postingID)
			if err != nil {
				return err
			}
			_, err = service.NewScoringService(f.writer).Record(ctx, score.Provenance{Model: "claude-sonnet-5", ProfileSHA256: "profile"}, []score.Entry{{PostingID: p.ID, Score: 4, Reason: "fits", ContentSHA256: p.ContentSHA256}})
			return err
		}, func(ctx context.Context, f fixture, row change.Event) (tea.Msg, error) {
			scores, err := f.s.Scores.ForPosting(ctx, f.postingID)
			if err != nil {
				return nil, err
			}
			return message.Logged(row.ID, mcp, message.Subject{Type: message.TypePosting, ID: f.postingID}, message.OpCreated, row.At, message.Score{
				ID: scores[0].ID, PostingID: f.postingID, Score: 4, Reason: "fits", Model: "claude-sonnet-5", ContentSHA256: scores[0].ContentSHA256, ScoredAt: scores[0].ScoredAt,
			}), nil
		}},
		{"document reviewed", "document_reviews", func(ctx context.Context, f fixture) error {
			_, err := service.NewDocumentService(f.writer).Review(ctx, f.applicationID, document.CoverLetter, "draft", document.Flagged, "too long")
			return err
		}, func(ctx context.Context, f fixture, row change.Event) (tea.Msg, error) {
			r, err := f.s.Documents.LatestReview(ctx, f.applicationID, document.CoverLetter)
			if err != nil {
				return nil, err
			}
			return message.Logged(row.ID, mcp, message.Subject{Type: message.TypeApplication, ID: f.applicationID}, message.OpCreated, row.At, reviewSnapshot(*r)), nil
		}},
		{"document written", "document_writes", func(ctx context.Context, f fixture) error {
			return service.NewDocumentService(f.writer).RecordWrite(ctx, f.applicationID, document.Resume, "draft", document.SourceWriteDocument)
		}, func(ctx context.Context, f fixture, row change.Event) (tea.Msg, error) {
			w, err := f.s.Documents.LatestWrite(ctx, f.applicationID, document.Resume)
			if err != nil {
				return nil, err
			}
			return message.Logged(row.ID, mcp, message.Subject{Type: message.TypeApplication, ID: f.applicationID}, message.OpCreated, row.At, message.DocumentWrite{
				ID: w.ID, ApplicationID: f.applicationID, DocumentType: message.DocumentResume, ContentSHA256: document.ContentSHA256("draft"), WrittenAt: w.WrittenAt,
			}), nil
		}},
		{"document exported", "document_exports", func(ctx context.Context, f fixture) error {
			return service.NewDocumentService(f.writer).RecordExport(ctx, f.applicationID, document.CoverLetter, "draft", "/out/cover_letter.pdf")
		}, func(ctx context.Context, f fixture, row change.Event) (tea.Msg, error) {
			exports, err := f.s.Documents.LatestExports(ctx, f.applicationID)
			if err != nil {
				return nil, err
			}
			e := exports[document.CoverLetter]
			return message.Logged(row.ID, mcp, message.Subject{Type: message.TypeApplication, ID: f.applicationID}, message.OpCreated, row.At, message.DocumentExport{
				ID: e.ID, ApplicationID: f.applicationID, DocumentType: message.DocumentCoverLetter, ContentSHA256: document.ContentSHA256("draft"), ExportedAt: e.ExportedAt,
			}), nil
		}},
	}
	var tables []string
	for _, tt := range tests {
		tables = append(tables, tt.table)
	}
	if diff := cmp.Diff(slices.Sorted(maps.Keys(migrations.Logged)), tables, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
		t.Errorf("tables with a case, against the logged tables (-logged +cases):\n%s", diff)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			s := testutil.NewTestApp(t)
			acme, err := s.Companies.Add(ctx, "Acme", jobboard.Ashby, "acme")
			if err != nil {
				t.Fatalf("Add: %v", err)
			}
			p := testutil.MustIngestPostingFor(t, s, acme.ID, "job-1", "Engineer")
			app := testutil.MustApplyTo(t, s, p.ID)
			f := fixture{s: s, writer: testutil.OpenStore(t, s.Path, mcp.Process), companyID: acme.ID, postingID: p.ID, applicationID: app.ID}
			since, err := s.Changes.Latest(ctx)
			if err != nil {
				t.Fatalf("Latest: %v", err)
			}
			if err := tt.write(ctx, f); err != nil {
				t.Fatalf("write: %v", err)
			}
			rows, err := s.Changes.After(ctx, since)
			if err != nil {
				t.Fatalf("After: %v", err)
			}
			var row change.Event
			var want []tea.Msg
			for _, r := range rows {
				if r.Table == tt.table {
					row = r
					e, err := tt.want(ctx, f, r)
					if err != nil {
						t.Fatalf("want: %v", err)
					}
					want = append(want, e)
				}
			}
			if len(want) != 1 {
				t.Fatalf("the write logged %d %s rows, want 1: %+v", len(want), tt.table, rows)
			}

			reader := newChangeReader(s.Companies, s.Postings, s.Applications, s.Documents, s.Scores, "tui:1")
			got, err := reader.changed(ctx, []change.Event{row})
			if err != nil {
				t.Fatalf("changed: %v", err)
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("events (-want +got):\n%s", diff)
			}
		})
	}
}

// The TUI's own writes come back through the log too, but the command
// that made each already sent its event.
func TestChangeReader_ItsOwnRows_AreSkipped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	acme, err := s.Companies.Add(ctx, "Acme", jobboard.Ashby, "acme")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	since, err := s.Changes.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if _, err := service.NewCompanyService(testutil.OpenStore(t, s.Path, "tui:1")).SetName(ctx, acme.ID, "Acme Corp"); err != nil {
		t.Fatalf("SetName by the TUI: %v", err)
	}
	if _, err := service.NewCompanyService(testutil.OpenStore(t, s.Path, mcp.Process)).SetName(ctx, acme.ID, "Acme Inc"); err != nil {
		t.Fatalf("SetName by the agent: %v", err)
	}
	rows, err := s.Changes.After(ctx, since)
	if err != nil {
		t.Fatalf("After: %v", err)
	}

	reader := newChangeReader(s.Companies, s.Postings, s.Applications, s.Documents, s.Scores, "tui:1")
	got, err := reader.changed(ctx, rows)
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	var from []string
	for _, e := range got {
		from = append(from, e.(message.Event[message.Changed[message.Company]]).Meta.Source.Process)
	}
	if diff := cmp.Diff([]string{mcp.Process}, from); diff != "" {
		t.Errorf("events' processes (-want +got):\n%s", diff)
	}
}

// A page of the log read twice, as after a restart, is the same events,
// so a consumer that has seen one can drop it.
func TestChangeReader_TheSameRowsReadTwice_AreTheSameEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	acme, err := s.Companies.Add(ctx, "Acme", jobboard.Ashby, "acme")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	since, err := s.Changes.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	writer := service.NewCompanyService(testutil.OpenStore(t, s.Path, mcp.Process))
	for _, name := range []string{"Acme Corp", "Acme Inc"} {
		if _, err := writer.SetName(ctx, acme.ID, name); err != nil {
			t.Fatalf("SetName: %v", err)
		}
	}
	rows, err := s.Changes.After(ctx, since)
	if err != nil {
		t.Fatalf("After: %v", err)
	}
	reader := newChangeReader(s.Companies, s.Postings, s.Applications, s.Documents, s.Scores, "tui:1")

	first, err := reader.changed(ctx, rows)
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	again, err := reader.changed(ctx, rows)
	if err != nil {
		t.Fatalf("changed again: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("changed gave %d events, want 2", len(first))
	}
	if diff := cmp.Diff(first, again); diff != "" {
		t.Errorf("the same rows read twice (-first +again):\n%s", diff)
	}
	if a, b := first[0].(message.Event[message.Changed[message.Company]]).Meta.ID, first[1].(message.Event[message.Changed[message.Company]]).Meta.ID; a == b {
		t.Errorf("two rows share the ID %q", a)
	}
}

// A row is read as its entity is now: one gone since is left out, as a
// later row is its delete, and a delete has nothing to read.
func TestChangeReader_AGoneRowIsLeftOutAndADeleteHasNoAfter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	at := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	rows := []change.Event{
		{ID: 1, Table: "postings", RowID: 9999, Op: "update", Origin: mcp.Process, At: at},
		{ID: 2, Table: "companies", RowID: 5, Op: "delete", Origin: mcp.Process, At: at},
	}
	reader := newChangeReader(s.Companies, s.Postings, s.Applications, s.Documents, s.Scores, "tui:1")

	got, err := reader.changed(ctx, rows)
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	want := []tea.Msg{message.Logged(2, mcp, message.Subject{Type: message.TypeCompany, ID: 5}, message.OpDeleted, at, message.Company{})}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("events (-want +got):\n%s", diff)
	}
}

// Every logged table's rows are an event, so a table logged later can't
// be skipped without saying so.
func TestChangeReader_EveryLoggedTable_HasAnEvent(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	reader := newChangeReader(s.Companies, s.Postings, s.Applications, s.Documents, s.Scores, "tui:1")
	for _, table := range slices.Sorted(maps.Keys(migrations.Logged)) {
		got, err := reader.changed(context.Background(), []change.Event{{ID: 1, Table: table, RowID: 5, Op: "delete", Origin: mcp.Process}})
		if err != nil {
			t.Errorf("%s: %v", table, err)
			continue
		}
		if len(got) != 1 {
			t.Errorf("%s: a delete gave %d events, want 1", table, len(got))
		}
	}
}

// A company deleted by another process is a delete, as the TUI's own is,
// though the log records its soft delete as an update.
func TestChangeReader_ACompanyDeletedElsewhere_IsADelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	acme, err := s.Companies.Add(ctx, "Acme", jobboard.Ashby, "acme")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	since, err := s.Changes.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if _, err := service.NewCompanyService(testutil.OpenStore(t, s.Path, mcp.Process)).Delete(ctx, acme.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	rows, err := s.Changes.After(ctx, since)
	if err != nil {
		t.Fatalf("After: %v", err)
	}

	got, err := newChangeReader(s.Companies, s.Postings, s.Applications, s.Documents, s.Scores, "tui:1").changed(ctx, rows)
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	var ops []message.Op
	for _, e := range got {
		if c, ok := e.(message.Event[message.Changed[message.Company]]); ok {
			ops = append(ops, c.Meta.Op)
		}
	}
	if diff := cmp.Diff([]message.Op{message.OpDeleted}, ops); diff != "" {
		t.Errorf("company events' ops (-want +got):\n%s", diff)
	}
}

// A company created and deleted elsewhere since the last read is only its
// delete: the create, read as it is now, would show a deleted company.
func TestChangeReader_ACompanyCreatedThenDeletedElsewhere_IsOnlyADelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	since, err := s.Changes.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	agent := service.NewCompanyService(testutil.OpenStore(t, s.Path, mcp.Process))
	acme, err := agent.Add(ctx, "Acme", jobboard.Ashby, "acme")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := agent.Delete(ctx, acme.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	rows, err := s.Changes.After(ctx, since)
	if err != nil {
		t.Fatalf("After: %v", err)
	}

	got, err := newChangeReader(s.Companies, s.Postings, s.Applications, s.Documents, s.Scores, "tui:1").changed(ctx, rows)
	if err != nil {
		t.Fatalf("changed: %v", err)
	}
	var ops []message.Op
	for _, e := range got {
		if c, ok := e.(message.Event[message.Changed[message.Company]]); ok {
			ops = append(ops, c.Meta.Op)
		}
	}
	if diff := cmp.Diff([]message.Op{message.OpDeleted}, ops); diff != "" {
		t.Errorf("company events' ops (-want +got):\n%s", diff)
	}
}
