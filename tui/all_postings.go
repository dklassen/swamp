package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// seenColWidth fits a first-seen date, "Sep 30".
const seenColWidth = 6

// fitColWidth fits the column's header, "Fit"; a score is one digit.
const fitColWidth = 3

const allPostingsHelp = "↑/↓ (j/k): select  ←/→ (h/l): page  s: sort  enter: detail  i: interested  x: archive  o: open  tab: applications"

const allPostingsSearchHelp = "type to search  ↑/↓ (ctrl+n/p): select  ←/→: page  enter: keep results  esc: clear"

// allPostingsResultsHelp is the help once enter has kept a search's results.
const allPostingsResultsHelp = "↑/↓ (j/k): select  ←/→ (h/l): page  s: sort  enter: detail  i: interested  x: archive  o: open  /: edit search  esc: clear search"

// allPostingsModel drives the all-postings view: every company's open
// postings that its filters let through, newest to Swamp first, a page at
// a time. Pages are fixed rather than scrolling, so a posting arriving
// lands on page 1 instead of shifting the one being read.
type allPostingsModel struct {
	postings *service.PostingService
	// openURL opens a URL in the browser: openInBrowser, swapped for a
	// fake in tests so they don't launch one.
	openURL func(src message.Source, about message.Subject, url string) tea.Cmd
	cursor  int
	// starts is where each page so far began, page 1's being the zero
	// start; the last is the page on screen.
	starts []pageStart
	order  message.PostingOrder
	// page is the page on screen, and the search it was read with.
	page    message.PostingSearch
	reading reading
	// searching is whether the '/' line is open and taking keys; query is
	// what's been typed into it, kept with its results once enter closes it.
	searching bool
	query     string

	width    int
	rows     int
	focused  bool
	instance message.ScreenInstance
}

func newAllPostingsModel(postings *service.PostingService) *allPostingsModel {
	return &allPostingsModel{
		postings: postings,
		openURL:  openInBrowser,
		starts:   []pageStart{{}},
		order:    message.OrderNewest,
		instance: message.NewScreenInstance(),
	}
}

func (m *allPostingsModel) Instance() message.ScreenInstance { return m.instance }

func (m *allPostingsModel) Focused() bool { return m.focused }

func (m *allPostingsModel) Routes() []*model.Route {
	return []*model.Route{
		model.Always(m.searchRead, model.From(m.instance)),
		model.Always(m.paused, model.From(m.instance)),
		model.Always(m.readFailed, model.From(m.instance)),
		model.Always(m.markupChanged),
		model.Always(m.postingChanged),
		model.Always(m.scoreChanged),
		model.Always(m.companyChanged),
		model.WhenFocused(m, m.keyPressed),
		model.WhenFocused(m, m.paste),
		model.Always(m.resize),
		model.Always(m.focusChanged),
	}
}

// Init reads page 1 for the tab's count.
func (m *allPostingsModel) Init() tea.Cmd { return m.load() }

// Update has nothing to do: everything the view handles is registered.
func (m *allPostingsModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *allPostingsModel) src() message.Source { return message.Source{Instance: m.instance} }

// pageStart is the row a page was read after: its ID, and its score for the
// order by fit.
type pageStart struct {
	id    int64
	score int
}

// searchPause is how long typing must stop before the search runs, so fast
// typing doesn't queue a load per key.
const searchPause = 150 * time.Millisecond

// searchOrders is the order each PostingOrder reads in.
var searchOrders = map[message.PostingOrder]posting.Order{
	message.OrderNewest:  posting.OrderIDDesc,
	message.OrderBestFit: posting.OrderScoreDesc,
}

// pageSize is how many rows fill height: what's left after the search and
// page lines, the table's chrome and the help.
func (m *allPostingsModel) pageSize(height int) int {
	return max(tableRows(height-2, model.HelpStyle.Render(allPostingsHelp)), 1)
}

