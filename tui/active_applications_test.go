package tui

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

func testActiveApplications() []posting.ActiveApplication {
	return []posting.ActiveApplication{
		{
			Application: application.Application{ID: 10, Status: application.Started},
			Posting:     posting.Posting{ID: 1, IngestedFields: posting.IngestedFields{Title: "Engineer"}},
			CompanyName: "Acme",
		},
		{
			Application: application.Application{ID: 20, Status: application.Interviewing},
			Posting:     posting.Posting{ID: 2, IngestedFields: posting.IngestedFields{Title: "Designer"}},
			CompanyName: "Globex",
		},
	}
}

// applicationsReadOf is m's own read of apps, with nothing drafted.
func applicationsReadOf(m *activeApplicationListModel, apps []posting.ActiveApplication) message.Event[message.Read[message.ActiveApplications]] {
	var read message.ActiveApplications
	for _, a := range apps {
		read.Rows = append(read.Rows, applicationRow(a, nil))
	}
	return message.New(m.src(), message.Subject{}, message.OpNone, time.Now().UTC(), message.Read[message.ActiveApplications]{Value: read})
}

// homeAt is a home screen showing apps in width columns and rows rows.
func homeAt(t *testing.T, apps []posting.ActiveApplication, width, rows int) *activeApplicationListModel {
	t.Helper()
	m := newActiveApplicationListModel(nil, nil, nil)
	m.resize(message.New(message.Source{}, message.Subject{}, message.OpNone, time.Now().UTC(), message.Signal[message.ContentSize]{Value: message.ContentSize{Width: width, Rows: rows}}))
	m.applicationsRead(applicationsReadOf(m, apps))
	return m
}

// homeWith is a home screen showing apps, 100 columns by 40 rows.
func homeWith(t *testing.T, apps []posting.ActiveApplication) *activeApplicationListModel {
	t.Helper()
	return homeAt(t, apps, 100, 40)
}

// pressHome sends key to m, returning what its command sends, or nil when
// it sends nothing.
func pressHome(t *testing.T, m *activeApplicationListModel, key tea.KeyPressMsg) tea.Msg {
	t.Helper()
	msgs := runCmd(t, m.keyPressed(key))
	switch len(msgs) {
	case 0:
		return nil
	case 1:
		return msgs[0]
	}
	t.Fatalf("%v sent %#v, want at most one message", key, msgs)
	return nil
}

func TestActiveApplicationListModel_CursorMovement_ClampsToApps(t *testing.T) {
	t.Parallel()

	m := homeWith(t, testActiveApplications())

	pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("cursor after two downs = %d, want 1 (clamped)", m.cursor)
	}

	pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != 0 {
		t.Fatalf("cursor after two ups = %d, want 0 (clamped)", m.cursor)
	}
}

func TestActiveApplicationListModel_View_ShowsReviewGlyphsPerApplication(t *testing.T) {
	t.Parallel()

	apps := testActiveApplications()
	apps[0].LatestReviews = map[document.Type]document.Review{
		document.CoverLetter: {Outcome: document.Flagged},
		document.Resume:      {Outcome: document.Passed},
	}
	// apps[1] is left with no LatestReviews -- neither document reviewed yet.

	got := ansi.Strip(homeAt(t, apps, 0, 20).render(time.Time{}))

	if !containsAll(got, "CL:✗ R:✓", "CL:- R:-") {
		t.Fatalf("View() = %q, want CL:✗ R:✓ for the reviewed application and CL:- R:- for the unreviewed one", got)
	}
}

// Tab switches to the company list; 'c' no longer does.
func TestActiveApplicationListModel_C_DoesNothing(t *testing.T) {
	t.Parallel()

	m := homeWith(t, testActiveApplications())
	if got := pressHome(t, m, runeKey('c')); got != nil {
		t.Errorf("'c' sent %#v, want nothing", got)
	}
}

func TestActiveApplicationListModel_Q_Quits(t *testing.T) {
	t.Parallel()

	m := homeWith(t, testActiveApplications())
	if got, ok := pressHome(t, m, runeKey('q')).(tea.QuitMsg); !ok {
		t.Fatalf("'q' sent %#v, want tea.QuitMsg", got)
	}
}

