package tui

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// detailOf is posting detail on p and app in width by rows, able to step
// through siblings.
func detailOf(docs *documents.Store, p message.Posting, app message.Application, siblings []int64, width, rows int) *postingDetailModel {
	return newPostingDetailModel(nil, nil, nil, docs, message.PostingDetail{Posting: p, Application: app, Siblings: siblings}, message.ContentSize{Width: width, Rows: rows})
}

// withReviews gives m reviews of its application, as its read would.
func withReviews(m *postingDetailModel, reviews ...message.Review) *postingDetailModel {
	m.reviewsRead(message.New(m.src(), message.Subject{Type: message.TypeApplication, ID: m.application.ID}, message.OpNone, time.Now().UTC(),
		message.Read[message.ReviewList]{Value: message.ReviewList{Reviews: reviews}}))
	return m
}

func detailView(m *postingDetailModel) string { return m.View().Content }

// pressDetail sends key to m and returns the messages its command produces.
func pressDetail(t *testing.T, m *postingDetailModel, key tea.KeyPressMsg) []tea.Msg {
	t.Helper()
	return runCmd(t, m.keyPressed(key))
}

var (
	testDetailPosting     = message.Posting{ID: 5, Title: "Engineer", JobURL: "https://example.com/5"}
	testDetailApplication = message.Application{ID: 9, PostingID: 5, Status: message.ApplicationSubmitted, Notes: "existing notes"}
)

func TestPostingDetailModel_New_RendersContent(t *testing.T) {
	t.Parallel()

	m := detailOf(nil, testDetailPosting, message.Application{}, nil, 80, 20)
	if !strings.Contains(detailView(m), "No application started") {
		t.Fatalf("View() = %q, want it to contain the no-application message", detailView(m))
	}
}

func TestPostingDetailModel_New_NoReviewsYet_ShowsNotReviewedBadge(t *testing.T) {
	t.Parallel()

	m := detailOf(documents.NewStore(t.TempDir()), testDetailPosting, testDetailApplication, nil, 80, 20)
	if !containsAll(detailView(m), "Cover Letter", "[not reviewed]", "Resume") {
		t.Fatalf("View() = %q, want both documents marked [not reviewed]", detailView(m))
	}
}

func TestPostingDetailModel_WithReviews_ShowsOutcomeAndNotes(t *testing.T) {
	t.Parallel()

	m := withReviews(detailOf(documents.NewStore(t.TempDir()), testDetailPosting, testDetailApplication, nil, 80, 20),
		message.Review{ApplicationID: 9, DocumentType: message.DocumentCoverLetter, Outcome: message.ReviewFlagged, Notes: "too generic, mention Go specifically"},
		message.Review{ApplicationID: 9, DocumentType: message.DocumentResume, Outcome: message.ReviewPassed},
	)
	if got := detailView(m); !containsAll(got, "[FLAGGED]", "too generic, mention Go specifically", "[PASSED]") {
		t.Fatalf("View() = %q, want FLAGGED cover letter with notes and PASSED resume", got)
	}
}

// A late read for an application the posting no longer has mustn't show.
func TestPostingDetailModel_ReviewsOfAnotherApplication_AreDropped(t *testing.T) {
	t.Parallel()

	m := detailOf(documents.NewStore(t.TempDir()), testDetailPosting, testDetailApplication, nil, 80, 20)
	m.reviewsRead(message.New(m.src(), message.Subject{Type: message.TypeApplication, ID: 99}, message.OpNone, time.Now().UTC(),
		message.Read[message.ReviewList]{Value: message.ReviewList{Reviews: []message.Review{{ApplicationID: 99, DocumentType: message.DocumentCoverLetter, Outcome: message.ReviewFlagged}}}}))

	if strings.Contains(detailView(m), "[FLAGGED]") {
		t.Errorf("View() = %q, want no review of application 99", detailView(m))
	}
}