// load reads the page on screen again, as many rows as fit.
func (m *allPostingsModel) load() tea.Cmd {
	return m.loadAt(m.pageSize(m.rows))
}

// reload reads again for a write, once at a time for a run of them.
func (m *allPostingsModel) reload() tea.Cmd { return m.reading.start(m.load) }

// loadAt reads the page on screen again, size rows long, with the query
// and the count without it.
func (m *allPostingsModel) loadAt(size int) tea.Cmd {
	s, src, start := m.postings, m.src(), m.starts[len(m.starts)-1]
	read := message.PostingSearch{Query: m.query, Order: m.order, AfterID: start.id, AfterScore: start.score, Limit: size}
	return func() tea.Msg {
		ctx := context.Background()
		fail := func(err error) tea.Msg {
			return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), model.ErrorOf(err))
		}
		open := posting.ListingOpen
		page, err := s.Search(ctx, posting.Search{
			ListingStatus: &open,
			Order:         searchOrders[read.Order],
			AfterID:       read.AfterID,
			AfterScore:    read.AfterScore,
			Query:         read.Query,
			Limit:         read.Limit,
		})
		if err != nil {
			return fail(err)
		}
		read.HasMore, read.Total, read.WithoutQuery = page.HasMore, page.Total, page.Total
		if read.Query != "" {
			all, err := s.Search(ctx, posting.Search{ListingStatus: &open, Limit: 1})
			if err != nil {
				return fail(err)
			}
			read.WithoutQuery = all.Total
		}
		read.Listings = make([]message.Listing, len(page.Listings))
		for i, l := range page.Listings {
			read.Listings[i] = listingSnapshot(l)
		}
		return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), message.Read[message.PostingSearch]{Value: read})
	}
}

// searchRead shows the page read and ends the read in flight.
func (m *allPostingsModel) searchRead(e message.Event[message.Read[message.PostingSearch]]) tea.Cmd {
	shown := m.showSearch(e)
	return tea.Batch(shown, m.reading.done(m.load))
}

func (m *allPostingsModel) readFailed(message.Event[message.Error]) tea.Cmd {
	return m.reading.done(m.load)
}

// showSearch shows the page read, if it's still the search on screen. A
// page past the first that has emptied, its postings archived or closed,
// steps back.
func (m *allPostingsModel) showSearch(e message.Event[message.Read[message.PostingSearch]]) tea.Cmd {
	read, start := e.Payload.Value, m.starts[len(m.starts)-1]
	if read.Query != m.query || read.Order != m.order || read.AfterID != start.id || read.AfterScore != start.score {
		return nil
	}
	if len(read.Listings) == 0 && len(m.starts) > 1 {
		m.starts = m.starts[:len(m.starts)-1]
		return m.loadAt(read.Limit)
	}
	m.page = read
	m.cursor = min(m.cursor, max(len(m.page.Listings)-1, 0))
	return nil
}

// markupChanged reloads, since a posting archived leaves the page.
func (m *allPostingsModel) markupChanged(message.Event[message.Changed[message.Markup]]) tea.Cmd {
	return m.reload()
}

// postingChanged reloads, since a posting closed leaves the page.
func (m *allPostingsModel) postingChanged(message.Event[message.Changed[message.Posting]]) tea.Cmd {
	return m.reload()
}

// scoreChanged reloads, since a row shows its score and best fit orders by
// it.
func (m *allPostingsModel) scoreChanged(message.Event[message.Changed[message.Score]]) tea.Cmd {
	return m.reload()
}

// companyChanged reloads, since a deleted company's postings leave the
// page.
func (m *allPostingsModel) companyChanged(message.Event[message.Changed[message.Company]]) tea.Cmd {
	return m.reload()
}

// paused searches, unless the query has changed since the pause began.
func (m *allPostingsModel) paused(e message.Event[message.Signal[message.SearchDebounce]]) tea.Cmd {
	if e.Payload.Value.Query != m.query {
		return nil
	}
	return m.firstPage()
}

