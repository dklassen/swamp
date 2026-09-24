package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

func TestPostingDetailModel_New_RendersTitleAndContent(t *testing.T) {
	t.Parallel()

	p := store.Posting{ID: 1, IngestedFields: store.IngestedFields{Title: "Engineer"}}
	m := newPostingDetailModel(nil, nil, 80, 20, p, store.Application{}, false, nil, true)
	if !containsAll(m.View(), "Engineer", "No application started") {
		t.Fatalf("View() = %q, want it to contain title and no-application message", m.View())
	}
}

func TestPostingDetailModel_New_NoReviewsYet_ShowsNotReviewedBadge(t *testing.T) {
	t.Parallel()

	app := store.Application{ID: 9, PostingID: 5}
	m := newPostingDetailModel(nil, documents.NewStore(t.TempDir()), 80, 20, store.Posting{ID: 5}, app, true, nil, true)
	if !containsAll(m.View(), "Cover Letter", "[not reviewed]", "Resume") {
		t.Fatalf("View() = %q, want both documents marked [not reviewed]", m.View())
	}
}

func TestPostingDetailModel_New_WithReviews_ShowsOutcomeAndNotes(t *testing.T) {
	t.Parallel()

	app := store.Application{ID: 9, PostingID: 5}
	reviews := map[store.DocumentType]store.DocumentReview{
		store.DocumentTypeCoverLetter: {Outcome: store.ReviewOutcomeFlagged, Notes: "too generic, mention Go specifically"},
		store.DocumentTypeResume:      {Outcome: store.ReviewOutcomePassed},
	}
	m := newPostingDetailModel(nil, documents.NewStore(t.TempDir()), 80, 20, store.Posting{ID: 5}, app, true, reviews, true)
	got := m.View()
	if !containsAll(got, "[FLAGGED]", "too generic, mention Go specifically", "[PASSED]") {
		t.Fatalf("View() = %q, want FLAGGED cover letter with notes and PASSED resume", got)
	}
}

func TestPostingDetailModel_L_ReturnsNavigateForward(t *testing.T) {
	t.Parallel()

	m := newPostingDetailModel(nil, nil, 80, 20, store.Posting{ID: 5}, store.Application{}, false, nil, true)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(navigatePostingMsg)
	if !ok {
		t.Fatalf("intent = %T, want navigatePostingMsg", intent)
	}
	if got.postingID != 5 || got.direction != 1 {
		t.Fatalf("navigatePostingMsg = %+v, want {postingID:5 direction:1}", got)
	}
}

func TestPostingDetailModel_H_ReturnsNavigateBackward(t *testing.T) {
	t.Parallel()

	m := newPostingDetailModel(nil, nil, 80, 20, store.Posting{ID: 5}, store.Application{}, false, nil, true)
	_, intent := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	got, ok := intent.(navigatePostingMsg)
	if !ok || got.direction != -1 {
		t.Fatalf("intent = %+v, want navigatePostingMsg{direction: -1}", intent)
	}
}

func TestPostingDetailModel_Esc_ReturnsBackToPostingListMsg(t *testing.T) {
	t.Parallel()

	m := newPostingDetailModel(nil, nil, 80, 20, store.Posting{ID: 5}, store.Application{}, false, nil, true)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(backToPostingListMsg); !ok {
		t.Fatalf("intent = %T, want backToPostingListMsg", intent)
	}
}

func TestPostingDetailModel_A_NoApplication_ReturnsCreateCmd(t *testing.T) {
	t.Parallel()

	m := newPostingDetailModel(nil, nil, 80, 20, store.Posting{ID: 5}, store.Application{}, false, nil, true)
	cmd, intent := m.Update(runeKey('a'))
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that creates the application")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}

func TestPostingDetailModel_A_HasApplication_NoOp(t *testing.T) {
	t.Parallel()

	m := newPostingDetailModel(nil, documents.NewStore(t.TempDir()), 80, 20, store.Posting{ID: 5}, store.Application{}, true, nil, true)
	cmd, intent := m.Update(runeKey('a'))
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (application already exists)", cmd, intent)
	}
}

func TestPostingDetailModel_S_HasApplication_ReturnsEnterStatusMsg(t *testing.T) {
	t.Parallel()

	app := store.Application{PostingID: 5, Status: store.ApplicationStatusSubmitted}
	m := newPostingDetailModel(nil, documents.NewStore(t.TempDir()), 80, 20, store.Posting{ID: 5}, app, true, nil, true)
	cmd, intent := m.Update(runeKey('s'))
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(enterApplicationStatusMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterApplicationStatusMsg", intent)
	}
	if got.postingID != 5 || got.currentStatus != store.ApplicationStatusSubmitted {
		t.Fatalf("enterApplicationStatusMsg = %+v, want postingID=5 currentStatus=%s", got, store.ApplicationStatusSubmitted)
	}
}

