package application_test

import (
	"context"
	"errors"
	"github.com/dklassen/swamp/application"
	"github.com/dklassen/swamp/store"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/applicationstatus"
)

// historyStatuses lists an application's recorded statuses, oldest first.
func historyStatuses(t *testing.T, s testStore, applicationID int64) []applicationstatus.Status {
	t.Helper()
	history, err := application.ListStatusHistory(context.Background(), s.DB(), applicationID)
	if err != nil {
		t.Fatalf("application.ListStatusHistory: %v", err)
	}
	statuses := make([]applicationstatus.Status, len(history))
	for i, change := range history {
		if change.ApplicationID != applicationID {
			t.Errorf("history row %d has ApplicationID %d, want %d", i, change.ApplicationID, applicationID)
		}
		if change.ChangedAt.IsZero() {
			t.Errorf("history row %d has a zero ChangedAt", i)
		}
		statuses[i] = change.Status
	}
	return statuses
}

func TestCreateApplication_RecordsStartedInHistory(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	app := mustCreateApplication(t, s, posting.ID)

	want := []applicationstatus.Status{applicationstatus.Started}
	if diff := cmp.Diff(want, historyStatuses(t, s, app.ID)); diff != "" {
		t.Errorf("history mismatch (-want +got):\n%s", diff)
	}
}

func TestUpdateApplicationStatus_RecordsChangeInHistory(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	app := mustCreateApplication(t, s, posting.ID)

	if _, err := updateApplicationStatus(context.Background(), s, posting.ID, applicationstatus.Submitted); err != nil {
		t.Fatalf("application.UpdateStatus: %v", err)
	}

	want := []applicationstatus.Status{applicationstatus.Started, applicationstatus.Submitted}
	if diff := cmp.Diff(want, historyStatuses(t, s, app.ID)); diff != "" {
		t.Errorf("history mismatch (-want +got):\n%s", diff)
	}
}