func TestActiveApplicationListModel_S_PushesStatusOnTheApplicationAtTheCursor(t *testing.T) {
	t.Parallel()

	apps := testActiveApplications()
	m := homeWith(t, apps)
	pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyDown})

	nav, ok := pressHome(t, m, runeKey('s')).(message.Event[message.Nav[message.Application]])
	if !ok {
		t.Fatalf("'s' sent no Nav to the status picker")
	}
	assertEventFrom(t, nav.Meta, message.Source{Instance: m.Instance()})
	if want := (message.Subject{Type: message.TypeApplication, ID: apps[1].ID}); nav.Meta.Subject != want {
		t.Errorf("Subject = %+v, want %+v", nav.Meta.Subject, want)
	}
	want := message.Nav[message.Application]{Op: message.NavPush, Screen: message.ScreenApplicationStatus, Value: applicationSnapshot(apps[1].Application)}
	if diff := cmp.Diff(want, nav.Payload); diff != "" {
		t.Errorf("Nav (-want +got):\n%s", diff)
	}
}

// Enter opens detail and d the delete confirmation, each on the row's
// snapshot.
func TestActiveApplicationListModel_EnterAndD_PushTheirScreenOnTheRowAtTheCursor(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		key    tea.KeyPressMsg
		screen message.Screen
	}{
		{key: tea.KeyPressMsg{Code: tea.KeyEnter}, screen: message.ScreenApplicationDetail},
		{key: runeKey('d'), screen: message.ScreenApplicationDelete},
	} {
		t.Run(tt.key.String(), func(t *testing.T) {
			t.Parallel()
			apps := testActiveApplications()
			apps[1].LatestReviews = map[document.Type]document.Review{document.Resume: {ID: 3, Outcome: document.Passed}}
			m := homeWith(t, apps)
			pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyDown})

			nav, ok := pressHome(t, m, tt.key).(message.Event[message.Nav[message.ApplicationDetail]])
			if !ok {
				t.Fatalf("%s sent no Nav on an application", tt.key)
			}
			assertEventFrom(t, nav.Meta, message.Source{Instance: m.Instance()})
			if want := (message.Subject{Type: message.TypeApplication, ID: apps[1].ID}); nav.Meta.Subject != want {
				t.Errorf("Subject = %+v, want %+v", nav.Meta.Subject, want)
			}
			want := message.Nav[message.ApplicationDetail]{Op: message.NavPush, Screen: tt.screen, Value: message.ApplicationDetail{
				Application: applicationSnapshot(apps[1].Application),
				Posting:     postingSnapshot(apps[1].Posting),
				CompanyName: apps[1].CompanyName,
				Reviews:     []message.Review{{ID: 3, DocumentType: message.DocumentResume, Outcome: message.ReviewPassed}},
			}}
			if diff := cmp.Diff(want, nav.Payload); diff != "" {
				t.Errorf("Nav (-want +got):\n%s", diff)
			}
		})
	}
}

// openedDetail is the application msg opens detail on, if it does.
func openedDetail(msg tea.Msg) (int64, bool) {
	nav, ok := msg.(message.Event[message.Nav[message.ApplicationDetail]])
	return nav.Payload.Value.Application.ID, ok
}

func TestActiveApplicationListModel_EmptyList_KeysDoNothing(t *testing.T) {
	t.Parallel()

	m := homeWith(t, nil)
	for _, key := range []tea.KeyPressMsg{runeKey('s'), runeKey('e'), runeKey('d'), {Code: tea.KeyEnter}} {
		if got := pressHome(t, m, key); got != nil {
			t.Fatalf("%v on empty list sent %#v, want nothing", key, got)
		}
	}
}

func TestActiveApplicationListModel_E_AsksForExportOfSelectedApplication(t *testing.T) {
	t.Parallel()

	apps := testActiveApplications()
	m := homeWith(t, apps)
	pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyDown})

	got, ok := pressHome(t, m, runeKey('e')).(enterApplicationExportMsg)
	if !ok {
		t.Fatalf("'e' sent no enterApplicationExportMsg")
	}
	if got.application.ID != apps[1].ID {
		t.Errorf("exported application ID = %d, want %d (the one under the cursor)", got.application.ID, apps[1].ID)
	}
}

func TestActiveApplicationListModel_View_AdvertisesExport(t *testing.T) {
	t.Parallel()

	if got := homeAt(t, testActiveApplications(), 0, 20).render(time.Time{}); !strings.Contains(got, "e: export") {
		t.Errorf("View() = %q, want it to advertise the export binding", got)
	}
}

