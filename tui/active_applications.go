package tui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

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

// activeApplicationListModel is the home screen: every application not at
// a dead-end status, across every company. Application-specific actions
// live one level down, on application detail; this screen keeps status
// as a shortcut for triage across many applications.
type activeApplicationListModel struct {
	applications    *service.ApplicationService
	documentService *service.DocumentService
	documents       *documents.Store
	// apps is what the last read found, in the order the screen lists
	// them, and progress each one's document progress by application ID,
	// from which, with its LatestReviews, each row's next step is derived.
	apps     []posting.ActiveApplication
	progress map[int64]map[document.Type]documentProgress
	cursor   int
	// searching is whether the '/' prompt is open and taking keys; query
	// is what's been typed into it.
	searching bool
	query     string

	width    int
	rows     int
	focused  bool
	instance message.ScreenInstance
}

func newActiveApplicationListModel(applications *service.ApplicationService, documentService *service.DocumentService, docs *documents.Store) *activeApplicationListModel {
	return &activeApplicationListModel{
		applications:    applications,
		documentService: documentService,
		documents:       docs,
		instance:        message.NewScreenInstance(),
	}
}

func (m *activeApplicationListModel) Instance() message.ScreenInstance { return m.instance }

func (m *activeApplicationListModel) Focused() bool { return m.focused }

func (m *activeApplicationListModel) Routes() []*model.Route {
	return []*model.Route{
		model.Always(m.applicationsRead, model.From(m.instance)),
		model.Always(m.loadRequested),
		model.Always(m.applicationChanged),
		model.WhenFocused(m, m.keyPressed),
		model.WhenFocused(m, m.paste),
		model.Always(m.resize),
		model.Always(m.focusChanged),
	}
}

func (m *activeApplicationListModel) Init() tea.Cmd { return m.load() }

// Update has nothing to do: everything the screen handles is registered.
func (m *activeApplicationListModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *activeApplicationListModel) src() message.Source {
	return message.Source{Instance: m.instance}
}

// load reads every active application, keeping only the reviews that
// still describe each document as it is on disk, which the services
// can't see.
func (m *activeApplicationListModel) load() tea.Cmd {
	applications, documentService, docs, src := m.applications, m.documentService, m.documents, m.src()
	return func() tea.Msg {
		fail := func(err error) tea.Msg {
			return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
		}
		apps, err := applications.Active(context.Background())
		if err != nil {
			return fail(err)
		}
		orderForHome(apps)
		var read message.ActiveApplications
		for _, app := range apps {
			app.LatestReviews, err = documents.Current(docs.Status(app.ID), app.LatestReviews, document.Review.IsCurrent)
			if err != nil {
				return fail(err)
			}
			progress, err := documentProgressOf(documentService, docs, app.ID)
			if err != nil {
				return fail(err)
			}
			read.Rows = append(read.Rows, applicationRow(app, progress))
		}
		return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), message.Read[message.ActiveApplications]{Value: read})
	}
}

// applicationsRead shows what was read, keeping the cursor on the
// application it was on.
func (m *activeApplicationListModel) applicationsRead(e message.Event[message.Read[message.ActiveApplications]]) tea.Cmd {
	previous := m.selected()
	rows := e.Payload.Value.Rows
	m.apps = make([]posting.ActiveApplication, len(rows))
	m.progress = make(map[int64]map[document.Type]documentProgress, len(rows))
	for i, row := range rows {
		m.apps[i], m.progress[row.Application.ID] = activeApplicationOf(row)
	}
	m.keepCursorOn(previous)
	return nil
}

func (m *activeApplicationListModel) loadRequested(message.Event[message.Load[message.ActiveApplications]]) tea.Cmd {
	return m.load()
}

// applicationChanged reads again, since a write can move an application
// to or from a dead end, or delete it.
func (m *activeApplicationListModel) applicationChanged(message.Event[message.Changed[message.Application]]) tea.Cmd {
	return m.load()
}

func (m *activeApplicationListModel) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	m.width, m.rows = e.Payload.Value.Width, e.Payload.Value.Rows
	return nil
}

// focusChanged reads again on coming into focus: documents change on disk,
// and reviews are written, with no event it hears.
func (m *activeApplicationListModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	gained := !m.focused && e.Payload.Value.Focused == m.instance
	m.focused = e.Payload.Value.Focused == m.instance
	if gained {
		return m.load()
	}
	return nil
}