func TestPostingDetailModel_PrevNext_ReplaceItWithTheSibling(t *testing.T) {
	t.Parallel()

	siblings := []int64{4, 5, 6}
	tests := []struct {
		name     string
		siblings []int64
		key      tea.KeyPressMsg
		want     int64 // 0 for no step
	}{
		{name: "right", siblings: siblings, key: tea.KeyPressMsg{Code: tea.KeyRight}, want: 6},
		{name: "l", siblings: siblings, key: runeKey('l'), want: 6},
		{name: "left", siblings: siblings, key: tea.KeyPressMsg{Code: tea.KeyLeft}, want: 4},
		{name: "h", siblings: siblings, key: runeKey('h'), want: 4},
		{name: "right at the last", siblings: []int64{4, 5}, key: tea.KeyPressMsg{Code: tea.KeyRight}},
		{name: "left at the first", siblings: []int64{5, 6}, key: tea.KeyPressMsg{Code: tea.KeyLeft}},
		{name: "no siblings", key: tea.KeyPressMsg{Code: tea.KeyRight}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := detailOf(nil, testDetailPosting, message.Application{}, tt.siblings, 80, 20)

			got := pressDetail(t, m, tt.key)
			if tt.want == 0 {
				if len(got) != 0 {
					t.Fatalf("%s sent %#v, want nothing", tt.name, got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("%s sent %#v, want one Nav", tt.name, got)
			}
			nav, ok := got[0].(message.Event[message.Nav[message.PostingDetail]])
			if !ok {
				t.Fatalf("%s sent %T, want a Nav to posting detail", tt.name, got[0])
			}
			assertEventFrom(t, nav.Meta, m.src())
			if want := (message.Subject{Type: message.TypePosting, ID: tt.want}); nav.Meta.Subject != want {
				t.Errorf("Subject = %+v, want %+v", nav.Meta.Subject, want)
			}
			want := message.Nav[message.PostingDetail]{Op: message.NavReplace, Screen: message.ScreenPostingDetail,
				Value: message.PostingDetail{Posting: message.Posting{ID: tt.want}, Siblings: tt.siblings}}
			if diff := cmp.Diff(want, nav.Payload); diff != "" {
				t.Errorf("Nav (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPostingDetailModel_EscOrB_Pops(t *testing.T) {
	t.Parallel()

	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEsc}, runeKey('b')} {
		t.Run(key.String(), func(t *testing.T) {
			t.Parallel()
			m := detailOf(nil, testDetailPosting, message.Application{}, nil, 80, 20)

			if got := pressDetail(t, m, key); !popsIn(got, m.Instance()) {
				t.Errorf("%s sent %#v, want a pop of the detail", key, got)
			}
		})
	}
}

// Status, notes and the review picker are still App's to open.
func TestPostingDetailModel_ApplicationKeys_AskAppToOpenItsScreens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  tea.KeyPressMsg
		want tea.Msg
	}{
		{key: runeKey('s'), want: enterApplicationStatusMsg{applicationID: 9}},
		{key: runeKey('n'), want: enterApplicationNotesMsg{applicationID: 9, currentNotes: "existing notes"}},
		{key: runeKey('r'), want: enterDocumentReviewSelectMsg{applicationID: 9}},
	}
	for _, tt := range tests {
		t.Run(tt.key.String(), func(t *testing.T) {
			t.Parallel()
			m := detailOf(documents.NewStore(t.TempDir()), testDetailPosting, testDetailApplication, nil, 80, 20)

			got := pressDetail(t, m, tt.key)
			if diff := cmp.Diff([]tea.Msg{tt.want}, got, cmp.AllowUnexported(enterApplicationStatusMsg{}, enterApplicationNotesMsg{}, enterDocumentReviewSelectMsg{})); diff != "" {
				t.Errorf("%s sent (-want +got):\n%s", tt.key, diff)
			}
		})
	}
}

func TestPostingDetailModel_ApplicationKeys_WithNoApplication_DoNothing(t *testing.T) {
	t.Parallel()

	for _, key := range []tea.KeyPressMsg{runeKey('s'), runeKey('n'), runeKey('r')} {
		t.Run(key.String(), func(t *testing.T) {
			t.Parallel()
			m := detailOf(nil, testDetailPosting, message.Application{}, nil, 80, 20)

			if got := pressDetail(t, m, key); len(got) != 0 {
				t.Errorf("%s sent %#v, want nothing: there's no application", key, got)
			}
		})
	}
}

func TestPostingDetailModel_A_WithAnApplication_DoesNothing(t *testing.T) {
	t.Parallel()

	m := detailOf(documents.NewStore(t.TempDir()), testDetailPosting, testDetailApplication, nil, 80, 20)
	if cmd := m.keyPressed(runeKey('a')); cmd != nil {
		t.Errorf("a with an application returned %v, want nil", cmd)
	}
}

// detailWith is posting detail registered on a router, for a posting in
// the store with an application whose cover letter was reviewed, and a
// score.
func detailWith(t *testing.T) (*postingDetailModel, *model.MsgRouter, testutil.TestApp) {
	t.Helper()
	ctx := context.Background()
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	p := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	app := mustCreateApplication(t, s, p.ID)
	docs := documents.NewStore(t.TempDir())
	if _, err := docs.EnsureDir(app.ID); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(mustDoc(t, docs.Status(app.ID), document.CoverLetter).Path, []byte("Dear Acme"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := createReview(ctx, s, app.ID, document.CoverLetter, "Dear Acme", document.Flagged, "say why Acme"); err != nil {
		t.Fatalf("Review: %v", err)
	}
	if _, err := recordScores(ctx, s, score.Provenance{Model: "claude", ProfileSHA256: "def"}, []score.Entry{{PostingID: p.ID, Score: 4, Reason: "fits", ContentSHA256: p.ContentSHA256}}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	m := newPostingDetailModel(s.Postings, s.Documents, s.Scores, docs, message.PostingDetail{Posting: message.Posting{ID: p.ID}}, message.ContentSize{Width: 100, Rows: 60})
	r := model.NewMsgRouter()
	if err := r.Register(m); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return m, r, s
}

// settle dispatches msgs through r, and what their handlers send, until
// nothing more comes.
func settle(t *testing.T, r *model.MsgRouter, msgs []tea.Msg) {
	t.Helper()
	for len(msgs) > 0 {
		var next []tea.Msg
		for _, msg := range msgs {
			next = append(next, runCmd(t, r.Dispatch(msg))...)
		}
		msgs = next
	}
}

func TestPostingDetailModel_Init_ReadsThePostingItsApplicationReviewsAndScores(t *testing.T) {
	t.Parallel()

	m, r, _ := detailWith(t)
	settle(t, r, runCmd(t, m.Init()))

	if m.posting.Title != "Engineer" {
		t.Errorf("posting after Init = %+v, want Engineer read in full", m.posting)
	}
	if got := detailView(m); !containsAll(got, applicationStatusLabel(application.Started), "[FLAGGED]", "say why Acme", "4 · fits") {
		t.Errorf("View() after Init = %q, want its application, the cover letter's review and the score", got)
	}
}

func TestPostingDetailModel_LoadRequests_SendTheirReads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		load tea.Msg
		want func(msg tea.Msg) bool
	}{
		{
			name: "posting application",
			load: message.New(message.Source{Instance: 9}, message.Subject{}, message.OpNone, time.Now().UTC(), message.Load[message.PostingApplication]{}),
			want: func(msg tea.Msg) bool {
				_, ok := msg.(message.Event[message.Read[message.PostingApplication]])
				return ok
			},
		},
		{
			name: "posting scores",
			load: message.New(message.Source{Instance: 9}, message.Subject{}, message.OpNone, time.Now().UTC(), message.Load[message.PostingScores]{}),
			want: func(msg tea.Msg) bool { _, ok := msg.(message.Event[message.Read[message.PostingScores]]); return ok },
		},
		{
			name: "review list",
			load: message.New(message.Source{Instance: 9}, message.Subject{}, message.OpNone, time.Now().UTC(), message.Load[message.ReviewList]{}),
			want: func(msg tea.Msg) bool { _, ok := msg.(message.Event[message.Read[message.ReviewList]]); return ok },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, r, _ := detailWith(t)
			settle(t, r, runCmd(t, m.Init()))

			got := runCmd(t, r.Dispatch(tt.load))
			if len(got) != 1 || !tt.want(got[0]) {
				t.Fatalf("a %s load sent %#v, want its read", tt.name, got)
			}
			assertEventFrom(t, got[0].(interface{ Metadata() message.Meta }).Metadata(), m.src())
		})
	}
}

// A write to the posting's application shows whoever made it, including
// one made while another screen covers the detail.
func TestPostingDetailModel_ApplicationChanged_AboutItsPosting_PatchesIt(t *testing.T) {
	t.Parallel()

	interviewing := message.Application{ID: 9, PostingID: 5, Status: message.ApplicationInterviewing}
	tests := []struct {
		name    string
		start   message.Application
		changed tea.Msg
		want    message.Application
	}{
		{name: "created", changed: message.Created(message.Source{Instance: 7}, 9, time.Now().UTC(), interviewing), want: interviewing},
		{name: "updated", start: testDetailApplication, changed: message.Updated(message.Source{Instance: 7}, 9, time.Now().UTC(), testDetailApplication, interviewing), want: interviewing},
		{name: "deleted", start: testDetailApplication, changed: message.Deleted(message.Source{Instance: 7}, 9, time.Now().UTC(), testDetailApplication)},
		{name: "another posting's", start: testDetailApplication, changed: message.Updated(message.Source{Instance: 7}, 10, time.Now().UTC(),
			message.Application{ID: 10, PostingID: 6}, message.Application{ID: 10, PostingID: 6, Status: message.ApplicationRejected}), want: testDetailApplication},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := detailOf(documents.NewStore(t.TempDir()), testDetailPosting, tt.start, nil, 80, 40)
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}

			r.Dispatch(tt.changed)
			if m.application != tt.want {
				t.Errorf("application = %+v, want %+v", m.application, tt.want)
			}
		})
	}
}

