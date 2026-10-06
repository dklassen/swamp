package store

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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
		Old:    `{"status":"application_started","notes":"","deleted_at":null}`,
		New:    `{"status":"interviewing","notes":"","deleted_at":null}`,
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