func TestActiveApplicationListModel_View_ShowsStatusLabelNotEnumValue(t *testing.T) {
	t.Parallel()

	apps := testActiveApplications()
	apps[0].Status = application.OfferReceived
	apps[1].Status = application.Started

	got := homeAt(t, apps, 0, 20).render(time.Time{})

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
	apps[0].Status = application.Interviewing
	apps[0].Posting.ListingStatus = posting.ListingClosed
	apps[1].Posting.ListingStatus = posting.ListingOpen

	got := homeAt(t, apps, 0, 20).render(time.Time{})

	if !strings.Contains(got, "closed") {
		t.Errorf("View() = %q, want the closed posting flagged", got)
	}
	if strings.Count(got, "closed") != 1 {
		t.Errorf("View() marks %d rows closed, want exactly 1 (only the application whose posting came down)", strings.Count(got, "closed"))
	}
}

func TestNextStep(t *testing.T) {
	t.Parallel()

	passed := document.Review{Outcome: document.Passed}
	flagged := document.Review{Outcome: document.Flagged}
	both := func(cl, r documentProgress) map[document.Type]documentProgress {
		return map[document.Type]documentProgress{document.CoverLetter: cl, document.Resume: r}
	}
	drafted := documentProgress{drafted: true}
	exported := documentProgress{drafted: true, exported: true}

	tests := []struct {
		name    string
		status  application.Status
		listing posting.ListingStatus
		reviews map[document.Type]document.Review
		docs    map[document.Type]documentProgress
		want    string
	}{
		{name: "not started: no next step", status: application.Interviewing, docs: both(exported, exported), want: ""},
		{name: "posting closed", status: application.Started, listing: posting.ListingClosed, docs: both(drafted, drafted), want: "withdraw?"},
		{name: "later stage, posting closed", status: application.Interviewing, listing: posting.ListingClosed, docs: both(exported, exported), want: "(closed)"},
		{name: "nothing drafted", status: application.Started, docs: both(documentProgress{}, documentProgress{}), want: "draft"},
		{name: "one document missing", status: application.Started, docs: both(drafted, documentProgress{}), want: "draft"},
		{name: "drafted, not reviewed", status: application.Started, docs: both(drafted, drafted), want: "review"},
		{name: "one passed, one not reviewed", status: application.Started,
			reviews: map[document.Type]document.Review{document.CoverLetter: passed}, docs: both(drafted, drafted), want: "review"},
		{name: "flagged beats unreviewed", status: application.Started,
			reviews: map[document.Type]document.Review{document.Resume: flagged}, docs: both(drafted, drafted), want: "revise"},
		{name: "passed, not exported", status: application.Started,
			reviews: map[document.Type]document.Review{document.CoverLetter: passed, document.Resume: passed}, docs: both(exported, drafted), want: "export"},
		{name: "passed and exported", status: application.Started,
			reviews: map[document.Type]document.Review{document.CoverLetter: passed, document.Resume: passed}, docs: both(exported, exported), want: "submit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := posting.ActiveApplication{
				Application:   application.Application{Status: tt.status},
				Posting:       posting.Posting{ListingStatus: tt.listing},
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

	got := homeAt(t, apps, 0, 20).render(now)

	for _, want := range []string{"Age", "Next", "21d", "0d", "draft"} {
		if !strings.Contains(got, want) {
			t.Errorf("View() = %q, want it to contain %q", got, want)
		}
	}
}

// Started applications come first, longest at their status first, so the
// stalest work is at the top; the rest keep the store's order
// (most-recently-changed first) below them.
func TestOrderForHome(t *testing.T) {
	t.Parallel()

	app := func(id int64, status application.Status, since time.Time) posting.ActiveApplication {
		return posting.ActiveApplication{Application: application.Application{ID: id, Status: status}, StatusSince: since}
	}
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	apps := []posting.ActiveApplication{
		app(1, application.Interviewing, day(28)),
		app(2, application.Started, day(20)),
		app(3, application.Submitted, day(25)),
		app(4, application.Started, day(1)),
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

// The home screen's read works out each started application's next step
// from what's on disk and in the store: drafts, current reviews and
// current exports.
func TestActiveApplicationListModel_Init_ReadsEachNextStep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	docs := documents.NewStore(t.TempDir())
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")

	mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer").ID)
	ready := mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-2", "Designer").ID)
	stale := mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-3", "Manager").ID)
	for _, target := range []application.Application{ready, stale} {
		paths, err := docs.EnsureDir(target.ID)
		if err != nil {
			t.Fatalf("EnsureDir: %v", err)
		}
		for documentType, path := range map[document.Type]string{document.CoverLetter: mustDoc(t, paths, document.CoverLetter).Path, document.Resume: mustDoc(t, paths, document.Resume).Path} {
			if err := os.WriteFile(path, []byte("# Draft"), 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			if _, err := createReview(ctx, s, target.ID, documentType, "# Draft", document.Passed, ""); err != nil {
				t.Fatalf("CreateReview: %v", err)
			}
			exported := "# Draft"
			if target.ID == stale.ID && documentType == document.Resume {
				exported = "# An earlier draft"
			}
			if err := s.Documents.RecordExport(ctx, target.ID, documentType, exported, "/out/x.pdf"); err != nil {
				t.Fatalf("RecordExport: %v", err)
			}
		}
	}
	m := newActiveApplicationListModel(s.Applications, s.Documents, docs)
	r := model.NewMsgRouter()
	if err := r.Register(m); err != nil {
		t.Fatalf("Register: %v", err)
	}
	r.Dispatch(sizedTo(message.ContentSize{Width: 120, Rows: 40}))

	for _, msg := range runCmd(t, m.Init()) {
		r.Dispatch(msg)
	}

	want := map[string]string{"Engineer": "draft", "Designer": "submit", "Manager": "export"}
	for _, line := range strings.Split(ansi.Strip(m.render(time.Time{})), "\n") {
		for title, step := range want {
			if strings.Contains(line, title) {
				if !strings.Contains(line, step) {
					t.Errorf("row for %s = %q, want next step %q", title, line, step)
				}
				delete(want, title)
			}
		}
	}
	if len(want) > 0 {
		t.Errorf("no rows for %v", want)
	}
}

// The widest possible row must fit the 100 columns the screen-fit tests
// use: wider lines are cut off on the right by the terminal, losing the
// Status, Posting and Review columns without any sign of it.
// The table spans the terminal, even with the widest value every fixed
// column can hold.
func TestActiveApplicationListModel_View_TableSpansTerminalWidth(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	widest := posting.ActiveApplication{
		Application: application.Application{ID: 1, Status: application.OfferReceived},
		Posting: posting.Posting{ListingStatus: posting.ListingClosed, IngestedFields: posting.IngestedFields{
			Title: strings.Repeat("Principal Engineer ", 5)}},
		CompanyName: strings.Repeat("Longcompanyname ", 3),
		StatusSince: now.Add(-400 * 24 * time.Hour),
		LatestReviews: map[document.Type]document.Review{
			document.CoverLetter: {Outcome: document.Flagged},
			document.Resume:      {Outcome: document.Passed},
		},
	}
	// The widest next step, "withdraw?", is only ever a started
	// application's, so a second row brings it; columns size to their
	// widest cell, so the table is as wide as one row holding both.
	withdraw := widest
	withdraw.ID = 2
	withdraw.Status = application.Started
	apps := []posting.ActiveApplication{widest, withdraw}

	tests := []struct {
		name  string
		width int
		want  int
	}{
		{"terminal the home screen was sized for", 100, 100},
		{"wide terminal", 160, 160},
		{"before the terminal reports its size", 0, fallbackTableWidth},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			view := ansi.Strip(homeAt(t, apps, tt.width, 20).render(now))
			if !strings.Contains(view, "withdraw?") || !strings.Contains(view, "Offer received") {
				t.Fatalf("View() = %q, want the widest next step and status in it", view)
			}
			for _, line := range strings.Split(view, "\n") {
				if strings.ContainsAny(line, "│╭╰├") {
					if got := ansi.StringWidth(line); got != tt.want {
						t.Errorf("table line is %d columns wide, want %d: %q", got, tt.want, line)
					}
				}
			}
		})
	}
}

// The home screen's review column has an entry for every document type
// in documents' table (RFC 0004), abbreviated from its label.
func TestReviewGlyphSummary_HasEveryDocumentType(t *testing.T) {
	t.Parallel()
	got := reviewGlyphSummary(nil)
	for _, documentType := range document.Types() {
		if want := documentAbbreviation(documentType) + ":"; !strings.Contains(got, want) {
			t.Errorf("reviewGlyphSummary(nil) = %q, want an entry %q for the %s", got, want, documentType.Label())
		}
	}
}

func TestReviewBadgeAndGlyph_Text(t *testing.T) {
	t.Parallel()

	unknown := document.Outcome(99)
	for _, tc := range []struct {
		name      string
		review    document.Review
		hasReview bool
		badge     string
		glyph     string
	}{
		{"no review", document.Review{}, false, "[not reviewed]", "-"},
		{"passed", document.Review{Outcome: document.Passed}, true, "[PASSED]", "✓"},
		{"flagged", document.Review{Outcome: document.Flagged}, true, "[FLAGGED]", "✗"},
		{"unknown outcome", document.Review{Outcome: unknown}, true, "[" + unknown.String() + "]", "?"},
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

// A new document.Outcome needs a display of its own, or it renders as
// the unknown-outcome fallback ("?").
func TestReviewGlyph_EveryOutcomeHasItsOwn(t *testing.T) {
	t.Parallel()

	seen := make(map[string]document.Outcome)
	for _, outcome := range document.Outcomes() {
		glyph := ansi.Strip(reviewGlyph(document.Review{Outcome: outcome}, true))
		if glyph == "?" {
			t.Errorf("%s renders as the unknown-outcome glyph %q; give it a display", outcome, glyph)
		}
		if other, ok := seen[glyph]; ok {
			t.Errorf("%s and %s both render as %q", outcome, other, glyph)
		}
		seen[glyph] = outcome
	}
}

// Short rows don't make the table narrower, so scrolling them into view
// doesn't change its width.
func TestActiveApplicationListModel_View_ShortRowsKeepTheTableWidth(t *testing.T) {
	t.Parallel()

	if got := tableWidthOf(t, homeAt(t, testActiveApplications(), 100, 20).render(time.Time{})); got != 100 {
		t.Errorf("table width with short rows = %d, want 100", got)
	}
}

// typeHomeKeys sends each rune of s to m as a key press.
func typeHomeKeys(t *testing.T, m *activeApplicationListModel, s string) {
	t.Helper()
	for _, r := range s {
		pressHome(t, m, runeKey(r))
	}
}

// openHomeHighlighted presses enter to keep the matches, then enter again
// to open the highlighted one, returning what that sends.
func openHomeHighlighted(t *testing.T, m *activeApplicationListModel) tea.Msg {
	t.Helper()
	pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	return pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
}

// testSearchApplications has three applications, at Acme, Globex and
// Initech, for the search tests.
func testSearchApplications() []posting.ActiveApplication {
	apps := testActiveApplications()
	return append(apps, posting.ActiveApplication{
		Application: application.Application{ID: 30, Status: application.Submitted},
		Posting:     posting.Posting{ID: 3, IngestedFields: posting.IngestedFields{Title: "Staff Analyst"}},
		CompanyName: "Initech",
	})
}

// Enter keeps the matches and stops typing, as on the company list and
// all-postings view, so letters act on the highlighted match instead of
// going into the query.
func TestActiveApplicationListModel_Search_EnterKeepsMatchesForCommands(t *testing.T) {
	t.Parallel()

	apps := testSearchApplications()
	m := homeWith(t, apps)

	typeHomeKeys(t, m, "/glo")
	if got := pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyEnter}); got != nil {
		t.Fatalf("enter while typing sent %#v, want nothing", got)
	}
	if m.searching || m.query != "glo" {
		t.Fatalf("after enter: searching %v, query %q; want not typing, query \"glo\"", m.searching, m.query)
	}

	globex := apps[1]
	if got, ok := pressHome(t, m, runeKey('e')).(enterApplicationExportMsg); !ok || got.application.ID != globex.ID {
		t.Errorf("e on the kept matches: sent %#v, want export for the Globex application (%d)", got, globex.ID)
	}
	if got, ok := pressHome(t, m, runeKey('s')).(message.Event[message.Nav[message.Application]]); !ok || got.Payload.Value.ID != globex.ID {
		t.Errorf("s on the kept matches: sent %#v, want status for the Globex application (%d)", got, globex.ID)
	}
	if got, ok := pressHome(t, m, runeKey('d')).(message.Event[message.Nav[message.ApplicationDetail]]); !ok || got.Payload.Screen != message.ScreenApplicationDelete || got.Payload.Value.Application.ID != globex.ID {
		t.Errorf("d on the kept matches: sent %#v, want delete for the Globex application (%d)", got, globex.ID)
	}
	if got, ok := openedDetail(pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})); !ok || got != globex.ID {
		t.Errorf("enter on the kept matches: sent %#v, want detail for the Globex application (%d)", got, globex.ID)
	}
}