func TestPostingDetailModel_Handlers_FailOnTheWrongMessage(t *testing.T) {
	t.Parallel()
	assertHandlersFailOnWrongMessage(t, detailOf(nil, testDetailPosting, message.Application{}, nil, 80, 20))
}

func TestPostingDetailModel_Resize_RebuildsViewportAtNewDimensions(t *testing.T) {
	t.Parallel()

	m := detailOf(nil, message.Posting{ID: 1, Title: "Engineer"}, message.Application{}, nil, 80, 20)
	m.resize(message.New(message.Source{}, message.Subject{}, message.OpNone, time.Now().UTC(), message.Signal[message.ContentSize]{Value: message.ContentSize{Width: 40, Rows: 10}}))
	if m.viewport.Width() != 40 {
		t.Fatalf("viewport width = %d, want 40", m.viewport.Width())
	}
	// The height is every row the screen gets, so the whole view -- viewport
	// and help -- fits in it (see setHeight).
	if lines := strings.Count(detailView(m), "\n") + 1; lines > 10 {
		t.Fatalf("View() after resize is %d lines, want at most 10 (the new height)", lines)
	}
	if !strings.Contains(detailView(m), "Application") {
		t.Fatalf("View() after resize = %q, want it to still contain the body", detailView(m))
	}
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// Prev/next needs the posting among the siblings it was opened with, which
// it isn't when opened outside the posting list (#93): the help line
// mustn't promise a move that does nothing.
func TestPostingDetailModel_View_HelpAdvertisesNavigationOnlyWhenAvailable(t *testing.T) {
	t.Parallel()

	docs := documents.NewStore(t.TempDir())

	withSiblings := detailOf(docs, testDetailPosting, message.Application{}, []int64{4, 5}, 80, 20)
	if !strings.Contains(detailView(withSiblings), "prev/next posting") {
		t.Errorf("View() with siblings available = %q, want the h/l hint present", detailView(withSiblings))
	}

	withoutSiblings := detailOf(docs, testDetailPosting, message.Application{}, nil, 80, 20)
	if strings.Contains(detailView(withoutSiblings), "prev/next posting") {
		t.Errorf("View() with no siblings = %q, want the h/l hint omitted -- pressing it does nothing", detailView(withoutSiblings))
	}
	// The rest of the help line must survive the conditional.
	for _, want := range []string{"o: open in browser", "esc/b: back"} {
		if !strings.Contains(detailView(withoutSiblings), want) {
			t.Errorf("View() with no siblings = %q, want it to still offer %q", detailView(withoutSiblings), want)
		}
	}
}

// TestPostingDetailModel_View_GroupsContentIntoSections checks the detail
// body is split into Posting, Application, and Description sections, in
// that order, with each piece of content under its own heading -- the
// metadata used to run straight into the application state and the
// description with nothing marking where one ended and the next began.
func TestPostingDetailModel_View_GroupsContentIntoSections(t *testing.T) {
	t.Parallel()

	p := message.Posting{ID: 5, Title: "Engineer", Location: "Ottawa, ON", DescriptionText: "We build tools for practitioners."}
	app := message.Application{ID: 9, PostingID: 5, Status: message.ApplicationStarted}
	m := detailOf(documents.NewStore(t.TempDir()), p, app, nil, 80, 60)
	lines := strings.Split(ansi.Strip(detailView(m)), "\n")

	// Each entry must appear on a later line than the one before it.
	// Headings are matched as headings, not substrings -- "Posting" also
	// appears in the temp directory path of the document lines.
	order := []struct {
		text    string
		heading bool
	}{
		{"Posting", true},
		{"Ottawa, ON", false},
		{"Application", true},
		{applicationStatusLabel(application.Started), false},
		{"Cover Letter", false},
		{"Description", true},
		{"We build tools for practitioners.", false},
	}
	prev := -1
	for _, want := range order {
		idx := -1
		for i := prev + 1; i < len(lines); i++ {
			if want.heading && isSectionHeading(lines[i], want.text) || !want.heading && strings.Contains(lines[i], want.text) {
				idx = i
				break
			}
		}
		if idx < 0 {
			t.Fatalf("%q (heading: %v) not found after line %d in view:\n%s", want.text, want.heading, prev, strings.Join(lines, "\n"))
		}
		prev = idx
	}
}

// isSectionHeading reports whether line is the section heading for name:
// the name, then a horizontal rule on the same line.
func isSectionHeading(line, name string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), name+" ─")
}

