package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/filter"
)

// mustInsertScore records a score directly, for a fixture's history.
func mustInsertScore(t *testing.T, s *Store, postingID int64, contentSHA256, profileSHA256 string) {
	t.Helper()
	if _, err := s.sqlDB.Exec(`INSERT INTO posting_scores (posting_id, score, reason, content_sha256, profile_sha256, model) VALUES (?, 3, 'fixture', ?, ?, 'model')`, postingID, contentSHA256, profileSHA256); err != nil {
		t.Fatalf("insert score: %v", err)
	}
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

// pendingReasons is every pending posting, by ID, with why.
func pendingReasons(t *testing.T, s *Store, rescoreProfile string) (map[int64]PendingReason, int) {
	t.Helper()
	page, err := s.PendingPostings(context.Background(), rescoreProfile, 0, 100)
	if err != nil {
		t.Fatalf("PendingPostings: %v", err)
	}
	got := map[int64]PendingReason{}
	for _, p := range page.Postings {
		got[p.ID] = p.Reason
	}
	return got, page.Total
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
	shown := mustUpsertPostingIn(t, s, globex.ID, "4", "Shown by the filter", "Engineering")
	mustUpsertPostingIn(t, s, globex.ID, "5", "Hidden by the filter", "Sales")
	closed := mustUpsertPostingIn(t, s, acme.ID, "6", "Closed", "")
	archived := mustUpsertPostingIn(t, s, acme.ID, "7", "Archived", "")
	interested := mustUpsertPostingIn(t, s, acme.ID, "8", "Interested", "")
	applied := mustUpsertPostingIn(t, s, acme.ID, "9", "Applied", "")
	mustUpsertPostingIn(t, s, gone.ID, "10", "At a deleted company", "")
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

	mustInsertScore(t, s, changed.ID, changed.ContentSHA256, "old profile")
	mustInsertScore(t, s, current.ID, current.ContentSHA256, "old profile")
	if _, err := s.UpsertPosting(ctx, CreatePostingParams{CompanyID: acme.ID, Source: "ashby", SourceID: "2",
		IngestedFields: IngestedFields{Title: "Changed", DescriptionText: "Rewritten.", RawPayload: "{}"}}); err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}

	tests := []struct {
		name           string
		rescoreProfile string
		want           map[int64]PendingReason
	}{
		{"default: no score on the current content", "", map[int64]PendingReason{fresh.ID: PendingNew, changed.ID: PendingChanged, shown.ID: PendingNew}},
		{"rescore against a new profile", "new profile", map[int64]PendingReason{fresh.ID: PendingNew, changed.ID: PendingChanged, current.ID: PendingRescore, shown.ID: PendingNew}},
		{"rescore against the profile already used", "old profile", map[int64]PendingReason{fresh.ID: PendingNew, changed.ID: PendingChanged, shown.ID: PendingNew}},
	}
	for _, tt := range tests {
		got, total := pendingReasons(t, s, tt.rescoreProfile)
		if diff := cmp.Diff(tt.want, got); diff != "" {
			t.Errorf("%s: pending postings and why (-want +got):\n%s", tt.name, diff)
		}
		if total != len(tt.want) {
			t.Errorf("%s: Total = %d, want %d", tt.name, total, len(tt.want))
		}
	}
}

