package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// historyStatuses lists an application's recorded statuses, oldest first.
func historyStatuses(t *testing.T, s *Store, applicationID int64) []ApplicationStatus {
	t.Helper()
	history, err := s.ListApplicationStatusHistory(context.Background(), applicationID)
	if err != nil {
		t.Fatalf("ListApplicationStatusHistory: %v", err)
	}
	statuses := make([]ApplicationStatus, len(history))
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

	application := mustCreateApplication(t, s, posting.ID)

	want := []ApplicationStatus{ApplicationStatusStarted}
	if diff := cmp.Diff(want, historyStatuses(t, s, application.ID)); diff != "" {
		t.Errorf("history mismatch (-want +got):\n%s", diff)
	}
}

func TestUpdateApplicationStatus_RecordsChangeInHistory(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)

	if _, err := s.UpdateApplicationStatus(context.Background(), posting.ID, ApplicationStatusSubmitted); err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}

	want := []ApplicationStatus{ApplicationStatusStarted, ApplicationStatusSubmitted}
	if diff := cmp.Diff(want, historyStatuses(t, s, application.ID)); diff != "" {
		t.Errorf("history mismatch (-want +got):\n%s", diff)
	}
}

// failHistoryInserts makes every later application_status_history insert
// fail, so a test can check that a status write whose history row fails
// commits neither.
func failHistoryInserts(t *testing.T, s *Store) {
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
	application := mustCreateApplication(t, s, posting.ID)
	failHistoryInserts(t, s)

	if _, err := s.UpdateApplicationStatus(ctx, posting.ID, ApplicationStatusSubmitted); err == nil {
		t.Fatal("UpdateApplicationStatus succeeded, want the injected history failure")
	}

	got, err := s.GetApplication(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if diff := cmp.Diff(application, got); diff != "" {
		t.Errorf("application changed despite the failed history write (-before +after):\n%s", diff)
	}
	want := []ApplicationStatus{ApplicationStatusStarted}
	if diff := cmp.Diff(want, historyStatuses(t, s, application.ID)); diff != "" {
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

	if _, err := s.CreateApplication(ctx, posting.ID); err == nil {
		t.Fatal("CreateApplication succeeded, want the injected history failure")
	}

	if _, err := s.GetApplication(ctx, posting.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetApplication error = %v, want ErrNotFound: the application should not exist", err)
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
	application := mustCreateApplication(t, s, posting.ID)

	if _, err := s.UpdateApplicationStatus(context.Background(), posting.ID, ApplicationStatusStarted); err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}

	want := []ApplicationStatus{ApplicationStatusStarted}
	if diff := cmp.Diff(want, historyStatuses(t, s, application.ID)); diff != "" {
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
	application := mustCreateApplication(t, s, posting.ID)
	if _, err := s.UpdateApplicationStatus(ctx, posting.ID, ApplicationStatusSubmitted); err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}

	result, err := s.ClosePosting(ctx, posting.ID, []ApplicationStatus{ApplicationStatusStarted, ApplicationStatusSubmitted})
	if err != nil {
		t.Fatalf("ClosePosting: %v", err)
	}
	if !result.ApplicationClosed {
		t.Fatal("ClosePosting did not close the application")
	}

	want := []ApplicationStatus{ApplicationStatusStarted, ApplicationStatusSubmitted, ApplicationStatusPostingClosed}
	if diff := cmp.Diff(want, historyStatuses(t, s, application.ID)); diff != "" {
		t.Errorf("history mismatch (-want +got):\n%s", diff)
	}
}

func TestClosePosting_HistoryFailure_LeavesPostingAndApplicationOpen(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	application := mustCreateApplication(t, s, posting.ID)
	failHistoryInserts(t, s)

	if _, err := s.ClosePosting(ctx, posting.ID, []ApplicationStatus{ApplicationStatusStarted}); err == nil {
		t.Fatal("ClosePosting succeeded, want the injected history failure")
	}

	after, err := s.GetPosting(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}
	if after.ListingStatus != "open" {
		t.Errorf("ListingStatus = %q, want open", after.ListingStatus)
	}
	got, err := s.GetApplication(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if diff := cmp.Diff(application, got); diff != "" {
		t.Errorf("application changed despite the failed history write (-before +after):\n%s", diff)
	}
}