// resize reads the page again when a different number of rows fits, if
// it's on screen; otherwise coming into focus reads it.
func (m *allPostingsModel) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
	m.width, m.rows = e.Payload.Value.Width, e.Payload.Value.Rows
	if m.focused && m.pageSize(m.rows) != m.page.Limit {
		return m.load()
	}
	return nil
}

// focusChanged reads the page again on coming into focus, since postings
// are synced, scored and marked while it's away.
func (m *allPostingsModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	gained := !m.focused && e.Payload.Value.Focused == m.instance
	m.focused = e.Payload.Value.Focused == m.instance
	if gained {
		return m.load()
	}
	return nil
}

func (m *allPostingsModel) firstPage() tea.Cmd {
	m.starts = []pageStart{{}}
	m.cursor = 0
	return m.load()
}

func (m *allPostingsModel) nextPage() tea.Cmd {
	n := len(m.page.Listings)
	if n == 0 || !m.page.HasMore {
		return nil
	}
	last := m.page.Listings[n-1]
	m.starts = append(m.starts, pageStart{id: last.ID, score: last.Score})
	m.cursor = 0
	return m.load()
}

func (m *allPostingsModel) previousPage() tea.Cmd {
	if len(m.starts) == 1 {
		return nil
	}
	m.starts = m.starts[:len(m.starts)-1]
	m.cursor = 0
	return m.load()
}

// typedKey changes the query and waits for a pause before searching.
func (m *allPostingsModel) typedKey(query string) tea.Cmd {
	m.query = query
	src := m.src()
	return tea.Tick(searchPause, func(time.Time) tea.Msg {
		return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), message.Signal[message.SearchDebounce]{Value: message.SearchDebounce{Query: query}})
	})
}

func (m *allPostingsModel) paste(msg tea.PasteMsg) tea.Cmd {
	if !m.searching {
		return nil
	}
	return m.typedKey(m.query + msg.Content)
}

// orderLabel names the order the pages are in.
func (m *allPostingsModel) orderLabel() string {
	if m.order == message.OrderBestFit {
		return "best fit first"
	}
	return "newest first"
}

// pages is how many pages Total fills at the page size.
func (m *allPostingsModel) pages() int {
	if m.page.Limit <= 0 {
		return 1
	}
	return max((m.page.Total+m.page.Limit-1)/m.page.Limit, 1)
}

// fitCell is a posting's current score, blank when it has none and dimmed
// when its content has changed since.
func fitCell(l message.Listing) string {
	if l.Score == 0 {
		return padCol("", fitColWidth)
	}
	cell := padCol(fmt.Sprint(l.Score), fitColWidth)
	if l.ScoreStale {
		return model.DimStyle.Render(cell)
	}
	return cell
}

// locationLabel is a posting's location with its workplace type, when the
// board gave one.
func locationLabel(l message.Listing) string {
	switch {
	case l.WorkplaceType == "":
		return l.Location
	case l.Location == "":
		return l.WorkplaceType
	}
	return l.Location + " (" + l.WorkplaceType + ")"
}

