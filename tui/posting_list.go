package tui

import (
	"context"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/ansi"

	"github.com/dklassen/swamp/internal/domain/filter"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// Column widths for the posting table; Title takes what's left. Values
// longer than these get truncated with an ellipsis rather than wrapped, so
// every row stays exactly one physical line (visibleWindow's cursor/scroll
// math assumes one line per posting).
const (
	markerColWidth     = 1
	departmentColWidth = 18
	locationColWidth   = 20
	statusColWidth     = 8
)

// postingTableChromeLines is the number of physical lines lipgloss/table's
// default border adds beyond one line per data row: top border, header,
// header separator, bottom border.
const postingTableChromeLines = 4

// tableRows is how many data rows a table screen can show in height
// terminal rows: what's left once the table's own chrome and everything
// else the screen draws around it (help, notices, ...) have taken
// theirs. Each of around is measured as rendered, margins and all, so a
// screen can't outgrow the terminal by drawing a line it forgot to count
// (issue #138). An empty string is something the screen isn't drawing
// this time, so it takes no rows.
func tableRows(height int, around ...string) int {
	rows := height - postingTableChromeLines
	for _, s := range around {
		if s != "" {
			rows -= lipgloss.Height(s)
		}
	}
	return max(rows, 0)
}

// truncateCol shortens s to at most max columns wide, replacing the tail
// with an ellipsis when it doesn't fit. Width-aware (not byte-aware) so
// multi-byte runes truncate correctly.
func truncateCol(s string, max int) string {
	return ansi.TruncateWc(s, max, "…")
}

// postingMarker renders a posting's markup state as a single-character
// column for the posting list. Archived takes precedence over interested
// when both are set, matching the same precedence the 00003 migration's
// Down path uses when collapsing both flags back into one enum value.
func postingMarker(m message.Markup) string {
	switch {
	case !m.ArchivedAt.IsZero():
		return "✕"
	case !m.InterestedAt.IsZero():
		return "★"
	default:
		return " "
	}
}

// filterSummaryLine renders the currently-active filters as a one-line
// summary (e.g. "Filtering: Department: Engineering | Location:
// Remote"), or "" if no filters are active -- so the filter state isn't
// invisible in the posting list.
func filterSummaryLine(departments, locations []string) string {
	if len(departments) == 0 && len(locations) == 0 {
		return ""
	}
	var parts []string
	if len(departments) > 0 {
		parts = append(parts, "Department: "+strings.Join(departments, ", "))
	}
	if len(locations) > 0 {
		parts = append(parts, "Location: "+strings.Join(locations, ", "))
	}
	return "Filtering: " + strings.Join(parts, " | ")
}

// postingListModel is one company's postings, which it loads and keeps.
type postingListModel struct {
	companyService *service.CompanyService
	postingService *service.PostingService

	company message.Company
	list    message.PostingList
	reading reading
	// hideArchived starts as App's, which it tells when it flips.
	hideArchived bool
	cursor       int

	focused  bool
	width    int
	height   int
	instance message.ScreenInstance
}

func newPostingListModel(companies *service.CompanyService, postings *service.PostingService, c message.Company, hideArchived bool, size message.ContentSize) *postingListModel {
	return &postingListModel{
		companyService: companies,
		postingService: postings,
		company:        c,
		hideArchived:   hideArchived,
		width:          size.Width,
		height:         size.Rows,
		instance:       message.NewScreenInstance(),
	}
}

func (m *postingListModel) Instance() message.ScreenInstance { return m.instance }

func (m *postingListModel) Focused() bool { return m.focused }

func (m *postingListModel) Routes() []*model.Route {
	return []*model.Route{
		model.Always(m.postingsRead, model.From(m.instance)),
		model.Always(m.readFailed, model.From(m.instance)),
		model.Always(m.synced, model.About(message.TypeCompany, m.company.ID)),
		model.Always(m.filtersSaved, model.About(message.TypeCompany, m.company.ID)),
		model.Always(m.markupChanged),
		model.Always(m.postingChanged),
		model.Always(m.detailOpened),
		model.WhenFocused(m, m.keyPressed),
		model.Always(m.resize),
		model.Always(m.focusChanged),
	}
}

func (m *postingListModel) Init() tea.Cmd { return m.load() }

// Update has nothing to do: everything the list handles is registered.
func (m *postingListModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

// reload reads again for a write, once at a time for a run of them.
func (m *postingListModel) reload() tea.Cmd { return m.reading.start(m.load) }

// load reads the company's postings as the list shows them: open ones
// only, since a closed listing can't be applied to; narrowed by the
// company's saved filters, since sync stores every posting; and without
// archived ones while they're hidden.
func (m *postingListModel) load() tea.Cmd {
	companies, postings, hideArchived := m.companyService, m.postingService, m.hideArchived
	src, companyID := message.Source{Instance: m.instance}, m.company.ID
	subject := message.Subject{Type: message.TypeCompany, ID: companyID}
	failed := func(err error) tea.Msg {
		return message.New(src, subject, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
	}
	return func() tea.Msg {
		ctx := context.Background()
		stored, err := postings.ListByCompany(ctx, companyID)
		if err != nil {
			return failed(err)
		}
		companyFilters, err := companies.Filters(ctx, companyID)
		if err != nil {
			return failed(err)
		}
		var list message.PostingList
		for _, f := range companyFilters {
			switch f.Field {
			case filter.FieldDepartment:
				list.Departments = append(list.Departments, f.Value)
			case filter.FieldLocation:
				list.Locations = append(list.Locations, f.Value)
			}
		}
		list.Postings = make([]message.Posting, 0, len(stored))
		list.Markup = make(map[int64]message.Markup, len(stored))
		for _, p := range stored {
			if p.ListingStatus == posting.ListingClosed {
				continue
			}
			// An unsupported field is an error rather than a filter left out.
			match, err := filter.Match(filter.Posting{Department: p.Department, Location: p.Location}, companyFilters)
			if err != nil {
				return failed(err)
			}
			if !match {
				continue
			}
			mk, err := postings.Markup(ctx, p.ID)
			if err != nil {
				return failed(err)
			}
			snapshot := markupSnapshot(*mk)
			list.Markup[p.ID] = snapshot
			if hideArchived && !snapshot.ArchivedAt.IsZero() {
				continue
			}
			list.Postings = append(list.Postings, postingSnapshot(p))
		}
		return message.New(src, subject, message.OpNone, time.Now().UTC(), message.Read[message.PostingList]{Value: list})
	}
}

// postingsRead keeps the cursor on its posting, since a sync can add some
// ahead of it.
func (m *postingListModel) postingsRead(e message.Event[message.Read[message.PostingList]]) tea.Cmd {
	previous := m.selected()
	m.list = e.Payload.Value
	m.keepCursorOn(previous)
	return m.reading.done(m.load)
}

func (m *postingListModel) readFailed(message.Event[message.Error]) tea.Cmd {
	return m.reading.done(m.load)
}

// postingChanged reads again for a posting of its company.
func (m *postingListModel) postingChanged(e message.Event[message.Changed[message.Posting]]) tea.Cmd {
	if e.Payload.After.CompanyID != m.company.ID {
		return nil
	}
	return m.reload()
}

// synced reloads even when the sync failed, since one can fail after
// writing postings.
func (m *postingListModel) synced(message.Event[message.Ran[message.SyncReport]]) tea.Cmd {
	return m.load()
}

func (m *postingListModel) filtersSaved(message.Event[message.Ran[message.FilterSelection]]) tea.Cmd {
	return m.load()
}

// markupChanged drops a posting archived while archived ones are hidden,
// since it can't be reached to unarchive until they're shown.
func (m *postingListModel) markupChanged(e message.Event[message.Changed[message.Markup]]) tea.Cmd {
	marked := e.Payload.After
	i := indexOfPosting(m.list.Postings, marked.PostingID)
	if i < 0 {
		return nil
	}
	m.list.Markup[marked.PostingID] = marked
	if m.hideArchived && !marked.ArchivedAt.IsZero() {
		m.list.Postings = slices.Delete(m.list.Postings, i, i+1)
		m.clampCursor(len(m.list.Postings))
	}
	return nil
}

// detailOpened moves the cursor to a posting detail stepped to, so going
// back lands on the posting last shown.
func (m *postingListModel) detailOpened(e message.Event[message.Nav[message.PostingDetail]]) tea.Cmd {
	if i := indexOfPosting(m.list.Postings, e.Payload.Value.Posting.ID); i >= 0 {
		m.cursor = i
	}
	return nil
}

func (m *postingListModel) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	m.width, m.height = e.Payload.Value.Width, e.Payload.Value.Rows
	return nil
}

func (m *postingListModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	m.focused = e.Payload.Value.Focused == m.instance
	return nil
}

func (m *postingListModel) keyPressed(key tea.KeyPressMsg) tea.Cmd {
	src := message.Source{Instance: m.instance}
	postings := m.list.Postings
	onPosting := m.cursor < len(postings)
	var current message.Posting
	if onPosting {
		current = postings[m.cursor]
	}
	switch key.String() {
	case "down", "j":
		if m.cursor < len(postings)-1 {
			m.cursor++
		}
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "esc", "b":
		return tea.Batch(message.Pop(src), message.ReportStatus(src, ""))
	case "enter":
		if onPosting {
			siblings := make([]int64, len(postings))
			for i, p := range postings {
				siblings[i] = p.ID
			}
			return message.Push(src, message.ScreenPostingDetail, message.Subject{Type: message.TypePosting, ID: current.ID},
				message.PostingDetail{Posting: current, Siblings: siblings})
		}
	case "o":
		if onPosting && current.JobURL != "" {
			return openInBrowser(src, message.Subject{Type: message.TypePosting, ID: current.ID}, current.JobURL)
		}
	case "r":
		return syncCompany(src, m.company)
	case "f":
		return message.Push(src, message.ScreenFilterSelect, message.Subject{Type: message.TypeCompany, ID: m.company.ID},
			message.FilterSelection{Company: m.company, Departments: m.list.Departments, Locations: m.list.Locations})
	case "i":
		if onPosting {
			return toggleInterested(m.postingService, src, current.ID, !m.list.Markup[current.ID].InterestedAt.IsZero())
		}
	case "x":
		if onPosting {
			return toggleArchived(m.postingService, src, current.ID, !m.list.Markup[current.ID].ArchivedAt.IsZero())
		}
	case "A":
		m.hideArchived = !m.hideArchived
		hidden := message.HideArchived{Hidden: m.hideArchived}
		return tea.Batch(m.load(), func() tea.Msg {
			return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), message.Signal[message.HideArchived]{Value: hidden})
		})
	}
	return nil
}

