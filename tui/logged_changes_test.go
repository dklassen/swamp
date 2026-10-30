package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/change"
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
			return message.Logged(row.ID, mcp, message.Subject{Type: message.TypeApplication, ID: a.ID}, message.OpUpdated, row.At, applicationSnapshot(*a)), nil
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

			reader := changeReader{companies: s.Companies, postings: s.Postings, applications: s.Applications, documents: s.Documents, scores: s.Scores, origin: "tui:1"}
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
