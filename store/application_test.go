package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func mustCreateApplication(t *testing.T, s *Store, postingID int64) Application {
	t.Helper()
	a, err := s.CreateApplication(context.Background(), postingID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	return a
}

func TestCreateApplication_ThenGet_ReturnsSameApplication(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")

	created, err := s.CreateApplication(ctx, posting.ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if created.Status != ApplicationStatusStarted {
		t.Fatalf("Status = %s, want %s", created.Status, ApplicationStatusStarted)
	}

	got, err := s.GetApplication(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if diff := cmp.Diff(created, got); diff != "" {
		t.Fatalf("GetApplication mismatch (-created +got):\n%s", diff)
	}
}

func TestGetApplication_NonexistentPostingID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetApplication(ctx, 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetApplication error = %v, want ErrNotFound", err)
	}
}

func TestUpdateApplicationStatus_UpdatesStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	mustCreateApplication(t, s, posting.ID)

	updated, err := s.UpdateApplicationStatus(ctx, posting.ID, ApplicationStatusInterviewing)
	if err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}
	if updated.Status != ApplicationStatusInterviewing {
		t.Fatalf("Status = %s, want %s", updated.Status, ApplicationStatusInterviewing)
	}

	got, err := s.GetApplication(ctx, posting.ID)
	if err != nil {
		t.Fatalf("GetApplication: %v", err)
	}
	if diff := cmp.Diff(updated, got); diff != "" {
		t.Fatalf("GetApplication mismatch (-updated +got):\n%s", diff)
	}
}

func TestUpdateApplicationStatus_NonexistentPostingID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.UpdateApplicationStatus(ctx, 999, ApplicationStatusInterviewing)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateApplicationStatus error = %v, want ErrNotFound", err)
	}
}

// TestGetApplication_NullStatusInDB_FailsLoudly verifies that a row with
// an actual NULL status (only reachable via something outside this
// package writing to the table directly, since store's own writes always
// supply a concrete status -- see applicationFromRow) surfaces as an
// error rather than silently coercing to some default status.
func TestGetApplication_NullStatusInDB_FailsLoudly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	if _, err := s.sqlDB.ExecContext(ctx, `INSERT INTO applications (posting_id) VALUES (?)`, posting.ID); err != nil {
		t.Fatalf("insert application with NULL status: %v", err)
	}

	if _, err := s.GetApplication(ctx, posting.ID); err == nil {
		t.Fatal("GetApplication with NULL status in DB = nil error, want an error")
	}
}

func TestUpdateApplicationNotes_UpdatesNotes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	mustCreateApplication(t, s, posting.ID)

	updated, err := s.UpdateApplicationNotes(ctx, posting.ID, "Follow up next week")
	if err != nil {
		t.Fatalf("UpdateApplicationNotes: %v", err)
	}
	if updated.Notes != "Follow up next week" {
		t.Fatalf("Notes = %q, want %q", updated.Notes, "Follow up next week")
	}
}

func TestGetApplicationByID_ReturnsTheApplication(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	created := mustCreateApplication(t, s, posting.ID)

	got, err := s.GetApplicationByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetApplicationByID: %v", err)
	}
	if diff := cmp.Diff(created, got); diff != "" {
		t.Fatalf("GetApplicationByID mismatch (-created +got):\n%s", diff)
	}
}

// TestGetApplicationByID_NonexistentID_ReturnsErrNotFound is the whole
// point of this lookup existing: callers holding only an application ID
// (`swamp export <application-id>`) need to tell an ID that names no row
// from one whose application simply has nothing drafted yet.
func TestGetApplicationByID_NonexistentID_ReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetApplicationByID(ctx, 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetApplicationByID error = %v, want ErrNotFound", err)
	}
}
