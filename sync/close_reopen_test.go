package sync

import (
	"context"
	"strings"
	"testing"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
)

func mustUpsertPosting(t *testing.T, s testutil.TestApp, companyID int64, sourceID, title string) posting.Posting {
	t.Helper()
	ingested, err := s.Postings.Ingest(context.Background(), companyID, jobboard.Ashby, sourceID, posting.IngestedFields{Title: title, RawPayload: "{}"})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return ingested.Posting
}

func mustCreateApplication(t *testing.T, s testutil.TestApp, postingID int64) application.Application {
	t.Helper()
	a, err := createApplication(context.Background(), s, postingID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return a
}

func mustClosePosting(t *testing.T, s testutil.TestApp, postingID int64, from []application.Status) {
	t.Helper()
	if err := s.Postings.Close(context.Background(), postingID, from); err != nil {
		t.Fatalf("closePosting: %v", err)
	}
}

func mustUpdateApplicationStatus(t *testing.T, s testutil.TestApp, postingID int64, status application.Status) {
	t.Helper()
	if _, err := updateApplicationStatus(context.Background(), s, postingID, status); err != nil {
		t.Fatalf("UpdateStatus(%s): %v", status, err)
	}
}

func TestClosePosting(t *testing.T) {
	t.Parallel()
	closeFrom := []application.Status{application.Started, application.Submitted}

	tests := []struct {
		name          string
		alreadyClosed bool
		application   *application.Status // nil: the posting has no application
		wantApp       application.Status
		wantHistory   int
	}{
		{name: "open, no application", wantHistory: 1},
		{
			name: "open, application at a status it ends", application: ptr(application.Started),
			wantApp: application.PostingClosed, wantHistory: 1,
		},
		{
			name: "open, application at a status it leaves alone", application: ptr(application.Interviewing),
			wantApp: application.Interviewing, wantHistory: 1,
		},
		{
			name: "already closed: changes nothing", alreadyClosed: true, application: ptr(application.Started),
			wantApp: application.Started, wantHistory: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)
			ctx := context.Background()
			acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
			posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
			if tt.application != nil {
				if _, err := createApplication(ctx, s, posting.ID); err != nil {
					t.Fatalf("application.Create: %v", err)
				}
				if _, err := updateApplicationStatus(ctx, s, posting.ID, *tt.application); err != nil {
					t.Fatalf("application.UpdateStatus: %v", err)
				}
			}
			if tt.alreadyClosed {
				if err := s.Postings.Close(ctx, posting.ID, nil); err != nil {
					t.Fatalf("Close: %v", err)
				}
			}
			before, err := s.Postings.History(ctx, posting.ID)
			if err != nil {
				t.Fatalf("ListPostingHistory: %v", err)
			}

			if err := s.Postings.Close(ctx, posting.ID, closeFrom); err != nil {
				t.Fatalf("Close: %v", err)
			}

			after, err := s.Postings.Get(ctx, posting.ID)
			if err != nil {
				t.Fatalf("GetPosting: %v", err)
			}
			if after.ListingStatus != "closed" {
				t.Errorf("ListingStatus = %q, want closed", after.ListingStatus)
			}
			history, err := s.Postings.History(ctx, posting.ID)
			if err != nil {
				t.Fatalf("ListPostingHistory: %v", err)
			}
			added := history[len(before):]
			if len(added) != tt.wantHistory {
				t.Fatalf("history rows added = %d, want %d", len(added), tt.wantHistory)
			}
			if tt.wantHistory == 1 {
				if added[0].ChangeType != "closed" || !strings.Contains(added[0].Snapshot, `"ListingStatus":"open"`) {
					t.Errorf("history = %q with snapshot %s, want a \"closed\" row snapshotting the posting as it was, still open", added[0].ChangeType, added[0].Snapshot)
				}
			}
			if tt.application != nil {
				app, err := s.Postings.Application(ctx, posting.ID)
				if err != nil {
					t.Fatalf("application.Get: %v", err)
				}
				if app.Status != tt.wantApp {
					t.Errorf("application status = %s, want %s", app.Status, tt.wantApp)
				}
			}
		})
	}
}

// TestReopenPosting_RestoresApplicationSyncClosed: a reopened posting
// undoes what its closing did to the application, and nothing else.
// The application goes back to the status it had before sync
// moved it to posting_closed, recorded as sync's change, in the same
// transaction as the reopen. A posting_closed the user set, a status the
// user changed since, or a close recorded before changed_by existed is
// left alone.
func TestReopenPosting_RestoresApplicationSyncClosed(t *testing.T) {
	t.Parallel()
	closeFrom := []application.Status{application.Started, application.Submitted}

	tests := []struct {
		name string
		// setup leaves the posting closed.
		setup         func(t *testing.T, s testutil.TestApp, postingID int64)
		wantApp       application.Status
		wantLastBy    application.ChangedBy
		noApplication bool
	}{
		{
			name: "sync closed it: back to started",
			setup: func(t *testing.T, s testutil.TestApp, postingID int64) {
				mustCreateApplication(t, s, postingID)
				mustClosePosting(t, s, postingID, closeFrom)
			},
			wantApp: application.Started, wantLastBy: application.ChangedBySync,
		},
		{
			name: "sync closed it from submitted: back to submitted",
			setup: func(t *testing.T, s testutil.TestApp, postingID int64) {
				mustCreateApplication(t, s, postingID)
				mustUpdateApplicationStatus(t, s, postingID, application.Submitted)
				mustClosePosting(t, s, postingID, closeFrom)
			},
			wantApp: application.Submitted, wantLastBy: application.ChangedBySync,
		},
		{
			name: "user set posting_closed: left alone",
			setup: func(t *testing.T, s testutil.TestApp, postingID int64) {
				mustCreateApplication(t, s, postingID)
				mustClosePosting(t, s, postingID, nil)
				mustUpdateApplicationStatus(t, s, postingID, application.PostingClosed)
			},
			wantApp: application.PostingClosed, wantLastBy: application.ChangedByUser,
		},
		{
			name: "user changed it after sync closed it: left alone",
			setup: func(t *testing.T, s testutil.TestApp, postingID int64) {
				mustCreateApplication(t, s, postingID)
				mustClosePosting(t, s, postingID, closeFrom)
				mustUpdateApplicationStatus(t, s, postingID, application.Withdrawn)
			},
			wantApp: application.Withdrawn, wantLastBy: application.ChangedByUser,
		},

		{
			name: "no application",
			setup: func(t *testing.T, s testutil.TestApp, postingID int64) {
				mustClosePosting(t, s, postingID, closeFrom)
			},
			noApplication: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := testutil.NewTestApp(t)
			ctx := context.Background()
			acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
			posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
			tt.setup(t, s, posting.ID)

			if err := s.Postings.Reopen(ctx, posting.ID); err != nil {
				t.Fatalf("Reopen: %v", err)
			}
			if tt.noApplication {
				return
			}
			app, err := s.Postings.Application(ctx, posting.ID)
			if err != nil {
				t.Fatalf("application.Get: %v", err)
			}
			if app.Status != tt.wantApp {
				t.Errorf("application status = %s, want %s", app.Status, tt.wantApp)
			}
			history, err := s.Applications.History(ctx, app.ID)
			if err != nil {
				t.Fatalf("application.ListStatusHistory: %v", err)
			}
			last := history[len(history)-1]
			if last.Status != tt.wantApp || last.ChangedBy != tt.wantLastBy {
				t.Errorf("last history row = %s by %s, want %s by %s", last.Status, last.ChangedBy, tt.wantApp, tt.wantLastBy)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