func TestActiveApplicationListModel_Search_SlashEditsKeptQuery(t *testing.T) {
	t.Parallel()

	m := homeWith(t, testSearchApplications())
	typeHomeKeys(t, m, "/er")
	pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	typeHomeKeys(t, m, "/s")
	if !m.searching || m.query != "ers" {
		t.Fatalf("after /, s on kept matches: searching %v, query %q; want typing, query \"ers\"", m.searching, m.query)
	}
}

func TestActiveApplicationListModel_Search_EscClearsKeptMatchesAndKeepsHighlight(t *testing.T) {
	t.Parallel()

	m := homeWith(t, testSearchApplications())
	typeHomeKeys(t, m, "/glo")
	pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyEsc}); got != nil {
		t.Errorf("esc on kept matches sent %#v, want nothing", got)
	}

	if m.searching || m.query != "" || m.cursor != 1 {
		t.Errorf("after esc: searching %v, query %q, cursor %d; want closed, empty, on Globex (1)", m.searching, m.query, m.cursor)
	}
}

func TestActiveApplicationListModel_View_KeptMatches(t *testing.T) {
	t.Parallel()

	m := homeAt(t, testSearchApplications(), 200, 20)
	typeHomeKeys(t, m, "/glo")
	if view := m.render(time.Time{}); !strings.Contains(view, "enter: keep results") {
		t.Errorf("typing help doesn't say enter keeps the results:\n%s", view)
	}

	pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	view := ansi.Strip(m.render(time.Time{}))
	for _, want := range []string{"🔍 glo  1 of 3", "/: edit search", "esc: clear search", "s: status"} {
		if !strings.Contains(view, want) {
			t.Errorf("view doesn't show %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "▏") || strings.Contains(view, "Initech") {
		t.Errorf("view shows a typing caret or a non-match after enter:\n%s", view)
	}
}

// The search matches the company or the posting title.
func TestActiveApplicationListModel_Search_MatchesCompanyOrTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		keys string
		want int64
	}{
		{"by company", "/glo", 20},
		{"by title", "/analyst", 30},
		{"ignoring case", "/INITECH", 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := homeWith(t, testSearchApplications())

			typeHomeKeys(t, m, tt.keys)
			got := openHomeHighlighted(t, m)
			if id, ok := openedDetail(got); !ok || id != tt.want {
				t.Fatalf("after %q, enter sent %#v, want detail on application %d", tt.keys, got, tt.want)
			}
		})
	}
}