// A rescore needs no session to resume: what it has done has scores
// against the new profile.
func TestPendingPostings_ARescoreResumes(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	done := mustUpsertPostingIn(t, s, acme.ID, "1", "Rescored", "")
	left := mustUpsertPostingIn(t, s, acme.ID, "2", "Not yet", "")
	mustInsertScore(t, s, done.ID, done.ContentSHA256, "old profile")
	mustInsertScore(t, s, left.ID, left.ContentSHA256, "old profile")

	mustInsertScore(t, s, done.ID, done.ContentSHA256, "new profile")

	got, _ := pendingReasons(t, s, "new profile")
	if diff := cmp.Diff(map[int64]PendingReason{left.ID: PendingRescore}, got); diff != "" {
		t.Errorf("rescore pending (-want +got):\n%s", diff)
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

	var got [][]int64
	var hasMore []bool
	var after int64
	for range 3 {
		page, err := s.PendingPostings(ctx, "", after, 2)
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

func TestRecordScores_RecordsValidEntriesAndRefusesTheRest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	good := mustUpsertPostingIn(t, s, acme.ID, "1", "Good", "")
	stale := mustUpsertPostingIn(t, s, acme.ID, "2", "Stale", "")
	other := mustUpsertPostingIn(t, s, acme.ID, "3", "Other", "")
	repeat := mustUpsertPostingIn(t, s, acme.ID, "4", "Already scored", "")
	provenance := ScoreProvenance{Model: "claude-opus-5-5", ClientName: "claude-code", ClientVersion: "2.4.1", ProfileSHA256: "profile"}
	if _, err := s.RecordScores(ctx, provenance, []ScoreEntry{{PostingID: repeat.ID, Score: 2, Reason: "first", ContentSHA256: repeat.ContentSHA256}}); err != nil {
		t.Fatalf("RecordScores: %v", err)
	}

	result, err := s.RecordScores(ctx, provenance, []ScoreEntry{
		{PostingID: good.ID, Score: 4, Reason: "Go, remote", ContentSHA256: good.ContentSHA256},
		{PostingID: stale.ID, Score: 3, Reason: "ok", ContentSHA256: "an older hash"},
		{PostingID: other.ID, Score: 6, Reason: "too high", ContentSHA256: other.ContentSHA256},
		{PostingID: other.ID, Score: 0, Reason: "too low", ContentSHA256: other.ContentSHA256},
		{PostingID: other.ID, Score: 2, Reason: "", ContentSHA256: other.ContentSHA256},
		{PostingID: 999, Score: 2, Reason: "no such posting", ContentSHA256: "x"},
		{PostingID: repeat.ID, Score: 2, Reason: "again", ContentSHA256: repeat.ContentSHA256},
		{PostingID: good.ID, Score: 1, Reason: "twice in one batch", ContentSHA256: good.ContentSHA256},
	})
	if err != nil {
		t.Fatalf("RecordScores: %v", err)
	}

	if diff := cmp.Diff([]int64{good.ID}, result.Recorded); diff != "" {
		t.Errorf("recorded (-want +got):\n%s", diff)
	}
	want := []RefusedScore{
		{PostingID: stale.ID, Why: ScoreContentChanged},
		{PostingID: other.ID, Why: ScoreOutOfRange},
		{PostingID: other.ID, Why: ScoreOutOfRange},
		{PostingID: other.ID, Why: ScoreNoReason},
		{PostingID: 999, Why: ScoreNoSuchPosting},
		{PostingID: repeat.ID, Why: ScoreRepeat},
		{PostingID: good.ID, Why: ScoreRepeat},
	}
	if diff := cmp.Diff(want, result.Refused); diff != "" {
		t.Errorf("refused (-want +got):\n%s", diff)
	}

	var model, client, version, profile string
	if err := s.sqlDB.QueryRow(`SELECT model, client_name, client_version, profile_sha256 FROM posting_scores WHERE posting_id = ?`, good.ID).Scan(&model, &client, &version, &profile); err != nil {
		t.Fatalf("read score: %v", err)
	}
	if got := (ScoreProvenance{Model: model, ClientName: client, ClientVersion: version, ProfileSHA256: profile}); got != provenance {
		t.Errorf("stored provenance %+v, want %+v", got, provenance)
	}
}

// The same content scored by another model, or against another profile,
// isn't a repeat.
func TestRecordScores_AnotherModelOrProfileIsntARepeat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	p := mustUpsertPostingIn(t, s, acme.ID, "1", "Role", "")
	entry := []ScoreEntry{{PostingID: p.ID, Score: 3, Reason: "ok", ContentSHA256: p.ContentSHA256}}

	for _, provenance := range []ScoreProvenance{
		{Model: "claude-opus-5-5", ProfileSHA256: "profile"},
		{Model: "claude-sonnet-5", ProfileSHA256: "profile"},
		{Model: "claude-opus-5-5", ProfileSHA256: "edited profile"},
	} {
		result, err := s.RecordScores(ctx, provenance, entry)
		if err != nil {
			t.Fatalf("RecordScores: %v", err)
		}
		if len(result.Recorded) != 1 {
			t.Errorf("%+v: refused %+v, want it recorded", provenance, result.Refused)
		}
	}
}

func TestRecordScores_RefusesACallWithNoModelOrProfile(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	p := mustUpsertPostingIn(t, s, acme.ID, "1", "Role", "")
	entry := []ScoreEntry{{PostingID: p.ID, Score: 3, Reason: "ok", ContentSHA256: p.ContentSHA256}}

	for _, provenance := range []ScoreProvenance{{ProfileSHA256: "profile"}, {Model: "claude-opus-5-5"}} {
		if _, err := s.RecordScores(context.Background(), provenance, entry); err == nil {
			t.Errorf("%+v: RecordScores succeeded, want it refused", provenance)
		}
	}
	var scores int
	if err := s.sqlDB.QueryRow(`SELECT count(*) FROM posting_scores`).Scan(&scores); err != nil {
		t.Fatalf("count scores: %v", err)
	}
	if scores != 0 {
		t.Errorf("%d scores stored, want 0", scores)
	}
}

func TestSearchPostings_RowsCarryTheNewestScore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	scored := mustUpsertPostingIn(t, s, acme.ID, "1", "Scored twice", "")
	unscored := mustUpsertPostingIn(t, s, acme.ID, "2", "Unscored", "")
	for _, e := range []struct {
		profile string
		entry   ScoreEntry
	}{
		{"profile", ScoreEntry{PostingID: scored.ID, Score: 2, Reason: "first look", ContentSHA256: scored.ContentSHA256}},
		{"edited profile", ScoreEntry{PostingID: scored.ID, Score: 5, Reason: "after the profile changed", ContentSHA256: scored.ContentSHA256}},
	} {
		if _, err := s.RecordScores(ctx, ScoreProvenance{Model: "claude-opus-5-5", ProfileSHA256: e.profile}, []ScoreEntry{e.entry}); err != nil {
			t.Fatalf("RecordScores: %v", err)
		}
	}

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