// TestPostingDetailModel_View_PadsTitleAndBody checks the title and the
// scrollable body keep a two-column margin on both sides: no text starts
// in the first two columns or runs into the last two, including wrapped
// description text and long values like URLs and document paths.
func TestPostingDetailModel_View_PadsTitleAndBody(t *testing.T) {
	t.Parallel()

	const width, pad = 60, 2
	p := message.Posting{
		ID:              5,
		Title:           "Engineer",
		JobURL:          "https://jobs.example.com/a-very-long-path/that-needs-to-wrap/somewhere-sensible",
		DescriptionText: strings.Repeat("We build tools for practitioners. ", 8),
	}
	app := message.Application{ID: 9, PostingID: 5}
	m := detailOf(documents.NewStore(t.TempDir()), p, app, nil, width, 60)

	title := strings.Split(ansi.Strip(detailView(m)), "\n")[0]
	body := strings.Split(ansi.Strip(m.viewport.View()), "\n")
	for i, line := range append([]string{title}, body...) {
		text := strings.TrimRight(line, " ")
		if text == "" {
			continue
		}
		if indent := len(text) - len(strings.TrimLeft(text, " ")); indent < pad {
			t.Errorf("line %d starts at column %d, want at least %d: %q", i, indent, pad, text)
		}
		if w := ansi.StringWidth(text); w > width-pad {
			t.Errorf("line %d runs to column %d, want at most %d: %q", i, w, width-pad, text)
		}
	}
}

