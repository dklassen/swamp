package sync

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/application"
	"github.com/dklassen/swamp/applicationstatus"
	"github.com/dklassen/swamp/store"
)

type closeResult struct{ Closed, ApplicationClosed bool }

func closeWith(ctx context.Context, s *store.Store, postingID int64, from []applicationstatus.Status) (closeResult, error) {
	closed, ended, err := closePosting(ctx, s, postingID, from)
	return closeResult{Closed: closed, ApplicationClosed: ended}, err
}

type reopenResult struct{ Reopened, ApplicationRestored bool }

func reopenWith(ctx context.Context, s *store.Store, postingID int64) (reopenResult, error) {
	reopened, restored, err := reopenPosting(ctx, s, postingID)
	return reopenResult{Reopened: reopened, ApplicationRestored: restored}, err
}

func mustUpsertPosting(t *testing.T, s *store.Store, companyID int64, sourceID, title string) store.Posting {
	t.Helper()
	p, err := s.UpsertPosting(context.Background(), store.CreatePostingParams{
		CompanyID: companyID, Source: "ashby", SourceID: sourceID,
		IngestedFields: store.IngestedFields{Title: title, RawPayload: "{}"},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	return p
}

func mustCreateApplication(t *testing.T, s *store.Store, postingID int64) application.Application {
	t.Helper()
	a, err := createApplication(context.Background(), s, postingID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return a
}

func mustClosePosting(t *testing.T, s *store.Store, postingID int64, from []applicationstatus.Status) {
	t.Helper()
	if _, err := closeWith(context.Background(), s, postingID, from); err != nil {
		t.Fatalf("closePosting: %v", err)
	}
}

func mustUpdateApplicationStatus(t *testing.T, s *store.Store, postingID int64, status applicationstatus.Status) {
	t.Helper()
	if _, err := updateApplicationStatus(context.Background(), s, postingID, status); err != nil {
		t.Fatalf("UpdateStatus(%s): %v", status, err)
	}
}

func TestClosePosting(t *testing.T) {
	t.Parallel()
	closeFrom := []applicationstatus.Status{applicationstatus.Started, applicationstatus.Submitted}

	tests := []struct {
		name          string
		alreadyClosed bool
		application   *applicationstatus.Status // nil: the posting has no application
		want          closeResult
		wantApp       applicationstatus.Status
		wantHistory   int
	}{
		{name: "open, no application", want: closeResult{Closed: true}, wantHistory: 1},
		{name: "open, application at a status it ends", application: ptr(applicationstatus.Started),
			want: closeResult{Closed: true, ApplicationClosed: true}, wantApp: applicationstatus.PostingClosed, wantHistory: 1},
		{name: "open, application at a status it leaves alone", application: ptr(applicationstatus.Interviewing),
			want: closeResult{Closed: true}, wantApp: applicationstatus.Interviewing, wantHistory: 1},
		{name: "already closed: changes nothing", alreadyClosed: true, application: ptr(applicationstatus.Started),
			want: closeResult{}, wantApp: applicationstatus.Started, wantHistory: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestStore(t)
			ctx := context.Background()
			acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
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
				if err := s.MarkPostingClosed(ctx, posting.ID); err != nil {
					t.Fatalf("MarkPostingClosed: %v", err)
				}
			}

			got, err := closeWith(ctx, s, posting.ID, closeFrom)
			if err != nil {
				t.Fatalf("ClosePosting: %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ClosePosting result mismatch (-want +got):\n%s", diff)
			}

			after, err := s.GetPosting(ctx, posting.ID)
			if err != nil {
				t.Fatalf("GetPosting: %v", err)
			}
			if after.ListingStatus != "closed" {
				t.Errorf("ListingStatus = %q, want closed", after.ListingStatus)
			}
			history, err := s.ListPostingHistory(ctx, posting.ID)
			if err != nil {
				t.Fatalf("ListPostingHistory: %v", err)
			}
			if len(history) != tt.wantHistory {
				t.Fatalf("history rows = %d, want %d", len(history), tt.wantHistory)
			}
			if tt.wantHistory == 1 {
				if history[0].ChangeType != "closed" || !strings.Contains(history[0].Snapshot, `"ListingStatus":"open"`) {
					t.Errorf("history = %q with snapshot %s, want a \"closed\" row snapshotting the posting as it was, still open", history[0].ChangeType, history[0].Snapshot)
				}
			}
			if tt.application != nil {
				app, err := application.Get(ctx, s.DB(), posting.ID)
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
	closeFrom := []applicationstatus.Status{applicationstatus.Started, applicationstatus.Submitted}

	tests := []struct {
		name string
		// setup leaves the posting closed.
		setup         func(t *testing.T, s *store.Store, sqlDB *sql.DB, postingID int64)
		want          reopenResult
		wantApp       applicationstatus.Status
		wantLastBy    application.ChangedBy
		noApplication bool
	}{
		{
			name: "sync closed it: back to started",
			setup: func(t *testing.T, s *store.Store, sqlDB *sql.DB, postingID int64) {
				mustCreateApplication(t, s, postingID)
				mustClosePosting(t, s, postingID, closeFrom)
			},
			want:    reopenResult{Reopened: true, ApplicationRestored: true},
			wantApp: applicationstatus.Started, wantLastBy: application.ChangedBySync,
		},
		{
			name: "sync closed it from submitted: back to submitted",
			setup: func(t *testing.T, s *store.Store, sqlDB *sql.DB, postingID int64) {
				mustCreateApplication(t, s, postingID)
				mustUpdateApplicationStatus(t, s, postingID, applicationstatus.Submitted)
				mustClosePosting(t, s, postingID, closeFrom)
			},
			want:    reopenResult{Reopened: true, ApplicationRestored: true},
			wantApp: applicationstatus.Submitted, wantLastBy: application.ChangedBySync,
		},
		{
			name: "user set posting_closed: left alone",
			setup: func(t *testing.T, s *store.Store, sqlDB *sql.DB, postingID int64) {
				mustCreateApplication(t, s, postingID)
				mustClosePosting(t, s, postingID, nil)
				mustUpdateApplicationStatus(t, s, postingID, applicationstatus.PostingClosed)
			},
			want:    reopenResult{Reopened: true},
			wantApp: applicationstatus.PostingClosed, wantLastBy: application.ChangedByUser,
		},
		{
			name: "user changed it after sync closed it: left alone",
			setup: func(t *testing.T, s *store.Store, sqlDB *sql.DB, postingID int64) {
				mustCreateApplication(t, s, postingID)
				mustClosePosting(t, s, postingID, closeFrom)
				mustUpdateApplicationStatus(t, s, postingID, applicationstatus.Withdrawn)
			},
			want:    reopenResult{Reopened: true},
			wantApp: applicationstatus.Withdrawn, wantLastBy: application.ChangedByUser,
		},
		{
			name: "closed before changed_by existed: left alone",
			setup: func(t *testing.T, s *store.Store, sqlDB *sql.DB, postingID int64) {
				mustCreateApplication(t, s, postingID)
				mustClosePosting(t, s, postingID, closeFrom)
				if _, err := sqlDB.ExecContext(context.Background(), `UPDATE application_status_history SET changed_by = 'unknown'`); err != nil {
					t.Fatalf("mark history unknown: %v", err)
				}
			},
			want:    reopenResult{Reopened: true},
			wantApp: applicationstatus.PostingClosed, wantLastBy: application.ChangedByUnknown,
		},
		{
			name: "no application",
			setup: func(t *testing.T, s *store.Store, sqlDB *sql.DB, postingID int64) {
				mustClosePosting(t, s, postingID, closeFrom)
			},
			want:          reopenResult{Reopened: true},
			noApplication: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, sqlDB := newTestStoreDB(t)
			ctx := context.Background()
			acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
			posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
			tt.setup(t, s, sqlDB, posting.ID)

			got, err := reopenWith(ctx, s, posting.ID)
			if err != nil {
				t.Fatalf("ReopenPosting: %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ReopenPosting result mismatch (-want +got):\n%s", diff)
			}
			if tt.noApplication {
				return
			}
			app, err := application.Get(ctx, s.DB(), posting.ID)
			if err != nil {
				t.Fatalf("application.Get: %v", err)
			}
			if app.Status != tt.wantApp {
				t.Errorf("application status = %s, want %s", app.Status, tt.wantApp)
			}
			history, err := application.ListStatusHistory(ctx, s.DB(), app.ID)
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

// TestReopenPosting_RestoreFails_PostingStaysClosed: the reopen and the
// application restore are one change, so a failed restore leaves
// the posting closed and unrecorded, and the next sync tries both again.
func TestReopenPosting_RestoreFails_PostingStaysClosed(t *testing.T) {
	t.Parallel()
	s, sqlDB := newTestStoreDB(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	mustCreateApplication(t, s, posting.ID)
	mustClosePosting(t, s, posting.ID, []applicationstatus.Status{applicationstatus.Started})
	if _, err := sqlDB.ExecContext(ctx, `CREATE TRIGGER fail_application_update BEFORE UPDATE ON applications
		BEGIN SELECT RAISE(ABORT, 'simulated failure'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if _, err := reopenWith(ctx, s, posting.ID); err == nil {
		t.Fatal("ReopenPosting with a failing application update: want an error, got nil")
	}

	after, err := s.GetPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}
	if after.ListingStatus != "closed" {
		t.Errorf("ListingStatus = %q, want still closed", after.ListingStatus)
	}
	history, err := s.ListPostingHistory(ctx, posting.ID)
	if err != nil {
		t.Fatalf("ListPostingHistory: %v", err)
	}
	if n := len(history); n != 1 || history[0].ChangeType != "closed" {
		t.Errorf("posting history = %d rows, want only the close", n)
	}
}

func ptr[T any](v T) *T { return &v }
