package store

import (
	"context"
	"github.com/dklassen/swamp/filter"
	"github.com/google/go-cmp/cmp"
	"testing"

	"github.com/dklassen/swamp/applicationstatus"
)

func TestListInterestedPostings_WithNoApplication_LeavesApplicationFieldsNil(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	if _, err := s.setPostingInterested(ctx, posting.ID); err != nil {
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
	if _, err := s.setPostingInterested(ctx, posting.ID); err != nil {
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
	if p.ApplicationStatus == nil || *p.ApplicationStatus != applicationstatus.Started {
		t.Fatalf("ApplicationStatus = %v, want %s", p.ApplicationStatus, applicationstatus.Started)
	}
}

func TestListInterestedPostings_ExcludesArchived(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Software Engineer")
	if _, err := s.setPostingInterested(ctx, posting.ID); err != nil {
		t.Fatalf("SetPostingInterested: %v", err)
	}
	// Sets archived_at directly, bypassing postingmarkup.SetArchived (which would
	// also clear interested_at): this guards ListInterestedPostings' WHERE
	// clause itself, which must exclude archived rows regardless of
	// interested_at, not just the specific way the TUI reaches "archived"
	// today (#58).
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

// Filters are the authority on what's shown and read, so an interested
// posting the company's list hides isn't listed either.
func TestListInterestedPostings_ExcludesWhatTheCompanyListHides(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		hide func(t *testing.T, s *Store, company Company, posting Posting)
	}{
		{"closed posting", func(t *testing.T, s *Store, _ Company, posting Posting) {
			if err := s.MarkPostingClosed(context.Background(), posting.ID); err != nil {
				t.Fatalf("MarkPostingClosed: %v", err)
			}
		}},
		{"deleted company", func(t *testing.T, s *Store, company Company, _ Posting) {
			if _, err := s.sqlDB.Exec(`UPDATE companies SET deleted_at = CURRENT_TIMESTAMP WHERE id = ?`, company.ID); err != nil {
				t.Fatalf("delete company: %v", err)
			}
		}},
		{"filtered out", func(t *testing.T, s *Store, company Company, _ Posting) {
			if _, err := s.createCompanyFilter(context.Background(), company.ID, filter.FieldDepartment, "Sales"); err != nil {
				t.Fatalf("createCompanyFilter: %v", err)
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			s := newTestStore(t)
			acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
			hidden := mustUpsertPostingIn(t, s, acme.ID, "job-1", "Engineer", "Engineering")
			if _, err := s.setPostingInterested(ctx, hidden.ID); err != nil {
				t.Fatalf("setPostingInterested: %v", err)
			}
			other := mustCreateCompany(t, s, "Globex", "ashby", "globex")
			shown := mustUpsertPostingIn(t, s, other.ID, "job-2", "Engineer", "Engineering")
			if _, err := s.setPostingInterested(ctx, shown.ID); err != nil {
				t.Fatalf("setPostingInterested: %v", err)
			}
			tt.hide(t, s, acme, hidden)

			got, err := s.ListInterestedPostings(ctx)
			if err != nil {
				t.Fatalf("ListInterestedPostings: %v", err)
			}
			var ids []int64
			for _, p := range got {
				ids = append(ids, p.Posting.ID)
			}
			if diff := cmp.Diff([]int64{shown.ID}, ids); diff != "" {
				t.Errorf("listed postings (-want +got):\n%s", diff)
			}
		})
	}
}
