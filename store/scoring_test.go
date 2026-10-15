package store

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
)

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

// insertScore records a score on p's current content directly: store's
// tests can't import scoring, which imports store.
func insertScore(t *testing.T, s *Store, p Posting, score int, reason, profileSHA256 string) {
	t.Helper()
	if _, err := s.sqlDB.Exec(`INSERT INTO posting_scores (posting_id, score, reason, content_sha256, profile_sha256, model) VALUES (?, ?, ?, ?, ?, 'model')`, p.ID, score, reason, p.ContentSHA256, profileSHA256); err != nil {
		t.Fatalf("insert score: %v", err)
	}
}

func TestSearchPostings_RowsCarryTheNewestScore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	scored := mustUpsertPostingIn(t, s, acme.ID, "1", "Scored twice", "")
	unscored := mustUpsertPostingIn(t, s, acme.ID, "2", "Unscored", "")
	insertScore(t, s, scored, 2, "first look", "profile")
	insertScore(t, s, scored, 5, "after the profile changed", "edited profile")

	type score struct {
		Score  int
		Reason string
	}
	want := map[int64]score{scored.ID: {5, "after the profile changed"}, unscored.ID: {}}
	for _, order := range PostingOrders() {
		page, err := s.SearchPostings(ctx, PostingSearch{Order: order, Limit: 10})
		if err != nil {
			t.Fatalf("%s: SearchPostings: %v", order, err)
		}
		got := map[int64]score{}
		for _, l := range page.Listings {
			got[l.ID] = score{l.Score, l.ScoreReason}
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("%s: scores (-want +got):\n%s", order, diff)
		}
	}
}

// A score given for content that has changed since is stale: the view dims it.
func TestSearchPostings_RowsSayWhenTheScoreIsStale(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	kept := mustUpsertPostingIn(t, s, acme.ID, "1", "Unchanged", "")
	rewritten := mustUpsertPostingIn(t, s, acme.ID, "2", "Rewritten", "")
	insertScore(t, s, kept, 4, "ok", "p")
	insertScore(t, s, rewritten, 4, "ok", "p")
	if _, err := s.UpsertPosting(ctx, CreatePostingParams{CompanyID: acme.ID, Source: "ashby", SourceID: "2",
		IngestedFields: IngestedFields{Title: "Rewritten", DescriptionText: "New text.", RawPayload: "{}"}}); err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}

	for _, order := range PostingOrders() {
		page, err := s.SearchPostings(ctx, PostingSearch{Order: order, Limit: 10})
		if err != nil {
			t.Fatalf("%s: SearchPostings: %v", order, err)
		}
		got := map[int64]bool{}
		for _, l := range page.Listings {
			got[l.ID] = l.ScoreStale
		}
		if diff := cmp.Diff(map[int64]bool{kept.ID: false, rewritten.ID: true}, got); diff != "" {
			t.Errorf("%s: stale scores (-want +got):\n%s", order, diff)
		}
	}
}

func TestSearchPostings_ScoreDesc(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	score := func(p Posting, value int, profile string) {
		t.Helper()
		insertScore(t, s, p, value, "r", profile)
	}
	fiveOld := mustUpsertPostingIn(t, s, acme.ID, "1", "Five, older", "")
	three := mustUpsertPostingIn(t, s, acme.ID, "2", "Three", "")
	unscoredOld := mustUpsertPostingIn(t, s, acme.ID, "3", "Unscored, older", "")
	fiveNew := mustUpsertPostingIn(t, s, acme.ID, "4", "Five, newer", "")
	unscoredNew := mustUpsertPostingIn(t, s, acme.ID, "5", "Unscored, newer", "")
	rescored := mustUpsertPostingIn(t, s, acme.ID, "6", "Three, then one", "")
	score(fiveOld, 5, "p")
	score(three, 3, "p")
	score(fiveNew, 5, "p")
	score(rescored, 3, "p")
	score(rescored, 1, "edited p")
	want := []int64{fiveNew.ID, fiveOld.ID, three.ID, rescored.ID, unscoredNew.ID, unscoredOld.ID}

	for limit := 1; limit <= len(want); limit++ {
		search := PostingSearch{Order: PostingOrderScoreDesc, Limit: limit}
		var got []int64
		for pages := 0; ; pages++ {
			if pages > len(want) {
				t.Fatalf("limit %d: paging didn't end", limit)
			}
			page, err := s.SearchPostings(ctx, search)
			if err != nil {
				t.Fatalf("limit %d: SearchPostings: %v", limit, err)
			}
			got = append(got, listingIDs(page.Listings)...)
			if !page.HasMore {
				break
			}
			last := page.Listings[len(page.Listings)-1]
			search.AfterID, search.AfterScore = last.ID, last.Score
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("limit %d: order (-want +got):\n%s", limit, diff)
		}
	}
}