// failHistoryInserts makes every later application_status_history insert
// fail, so a test can check that a status write whose history row fails
// commits neither.
func failHistoryInserts(t *testing.T, s testStore) {
	t.Helper()
	if _, err := s.sqlDB.Exec(`CREATE TRIGGER fail_history BEFORE INSERT ON application_status_history
		BEGIN SELECT RAISE(ABORT, 'injected history failure'); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
}

func TestUpdateApplicationStatus_HistoryFailure_LeavesStatusUnchanged(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	app := mustCreateApplication(t, s, posting.ID)
	failHistoryInserts(t, s)

	if _, err := updateApplicationStatus(ctx, s, posting.ID, applicationstatus.Submitted); err == nil {
		t.Fatal("application.UpdateStatus succeeded, want the injected history failure")
	}

	got, err := application.Get(ctx, s.DB(), posting.ID)
	if err != nil {
		t.Fatalf("application.Get: %v", err)
	}
	if diff := cmp.Diff(app, got); diff != "" {
		t.Errorf("application changed despite the failed history write (-before +after):\n%s", diff)
	}
	want := []applicationstatus.Status{applicationstatus.Started}
	if diff := cmp.Diff(want, historyStatuses(t, s, app.ID)); diff != "" {
		t.Errorf("history mismatch (-want +got):\n%s", diff)
	}
}

func TestCreateApplication_HistoryFailure_CreatesNoApplication(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	failHistoryInserts(t, s)

	if _, err := createApplication(ctx, s, posting.ID); err == nil {
		t.Fatal("application.Create succeeded, want the injected history failure")
	}

	if _, err := application.Get(ctx, s.DB(), posting.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("application.Get error = %v, want store.ErrNotFound: the application should not exist", err)
	}
}

// The status screen opens with its cursor on the current status, so
// saving without moving it is easy; that isn't a change and would make
// the application look like it re-entered the status later than it did.
func TestUpdateApplicationStatus_SameStatus_RecordsNothing(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	app := mustCreateApplication(t, s, posting.ID)

	if _, err := updateApplicationStatus(context.Background(), s, posting.ID, applicationstatus.Started); err != nil {
		t.Fatalf("application.UpdateStatus: %v", err)
	}

	want := []applicationstatus.Status{applicationstatus.Started}
	if diff := cmp.Diff(want, historyStatuses(t, s, app.ID)); diff != "" {
		t.Errorf("history mismatch (-want +got):\n%s", diff)
	}
}

// The example from #162: an application submitted before sync closed its
// posting keeps its submission in history, with the time of each change.
func TestClosePosting_KeepsSubmittedThenClosedInHistory(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	app := mustCreateApplication(t, s, posting.ID)
	if _, err := updateApplicationStatus(ctx, s, posting.ID, applicationstatus.Submitted); err != nil {
		t.Fatalf("application.UpdateStatus: %v", err)
	}

	_, ended, err := closePosting(ctx, s, posting.ID, []applicationstatus.Status{applicationstatus.Started, applicationstatus.Submitted})
	if err != nil {
		t.Fatalf("ClosePosting: %v", err)
	}
	if !ended {
		t.Fatal("ClosePosting did not close the application")
	}

	want := []applicationstatus.Status{applicationstatus.Started, applicationstatus.Submitted, applicationstatus.PostingClosed}
	if diff := cmp.Diff(want, historyStatuses(t, s, app.ID)); diff != "" {
		t.Errorf("history mismatch (-want +got):\n%s", diff)
	}
}

func TestClosePosting_HistoryFailure_LeavesPostingAndApplicationOpen(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	app := mustCreateApplication(t, s, posting.ID)
	failHistoryInserts(t, s)

	if _, _, err := closePosting(ctx, s, posting.ID, []applicationstatus.Status{applicationstatus.Started}); err == nil {
		t.Fatal("ClosePosting succeeded, want the injected history failure")
	}

	after, err := s.GetPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}
	if after.ListingStatus != "open" {
		t.Errorf("ListingStatus = %q, want open", after.ListingStatus)
	}
	got, err := application.Get(ctx, s.DB(), posting.ID)
	if err != nil {
		t.Fatalf("application.Get: %v", err)
	}
	if diff := cmp.Diff(app, got); diff != "" {
		t.Errorf("application changed despite the failed history write (-before +after):\n%s", diff)
	}
}

// TestApplicationStatusHistory_RecordsWhoChangedIt: starting an
// application and setting its status are the user's changes; closing its
// posting is sync's, which is what lets a reopened posting undo
// only what sync did.
func TestApplicationStatusHistory_RecordsWhoChangedIt(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	app := mustCreateApplication(t, s, posting.ID)
	if _, err := updateApplicationStatus(ctx, s, posting.ID, applicationstatus.Submitted); err != nil {
		t.Fatalf("application.UpdateStatus: %v", err)
	}
	if _, err := updateApplicationStatus(ctx, s, posting.ID, applicationstatus.Started); err != nil {
		t.Fatalf("application.UpdateStatus: %v", err)
	}
	if _, _, err := closePosting(ctx, s, posting.ID, []applicationstatus.Status{applicationstatus.Started}); err != nil {
		t.Fatalf("ClosePosting: %v", err)
	}

	history, err := application.ListStatusHistory(ctx, s.DB(), app.ID)
	if err != nil {
		t.Fatalf("application.ListStatusHistory: %v", err)
	}
	type change struct {
		Status    applicationstatus.Status
		ChangedBy application.ChangedBy
	}
	got := make([]change, len(history))
	for i, h := range history {
		got[i] = change{h.Status, h.ChangedBy}
	}
	want := []change{
		{applicationstatus.Started, application.ChangedByUser},
		{applicationstatus.Submitted, application.ChangedByUser},
		{applicationstatus.Started, application.ChangedByUser},
		{applicationstatus.PostingClosed, application.ChangedBySync},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("history mismatch (-want +got):\n%s", diff)
	}
}

func TestListApplicationStatusHistory_UnknownChangedBy_ReturnsError(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	app := mustCreateApplication(t, s, posting.ID)
	if _, err := s.sqlDB.ExecContext(ctx, `UPDATE application_status_history SET changed_by = 'robot'`); err != nil {
		t.Fatalf("set changed_by: %v", err)
	}

	if _, err := application.ListStatusHistory(ctx, s.DB(), app.ID); err == nil {
		t.Error("application.ListStatusHistory with changed_by 'robot': want an error, got nil")
	}
}