func TestPostingDetailModel_S_NoApplication_NoOp(t *testing.T) {
	t.Parallel()

	m := newPostingDetailModel(nil, nil, 80, 20, store.Posting{ID: 5}, store.Application{}, false, nil, true)
	cmd, intent := m.Update(runeKey('s'))
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (no application yet)", cmd, intent)
	}
}

func TestPostingDetailModel_N_HasApplication_ReturnsEnterNotesMsg(t *testing.T) {
	t.Parallel()

	app := store.Application{PostingID: 5, Notes: "existing notes"}
	m := newPostingDetailModel(nil, documents.NewStore(t.TempDir()), 80, 20, store.Posting{ID: 5}, app, true, nil, true)
	cmd, intent := m.Update(runeKey('n'))
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(enterApplicationNotesMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterApplicationNotesMsg", intent)
	}
	if got.postingID != 5 || got.currentNotes != "existing notes" {
		t.Fatalf("enterApplicationNotesMsg = %+v, want postingID=5 currentNotes=%q", got, "existing notes")
	}
}

func TestPostingDetailModel_R_HasApplication_ReturnsEnterDocumentReviewSelectMsg(t *testing.T) {
	t.Parallel()

	app := store.Application{ID: 9, PostingID: 5}
	m := newPostingDetailModel(nil, documents.NewStore(t.TempDir()), 80, 20, store.Posting{ID: 5}, app, true, nil, true)
	cmd, intent := m.Update(runeKey('r'))
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(enterDocumentReviewSelectMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterDocumentReviewSelectMsg", intent)
	}
	if got.applicationID != 9 {
		t.Fatalf("enterDocumentReviewSelectMsg = %+v, want applicationID=9", got)
	}
}

func TestPostingDetailModel_R_NoApplication_NoOp(t *testing.T) {
	t.Parallel()

	m := newPostingDetailModel(nil, nil, 80, 20, store.Posting{ID: 5}, store.Application{}, false, nil, true)
	cmd, intent := m.Update(runeKey('r'))
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (no application yet)", cmd, intent)
	}
}

