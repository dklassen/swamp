package stage

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/store"
)

// searchFixture is two companies' postings covering every filter
// Search has: applications at two statuses, interested, closed and
// archived postings, and words in each searched field.
func searchFixture(t *testing.T) (*Stage, *store.Store) {
	t.Helper()
	st, s, _ := newTestStage(t)
	ctx := context.Background()
	acme := mustCreateCompany(t, s, "Acme")
	beta := mustCreateCompany(t, s, "Beta")

	posting := func(companyID int64, sourceID, title, department, location string) store.Posting {
		t.Helper()
		p, err := s.UpsertPosting(ctx, store.CreatePostingParams{
			CompanyID: companyID, Source: "ashby", SourceID: sourceID,
			IngestedFields: store.IngestedFields{Title: title, Department: department, Location: location, RawPayload: "{}"},
		})
		if err != nil {
			t.Fatalf("UpsertPosting: %v", err)
		}
		return p
	}
	platform := posting(acme.ID, "1", "Senior Software Engineer, Platform", "Engineering", "Toronto")
	data := posting(acme.ID, "2", "Senior Software Engineer, Data", "Engineering", "Remote")
	posting(acme.ID, "3", "Designer", "Design", "Remote")
	writer := posting(acme.ID, "4", "Writer", "Marketing", "Remote")
	archived := posting(acme.ID, "5", "Archived Engineer", "Engineering", "Remote")
	posting(beta.ID, "6", "Software Engineer", "Engineering", "Vancouver")

	mustMarkInterested(t, s, platform.ID)
	for _, p := range []store.Posting{platform, data} {
		if _, err := s.CreateApplication(ctx, p.ID); err != nil {
			t.Fatalf("CreateApplication: %v", err)
		}
	}
	if _, err := s.UpdateApplicationStatus(ctx, data.ID, store.ApplicationStatusSubmitted); err != nil {
		t.Fatalf("UpdateApplicationStatus: %v", err)
	}
	if err := s.MarkPostingClosed(ctx, writer.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}
	if _, err := s.SetPostingArchived(ctx, archived.ID); err != nil {
		t.Fatalf("SetPostingArchived: %v", err)
	}
	return st, s
}

func TestSearch(t *testing.T) {
	t.Parallel()
	yes, no := true, false

	tests := []struct {
		name      string
		opts      SearchOptions
		wantTitle []string
		wantTotal int
		wantErr   bool
	}{
		{
			name:      "defaults: open, not archived, by company then title",
			wantTitle: []string{"Designer", "Senior Software Engineer, Data", "Senior Software Engineer, Platform", "Software Engineer"},
			wantTotal: 4,
		},
		{
			name:      "every query word must match, across company and title",
			opts:      SearchOptions{Query: "acme platform"},
			wantTitle: []string{"Senior Software Engineer, Platform"},
			wantTotal: 1,
		},
		{
			name:      "query is case-insensitive",
			opts:      SearchOptions{Query: "SOFTWARE engineer"},
			wantTitle: []string{"Senior Software Engineer, Data", "Senior Software Engineer, Platform", "Software Engineer"},
			wantTotal: 3,
		},
		{
			name:      "query matches location and department",
			opts:      SearchOptions{Query: "toronto engineering"},
			wantTitle: []string{"Senior Software Engineer, Platform"},
			wantTotal: 1,
		},
		{
			name:      "company, ignoring case",
			opts:      SearchOptions{Company: "beta"},
			wantTitle: []string{"Software Engineer"},
			wantTotal: 1,
		},
		{
			name:      "with an application",
			opts:      SearchOptions{HasApplication: &yes},
			wantTitle: []string{"Senior Software Engineer, Data", "Senior Software Engineer, Platform"},
			wantTotal: 2,
		},
		{
			name:      "without an application",
			opts:      SearchOptions{HasApplication: &no},
			wantTitle: []string{"Designer", "Software Engineer"},
			wantTotal: 2,
		},
		{
			name:      "application statuses",
			opts:      SearchOptions{ApplicationStatuses: []store.ApplicationStatus{store.ApplicationStatusSubmitted}},
			wantTitle: []string{"Senior Software Engineer, Data"},
			wantTotal: 1,
		},
		{
			name:      "interested",
			opts:      SearchOptions{Interested: &yes},
			wantTitle: []string{"Senior Software Engineer, Platform"},
			wantTotal: 1,
		},
		{
			name:      "closed postings",
			opts:      SearchOptions{ListingStatus: "closed"},
			wantTitle: []string{"Writer"},
			wantTotal: 1,
		},
		{
			name:      "any listing status",
			opts:      SearchOptions{ListingStatus: "any"},
			wantTitle: []string{"Designer", "Senior Software Engineer, Data", "Senior Software Engineer, Platform", "Writer", "Software Engineer"},
			wantTotal: 5,
		},
		{
			name:      "archived included",
			opts:      SearchOptions{IncludeArchived: true},
			wantTitle: []string{"Archived Engineer", "Designer", "Senior Software Engineer, Data", "Senior Software Engineer, Platform", "Software Engineer"},
			wantTotal: 5,
		},
		{
			name:      "limit caps results; total counts every match",
			opts:      SearchOptions{Limit: 2},
			wantTitle: []string{"Designer", "Senior Software Engineer, Data"},
			wantTotal: 4,
		},
		{
			name:    "unknown listing status",
			opts:    SearchOptions{ListingStatus: "pending"},
			wantErr: true,
		},
	}
	st, _ := searchFixture(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := st.Search(context.Background(), tt.opts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Search error = %v, want error: %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			titles := make([]string, len(got.Postings))
			for i, match := range got.Postings {
				titles[i] = match.Posting.Title
			}
			if diff := cmp.Diff(tt.wantTitle, titles); diff != "" {
				t.Errorf("titles mismatch (-want +got):\n%s", diff)
			}
			if got.Total != tt.wantTotal {
				t.Errorf("Total = %d, want %d", got.Total, tt.wantTotal)
			}
		})
	}
}
