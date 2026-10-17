package tui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
)

// activeApplicationListModel drives the active-applications screen -- the
// app's home screen (#43): every application not at a
// terminal dead-end status (rejected, offer_declined), across every
// company, in one place. It holds only its own private cursor -- the
// application list itself is domain data owned by App, passed in on
// every call, never cached here. Application-specific actions
// (editing/reviewing documents, viewing the related posting) live one
// level down, on applicationDetailModel -- entered via enter (see
// enterApplicationDetailMsg below); this screen keeps only
// status-setting as a quick shortcut for fast triage across many
// applications (see the #86 follow-up).

// Home screen column widths; Title takes what's left, which is 30 at
// 100 columns, the width the home screen was first sized for.
const (
	homeCompanyColWidth = 16
	// homeAgeColWidth fits up to "999d".
	homeAgeColWidth = 4
	// homeNextColWidth fits the longest next step, "withdraw?".
	homeNextColWidth = 9
)

// Measured rather than counted, so a new status or document type widens
// its column instead of being cut off.
var (
	homeStatusColWidth = longestStatusLabel()
	homeReviewColWidth = lipgloss.Width(reviewGlyphSummary(nil))
)

func longestStatusLabel() int {
	longest := 0
	for _, status := range application.All() {
		longest = max(longest, lipgloss.Width(applicationStatusLabel(status)))
	}
	return longest
}

type activeApplicationListModel struct {
	cursor int
	// searching is whether the '/' prompt is open and taking keys; query
	// is what's been typed into it.
	searching bool
	query     string
}

func newActiveApplicationListModel() activeApplicationListModel {
	return activeApplicationListModel{}
}

// enterApplicationDetailMsg signals that App should switch to the
// application-detail screen for this application.
type enterApplicationDetailMsg struct {
	application posting.ActiveApplication
}

// enterApplicationExportMsg signals that App should switch to the
// export screen for this application, where the user picks the
// destination folder for its generated PDFs.
type enterApplicationExportMsg struct {
	application posting.ActiveApplication
}

// Update handles one key press. The returned tea.Cmd (if non-nil) is a
// real async command for App to run through bubbletea as usual. The
// returned tea.Msg (if non-nil) is an intent for App to apply
// synchronously -- see companyListModel.Update for the same convention.
func (m *activeApplicationListModel) Update(msg tea.KeyMsg, apps []posting.ActiveApplication) (tea.Cmd, tea.Msg) {
	if m.searching {
		return m.updateSearch(msg, apps)
	}
	// Commands act on the matches a kept search shows, not the whole list.
	visible := m.visible(apps)
	switch {
	case msg.String() == "/":
		m.searching = true
	case msg.Type == tea.KeyEsc:
		m.clearSearch(apps)
	case msg.Type == tea.KeyDown, msg.String() == "j":
		if m.cursor < len(visible)-1 {
			m.cursor++
		}
	case msg.Type == tea.KeyUp, msg.String() == "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case msg.String() == "q":
		return tea.Quit, nil
	case msg.String() == "s":
		if m.cursor < len(visible) {
			a := visible[m.cursor]
			return nil, enterApplicationStatusMsg{applicationID: a.ID}
		}
	case msg.String() == "e":
		if m.cursor < len(visible) {
			return nil, enterApplicationExportMsg{application: visible[m.cursor]}
		}
	case msg.String() == "d":
		if m.cursor < len(visible) {
			return nil, enterApplicationDeleteMsg{application: visible[m.cursor]}
		}
	case msg.Type == tea.KeyEnter:
		if m.cursor < len(visible) {
			return nil, enterApplicationDetailMsg{application: visible[m.cursor]}
		}
	}
	return nil, nil
}

// updateSearch handles a key while the '/' prompt is open. Every edit
// to the query re-filters straight away and puts the cursor back on the
// first match.
func (m *activeApplicationListModel) updateSearch(msg tea.KeyMsg, apps []posting.ActiveApplication) (tea.Cmd, tea.Msg) {
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace:
		m.query += string(msg.Runes)
		m.cursor = 0
	case tea.KeyBackspace:
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
			m.cursor = 0
		} else {
			m.searching = false
		}
	case tea.KeyEsc:
		m.searching = false
		m.clearSearch(apps)
	case tea.KeyDown, tea.KeyCtrlN:
		if m.cursor < len(m.visible(apps))-1 {
			m.cursor++
		}
	case tea.KeyUp, tea.KeyCtrlP:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyEnter:
		// Keeps the matches, so letters act on them instead of being typed.
		m.searching = false
	}
	return nil, nil
}

