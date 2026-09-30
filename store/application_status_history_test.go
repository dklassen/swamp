package store

import (
	"context"
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
