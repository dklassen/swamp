package sync

import (
	"context"
	"errors"
	"testing"

	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

func TestAddCompany_NewValidBoard_CreatesCompanyWithDescription(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	fetcher := &perBoardFetcher{
		postings: map[string][]jobboard.Posting{
			"acme": {
				samplePosting("job-1", "Engineer", "Engineering", "Remote"),
				samplePosting("job-2", "Designer", "Design", "Remote"),
			},
		},
	}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})

	const description = "Acme builds rockets for roadrunner enthusiasts."
	got, err := syncer.AddCompany(ctx, "Acme", "ashby", "acme", description)
	if err != nil {
		t.Fatalf("AddCompany: %v", err)
	}
	if got.Outcome != AddCompanyCreated {
		t.Errorf("Outcome = %v, want AddCompanyCreated", got.Outcome)
	}
	if got.OpenJobs != 2 {
		t.Errorf("OpenJobs = %d, want 2", got.OpenJobs)
	}

	stored, err := s.GetCompany(ctx, got.Company.ID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	if stored.Name != "Acme" || stored.Source != "ashby" || stored.SourceRef != "acme" {
		t.Errorf("stored company = %q %s/%s, want Acme ashby/acme", stored.Name, stored.Source, stored.SourceRef)
	}
	if stored.Description != description {
		t.Errorf("stored Description = %q, want %q", stored.Description, description)
	}

	postings, err := s.ListPostingsByCompany(ctx, stored.ID)
	if err != nil {
		t.Fatalf("ListPostingsByCompany: %v", err)
	}
	if len(postings) != 0 {
		t.Errorf("got %d postings, want 0 (adding a company doesn't sync it)", len(postings))
	}
}

func TestAddCompany_BoardRejectsSlug_ReturnsErrorAndCreatesNothing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	fetcher := &perBoardFetcher{errBoards: map[string]error{"nope": errors.New("404 not found")}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})

	if _, err := syncer.AddCompany(ctx, "Nope", "ashby", "nope", "whatever"); err == nil {
		t.Fatal("AddCompany error = nil, want an error for a slug the board rejects")
	}

	companies, err := s.ListActiveCompanies(ctx)
	if err != nil {
		t.Fatalf("ListActiveCompanies: %v", err)
	}
	if len(companies) != 0 {
		t.Fatalf("got %d companies, want 0", len(companies))
	}
}

// Deleting a company is the user's decision; an agent re-discovering it
// mustn't quietly bring it back (unlike CreateCompany, which restores).
func TestAddCompany_PreviouslyDeletedCompany_SkippedAndStaysDeleted(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	if err := s.SoftDeleteCompany(ctx, acme.ID); err != nil {
		t.Fatalf("SoftDeleteCompany: %v", err)
	}
	fetcher := &perBoardFetcher{postings: map[string][]jobboard.Posting{"acme": nil}}
	syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})

	got, err := syncer.AddCompany(ctx, "Acme", "ashby", "acme", "new description")
	if err != nil {
		t.Fatalf("AddCompany: %v", err)
	}
	if got.Outcome != AddCompanySkippedDeleted {
		t.Errorf("Outcome = %v, want AddCompanySkippedDeleted", got.Outcome)
	}
	if got.Company.ID != acme.ID {
		t.Errorf("Company.ID = %d, want the deleted company's id %d", got.Company.ID, acme.ID)
	}

	if _, err := s.GetCompany(ctx, acme.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetCompany after AddCompany error = %v, want ErrNotFound (still deleted)", err)
	}
}

// Re-adding an active company reports it rather than duplicating it. The
// description is filled in if missing (e.g. a retry after a failed
// description write) but never overwrites one that's already there.
func TestAddCompany_ActiveCompanyAlreadyExists_FillsOnlyMissingDescription(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, existing, want string
	}{
		{"no description yet", "", "new description"},
		{"has a description", "original description", "original description"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := newTestStore(t)
			ctx := context.Background()

			acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
			if tc.existing != "" {
				if _, err := s.UpdateCompanyDescription(ctx, acme.ID, tc.existing); err != nil {
					t.Fatalf("UpdateCompanyDescription: %v", err)
				}
			}
			fetcher := &perBoardFetcher{postings: map[string][]jobboard.Posting{"acme": nil}}
			syncer := New(s, map[string]PostingFetcher{"ashby": fetcher})

			got, err := syncer.AddCompany(ctx, "Acme Renamed", "ashby", "acme", "new description")
			if err != nil {
				t.Fatalf("AddCompany: %v", err)
			}
			if got.Outcome != AddCompanyAlreadyExists {
				t.Errorf("Outcome = %v, want AddCompanyAlreadyExists", got.Outcome)
			}

			stored, err := s.GetCompany(ctx, acme.ID)
			if err != nil {
				t.Fatalf("GetCompany: %v", err)
			}
			if stored.Description != tc.want {
				t.Errorf("Description = %q, want %q", stored.Description, tc.want)
			}
			if stored.Name != "Acme" {
				t.Errorf("Name = %q, want unchanged %q", stored.Name, "Acme")
			}
			companies, err := s.ListActiveCompanies(ctx)
			if err != nil {
				t.Fatalf("ListActiveCompanies: %v", err)
			}
			if len(companies) != 1 {
				t.Errorf("got %d companies, want 1 (no duplicate)", len(companies))
			}
		})
	}
}