// View renders the view in the last content size.
func (m *allPostingsModel) View() tea.View {
	var b strings.Builder
	help := model.HelpStyle.Render(allPostingsHelp)
	switch {
	case m.searching:
		help = model.HelpStyle.Render(allPostingsSearchHelp)
	case m.query != "":
		help = model.HelpStyle.Render(allPostingsResultsHelp)
	}
	b.WriteString(searchLine(m.searching, m.query, m.page.Total, m.page.WithoutQuery) + "\n")
	fmt.Fprintf(&b, "%s\n", model.DimStyle.Render(fmt.Sprintf("Page %d of %d · %d postings · %s", len(m.starts), m.pages(), m.page.Total, m.orderLabel())))
	switch {
	case len(m.page.Listings) == 0 && m.query != "":
		fmt.Fprintf(&b, "No postings match %q.\n", m.query)
	case len(m.page.Listings) == 0:
		b.WriteString("No open postings.\n")
	default:
		// The page can be read before the rows that fit change, so draw what fits.
		start, end := visibleWindow(m.cursor, len(m.page.Listings), tableRows(m.rows-2, help))
		cursorRow := m.cursor - start
		t := table.New().
			Border(lipgloss.RoundedBorder()).
			Headers("Seen", "Company", "Title", "Department", "Location", "", "Fit").
			StyleFunc(func(row, _ int) lipgloss.Style {
				style := lipgloss.NewStyle().Padding(0, 1)
				if row == cursorRow {
					return style.Inherit(model.CursorStyle)
				}
				return style
			})
		titleWidth := flexColWidth(m.width, seenColWidth, homeCompanyColWidth, departmentColWidth, locationColWidth, markerColWidth, fitColWidth)
		for _, l := range m.page.Listings[start:end] {
			marker := " "
			if l.Interested {
				marker = "★"
			}
			t.Row(
				padCol(l.FirstSeenAt.Local().Format("Jan 2"), seenColWidth),
				padCol(l.CompanyName, homeCompanyColWidth),
				padCol(l.Title, titleWidth),
				padCol(l.Department, departmentColWidth),
				padCol(locationLabel(l), locationColWidth),
				padCol(marker, markerColWidth),
				fitCell(l),
			)
		}
		b.WriteString(t.Render() + "\n")
	}
	b.WriteString(help)
	return tea.NewView(b.String())
}

// keyPressed handles a key. Each command carries a copy of what it needs,
// since it runs off the event loop.
func (m *allPostingsModel) keyPressed(msg tea.KeyPressMsg) tea.Cmd {
	if m.searching {
		return m.updateSearch(msg)
	}
	var current message.Listing
	onPosting := m.cursor < len(m.page.Listings)
	if onPosting {
		current = m.page.Listings[m.cursor]
	}
	about := message.Subject{Type: message.TypePosting, ID: current.ID}
	switch msg.String() {
	case "down", "j":
		if m.cursor < len(m.page.Listings)-1 {
			m.cursor++
		}
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "right", "l":
		return m.nextPage()
	case "left", "h":
		return m.previousPage()
	case "enter":
		if onPosting {
			return message.Push(m.src(), message.ScreenPostingDetail, about, message.PostingDetail{Posting: listingPosting(current)})
		}
	case "s":
		if m.order == message.OrderBestFit {
			m.order = message.OrderNewest
		} else {
			m.order = message.OrderBestFit
		}
		return m.firstPage()
	case "/":
		m.searching = true
	case "esc":
		return m.clearSearch()
	case "i":
		if onPosting {
			return toggleInterested(m.postings, m.src(), current.ID, current.Interested)
		}
	case "x":
		if onPosting {
			return toggleArchived(m.postings, m.src(), current.ID, current.Archived)
		}
	case "o":
		if onPosting && current.JobURL != "" {
			return m.openURL(m.src(), about, current.JobURL)
		}
	}
	return nil
}

// updateSearch handles a key while the search line is open: letters go
// into the query, so only keys that aren't text move or page.
func (m *allPostingsModel) updateSearch(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	default:
		if msg.Text != "" {
			return m.typedKey(m.query + msg.Text)
		}
	case "backspace":
		if r := []rune(m.query); len(r) > 0 {
			return m.typedKey(string(r[:len(r)-1]))
		}
		m.searching = false
	case "esc":
		m.searching = false
		return m.clearSearch()
	case "down", "ctrl+n":
		if m.cursor < len(m.page.Listings)-1 {
			m.cursor++
		}
	case "up", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
		}
	case "right":
		return m.nextPage()
	case "left":
		return m.previousPage()
	case "enter":
		// Keeps the results, so letters act on them instead of being typed.
		m.searching = false
	}
	return nil
}

// clearSearch drops the query and reloads page 1 without it, if there was one.
func (m *allPostingsModel) clearSearch() tea.Cmd {
	if m.query == "" {
		return nil
	}
	m.query = ""
	return m.firstPage()
}
