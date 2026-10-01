package tui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

func testActiveApplications() []store.ApplicationView {
	return []store.ApplicationView{
		{
			Application: store.Application{ID: 10, Status: store.ApplicationStatusStarted},
			Posting:     store.Posting{ID: 1, IngestedFields: store.IngestedFields{Title: "Engineer"}},
			CompanyName: "Acme",
		},
		{
			Application: store.Application{ID: 20, Status: store.ApplicationStatusInterviewing},
			Posting:     store.Posting{ID: 2, IngestedFields: store.IngestedFields{Title: "Designer"}},
			CompanyName: "Globex",
		},
	}
}

func TestActiveApplicationListModel_CursorMovement_ClampsToApps(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel()
	apps := testActiveApplications()

	m.Update(tea.KeyMsg{Type: tea.KeyDown}, apps)
	m.Update(tea.KeyMsg{Type: tea.KeyDown}, apps)
	if m.cursor != 1 {
		t.Fatalf("cursor after two downs = %d, want 1 (clamped)", m.cursor)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyUp}, apps)
	m.Update(tea.KeyMsg{Type: tea.KeyUp}, apps)
	if m.cursor != 0 {
		t.Fatalf("cursor after two ups = %d, want 0 (clamped)", m.cursor)
	}
}

func TestActiveApplicationListModel_View_ShowsReviewGlyphsPerApplication(t *testing.T) {
	t.Parallel()

	apps := testActiveApplications()
	apps[0].LatestReviews = map[documents.Type]store.DocumentReview{
		documents.CoverLetter: {Outcome: store.ReviewOutcomeFlagged},
		documents.Resume:      {Outcome: store.ReviewOutcomePassed},
	}
	// apps[1] is left with no LatestReviews -- neither document reviewed yet.

	m := newActiveApplicationListModel()
	got := m.View(apps, nil, time.Time{}, 20)

	if !containsAll(got, "CL:✗ R:✓", "CL:- R:-") {
		t.Fatalf("View() = %q, want CL:✗ R:✓ for the reviewed application and CL:- R:- for the unreviewed one", got)
	}
}

func TestActiveApplicationListModel_C_ReturnsBackToCompanyListMsg(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel()
	cmd, intent := m.Update(runeKey('c'), testActiveApplications())
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(backToCompanyListMsg); !ok {
		t.Fatalf("intent = %T, want backToCompanyListMsg", intent)
	}
}

func TestActiveApplicationListModel_Q_ReturnsQuitCmd(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel()
	cmd, _ := m.Update(runeKey('q'), testActiveApplications())
	if cmd == nil {
		t.Fatal("Update on 'q' returned nil Cmd, want tea.Quit")
	}
}

func TestActiveApplicationListModel_S_ReturnsEnterApplicationStatusMsg(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel()
	apps := testActiveApplications()

	cmd, intent := m.Update(runeKey('s'), apps)
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(enterApplicationStatusMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterApplicationStatusMsg", intent)
	}
	if got.postingID != 1 {
		t.Fatalf("postingID = %d, want 1 (posting at cursor 0)", got.postingID)
	}
	if got.currentStatus != store.ApplicationStatusStarted {
		t.Fatalf("currentStatus = %s, want %s", got.currentStatus, store.ApplicationStatusStarted)
	}
}

func TestActiveApplicationListModel_Enter_ReturnsEnterApplicationDetailMsg(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel()
	apps := testActiveApplications()

	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter}, apps)
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(enterApplicationDetailMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterApplicationDetailMsg", intent)
	}
	if got.application.ID != apps[0].ID {
		t.Fatalf("application.ID = %d, want %d (application at cursor 0)", got.application.ID, apps[0].ID)
	}
}

func TestActiveApplicationListModel_EmptyList_KeysDoNotPanic(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel()
	for _, key := range []tea.KeyMsg{runeKey('s'), {Type: tea.KeyEnter}} {
		if cmd, intent := m.Update(key, nil); cmd != nil || intent != nil {
			t.Fatalf("Update(%v) on empty list = %v, %v, want nil, nil", key, cmd, intent)
		}
	}
}