func TestPostingDetailModel_Resize_RebuildsViewportAtNewDimensions(t *testing.T) {
	t.Parallel()

	m := newPostingDetailModel(nil, nil, 80, 20, store.Posting{ID: 1, IngestedFields: store.IngestedFields{Title: "Engineer"}}, store.Application{}, false, nil, true)
	m.resize(40, 10)
	if m.viewport.Width != 40 {
		t.Fatalf("viewport width = %d, want 40", m.viewport.Width)
	}
	// The viewport itself is shorter than 10: it gives up whatever rows the
	// title and help need beyond chromeRows (see newPostingDetailModel).
	if lines := strings.Count(m.View(), "\n") + 1; lines > 10+chromeRows {
		t.Fatalf("View() after resize is %d lines, want at most %d (the new terminal height)", lines, 10+chromeRows)
	}
	if !strings.Contains(m.View(), "Engineer") {
		t.Fatalf("View() after resize = %q, want it to still contain the title", m.View())
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

// TestPostingDetailModel_View_HelpAdvertisesNavigationOnlyWhenAvailable
// covers #93. Prev/next posting navigation needs App.postings to contain
// this posting, which it doesn't when posting detail was reached via
// application detail's 'p' fast path. The keys already no-op safely
// there; the bug is the help line promising something that silently does
// nothing, with no indication why.
func TestPostingDetailModel_View_HelpAdvertisesNavigationOnlyWhenAvailable(t *testing.T) {
	t.Parallel()

	posting := store.Posting{ID: 1, IngestedFields: store.IngestedFields{Title: "Engineer"}}
	docs := documents.NewStore(t.TempDir())

	withSiblings := newPostingDetailModel(nil, docs, 80, 20, posting, store.Application{}, false, nil, true)
	if !strings.Contains(withSiblings.View(), "prev/next posting") {
		t.Errorf("View() with siblings available = %q, want the h/l hint present", withSiblings.View())
	}

	withoutSiblings := newPostingDetailModel(nil, docs, 80, 20, posting, store.Application{}, false, nil, false)
	if strings.Contains(withoutSiblings.View(), "prev/next posting") {
		t.Errorf("View() with no siblings = %q, want the h/l hint omitted -- pressing it does nothing", withoutSiblings.View())
	}
	// The rest of the help line must survive the conditional.
	for _, want := range []string{"o: open in browser", "esc/b: back"} {
		if !strings.Contains(withoutSiblings.View(), want) {
			t.Errorf("View() with no siblings = %q, want it to still offer %q", withoutSiblings.View(), want)
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

	p := store.Posting{ID: 5, IngestedFields: store.IngestedFields{
		Title:           "Engineer",
		Location:        "Ottawa, ON",
		DescriptionText: "We build tools for practitioners.",
	}}
	app := store.Application{ID: 9, PostingID: 5, Status: store.ApplicationStatusStarted}
	m := newPostingDetailModel(nil, documents.NewStore(t.TempDir()), 80, 60, p, app, true, nil, true)
	lines := strings.Split(ansi.Strip(m.View()), "\n")

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
		{applicationStatusLabel(store.ApplicationStatusStarted), false},
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
	p := store.Posting{ID: 5, IngestedFields: store.IngestedFields{
		Title:           "Engineer",
		JobURL:          "https://jobs.example.com/a-very-long-path/that-needs-to-wrap/somewhere-sensible",
		DescriptionText: strings.Repeat("We build tools for practitioners. ", 8),
	}}
	app := store.Application{ID: 9, PostingID: 5}
	m := newPostingDetailModel(nil, documents.NewStore(t.TempDir()), width, 60, p, app, true, nil, true)

	title := strings.Split(ansi.Strip(m.View()), "\n")[0]
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

	p := store.Posting{ID: 5, IngestedFields: store.IngestedFields{
		Title:      "Engineer",
		Department: "Engineering",
		Location:   "Ottawa, ON",
		JobURL:     "https://jobs.example.com/a-very-long-path/that-needs-to-wrap/somewhere-sensible",
	}}
	app := store.Application{ID: 9, PostingID: 5, Status: store.ApplicationStatusStarted}
	reviews := map[store.DocumentType]store.DocumentReview{
		store.DocumentTypeCoverLetter: {Outcome: store.ReviewOutcomeFlagged, Notes: "too generic"},
	}
	m := newPostingDetailModel(nil, documents.NewStore(t.TempDir()), 70, 60, p, app, true, reviews, true)
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
		{applicationStatusLabel(store.ApplicationStatusStarted), false},
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
// the temp directory's length.
func TestPostingDetailModel_View_NeverSplitsAReviewBadge(t *testing.T) {
	t.Parallel()

	docs := documents.NewStore(t.TempDir())
	app := store.Application{ID: 9, PostingID: 5}
	for width := 40; width <= 140; width++ {
		m := newPostingDetailModel(nil, docs, width, 60, store.Posting{ID: 5}, app, true, nil, true)
		view := ansi.Strip(m.viewport.View())
		if n := strings.Count(view, "[not reviewed]"); n != 2 {
			t.Errorf("width %d: found %d intact [not reviewed] badges, want 2 in view:\n%s", width, n, view)
		}
	}
}

// TestPostingDetailModel_View_FitsTheTerminal checks the whole screen --
// title, scrollable body, and help line -- fits in the terminal, so the
// title stays visible at the top. App sizes the model with listRows(), the
// terminal height minus chromeRows; the detail screen spends more than
// that on its own title and help, and the help line wraps on narrower
// terminals, so the view used to run past the bottom and push the title
// off the top.
func TestPostingDetailModel_View_FitsTheTerminal(t *testing.T) {
	t.Parallel()

	const termHeight = 30
	p := store.Posting{ID: 5, IngestedFields: store.IngestedFields{
		Title:           "Senior Developer, Fullstack",
		DescriptionText: strings.Repeat("A long description paragraph that fills the body.\n\n", 40),
	}}
	for _, width := range []int{60, 80, 110, 200} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			t.Parallel()

			m := newPostingDetailModel(nil, nil, width, termHeight-chromeRows, p, store.Application{}, false, nil, true)
			lines := strings.Split(ansi.Strip(m.View()), "\n")
			if len(lines) > termHeight {
				t.Errorf("view is %d lines, want at most %d (the terminal height)", len(lines), termHeight)
			}
			if !strings.Contains(lines[0], "Senior Developer, Fullstack") {
				t.Errorf("first line = %q, want the posting title", lines[0])
			}
			for i, line := range lines {
				if w := ansi.StringWidth(line); w > width {
					t.Errorf("line %d is %d columns, want at most %d: %q", i, w, width, line)
				}
			}
		})
	}
}