// clearSearch drops the query, keeping the highlighted application
// highlighted in the full list.
func (m *activeApplicationListModel) clearSearch(apps []posting.ActiveApplication) {
	cursor := 0
	if visible := m.visible(apps); m.cursor < len(visible) {
		id := visible[m.cursor].ID
		cursor = max(slices.IndexFunc(apps, func(a posting.ActiveApplication) bool { return a.ID == id }), 0)
	}
	m.query = ""
	m.cursor = cursor
}

// visible is the part of apps the query matches: a case-insensitive
// substring of the company or the posting title, in the order given.
// With no query, it's all of them.
func (m *activeApplicationListModel) visible(apps []posting.ActiveApplication) []posting.ActiveApplication {
	if m.query == "" {
		return apps
	}
	q := strings.ToLower(m.query)
	var matches []posting.ActiveApplication
	for _, a := range apps {
		if strings.Contains(strings.ToLower(a.CompanyName), q) || strings.Contains(strings.ToLower(a.Posting.Title), q) {
			matches = append(matches, a)
		}
	}
	return matches
}

// View renders the list in height terminal rows (App.screenRows).
// progress holds each application's document progress by application ID,
// from which, with its LatestReviews, each row's next step is derived
// (see nextStep), and now is what each row's Age counts up to.
func (m *activeApplicationListModel) View(apps []posting.ActiveApplication, progress map[int64]map[document.Type]documentProgress, now time.Time, width, height int) string {
	var b strings.Builder
	help := helpStyle.Render("↑/↓ (j/k): select  enter: application detail  /: search  s: status  e: export PDFs  d: delete  tab: companies  q: quit")
	switch {
	case m.searching:
		help = helpStyle.Render("type to filter  ↑/↓ (ctrl+n/p): select  enter: keep results  esc: clear")
	case m.query != "":
		help = helpStyle.Render("↑/↓ (j/k): select  enter: application detail  /: edit search  esc: clear search  s: status  e: export PDFs  d: delete  q: quit")
	}
	visible := m.visible(apps)
	// A reload landing while the prompt is open can shrink visible under
	// the cursor.
	cursor := min(m.cursor, max(len(visible)-1, 0))
	var prompt string
	if len(apps) > 0 {
		prompt = searchLine(m.searching, m.query, len(visible), len(apps))
		b.WriteString(prompt + "\n")
	}
	switch {
	case len(apps) == 0:
		b.WriteString("No active applications. Press tab to browse companies.\n")
	case len(visible) == 0:
		fmt.Fprintf(&b, "No applications match %q.\n", m.query)
	default:
		start, end := visibleWindow(cursor, len(visible), tableRows(height, help, prompt))
		cursorRow := cursor - start
		t := table.New().
			Headers("Company", "Title", "Age", "Next", "Status", "Review").
			StyleFunc(func(row, _ int) lipgloss.Style {
				style := lipgloss.NewStyle().Padding(0, 1)
				if row == cursorRow {
					return style.Inherit(cursorStyle)
				}
				return style
			})
		titleWidth := flexColWidth(width, homeCompanyColWidth, homeAgeColWidth, homeNextColWidth, homeStatusColWidth, homeReviewColWidth)
		for i := start; i < end; i++ {
			a := visible[i]
			t.Row(
				padCol(a.CompanyName, homeCompanyColWidth),
				padCol(a.Posting.Title, titleWidth),
				padCol(statusAge(a.StatusSince, now), homeAgeColWidth),
				padCol(nextStep(a, progress[a.ID]), homeNextColWidth),
				padCol(applicationStatusLabel(a.Status), homeStatusColWidth),
				padCol(reviewGlyphSummary(a.LatestReviews), homeReviewColWidth),
			)
		}
		b.WriteString(t.Render() + "\n")
	}
	b.WriteString(help)
	return b.String()
}

// selected is the ID of the application under the cursor, or 0. The
// cursor indexes what the search shows, which is all of apps when there's
// no query.
func (m *activeApplicationListModel) selected(apps []posting.ActiveApplication) int64 {
	if visible := m.visible(apps); m.cursor < len(visible) {
		return visible[m.cursor].ID
	}
	return 0
}