// enterApplicationDetailMsg asks App to open application detail on this
// application.
type enterApplicationDetailMsg struct {
	application posting.ActiveApplication
}

// enterApplicationExportMsg asks App to open the export screen, where the
// user picks the folder for this application's PDFs.
type enterApplicationExportMsg struct {
	application posting.ActiveApplication
}

// ask sends msg to App, for the screens App still opens itself.
func ask(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

func (m *activeApplicationListModel) keyPressed(msg tea.KeyPressMsg) tea.Cmd {
	if m.searching {
		return m.updateSearch(msg)
	}
	// Commands act on the matches a kept search shows, not the whole list.
	visible := m.visible()
	onRow := m.cursor < len(visible)
	switch msg.String() {
	case "/":
		m.searching = true
	case "esc":
		m.clearSearch()
	case "down", "j":
		if m.cursor < len(visible)-1 {
			m.cursor++
		}
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "q":
		return tea.Quit
	case "s":
		if onRow {
			a := visible[m.cursor].Application
			return message.Push(m.src(), message.ScreenApplicationStatus, message.Subject{Type: message.TypeApplication, ID: a.ID}, applicationSnapshot(a))
		}
	case "e":
		if onRow {
			return ask(enterApplicationExportMsg{application: visible[m.cursor]})
		}
	case "d":
		if onRow {
			return ask(enterApplicationDeleteMsg{application: visible[m.cursor]})
		}
	case "enter":
		if onRow {
			return ask(enterApplicationDetailMsg{application: visible[m.cursor]})
		}
	}
	return nil
}

// updateSearch handles a key while the '/' prompt is open. Every edit
// to the query re-filters straight away and puts the cursor back on the
// first match.
func (m *activeApplicationListModel) updateSearch(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	default:
		if msg.Text != "" {
			m.query += msg.Text
			m.cursor = 0
		}
	case "backspace":
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
			m.cursor = 0
		} else {
			m.searching = false
		}
	case "esc":
		m.searching = false
		m.clearSearch()
	case "down", "ctrl+n":
		if m.cursor < len(m.visible())-1 {
			m.cursor++
		}
	case "up", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
		}
	case "enter":
		// Keeps the matches, so letters act on them instead of being typed.
		m.searching = false
	}
	return nil
}

func (m *activeApplicationListModel) paste(msg tea.PasteMsg) tea.Cmd {
	if m.searching {
		m.query += msg.Content
		m.cursor = 0
	}
	return nil
}

// clearSearch drops the query, keeping the highlighted application
// highlighted in the full list.
func (m *activeApplicationListModel) clearSearch() {
	cursor := 0
	if visible := m.visible(); m.cursor < len(visible) {
		id := visible[m.cursor].ID
		cursor = max(slices.IndexFunc(m.apps, func(a posting.ActiveApplication) bool { return a.ID == id }), 0)
	}
	m.query = ""
	m.cursor = cursor
}

// visible is the applications the query matches: a case-insensitive
// substring of the company or the posting title, in the order listed.
// With no query, it's all of them.
func (m *activeApplicationListModel) visible() []posting.ActiveApplication {
	if m.query == "" {
		return m.apps
	}
	q := strings.ToLower(m.query)
	var matches []posting.ActiveApplication
	for _, a := range m.apps {
		if strings.Contains(strings.ToLower(a.CompanyName), q) || strings.Contains(strings.ToLower(a.Posting.Title), q) {
			matches = append(matches, a)
		}
	}
	return matches
}

// View renders the list in the last content size.
func (m *activeApplicationListModel) View() tea.View {
	return tea.NewView(m.render(time.Now()))
}

