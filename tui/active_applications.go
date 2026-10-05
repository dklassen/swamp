package tui

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// activeApplicationListModel drives the active-applications screen -- the
// app's home screen (see decisions.log, #43): every application not at a
// terminal dead-end status (rejected, offer_declined), across every
// company, in one place. It holds only its own private cursor -- the
// application list itself is domain data owned by App, passed in on
// every call, never cached here. Application-specific actions
// (editing/reviewing documents, viewing the related posting) live one
// level down, on applicationDetailModel -- entered via enter (see
// enterApplicationDetailMsg below); this screen keeps only
// status-setting as a quick shortcut for fast triage across many
// applications (see decisions.log, the #86 follow-up).
// Home screen column widths, narrower than the posting list's so the
// widest row fits 100 columns with the Age and Next columns (#164).
const (
	homeCompanyColWidth = 16
	homeTitleColWidth   = 30
)

type activeApplicationListModel struct {
	cursor int
}

func newActiveApplicationListModel() activeApplicationListModel {
	return activeApplicationListModel{}
}

// enterApplicationDetailMsg signals that App should switch to the
// application-detail screen for this application.
type enterApplicationDetailMsg struct {
	application store.ApplicationView
}

// enterApplicationExportMsg signals that App should switch to the
// export screen for this application, where the user picks the
// destination folder for its generated PDFs.
type enterApplicationExportMsg struct {
	application store.ApplicationView
}

// Update handles one key press. The returned tea.Cmd (if non-nil) is a
// real async command for App to run through bubbletea as usual. The
// returned tea.Msg (if non-nil) is an intent for App to apply
// synchronously -- see companyListModel.Update for the same convention.
func (m *activeApplicationListModel) Update(msg tea.KeyMsg, apps []store.ApplicationView) (tea.Cmd, tea.Msg) {
	switch {
	case msg.Type == tea.KeyDown, msg.String() == "j":
		if m.cursor < len(apps)-1 {
			m.cursor++
		}
	case msg.Type == tea.KeyUp, msg.String() == "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case msg.String() == "q":
		return tea.Quit, nil
	case msg.String() == "c":
		return nil, backToCompanyListMsg{}
	case msg.String() == "s":
		if m.cursor < len(apps) {
			a := apps[m.cursor]
			return nil, enterApplicationStatusMsg{postingID: a.Posting.ID, currentStatus: a.Status}
		}
	case msg.String() == "e":
		if m.cursor < len(apps) {
			return nil, enterApplicationExportMsg{application: apps[m.cursor]}
		}
	case msg.Type == tea.KeyEnter:
		if m.cursor < len(apps) {
			return nil, enterApplicationDetailMsg{application: apps[m.cursor]}
		}
	}
	return nil, nil
}

// View renders the list in height terminal rows (App.screenRows).
// progress holds each application's document progress by application ID,
// from which, with its LatestReviews, each row's next step is derived
// (see nextStep), and now is what each row's Age counts up to.
func (m *activeApplicationListModel) View(apps []store.ApplicationView, progress map[int64]map[documents.Type]documentProgress, now time.Time, height int) string {
	var b strings.Builder
	title := titleStyle.Render("Active Applications")
	help := helpStyle.Render("↑/↓ (j/k): select  enter: application detail  s: status  e: export PDFs  c: companies  q: quit")
	b.WriteString(title + "\n")
	if len(apps) == 0 {
		b.WriteString("No active applications. Press 'c' to browse companies.\n")
	} else {
		start, end := visibleWindow(m.cursor, len(apps), tableRows(height, title, help))
		cursorRow := m.cursor - start
		t := table.New().
			Headers("Company", "Title", "Age", "Next", "Status", "Review").
			StyleFunc(func(row, _ int) lipgloss.Style {
				style := lipgloss.NewStyle().Padding(0, 1)
				if row == cursorRow {
					return style.Inherit(cursorStyle)
				}
				return style
			})
		for i := start; i < end; i++ {
			a := apps[i]
			t.Row(
				truncateCol(a.CompanyName, homeCompanyColWidth),
				truncateCol(a.Posting.Title, homeTitleColWidth),
				statusAge(a.StatusSince, now),
				nextStep(a, progress[a.ID]),
				applicationStatusLabel(a.Status),
				reviewGlyphSummary(a.LatestReviews),
			)
		}
		b.WriteString(t.Render() + "\n")
	}
	b.WriteString(help)
	return b.String()
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
// PDF (store.DocumentExport.IsCurrent, #188).
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
func nextStep(a store.ApplicationView, docs map[documents.Type]documentProgress) string {
	closed := a.Posting.ListingStatus == "closed"
	if a.Status != store.ApplicationStatusStarted {
		if closed {
			return "(closed)"
		}
		return ""
	}
	if closed {
		return "withdraw?"
	}
	documentTypes := documents.Types()
	for _, documentType := range documentTypes {
		if !docs[documentType].drafted {
			return "draft"
		}
	}
	for _, documentType := range documentTypes {
		if review, ok := a.LatestReviews[documentType]; ok && review.Outcome == store.ReviewOutcomeFlagged {
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
func orderForHome(apps []store.ApplicationView) {
	slices.SortStableFunc(apps, func(a, b store.ApplicationView) int {
		aStarted, bStarted := a.Status == store.ApplicationStatusStarted, b.Status == store.ApplicationStatusStarted
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
func documentProgressOf(s *store.Store, docs *documents.Store, applicationID int64) (map[documents.Type]documentProgress, error) {
	exports, err := s.LatestDocumentExports(context.Background(), applicationID)
	if err != nil {
		return nil, err
	}
	status := docs.Status(applicationID)
	currentExports, err := documents.Current(status, exports, store.DocumentExport.IsCurrent)
	if err != nil {
		return nil, err
	}
	progress := make(map[documents.Type]documentProgress, len(documents.Types()))
	for _, documentType := range documents.Types() {
		doc, err := status.Doc(documentType)
		if err != nil {
			return nil, err
		}
		_, exported := currentExports[documentType]
		progress[documentType] = documentProgress{drafted: doc.Exists, exported: exported}
	}
	return progress, nil
}