// keepCursorOn moves the cursor to application id after a reload, since
// rows above it may have come or gone; if it's gone, the cursor stays in
// range.
func (m *activeApplicationListModel) keepCursorOn(id int64, apps []posting.ActiveApplication) {
	visible := m.visible(apps)
	if i := slices.IndexFunc(visible, func(a posting.ActiveApplication) bool { return a.ID == id }); i >= 0 {
		m.cursor = i
		return
	}
	m.resetCursorIfOutOfBounds(len(visible))
}

// resetCursorIfOutOfBounds resets the cursor to the top if it's no
// longer a valid index into n applications -- used after a status
// change moves an application to a terminal status and it drops out of
// the list.
func (m *activeApplicationListModel) resetCursorIfOutOfBounds(n int) {
	if m.cursor >= n {
		m.cursor = 0
	}
}

// documentProgress is one document's state on disk, for nextStep: whether
// it's drafted, and whether its current content has been exported as a
// PDF (document.Export.IsCurrent).
type documentProgress struct {
	drafted  bool
	exported bool
}

// nextStep is what a started application is waiting on, for the home
// screen's Next column (#164). Only started applications have one, except
// that a later-stage application whose posting has closed shows
// "(closed)": sync deliberately leaves those alone (#105), and this is
// the only place the screen says the posting is gone. For a started
// application, the first rule that applies wins:
//
//   - withdraw? -- the posting has closed;
//   - draft     -- a document isn't drafted yet;
//   - revise    -- a current review is flagged;
//   - review    -- a document has no current review;
//   - export    -- every document passed, but one isn't exported as it
//     stands now;
//   - submit    -- everything passed and is exported.
//
// a.LatestReviews must already be filtered to current reviews (see
// documents.Current), as loadActiveApplications does.
func nextStep(a posting.ActiveApplication, docs map[document.Type]documentProgress) string {
	closed := a.Posting.ListingStatus == "closed"
	if a.Status != application.Started {
		if closed {
			return "(closed)"
		}
		return ""
	}
	if closed {
		return "withdraw?"
	}
	documentTypes := document.Types()
	for _, documentType := range documentTypes {
		if !docs[documentType].drafted {
			return "draft"
		}
	}
	for _, documentType := range documentTypes {
		if review, ok := a.LatestReviews[documentType]; ok && review.Outcome == document.Flagged {
			return "revise"
		}
	}
	for _, documentType := range documentTypes {
		if _, ok := a.LatestReviews[documentType]; !ok {
			return "review"
		}
	}
	for _, documentType := range documentTypes {
		if !docs[documentType].exported {
			return "export"
		}
	}
	return "submit"
}

// statusAge renders how long an application has been at its current
// status as whole days ("21d"), or "" when either time is unknown.
func statusAge(since, now time.Time) string {
	if since.IsZero() || now.IsZero() {
		return ""
	}
	return strconv.Itoa(int(now.Sub(since).Hours()/24)) + "d"
}

// orderForHome sorts apps in place for the home screen (#164): started
// applications first, longest at their status first, then everything
// else in the order it came in (the store's most-recently-changed
// first).
func orderForHome(apps []posting.ActiveApplication) {
	slices.SortStableFunc(apps, func(a, b posting.ActiveApplication) int {
		aStarted, bStarted := a.Status == application.Started, b.Status == application.Started
		switch {
		case aStarted && bStarted:
			return a.StatusSince.Compare(b.StatusSince)
		case aStarted:
			return -1
		case bStarted:
			return 1
		}
		return 0
	})
}

// documentProgressOf is, for nextStep, whether each of applicationID's
// documents is drafted, and whether its current content was exported
// (#188). A read failure is an error rather than "not exported" (see
// documents.Current).
func documentProgressOf(documentService *service.DocumentService, docs *documents.Store, applicationID int64) (map[document.Type]documentProgress, error) {
	exports, err := documentService.LatestExports(context.Background(), applicationID)
	if err != nil {
		return nil, err
	}
	status := docs.Status(applicationID)
	currentExports, err := documents.Current(status, exports, document.Export.IsCurrent)
	if err != nil {
		return nil, err
	}
	progress := make(map[document.Type]documentProgress, len(document.Types()))
	for _, documentType := range document.Types() {
		doc, err := status.Doc(documentType)
		if err != nil {
			return nil, err
		}
		_, exported := currentExports[documentType]
		progress[documentType] = documentProgress{drafted: doc.Exists, exported: exported}
	}
	return progress, nil
}