func TestActiveApplicationListModel_E_EntersExportForSelectedApplication(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel()
	apps := testActiveApplications()
	m.Update(tea.KeyMsg{Type: tea.KeyDown}, apps)

	_, intent := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")}, apps)

	got, ok := intent.(enterApplicationExportMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterApplicationExportMsg", intent)
	}
	if got.application.ID != apps[1].ID {
		t.Errorf("exported application ID = %d, want %d (the one under the cursor)", got.application.ID, apps[1].ID)
	}
}

func TestActiveApplicationListModel_View_AdvertisesExport(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel()

	if got := m.View(testActiveApplications(), nil, time.Time{}, 20); !strings.Contains(got, "e: export") {
		t.Errorf("View() = %q, want it to advertise the export binding", got)
	}
}

func TestActiveApplicationListModel_View_ShowsStatusLabelNotEnumValue(t *testing.T) {
	t.Parallel()

	apps := testActiveApplications()
	apps[0].Status = store.ApplicationStatusOfferReceived
	apps[1].Status = store.ApplicationStatusStarted

	m := newActiveApplicationListModel()
	got := m.View(apps, nil, time.Time{}, 20)

	if !containsAll(got, "Offer received", "Started") {
		t.Errorf("View() = %q, want human-readable status labels", got)
	}
	for _, raw := range []string{"offer_received", "application_started"} {
		if strings.Contains(got, raw) {
			t.Errorf("View() leaks the raw enum value %q into the UI", raw)
		}
	}
}

// TestActiveApplicationListModel_View_FlagsClosedPosting covers the
// other half of #105: the syncer deliberately leaves an application at
// interviewing or beyond alone when its posting comes down, so the list
// has to say the posting is gone or that fact is invisible.
func TestActiveApplicationListModel_View_FlagsClosedPosting(t *testing.T) {
	t.Parallel()

	apps := testActiveApplications()
	apps[0].Status = store.ApplicationStatusInterviewing
	apps[0].Posting.ListingStatus = "closed"
	apps[1].Posting.ListingStatus = "open"

	m := newActiveApplicationListModel()
	nextSteps := map[int64]string{}
	for _, a := range apps {
		nextSteps[a.ID] = nextStep(a, nil)
	}
	got := m.View(apps, nextSteps, time.Time{}, 20)

	if !strings.Contains(got, "closed") {
		t.Errorf("View() = %q, want the closed posting flagged", got)
	}
	if strings.Count(got, "closed") != 1 {
		t.Errorf("View() marks %d rows closed, want exactly 1 (only the application whose posting came down)", strings.Count(got, "closed"))
	}
}