func TestActiveApplicationListModel_Search_ArrowsMoveThroughMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		down, up tea.KeyPressMsg
	}{
		{name: "arrows", down: tea.KeyPressMsg{Code: tea.KeyDown}, up: tea.KeyPressMsg{Code: tea.KeyUp}},
		{name: "ctrl+n/ctrl+p", down: tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}, up: tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := homeWith(t, testSearchApplications())

			typeHomeKeys(t, m, "/er") // Engineer at Acme, Designer at Globex
			pressHome(t, m, tt.down)
			pressHome(t, m, tt.down) // past the last match: stays on it
			got := openHomeHighlighted(t, m)
			if id, ok := openedDetail(got); !ok || id != 20 {
				t.Fatalf("down, down, enter sent %#v, want the Globex application (20)", got)
			}

			pressHome(t, m, runeKey('/'))
			pressHome(t, m, tt.up)
			got = openHomeHighlighted(t, m)
			if id, ok := openedDetail(got); !ok || id != 10 {
				t.Fatalf("up, enter sent %#v, want the Acme application (10)", got)
			}
		})
	}
}

func TestActiveApplicationListModel_Search_BackspaceWidensMatches(t *testing.T) {
	t.Parallel()

	m := homeWith(t, testSearchApplications())

	typeHomeKeys(t, m, "/glx") // matches nothing
	pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	got := openHomeHighlighted(t, m)
	if id, ok := openedDetail(got); !ok || id != 20 {
		t.Fatalf("backspace, enter sent %#v, want the Globex application (20)", got)
	}
}