// render draws the list, each row's Age counting up to now.
func (m *activeApplicationListModel) render(now time.Time) string {
	var b strings.Builder
	help := model.HelpStyle.Render("↑/↓ (j/k): select  enter: application detail  /: search  s: status  e: export PDFs  d: delete  tab: companies  q: quit")
	switch {
	case m.searching:
		help = model.HelpStyle.Render("type to filter  ↑/↓ (ctrl+n/p): select  enter: keep results  esc: clear")
	case m.query != "":
		help = model.HelpStyle.Render("↑/↓ (j/k): select  enter: application detail  /: edit search  esc: clear search  s: status  e: export PDFs  d: delete  q: quit")
	}
	visible := m.visible()
	// A reload landing while the prompt is open can shrink visible under
	// the cursor.
	cursor := min(m.cursor, max(len(visible)-1, 0))
	var prompt string
	if len(m.apps) > 0 {
		prompt = searchLine(m.searching, m.query, len(visible), len(m.apps))
		b.WriteString(prompt + "\n")
	}
	switch {
	case len(m.apps) == 0:
		b.WriteString("No active applications. Press tab to browse companies.\n")
	case len(visible) == 0:
		fmt.Fprintf(&b, "No applications match %q.\n", m.query)
	default:
		start, end := visibleWindow(cursor, len(visible), tableRows(m.rows, help, prompt))
		cursorRow := cursor - start
		t := table.New().
			Border(lipgloss.RoundedBorder()).
			Headers("Company", "Title", "Age", "Next", "Status", "Review").
			StyleFunc(func(row, _ int) lipgloss.Style {
				style := lipgloss.NewStyle().Padding(0, 1)
				if row == cursorRow {
					return style.Inherit(model.CursorStyle)
				}
				return style
			})
		titleWidth := flexColWidth(m.width, homeCompanyColWidth, homeAgeColWidth, homeNextColWidth, homeStatusColWidth, homeReviewColWidth)
		for i := start; i < end; i++ {
			a := visible[i]
			t.Row(
				padCol(a.CompanyName, homeCompanyColWidth),
				padCol(a.Posting.Title, titleWidth),
				padCol(statusAge(a.StatusSince, now), homeAgeColWidth),
				padCol(nextStep(a, m.progress[a.ID]), homeNextColWidth),
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
// cursor indexes what the search shows, which is every application when
// there's no query.
func (m *activeApplicationListModel) selected() int64 {
	if visible := m.visible(); m.cursor < len(visible) {
		return visible[m.cursor].ID
	}
	return 0
}

// keepCursorOn moves the cursor to application id after a reload, since
// rows above it may have come or gone; if it's gone, the cursor stays in
// range.
func (m *activeApplicationListModel) keepCursorOn(id int64) {
	visible := m.visible()
	if i := slices.IndexFunc(visible, func(a posting.ActiveApplication) bool { return a.ID == id }); i >= 0 {
		m.cursor = i
		return
	}
	if m.cursor >= len(visible) {
		m.cursor = 0
	}
}

// applicationRow is the snapshot of an active application and its
// documents' progress.
func applicationRow(a posting.ActiveApplication, progress map[document.Type]documentProgress) message.ApplicationRow {
	row := message.ApplicationRow{
		Application: applicationSnapshot(a.Application),
		Posting:     postingSnapshot(a.Posting),
		CompanyName: a.CompanyName,
		StatusSince: a.StatusSince,
	}
	for _, documentType := range document.Types() {
		if review, ok := a.LatestReviews[documentType]; ok {
			review.DocumentType = documentType
			row.Reviews = append(row.Reviews, reviewSnapshot(review))
		}
		p := progress[documentType]
		row.Documents = append(row.Documents, message.DocumentProgress{DocumentType: documentTypeSnapshots[documentType], Drafted: p.drafted, Exported: p.exported})
	}
	return row
}

// activeApplicationOf is the application and document progress a row is
// of, for the screens that still hold posting.ActiveApplication. It has
// only the fields the row carries.
func activeApplicationOf(row message.ApplicationRow) (posting.ActiveApplication, map[document.Type]documentProgress) {
	a := posting.ActiveApplication{
		Application:   applicationOf(row.Application),
		Posting:       postingOf(row.Posting),
		CompanyName:   row.CompanyName,
		LatestReviews: reviewsOf(message.ReviewList{Reviews: row.Reviews}),
		StatusSince:   row.StatusSince,
	}
	progress := make(map[document.Type]documentProgress, len(row.Documents))
	for _, d := range row.Documents {
		for documentType, snapshot := range documentTypeSnapshots {
			if snapshot == d.DocumentType {
				progress[documentType] = documentProgress{drafted: d.Drafted, exported: d.Exported}
			}
		}
	}
	return a, progress
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
	closed := a.Posting.ListingStatus == posting.ListingClosed
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