// View renders the list in the rows of the last content size. Every line
// drawn above or below the table -- the description, filter summary and
// archived notice included -- costs the table a row, so the list still
// fits.
func (m *postingListModel) crumbs(message.Screen) []string {
	return []string{m.company.Name}
}

func (m *postingListModel) View() tea.View {
	var b strings.Builder
	help := model.HelpStyle.Render("↑/↓ (j/k): select  enter: view detail  o: open in browser  r: refresh  f: filters  i: interested  x: archive  A: toggle archived visibility  esc/b: back")
	// The description gets one line, truncated to the table's width.
	var description, summary, archived string
	if m.company.Description != "" {
		description = model.DimStyle.Render(truncateCol(strings.Join(strings.Fields(m.company.Description), " "), tableWidth(m.width)))
	}
	if s := filterSummaryLine(m.list.Departments, m.list.Locations); s != "" {
		summary = model.HelpStyle.Render(s)
	}
	if m.hideArchived {
		archived = model.HelpStyle.Render("Archived postings hidden (press 'A' to show)")
	}
	for _, line := range []string{description, summary, archived} {
		if line != "" {
			b.WriteString(line + "\n")
		}
	}
	postings := m.list.Postings
	if len(postings) == 0 {
		b.WriteString("No postings yet. Press 'r' to refresh.\n")
	} else {
		start, end := visibleWindow(m.cursor, len(postings), tableRows(m.height, description, summary, archived, help))
		cursorRow := m.cursor - start
		t := table.New().
			Border(lipgloss.RoundedBorder()).
			Headers("", "Title", "Department", "Location", "Status").
			StyleFunc(func(row, _ int) lipgloss.Style {
				style := lipgloss.NewStyle().Padding(0, 1)
				if row == cursorRow {
					return style.Inherit(model.CursorStyle)
				}
				return style
			})
		titleWidth := flexColWidth(m.width, markerColWidth, departmentColWidth, locationColWidth, statusColWidth)
		for i := start; i < end; i++ {
			p := postings[i]
			t.Row(
				padCol(postingMarker(m.list.Markup[p.ID]), markerColWidth),
				padCol(p.Title, titleWidth),
				padCol(p.Department, departmentColWidth),
				padCol(p.Location, locationColWidth),
				padCol(p.ListingStatus.String(), statusColWidth),
			)
		}
		b.WriteString(t.Render() + "\n")
	}
	b.WriteString(help)
	return tea.NewView(b.String())
}

// clampCursor keeps the cursor in bounds after postings shrinks (e.g.
// archiving while hideArchived is on).
func (m *postingListModel) clampCursor(n int) {
	if m.cursor >= n && m.cursor > 0 {
		m.cursor = n - 1
	}
}

// selected is the ID of the posting under the cursor, or 0.
func (m *postingListModel) selected() int64 {
	if m.cursor < len(m.list.Postings) {
		return m.list.Postings[m.cursor].ID
	}
	return 0
}

// keepCursorOn moves the cursor to posting id after a reload in place; if
// it's gone, the cursor stays in range.
func (m *postingListModel) keepCursorOn(id int64) {
	if i := indexOfPosting(m.list.Postings, id); i >= 0 {
		m.cursor = i
		return
	}
	m.clampCursor(len(m.list.Postings))
}

// indexOfPosting returns id's index in postings, or -1 if not present.
func indexOfPosting(postings []message.Posting, id int64) int {
	for i, p := range postings {
		if p.ID == id {
			return i
		}
	}
	return -1
}