func TestActiveApplicationListModel_Search_BackspaceOnEmptyQueryCloses(t *testing.T) {
	t.Parallel()

	m := homeWith(t, testSearchApplications())

	typeHomeKeys(t, m, "/")
	pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got, ok := pressHome(t, m, runeKey('q')).(tea.QuitMsg); !ok {
		t.Fatalf("'q' after backspace on an empty query sent %#v, want tea.QuitMsg (the prompt should be closed)", got)
	}
}

// Esc keeps the highlighted application selected in the full list, so '/'
// also serves to jump to an application and then set its status or
// export it.
func TestActiveApplicationListModel_Search_EscClearsAndKeepsHighlight(t *testing.T) {
	t.Parallel()

	m := homeWith(t, testSearchApplications())

	typeHomeKeys(t, m, "/glo")
	if got := pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyEsc}); got != nil {
		t.Fatalf("esc in the prompt sent %#v, want nothing (esc closes the prompt, it doesn't leave the screen)", got)
	}
	if got, ok := pressHome(t, m, runeKey('e')).(enterApplicationExportMsg); !ok || got.application.ID != 20 {
		t.Fatalf("'e' after esc sent %#v, want export for the Globex application (20)", got)
	}
	pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got, ok := pressHome(t, m, runeKey('e')).(enterApplicationExportMsg); !ok || got.application.ID != 30 {
		t.Fatalf("down, 'e' after esc sent %#v, want export for the Initech application (30): the full list is back", got)
	}
}

