package tui

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/tui/message"
)

func TestReviewSnapshot_KeepsEveryDocumentTypeAndOutcome(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	for _, documentType := range document.Types() {
		for _, outcome := range document.Outcomes() {
			t.Run(documentType.String()+"/"+outcome.String(), func(t *testing.T) {
				t.Parallel()
				r := document.Review{ID: 8, ApplicationID: 4, DocumentType: documentType, Outcome: outcome, Notes: "tighten it", CreatedAt: at}

				s := reviewSnapshot(r)
				if s.DocumentType.String() != documentType.String() || s.Outcome.String() != outcome.String() {
					t.Errorf("snapshot = %v %v, want %v %v", s.DocumentType, s.Outcome, documentType, outcome)
				}
				want := map[document.Type]document.Review{documentType: {ID: 8, ApplicationID: 4, DocumentType: documentType, Outcome: outcome, Notes: "tighten it", CreatedAt: at}}
				if diff := cmp.Diff(want, reviewsOf(message.ReviewList{Reviews: []message.Review{s}})); diff != "" {
					t.Errorf("reviewsOf(reviewSnapshot(r)) (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func TestPostingSnapshot_KeepsTheContentHash(t *testing.T) {
	t.Parallel()

	p := posting.Posting{ID: 5, ContentSHA256: "abc"}
	if got := postingOf(postingSnapshot(p)).ContentSHA256; got != "abc" {
		t.Errorf("ContentSHA256 after a snapshot and back = %q, want %q", got, "abc")
	}
}

func TestScoreSnapshot_KeepsWhatTheFitSectionShows(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	s := score.Score{
		ID:            3,
		Provenance:    score.Provenance{Model: "claude", ClientName: "cli", ClientVersion: "1.0", ProfileSHA256: "def"},
		Score:         4,
		Reason:        "fits",
		ContentSHA256: "abc",
		ScoredAt:      at,
	}
	want := message.Score{Score: 4, Reason: "fits", Model: "claude", ClientName: "cli", ClientVersion: "1.0", ContentSHA256: "abc", ScoredAt: at}
	if diff := cmp.Diff(want, scoreSnapshot(s)); diff != "" {
		t.Errorf("scoreSnapshot (-want +got):\n%s", diff)
	}
}