// TestPostingDetailModel_View_AlignsFieldValues checks field values line
// up in one column across the Posting and Application sections, and that
// a value too long for one line (a URL, a document path with its review
// badge) continues under its own value column rather than wrapping back
// to the margin underneath the labels.
func TestPostingDetailModel_View_AlignsFieldValues(t *testing.T) {
	t.Parallel()

	p := message.Posting{
		ID:         5,
		Title:      "Engineer",
		Department: "Engineering",
		Location:   "Ottawa, ON",
		JobURL:     "https://jobs.example.com/a-very-long-path/that-needs-to-wrap/somewhere-sensible",
	}
	app := message.Application{ID: 9, PostingID: 5, Status: message.ApplicationStarted}
	m := withReviews(detailOf(documents.NewStore(t.TempDir()), p, app, nil, 70, 60),
		message.Review{ApplicationID: 9, DocumentType: message.DocumentCoverLetter, Outcome: message.ReviewFlagged, Notes: "too generic"})
	lines := strings.Split(ansi.Strip(m.viewport.View()), "\n")

	// column is where text starts on its line or, for text on a wrapped
	// continuation line (which may begin partway through a value), where
	// that line's text starts.
	column := func(text string, continuation bool) int {
		t.Helper()
		for _, line := range lines {
			if i := strings.Index(line, text); i >= 0 {
				if continuation {
					return len(line) - len(strings.TrimLeft(line, " "))
				}
				return ansi.StringWidth(line[:i])
			}
		}
		t.Fatalf("%q not found in view:\n%s", text, strings.Join(lines, "\n"))
		return -1
	}

	want := column("Engineering", false)
	for _, value := range []struct {
		text         string
		continuation bool
	}{
		{"Ottawa, ON", false},
		{"https://jobs.example.com", false},
		{"somewhere-sensible", true}, // the Job URL's wrapped continuation
		{applicationStatusLabel(application.Started), false},
		{"not found", false},          // cover letter and resume rows
		{"[FLAGGED]", true},           // wraps past the cover letter's long temp path
		{"Notes: too generic", false}, // the review's notes
	} {
		if got := column(value.text, value.continuation); got != want {
			t.Errorf("%q starts at column %d, want %d (the value column) in view:\n%s", value.text, got, want, strings.Join(lines, "\n"))
		}
	}
}