// While the prompt is open, the screen's command letters are just text.
func TestActiveApplicationListModel_Search_CommandLettersAreTyped(t *testing.T) {
	t.Parallel()

	for _, r := range "qsejk/" {
		t.Run(string(r), func(t *testing.T) {
			t.Parallel()
			apps := testSearchApplications()
			apps[1].CompanyName = "x" + string(r) + "y"
			m := homeWith(t, apps)

			typeHomeKeys(t, m, "/x")
			if got := pressHome(t, m, runeKey(r)); got != nil {
				t.Fatalf("%q in the prompt sent %#v, want nothing", r, got)
			}
			got := openHomeHighlighted(t, m)
			if id, ok := openedDetail(got); !ok || id != 20 {
				t.Fatalf("enter after typing %q sent %#v, want the application at %q (20)", r, got, apps[1].CompanyName)
			}
		})
	}
}

func TestActiveApplicationListModel_View_Search(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		keys    string
		want    []string
		notWant []string
	}{
		{name: "prompt open, no query yet", keys: "/", want: []string{"🔍 ", "3 of 3", "Acme", "Globex", "Initech"}},
		{name: "some match", keys: "/er", want: []string{"🔍 er", "2 of 3", "Acme", "Globex"}, notWant: []string{"Initech"}},
		{name: "none match", keys: "/zz", want: []string{"🔍 zz", "0 of 3", `No applications match "zz".`}, notWant: []string{"Acme", "Globex", "No active applications"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := homeWith(t, testSearchApplications())
			typeHomeKeys(t, m, tt.keys)
			got := m.render(time.Time{})
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("View missing %q:\n%s", want, got)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(got, notWant) {
					t.Errorf("View unexpectedly contains %q:\n%s", notWant, got)
				}
			}
		})
	}
}

func TestActiveApplicationListModel_View_HelpMatchesMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		keys    string
		want    []string
		notWant []string
	}{
		{name: "list", keys: "", want: []string{"/: search"}},
		{name: "prompt open", keys: "/", want: []string{"type to filter", "enter: keep results", "esc: clear"}, notWant: []string{"s: status", "q: quit"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := homeWith(t, testSearchApplications())
			typeHomeKeys(t, m, tt.keys)
			got := m.render(time.Time{})
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("View missing %q:\n%s", want, got)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(got, notWant) {
					t.Errorf("View unexpectedly contains %q:\n%s", notWant, got)
				}
			}
		})
	}
}

// A reload (another process changed something) keeps the cursor on the
// same match while the prompt is open, as it does on the full list.
func TestActiveApplicationListModel_Search_ReloadKeepsTheHighlightedMatch(t *testing.T) {
	t.Parallel()

	apps := testSearchApplications()
	m := homeWith(t, apps)
	typeHomeKeys(t, m, "/s")                            // Designer at Globex, Staff Analyst at Initech
	pressHome(t, m, tea.KeyPressMsg{Code: tea.KeyDown}) // the Initech application (30)

	// The reload brings in an application ahead of it that doesn't match,
	// so its place in the full list moves and its place among the matches
	// doesn't.
	reloaded := append([]posting.ActiveApplication{{
		Application: application.Application{ID: 40},
		Posting:     posting.Posting{ID: 4, IngestedFields: posting.IngestedFields{Title: "Engineer II"}},
		CompanyName: "Hooli",
	}}, apps...)
	m.applicationsRead(applicationsReadOf(m, reloaded))

	got := openHomeHighlighted(t, m)
	if id, ok := openedDetail(got); !ok || id != 30 {
		t.Fatalf("enter after the reload sent %#v, want the Initech application (30)", got)
	}
}

