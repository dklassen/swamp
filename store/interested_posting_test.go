package store

import (
	"context"
	"testing"
)

func TestListInterestedPostings_WithNoApplication_LeavesApplicationFieldsNil(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	if _, err := s.SetPostingInterested(ctx, posting.ID); err != nil {
		t.Fatalf("SetPostingInterested: %v", err)
	}

	got, err := s.ListInterestedPostings(ctx)
	if err != nil {
		t.Fatalf("ListInterestedPostings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}

	p := got[0]
	if p.CompanyName != "Acme" {
		t.Fatalf("CompanyName = %q, want %q", p.CompanyName, "Acme")
	}
	if p.Posting.Title != "Software Engineer" {
		t.Fatalf("Posting.Title = %q, want %q", p.Posting.Title, "Software Engineer")
	}
	if p.Posting.ID != posting.ID {
		t.Fatalf("Posting.ID = %d, want %d", p.Posting.ID, posting.ID)
	}
	if p.ApplicationID != nil {
		t.Fatalf("ApplicationID = %v, want nil (no application started)", p.ApplicationID)
	}
	if p.ApplicationStatus != nil {
		t.Fatalf("ApplicationStatus = %v, want nil (no application started)", p.ApplicationStatus)
	}
}

func TestListInterestedPostings_WithApplication_IncludesApplicationFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	if _, err := s.SetPostingInterested(ctx, posting.ID); err != nil {
		t.Fatalf("SetPostingInterested: %v", err)
	}
	app := mustCreateApplication(t, s, posting.ID)

	got, err := s.ListInterestedPostings(ctx)
	if err != nil {
		t.Fatalf("ListInterestedPostings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}

	p := got[0]
	if p.ApplicationID == nil || *p.ApplicationID != app.ID {
		t.Fatalf("ApplicationID = %v, want %d", p.ApplicationID, app.ID)
	}
	if p.ApplicationStatus == nil || *p.ApplicationStatus != ApplicationStatusStarted {
		t.Fatalf("ApplicationStatus = %v, want %s", p.ApplicationStatus, ApplicationStatusStarted)
	}
}

func TestListInterestedPostings_ExcludesArchived(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	if _, err := s.SetPostingInterested(ctx, posting.ID); err != nil {
		t.Fatalf("SetPostingInterested: %v", err)
	}
	// Sets archived_at directly, bypassing SetPostingArchived (which would
	// also clear interested_at): this guards ListInterestedPostings' WHERE
	// clause itself, which must exclude archived rows regardless of
	// interested_at, not just the specific way the TUI reaches "archived"
	// today (see decisions.log, #58).
	if _, err := s.sqlDB.ExecContext(ctx, "UPDATE posting_markup SET archived_at = CURRENT_TIMESTAMP WHERE posting_id = ?", posting.ID); err != nil {
		t.Fatalf("set archived_at: %v", err)
	}

	got, err := s.ListInterestedPostings(ctx)
	if err != nil {
		t.Fatalf("ListInterestedPostings: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0 (archived)", len(got))
	}
}
