package service_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/filter"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/internal/testutil"
)

func TestScoringService_Record_RecordsValidEntriesAndRefusesTheRest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	scoring := s.Scores
	acme := testutil.MustAddCompany(t, s, "Acme")
	good := testutil.MustIngestPostingFor(t, s, acme.ID, "1", "Good")
	stale := testutil.MustIngestPostingFor(t, s, acme.ID, "2", "Stale")
	other := testutil.MustIngestPostingFor(t, s, acme.ID, "3", "Other")
	repeat := testutil.MustIngestPostingFor(t, s, acme.ID, "4", "Already scored")
	provenance := score.Provenance{Model: "claude-opus-5-5", ClientName: "claude-code", ClientVersion: "2.4.1", ProfileSHA256: "profile"}
	if _, err := scoring.Record(ctx, provenance, []score.Entry{{PostingID: repeat.ID, Score: 2, Reason: "first", ContentSHA256: repeat.ContentSHA256}}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	result, err := scoring.Record(ctx, provenance, []score.Entry{
		{PostingID: good.ID, Score: 4, Reason: "Go, remote", ContentSHA256: good.ContentSHA256},
		{PostingID: stale.ID, Score: 3, Reason: "ok", ContentSHA256: "an older hash"},
		{PostingID: other.ID, Score: score.Max + 1, Reason: "too high", ContentSHA256: other.ContentSHA256},
		{PostingID: other.ID, Score: score.Min - 1, Reason: "too low", ContentSHA256: other.ContentSHA256},
		{PostingID: other.ID, Score: 2, Reason: "", ContentSHA256: other.ContentSHA256},
		{PostingID: 999, Score: 2, Reason: "no such posting", ContentSHA256: "x"},
		{PostingID: repeat.ID, Score: 2, Reason: "again", ContentSHA256: repeat.ContentSHA256},
		{PostingID: good.ID, Score: 1, Reason: "twice in one batch", ContentSHA256: good.ContentSHA256},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	if diff := cmp.Diff([]int64{good.ID}, result.Recorded); diff != "" {
		t.Errorf("recorded (-want +got):\n%s", diff)
	}
	want := []score.Refused{
		{PostingID: stale.ID, Why: score.ContentChanged},
		{PostingID: other.ID, Why: score.OutOfRange},
		{PostingID: other.ID, Why: score.OutOfRange},
		{PostingID: other.ID, Why: score.NoReason},
		{PostingID: 999, Why: score.NoSuchPosting},
		{PostingID: repeat.ID, Why: score.Repeat},
		{PostingID: good.ID, Why: score.Repeat},
	}
	if diff := cmp.Diff(want, result.Refused); diff != "" {
		t.Errorf("refused (-want +got):\n%s", diff)
	}

	scores, err := scoring.ForPosting(ctx, good.ID)
	if err != nil {
		t.Fatalf("ForPosting: %v", err)
	}
	if len(scores) != 1 || scores[0].Provenance != provenance {
		t.Errorf("stored scores %+v, want one with provenance %+v", scores, provenance)
	}
}

func TestScoringService_Record_AnotherModelOrProfileIsntARepeat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	scoring := s.Scores
	p := testutil.MustIngestPosting(t, s, "1", "Role")
	entry := []score.Entry{{PostingID: p.ID, Score: 3, Reason: "ok", ContentSHA256: p.ContentSHA256}}

	for _, provenance := range []score.Provenance{
		{Model: "claude-opus-5-5", ProfileSHA256: "profile"},
		{Model: "claude-sonnet-5", ProfileSHA256: "profile"},
		{Model: "claude-opus-5-5", ProfileSHA256: "edited profile"},
	} {
		result, err := scoring.Record(ctx, provenance, entry)
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		if len(result.Recorded) != 1 {
			t.Errorf("%+v: refused %+v, want it recorded", provenance, result.Refused)
		}
	}
}

func TestScoringService_Record_RefusesACallWithNoModelOrProfile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	scoring := s.Scores
	p := testutil.MustIngestPosting(t, s, "1", "Role")
	entry := []score.Entry{{PostingID: p.ID, Score: 3, Reason: "ok", ContentSHA256: p.ContentSHA256}}

	for _, provenance := range []score.Provenance{{ProfileSHA256: "profile"}, {Model: "claude-opus-5-5"}} {
		if _, err := scoring.Record(ctx, provenance, entry); err == nil {
			t.Errorf("%+v: Record succeeded, want it refused", provenance)
		}
	}
	scores, err := scoring.ForPosting(ctx, p.ID)
	if err != nil {
		t.Fatalf("ForPosting: %v", err)
	}
	if len(scores) != 0 {
		t.Errorf("stored %+v, want no scores", scores)
	}
}