// The search line is always there, so opening it doesn't push the table
// down.
func TestActiveApplicationListModel_View_SearchLineDoesNotMoveTheTable(t *testing.T) {
	t.Parallel()

	m := homeWith(t, testSearchApplications())
	idle := m.render(time.Time{})
	if !strings.Contains(idle, "🔍") {
		t.Errorf("idle view has no search line:\n%s", idle)
	}
	typeHomeKeys(t, m, "/")
	if got, want := tableTop(t, m.render(time.Time{})), tableTop(t, idle); got != want {
		t.Errorf("table starts on line %d with the search open, want %d (as when idle)", got, want)
	}
}

func TestActiveApplicationListModel_Handlers_FailOnTheWrongMessage(t *testing.T) {
	t.Parallel()
	assertHandlersFailOnWrongMessage(t, newActiveApplicationListModel(nil, nil, nil))
}

func TestActiveApplicationListModel_Read_ShowsOnlyItsOwn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		from  func(m *activeApplicationListModel) message.ScreenInstance
		shown bool
	}{
		{name: "its own", from: (*activeApplicationListModel).Instance, shown: true},
		{name: "another screen's", from: func(*activeApplicationListModel) message.ScreenInstance { return 9 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := homeWith(t, testActiveApplications())
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}
			e := applicationsReadOf(m, testSearchApplications())
			e.Meta.Source.Instance = tt.from(m)

			r.Dispatch(e)

			want := len(testActiveApplications())
			if tt.shown {
				want = len(testSearchApplications())
			}
			if got := len(m.apps); got != want {
				t.Errorf("shows %d applications, want %d", got, want)
			}
		})
	}
}

// storedHome is a home screen over a store with one application,
// registered with a router, in focus or not.
func storedHome(t *testing.T, focused bool) (*activeApplicationListModel, *model.MsgRouter) {
	t.Helper()
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer").ID)
	m := newActiveApplicationListModel(s.Applications, s.Documents, documents.NewStore(t.TempDir()))
	r := model.NewMsgRouter()
	if err := r.Register(m); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if focused {
		r.Dispatch(focusOn(m.Instance()))
	}
	return m, r
}

// applicationsReadFrom is whether msgs hold m's read of its applications.
func applicationsReadFrom(t *testing.T, m *activeApplicationListModel, msgs []tea.Msg) bool {
	t.Helper()
	read := false
	for _, msg := range msgs {
		if e, ok := msg.(message.Event[message.Read[message.ActiveApplications]]); ok {
			assertEventFrom(t, e.Meta, message.Source{Instance: m.Instance()})
			read = true
		}
	}
	return read
}

func TestActiveApplicationListModel_ReadsItsApplicationsAgain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		focused bool
		msg     func(m *activeApplicationListModel) tea.Msg
		read    bool
	}{
		{name: "on coming into focus", msg: func(m *activeApplicationListModel) tea.Msg { return focusOn(m.Instance()) }, read: true},
		{name: "not on staying in focus", focused: true, msg: func(m *activeApplicationListModel) tea.Msg { return focusOn(m.Instance()) }},
		{name: "not on another screen's focus", msg: func(*activeApplicationListModel) tea.Msg { return focusOn(9) }},
		{name: "on a load request", msg: func(*activeApplicationListModel) tea.Msg {
			return message.New(message.Source{Instance: 9}, message.Subject{}, message.OpNone, time.Now().UTC(), message.Load[message.ActiveApplications]{})
		}, read: true},
		{name: "on an application changed elsewhere, out of focus", msg: func(*activeApplicationListModel) tea.Msg {
			return message.Updated(message.Source{Instance: 9}, 1, time.Now().UTC(),
				message.Application{ID: 1, Status: message.ApplicationStarted}, message.Application{ID: 1, Status: message.ApplicationRejected})
		}, read: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, r := storedHome(t, tt.focused)

			if got := applicationsReadFrom(t, m, runCmd(t, r.Dispatch(tt.msg(m)))); got != tt.read {
				t.Errorf("read = %t, want %t", got, tt.read)
			}
		})
	}
}
