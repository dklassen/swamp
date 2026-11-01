package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/ansi"

	"github.com/dklassen/swamp/internal/domain/filter"
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
func postingMarker(l message.Listing) string {
	switch {
	case l.Archived:
		return "✕"
	case l.Interested:
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

func (m *postingListModel) Init() tea.Cmd { return m.reload() }

// Update has nothing to do: everything the list handles is registered.
func (m *postingListModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

// reload reads again for a write, once at a time for a run of them.
func (m *postingListModel) reload() tea.Cmd { return m.reading.start(m.load) }

// load reads a page of the company's postings as the list shows them, as
// many as fit.
func (m *postingListModel) load() tea.Cmd {
	companies, companyID := m.companyService, m.company.ID
	page := service.PostingListPage{IncludeArchived: !m.hideArchived, Limit: m.pageSize()}
	src := message.Source{Instance: m.instance}
	subject := message.Subject{Type: message.TypeCompany, ID: companyID}
	return func() tea.Msg {
		listed, err := companies.ListedPostings(context.Background(), companyID, page)
		if err != nil {
			return message.New(src, subject, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
		}
		list := message.PostingList{Limit: page.Limit, HasMore: listed.Page.HasMore, Total: listed.Page.Total}
		for _, f := range listed.Filters {
			switch f.Field {
			case filter.FieldDepartment:
				list.Departments = append(list.Departments, f.Value)
			case filter.FieldLocation:
				list.Locations = append(list.Locations, f.Value)
			}
		}
		list.Listings = make([]message.Listing, len(listed.Page.Listings))
		for i, l := range listed.Page.Listings {
			list.Listings[i] = listingSnapshot(l)
		}
		return message.New(src, subject, message.OpNone, time.Now().UTC(), message.Read[message.PostingList]{Value: list})
	}
}

// postingsRead keeps the cursor on its posting, since a sync can add some
// ahead of it, and reads again when what's drawn around the table now
// leaves room for a different number of rows.
func (m *postingListModel) postingsRead(e message.Event[message.Read[message.PostingList]]) tea.Cmd {
	previous := m.selected()
	m.list = e.Payload.Value
	m.keepCursorOn(previous)
	cmd := m.reading.done(m.load)
	if cmd == nil && m.pageSize() != m.list.Limit {
		cmd = m.reload()
	}
	return cmd
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

// markupChanged reads again for a posting on the page, since it's drawn
// with its markup and one archived can leave the list.
func (m *postingListModel) markupChanged(e message.Event[message.Changed[message.Markup]]) tea.Cmd {
	if indexOfListing(m.list.Listings, e.Payload.After.PostingID) < 0 {
		return nil
	}
	return m.reload()
}

// detailOpened moves the cursor to a posting detail stepped to, so going
// back lands on the posting last shown.
func (m *postingListModel) detailOpened(e message.Event[message.Nav[message.PostingDetail]]) tea.Cmd {
	if i := indexOfListing(m.list.Listings, e.Payload.Value.Posting.ID); i >= 0 {
		m.cursor = i
	}
	return nil
}

// resize reads again when a different number of rows fits.
func (m *postingListModel) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	m.width, m.height = e.Payload.Value.Width, e.Payload.Value.Rows
	if m.pageSize() != m.list.Limit {
		return m.reload()
	}
	return nil
}

func (m *postingListModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	m.focused = e.Payload.Value.Focused == m.instance
	return nil
}

func (m *postingListModel) keyPressed(key tea.KeyPressMsg) tea.Cmd {
	src := message.Source{Instance: m.instance}
	listings := m.list.Listings
	onPosting := m.cursor < len(listings)
	var current message.Listing
	if onPosting {
		current = listings[m.cursor]
	}
	switch key.String() {
	case "down", "j":
		if m.cursor < len(listings)-1 {
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
			siblings := make([]int64, len(listings))
			for i, l := range listings {
				siblings[i] = l.ID
			}
			return message.Push(src, message.ScreenPostingDetail, message.Subject{Type: message.TypePosting, ID: current.ID},
				message.PostingDetail{Posting: listingPosting(current), Siblings: siblings})
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
			return toggleInterested(m.postingService, src, current.ID, current.Interested)
		}
	case "x":
		if onPosting {
			return toggleArchived(m.postingService, src, current.ID, current.Archived)
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

const postingListHelp = "↑/↓ (j/k): select  enter: view detail  o: open in browser  r: refresh  f: filters  i: interested  x: archive  A: toggle archived visibility  esc/b: back"

// aroundTable is the lines drawn above the table, each "" when it isn't
// drawn: the company's description, truncated to the table's width, the
// filters, the archived notice and how many postings there are when the
// page isn't all of them.
func (m *postingListModel) aroundTable() []string {
	var description, summary, archived, showing string
	if m.company.Description != "" {
		description = model.DimStyle.Render(truncateCol(strings.Join(strings.Fields(m.company.Description), " "), tableWidth(m.width)))
	}
	if s := filterSummaryLine(m.list.Departments, m.list.Locations); s != "" {
		summary = model.HelpStyle.Render(s)
	}
	if m.hideArchived {
		archived = model.HelpStyle.Render("Archived postings hidden (press 'A' to show)")
	}
	if m.list.HasMore {
		showing = model.DimStyle.Render(fmt.Sprintf("Showing %d of %d postings", len(m.list.Listings), m.list.Total))
	}
	return []string{description, summary, archived, showing}
}

// pageSize is how many postings fit: the table's rows once the lines
// around it have taken theirs.
func (m *postingListModel) pageSize() int {
	return max(tableRows(m.height, append(m.aroundTable(), model.HelpStyle.Render(postingListHelp))...), 1)
}

// View renders the page read, which is as many postings as fit.
func (m *postingListModel) View() tea.View {
	var b strings.Builder
	for _, line := range m.aroundTable() {
		if line != "" {
			b.WriteString(line + "\n")
		}
	}
	listings := m.list.Listings
	if len(listings) == 0 {
		b.WriteString("No postings yet. Press 'r' to refresh.\n")
	} else {
		t := table.New().
			Border(lipgloss.RoundedBorder()).
			Headers("", "Title", "Department", "Location", "Status").
			StyleFunc(func(row, _ int) lipgloss.Style {
				style := lipgloss.NewStyle().Padding(0, 1)
				if row == m.cursor {
					return style.Inherit(model.CursorStyle)
				}
				return style
			})
		titleWidth := flexColWidth(m.width, markerColWidth, departmentColWidth, locationColWidth, statusColWidth)
		for _, l := range listings {
			t.Row(
				padCol(postingMarker(l), markerColWidth),
				padCol(l.Title, titleWidth),
				padCol(l.Department, departmentColWidth),
				padCol(l.Location, locationColWidth),
				padCol(l.ListingStatus.String(), statusColWidth),
			)
		}
		b.WriteString(t.Render() + "\n")
	}
	b.WriteString(model.HelpStyle.Render(postingListHelp))
	return tea.NewView(b.String())
}

// clampCursor keeps the cursor in bounds after the page shrinks.
func (m *postingListModel) clampCursor(n int) {
	if m.cursor >= n && m.cursor > 0 {
		m.cursor = n - 1
	}
}

// selected is the ID of the posting under the cursor, or 0.
func (m *postingListModel) selected() int64 {
	if m.cursor < len(m.list.Listings) {
		return m.list.Listings[m.cursor].ID
	}
	return 0
}

// keepCursorOn moves the cursor to posting id after a reload in place; if
// it's gone, the cursor stays in range.
func (m *postingListModel) keepCursorOn(id int64) {
	if i := indexOfListing(m.list.Listings, id); i >= 0 {
		m.cursor = i
		return
	}
	m.clampCursor(len(m.list.Listings))
}

// indexOfListing returns id's index in listings, or -1 if not present.
func indexOfListing(listings []message.Listing, id int64) int {
	for i, l := range listings {
		if l.ID == id {
			return i
		}
	}
	return -1
}