// pendingReasons is every pending posting, by ID, with why.
func pendingReasons(t *testing.T, s testutil.TestApp, rescoreProfile string) (map[int64]score.PendingReason, int) {
	t.Helper()
	page, err := s.Scores.Pending(context.Background(), rescoreProfile, 0, 100)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	got := map[int64]score.PendingReason{}
	for _, p := range page.Postings {
		got[p.ID] = p.Reason
	}
	return got, page.Total
}

// mustRecordScore scores p's current content against profile.
func mustRecordScore(t *testing.T, s testutil.TestApp, p posting.Posting, profile string) {
	t.Helper()
	result, err := s.Scores.Record(context.Background(), score.Provenance{Model: "model", ProfileSHA256: profile},
		[]score.Entry{{PostingID: p.ID, Score: 3, Reason: "fixture", ContentSHA256: p.ContentSHA256}})
	if err != nil || len(result.Recorded) != 1 {
		t.Fatalf("Record: result %+v, err %v", result, err)
	}
}

// A posting is pending when it's open, unarchived, not interested, has no
// application, is at a live company that doesn't filter it out, and has no
// score on its current content (or, for a rescore, none against the new
// profile).
func TestScoringService_Pending(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	companies := s.Companies
	postings := s.Postings
	acme := testutil.MustAddCompany(t, s, "Acme")
	globex := testutil.MustAddCompany(t, s, "Globex")
	gone := testutil.MustAddCompany(t, s, "Gone")
	if err := companies.SetFilters(ctx, globex.ID, []filter.Filter{{Field: filter.FieldDepartment, Value: "Engineering"}}); err != nil {
		t.Fatalf("SetFilters: %v", err)
	}
	ingestIn := func(companyID int64, sourceID, title, department string) posting.Posting {
		t.Helper()
		ingested, err := postings.Ingest(ctx, companyID, "ashby", sourceID, posting.IngestedFields{Title: title, Department: department, RawPayload: "{}"})
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		return ingested.Posting
	}

	fresh := ingestIn(acme.ID, "1", "Fresh", "")
	changed := ingestIn(acme.ID, "2", "Changed", "")
	current := ingestIn(acme.ID, "3", "Current", "")
	shown := ingestIn(globex.ID, "4", "Shown by the filter", "Engineering")
	ingestIn(globex.ID, "5", "Hidden by the filter", "Sales")
	closed := ingestIn(acme.ID, "6", "Closed", "")
	archived := ingestIn(acme.ID, "7", "Archived", "")
	interested := ingestIn(acme.ID, "8", "Interested", "")
	applied := ingestIn(acme.ID, "9", "Applied", "")
	ingestIn(gone.ID, "10", "At a deleted company", "")
	if _, _, err := postings.Close(ctx, closed.ID, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := postings.SetArchived(ctx, archived.ID); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}
	if _, err := postings.SetInterested(ctx, interested.ID); err != nil {
		t.Fatalf("SetInterested: %v", err)
	}
	testutil.MustApplyTo(t, s, applied.ID)
	if err := s.Companies.Delete(ctx, gone.ID); err != nil {
		t.Fatalf("delete company: %v", err)
	}
	mustRecordScore(t, s, changed, "old profile")
	mustRecordScore(t, s, current, "old profile")
	ingestIn(acme.ID, "2", "Changed", "Rewritten")

	tests := []struct {
		name           string
		rescoreProfile string
		want           map[int64]score.PendingReason
	}{
		{"default: no score on the current content", "", map[int64]score.PendingReason{fresh.ID: score.PendingNew, changed.ID: score.PendingChanged, shown.ID: score.PendingNew}},
		{"rescore against a new profile", "new profile", map[int64]score.PendingReason{fresh.ID: score.PendingNew, changed.ID: score.PendingChanged, current.ID: score.PendingRescore, shown.ID: score.PendingNew}},
		{"rescore against the profile already used", "old profile", map[int64]score.PendingReason{fresh.ID: score.PendingNew, changed.ID: score.PendingChanged, shown.ID: score.PendingNew}},
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
func TestScoringService_Pending_ARescoreResumes(t *testing.T) {
	t.Parallel()
	s := testutil.NewTestApp(t)
	acme := testutil.MustAddCompany(t, s, "Acme")
	done := testutil.MustIngestPostingFor(t, s, acme.ID, "1", "Rescored")
	left := testutil.MustIngestPostingFor(t, s, acme.ID, "2", "Not yet")
	mustRecordScore(t, s, done, "old profile")
	mustRecordScore(t, s, left, "old profile")

	mustRecordScore(t, s, done, "new profile")

	got, _ := pendingReasons(t, s, "new profile")
	if diff := cmp.Diff(map[int64]score.PendingReason{left.ID: score.PendingRescore}, got); diff != "" {
		t.Errorf("rescore pending (-want +got):\n%s", diff)
	}
}

func TestScoringService_Pending_Pages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	scoring := s.Scores
	acme := testutil.MustAddCompany(t, s, "Acme")
	var ids []int64
	for i := 1; i <= 5; i++ {
		ids = append(ids, testutil.MustIngestPostingFor(t, s, acme.ID, fmt.Sprint(i), fmt.Sprintf("Role %d", i)).ID)
	}

	var got [][]int64
	var hasMore []bool
	var before int64
	for range 3 {
		page, err := scoring.Pending(ctx, "", before, 2)
		if err != nil {
			t.Fatalf("Pending: %v", err)
		}
		if page.Total != len(ids) {
			t.Errorf("Total = %d, want %d", page.Total, len(ids))
		}
		var pageIDs []int64
		for _, p := range page.Postings {
			pageIDs = append(pageIDs, p.ID)
		}
		got = append(got, pageIDs)
		hasMore = append(hasMore, page.HasMore)
		before = pageIDs[len(pageIDs)-1]
	}
	if diff := cmp.Diff([][]int64{{ids[4], ids[3]}, {ids[2], ids[1]}, {ids[0]}}, got); diff != "" {
		t.Errorf("pages (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]bool{true, true, false}, hasMore); diff != "" {
		t.Errorf("HasMore (-want +got):\n%s", diff)
	}
}

func TestScoringService_ForPosting_NewestFirstWithWhatProducedThem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	scoring := s.Scores
	acme := testutil.MustAddCompany(t, s, "Acme")
	p := testutil.MustIngestPostingFor(t, s, acme.ID, "1", "Role")
	other := testutil.MustIngestPostingFor(t, s, acme.ID, "2", "Other")
	first := score.Provenance{Model: "claude-sonnet-5", ClientName: "claude-code", ClientVersion: "2.4.0", ProfileSHA256: "profile"}
	second := score.Provenance{Model: "claude-opus-5-5", ClientName: "claude-code", ClientVersion: "2.4.1", ProfileSHA256: "edited profile"}
	for _, r := range []struct {
		provenance score.Provenance
		entries    []score.Entry
	}{
		{first, []score.Entry{{PostingID: p.ID, Score: 2, Reason: "first look", ContentSHA256: p.ContentSHA256}, {PostingID: other.ID, Score: 1, Reason: "not this one", ContentSHA256: other.ContentSHA256}}},
		{second, []score.Entry{{PostingID: p.ID, Score: 4, Reason: "after the edit", ContentSHA256: p.ContentSHA256}}},
	} {
		if _, err := scoring.Record(ctx, r.provenance, r.entries); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	scores, err := scoring.ForPosting(ctx, p.ID)
	if err != nil {
		t.Fatalf("ForPosting: %v", err)
	}
	type summary struct {
		Score      int
		Reason     string
		Provenance score.Provenance
		Content    string
	}
	var got []summary
	for _, sc := range scores {
		if sc.ScoredAt.IsZero() {
			t.Errorf("score %d has no ScoredAt", sc.ID)
		}
		got = append(got, summary{sc.Score, sc.Reason, sc.Provenance, sc.ContentSHA256})
	}
	want := []summary{{4, "after the edit", second, p.ContentSHA256}, {2, "first look", first, p.ContentSHA256}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("scores (-want +got):\n%s", diff)
	}
}