func TestNextStep(t *testing.T) {
	t.Parallel()

	passed := store.DocumentReview{Outcome: store.ReviewOutcomePassed}
	flagged := store.DocumentReview{Outcome: store.ReviewOutcomeFlagged}
	both := func(cl, r documentProgress) map[documents.Type]documentProgress {
		return map[documents.Type]documentProgress{documents.CoverLetter: cl, documents.Resume: r}
	}
	drafted := documentProgress{drafted: true}
	exported := documentProgress{drafted: true, exported: true}

	tests := []struct {
		name    string
		status  store.ApplicationStatus
		listing string
		reviews map[documents.Type]store.DocumentReview
		docs    map[documents.Type]documentProgress
		want    string
	}{
		{name: "not started: no next step", status: store.ApplicationStatusInterviewing, docs: both(exported, exported), want: ""},
		{name: "posting closed", status: store.ApplicationStatusStarted, listing: "closed", docs: both(drafted, drafted), want: "withdraw?"},
		{name: "later stage, posting closed", status: store.ApplicationStatusInterviewing, listing: "closed", docs: both(exported, exported), want: "(closed)"},
		{name: "nothing drafted", status: store.ApplicationStatusStarted, docs: both(documentProgress{}, documentProgress{}), want: "draft"},
		{name: "one document missing", status: store.ApplicationStatusStarted, docs: both(drafted, documentProgress{}), want: "draft"},
		{name: "drafted, not reviewed", status: store.ApplicationStatusStarted, docs: both(drafted, drafted), want: "review"},
		{name: "one passed, one not reviewed", status: store.ApplicationStatusStarted,
			reviews: map[documents.Type]store.DocumentReview{documents.CoverLetter: passed}, docs: both(drafted, drafted), want: "review"},
		{name: "flagged beats unreviewed", status: store.ApplicationStatusStarted,
			reviews: map[documents.Type]store.DocumentReview{documents.Resume: flagged}, docs: both(drafted, drafted), want: "revise"},
		{name: "passed, not exported", status: store.ApplicationStatusStarted,
			reviews: map[documents.Type]store.DocumentReview{documents.CoverLetter: passed, documents.Resume: passed}, docs: both(exported, drafted), want: "export"},
		{name: "passed and exported", status: store.ApplicationStatusStarted,
			reviews: map[documents.Type]store.DocumentReview{documents.CoverLetter: passed, documents.Resume: passed}, docs: both(exported, exported), want: "submit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := store.ApplicationView{
				Application:   store.Application{Status: tt.status},
				Posting:       store.Posting{ListingStatus: tt.listing},
				LatestReviews: tt.reviews,
			}
			if got := nextStep(a, tt.docs); got != tt.want {
				t.Errorf("nextStep() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestActiveApplicationListModel_View_ShowsAgeAndNextStep(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	apps := testActiveApplications()
	apps[0].StatusSince = now.Add(-21*24*time.Hour - time.Hour)
	apps[1].StatusSince = now.Add(-2 * time.Hour)
	nextSteps := map[int64]string{apps[0].ID: "draft"}

	m := newActiveApplicationListModel()
	got := m.View(apps, nextSteps, now, 20)

	for _, want := range []string{"Age", "Next", "21d", "0d", "draft"} {
		if !strings.Contains(got, want) {
			t.Errorf("View() = %q, want it to contain %q", got, want)
		}
	}
}

// Started applications come first, longest at their status first, so the
// stalest work is at the top; the rest keep the store's order
// (most-recently-changed first) below them (#164).
func TestOrderForHome(t *testing.T) {
	t.Parallel()

	app := func(id int64, status store.ApplicationStatus, since time.Time) store.ApplicationView {
		return store.ApplicationView{Application: store.Application{ID: id, Status: status}, StatusSince: since}
	}
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	apps := []store.ApplicationView{
		app(1, store.ApplicationStatusInterviewing, day(28)),
		app(2, store.ApplicationStatusStarted, day(20)),
		app(3, store.ApplicationStatusSubmitted, day(25)),
		app(4, store.ApplicationStatusStarted, day(1)),
	}

	orderForHome(apps)

	var got []int64
	for _, a := range apps {
		got = append(got, a.ID)
	}
	if diff := cmp.Diff([]int64{4, 2, 1, 3}, got); diff != "" {
		t.Errorf("order mismatch (-want +got):\n%s", diff)
	}
}

// loadActiveApplications works out each started application's next step
// from what's on disk and in the store: drafts, current reviews and
// current exports (#164, #188).
func TestLoadActiveApplications_NextSteps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := newTestStore(t)
	docs := documents.NewStore(t.TempDir())
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")

	undrafted := mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer").ID)
	ready := mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-2", "Designer").ID)
	stale := mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-3", "Manager").ID)
	for _, application := range []store.Application{ready, stale} {
		paths, err := docs.EnsureDir(application.ID)
		if err != nil {
			t.Fatalf("EnsureDir: %v", err)
		}
		for documentType, path := range map[documents.Type]string{documents.CoverLetter: mustDoc(t, paths, documents.CoverLetter).Path, documents.Resume: mustDoc(t, paths, documents.Resume).Path} {
			if err := os.WriteFile(path, []byte("# Draft"), 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			if _, err := s.CreateDocumentReview(ctx, application.ID, documentType, "# Draft", store.ReviewOutcomePassed, ""); err != nil {
				t.Fatalf("CreateDocumentReview: %v", err)
			}
			exported := "# Draft"
			if application.ID == stale.ID && documentType == documents.Resume {
				exported = "# An earlier draft"
			}
			if err := s.RecordDocumentExport(ctx, application.ID, documentType, exported, "/out/x.pdf"); err != nil {
				t.Fatalf("RecordDocumentExport: %v", err)
			}
		}
	}

	msg, ok := loadActiveApplications(s, docs)().(activeApplicationsLoadedMsg)
	if !ok || msg.err != nil {
		t.Fatalf("loadActiveApplications() = %+v, want a loaded message without error", msg)
	}
	want := map[int64]string{undrafted.ID: "draft", ready.ID: "submit", stale.ID: "export"}
	if diff := cmp.Diff(want, msg.nextSteps); diff != "" {
		t.Errorf("next steps mismatch (-want +got):\n%s", diff)
	}
}

// The widest possible row must fit the 100 columns the screen-fit tests
// use: wider lines are cut off on the right by the terminal, losing the
// Status, Posting and Review columns without any sign of it.
func TestActiveApplicationListModel_View_WidestRowFits100Columns(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	apps := []store.ApplicationView{{
		Application: store.Application{ID: 1, Status: store.ApplicationStatusOfferReceived},
		Posting: store.Posting{ListingStatus: "closed", IngestedFields: store.IngestedFields{
			Title: strings.Repeat("Principal Engineer ", 5)}},
		CompanyName: strings.Repeat("Longcompanyname ", 3),
		StatusSince: now.Add(-400 * 24 * time.Hour),
		LatestReviews: map[documents.Type]store.DocumentReview{
			documents.CoverLetter: {Outcome: store.ReviewOutcomeFlagged},
			documents.Resume:      {Outcome: store.ReviewOutcomePassed},
		},
	}}

	m := newActiveApplicationListModel()
	view := ansi.Strip(m.View(apps, map[int64]string{1: "withdraw?"}, now, 20))

	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "│") {
			if width := ansi.StringWidth(line); width > 100 {
				t.Errorf("row is %d columns wide, want at most 100: %q", width, line)
			}
		}
	}
}

// The home screen's review column has an entry for every document type
// in documents' table (RFC 0004), abbreviated from its label.
func TestReviewGlyphSummary_HasEveryDocumentType(t *testing.T) {
	t.Parallel()
	got := reviewGlyphSummary(nil)
	for _, documentType := range documents.Types() {
		if want := documentAbbreviation(documentType) + ":"; !strings.Contains(got, want) {
			t.Errorf("reviewGlyphSummary(nil) = %q, want an entry %q for the %s", got, want, documentType.Label())
		}
	}
}

func TestReviewBadgeAndGlyph_Text(t *testing.T) {
	t.Parallel()

	unknown := store.ReviewOutcome(99)
	for _, tc := range []struct {
		name      string
		review    store.DocumentReview
		hasReview bool
		badge     string
		glyph     string
	}{
		{"no review", store.DocumentReview{}, false, "[not reviewed]", "-"},
		{"passed", store.DocumentReview{Outcome: store.ReviewOutcomePassed}, true, "[PASSED]", "✓"},
		{"flagged", store.DocumentReview{Outcome: store.ReviewOutcomeFlagged}, true, "[FLAGGED]", "✗"},
		{"unknown outcome", store.DocumentReview{Outcome: unknown}, true, "[" + unknown.String() + "]", "?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ansi.Strip(reviewBadge(tc.review, tc.hasReview)); got != tc.badge {
				t.Errorf("reviewBadge() = %q, want %q", got, tc.badge)
			}
			if got := ansi.Strip(reviewGlyph(tc.review, tc.hasReview)); got != tc.glyph {
				t.Errorf("reviewGlyph() = %q, want %q", got, tc.glyph)
			}
		})
	}
}

// A new store.ReviewOutcome needs a display of its own, or it renders as
// the unknown-outcome fallback ("?").
func TestReviewGlyph_EveryOutcomeHasItsOwn(t *testing.T) {
	t.Parallel()

	seen := make(map[string]store.ReviewOutcome)
	for _, outcome := range store.ReviewOutcomes() {
		glyph := ansi.Strip(reviewGlyph(store.DocumentReview{Outcome: outcome}, true))
		if glyph == "?" {
			t.Errorf("%s renders as the unknown-outcome glyph %q; give it a display", outcome, glyph)
		}
		if other, ok := seen[glyph]; ok {
			t.Errorf("%s and %s both render as %q", outcome, other, glyph)
		}
		seen[glyph] = outcome
	}
}