// TestPostingDetailModel_View_NeverSplitsAReviewBadge checks a document's
// review badge stays on one line however the path before it wraps --
// "[not reviewed]" contains a space, so plain word wrapping could break
// it in two. Swept across widths because where the path wraps depends on
// the temp directory's length, down to the narrowest terminal whose inner
// width still fits the badge at all -- below that, the value column is
// narrower than the badge.
func TestPostingDetailModel_View_NeverSplitsAReviewBadge(t *testing.T) {
	t.Parallel()

	docs := documents.NewStore(t.TempDir())
	app := message.Application{ID: 9, PostingID: 5}
	minWidth := len("[not reviewed]") + 2*detailPadding
	for width := minWidth; width <= 140; width++ {
		// Tall enough to show the whole body: at the narrowest widths the
		// path wraps a character or two per line.
		m := detailOf(docs, message.Posting{ID: 5}, app, nil, width, 1000)
		view := ansi.Strip(m.viewport.View())
		if n := strings.Count(view, "[not reviewed]"); n != 2 {
			t.Errorf("width %d: found %d intact [not reviewed] badges, want 2 in view:\n%s", width, n, view)
		}
	}
}

// TestPostingDetailModel_View_FitsTheTerminal checks the whole screen --
// scrollable body and help line -- fits in the rows App leaves it (see
// App.screenRows), so App's header stays visible at the top. The help line
// wraps on narrower terminals, so a fixed allowance for it used to let the
// view run past the bottom and push the title off the top.
func TestPostingDetailModel_View_FitsTheTerminal(t *testing.T) {
	t.Parallel()

	const termHeight = 30
	p := message.Posting{
		ID:              5,
		Title:           "Senior Developer, Fullstack",
		DescriptionText: strings.Repeat("A long description paragraph that fills the body.\n\n", 40),
	}
	for _, width := range []int{60, 80, 110, 200} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			t.Parallel()

			m := detailOf(nil, p, message.Application{}, []int64{5}, width, termHeight)
			lines := strings.Split(ansi.Strip(detailView(m)), "\n")
			if len(lines) > termHeight {
				t.Errorf("view is %d lines, want at most %d (the terminal height)", len(lines), termHeight)
			}
			for i, line := range lines {
				if w := ansi.StringWidth(line); w > width {
					t.Errorf("line %d is %d columns, want at most %d: %q", i, w, width, line)
				}
			}
		})
	}
}
