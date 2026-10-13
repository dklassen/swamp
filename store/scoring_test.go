package store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/filter"
)

func TestParseScoringScope(t *testing.T) {
	t.Parallel()

	for _, scope := range ScoringScopes() {
		got, err := ParseScoringScope(scope.String())
		if err != nil || got != scope {
			t.Errorf("ParseScoringScope(%q) = %v, %v; want %v", scope, got, err, scope)
		}
	}
	for _, name := range []string{"unscored", "all"} {
		if _, err := ParseScoringScope(name); err != nil {
			t.Errorf("ParseScoringScope(%q): %v", name, err)
		}
	}
	for _, name := range []string{"", "ALL", "rescore"} {
		if _, err := ParseScoringScope(name); err == nil {
			t.Errorf("ParseScoringScope(%q) succeeded, want it refused", name)
		}
	}
}

// mustInsertScore records a score directly, for a fixture's history.
func mustInsertScore(t *testing.T, s *Store, runID, postingID int64, contentSHA256 string) {
	t.Helper()
	if _, err := s.sqlDB.Exec(`INSERT INTO posting_scores (run_id, posting_id, score, reason, content_sha256) VALUES (?, ?, 50, 'fixture', ?)`, runID, postingID, contentSHA256); err != nil {
		t.Fatalf("insert score: %v", err)
	}
}

func mustStartScoringRun(t *testing.T, s *Store, scope ScoringScope) ScoringRun {
	t.Helper()
	run, err := s.StartScoringRun(context.Background(), StartScoringRunParams{Model: "claude-opus-5-5", ProfileSHA256: "profile", Scope: scope})
	if err != nil {
		t.Fatalf("StartScoringRun: %v", err)
	}
	return run
}

// mustUpsertPostingIn is mustUpsertPosting with a department.
func mustUpsertPostingIn(t *testing.T, s *Store, companyID int64, sourceID, title, department string) Posting {
	t.Helper()
	p, err := s.UpsertPosting(context.Background(), CreatePostingParams{
		CompanyID: companyID, Source: "ashby", SourceID: sourceID,
		IngestedFields: IngestedFields{Title: title, Department: department, RawPayload: "{}"},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	return p
}

func TestPendingPostings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	globex := mustCreateCompany(t, s, "Globex", "ashby", "globex")
	gone := mustCreateCompany(t, s, "Gone", "ashby", "gone")
	if _, err := s.CreateCompanyFilter(ctx, globex.ID, filter.FieldDepartment, "Engineering"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}

	fresh := mustUpsertPostingIn(t, s, acme.ID, "1", "Fresh", "")
	changed := mustUpsertPostingIn(t, s, acme.ID, "2", "Changed", "")
	current := mustUpsertPostingIn(t, s, acme.ID, "3", "Current", "")
	scoredInRun := mustUpsertPostingIn(t, s, acme.ID, "4", "Scored in this run", "")
	shown := mustUpsertPostingIn(t, s, globex.ID, "5", "Shown by the filter", "Engineering")
	mustUpsertPostingIn(t, s, globex.ID, "6", "Hidden by the filter", "Sales")
	closed := mustUpsertPostingIn(t, s, acme.ID, "7", "Closed", "")
	archived := mustUpsertPostingIn(t, s, acme.ID, "8", "Archived", "")
	interested := mustUpsertPostingIn(t, s, acme.ID, "9", "Interested", "")
	applied := mustUpsertPostingIn(t, s, acme.ID, "10", "Applied", "")
	mustUpsertPostingIn(t, s, gone.ID, "11", "At a deleted company", "")
	if err := s.MarkPostingClosed(ctx, closed.ID); err != nil {
		t.Fatalf("MarkPostingClosed: %v", err)
	}
	if _, err := s.SetPostingArchived(ctx, archived.ID); err != nil {
		t.Fatalf("SetPostingArchived: %v", err)
	}
	if _, err := s.SetPostingInterested(ctx, interested.ID); err != nil {
		t.Fatalf("SetPostingInterested: %v", err)
	}
	mustCreateApplication(t, s, applied.ID)
	if err := s.SoftDeleteCompany(ctx, gone.ID); err != nil {
		t.Fatalf("SoftDeleteCompany: %v", err)
	}

	earlier := mustStartScoringRun(t, s, ScoringScopeUnscored)
	mustInsertScore(t, s, earlier.ID, changed.ID, changed.ContentSHA256)
	mustInsertScore(t, s, earlier.ID, current.ID, current.ContentSHA256)
	if _, err := s.UpsertPosting(ctx, CreatePostingParams{CompanyID: acme.ID, Source: "ashby", SourceID: "2",
		IngestedFields: IngestedFields{Title: "Changed", DescriptionText: "Rewritten.", RawPayload: "{}"}}); err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}

	tests := []struct {
		scope ScoringScope
		want  map[int64]PendingReason
	}{
		{ScoringScopeUnscored, map[int64]PendingReason{fresh.ID: PendingNew, changed.ID: PendingChanged, shown.ID: PendingNew}},
		{ScoringScopeAll, map[int64]PendingReason{fresh.ID: PendingNew, changed.ID: PendingChanged, current.ID: PendingRescore, shown.ID: PendingNew}},
	}
	for _, tt := range tests {
		t.Run(tt.scope.String(), func(t *testing.T) {
			run := mustStartScoringRun(t, s, tt.scope)
			mustInsertScore(t, s, run.ID, scoredInRun.ID, scoredInRun.ContentSHA256)

			page, err := s.PendingPostings(ctx, run.ID, 0, 100)
			if err != nil {
				t.Fatalf("PendingPostings: %v", err)
			}
			got := map[int64]PendingReason{}
			for _, p := range page.Postings {
				got[p.ID] = p.Reason
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("pending postings and why (-want +got):\n%s", diff)
			}
			if page.Total != len(tt.want) {
				t.Errorf("Total = %d, want %d", page.Total, len(tt.want))
			}
		})
	}

	if _, err := s.PendingPostings(ctx, 999, 0, 100); !errors.Is(err, ErrNotFound) {
		t.Errorf("PendingPostings for no such run: err = %v, want ErrNotFound", err)
	}
}

func TestPendingPostings_Pages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	var ids []int64
	for i := 1; i <= 5; i++ {
		ids = append(ids, mustUpsertPostingIn(t, s, acme.ID, fmt.Sprint(i), fmt.Sprintf("Role %d", i), "").ID)
	}
	run := mustStartScoringRun(t, s, ScoringScopeUnscored)

	var got [][]int64
	var hasMore []bool
	var after int64
	for range 3 {
		page, err := s.PendingPostings(ctx, run.ID, after, 2)
		if err != nil {
			t.Fatalf("PendingPostings: %v", err)
		}
		if page.Total != 5 {
			t.Errorf("Total = %d, want 5", page.Total)
		}
		var pageIDs []int64
		for _, p := range page.Postings {
			pageIDs = append(pageIDs, p.ID)
		}
		got = append(got, pageIDs)
		hasMore = append(hasMore, page.HasMore)
		after = pageIDs[len(pageIDs)-1]
	}
	if diff := cmp.Diff([][]int64{ids[0:2], ids[2:4], ids[4:5]}, got); diff != "" {
		t.Errorf("pages (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]bool{true, true, false}, hasMore); diff != "" {
		t.Errorf("HasMore (-want +got):\n%s", diff)
	}
}
