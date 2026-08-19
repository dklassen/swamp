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
