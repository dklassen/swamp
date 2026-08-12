// Package tui is the terminal UI: Bubble Tea models for browsing and
// managing companies and their postings. Markup editing (status, notes,
// tags, interview stages) is a later increment. Update stays
// side-effect-free -- store calls are wrapped in tea.Cmd so the
// message-passing logic is testable without a real terminal; View
// (rendering) is not held to automated test coverage, per this project's
// testing decisions.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).MarginBottom(1)
	cursorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).MarginTop(1)
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	fieldLabel   = lipgloss.NewStyle().Bold(true)
	focusedLabel = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
)

type screen int

const (
	screenCompanyList screen = iota
	screenCompanyForm
	screenPostingList
	screenPostingDetail
)

// formInputs indices: name, then source ref. Source is fixed to "ashby"
// for now (the only supported job board), so there's no source picker.
const (
	formFieldName = iota
	formFieldSourceRef
	formFieldCount
)

type App struct {
	store           *store.Store
	syncer          *sync.Syncer
	companies       []store.Company
	cursor          int
	screen          screen
	formInputs      []textinput.Model
	formFocus       int
	status          string
	err             error
	selectedCompany store.Company
	postings        []store.Posting
	postingCursor   int
	detailViewport  viewport.Model
	width           int
	height          int
}

// chromeRows is the number of lines View() spends on title/help text
// around a list, reserved when computing how many rows are free for the
// list itself.
const chromeRows = 3

func (a *App) listRows() int {
	rows := a.height - chromeRows
	if rows < 0 {
		rows = 0
	}
	return rows
}

func derefOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}

func New(s *store.Store, syncer *sync.Syncer) *App {
	inputs := make([]textinput.Model, formFieldCount)
	for i := range inputs {
		inputs[i] = textinput.New()
	}
	return &App{store: s, syncer: syncer, formInputs: inputs, detailViewport: viewport.New(0, 0)}
}

// postingDetailContent renders a posting's fields and description as the
// scrollable body of the detail view (title is rendered separately, as a
// fixed header outside the viewport).
func postingDetailContent(p store.Posting) string {
	var b strings.Builder
	fields := []struct{ label, value string }{
		{"Department", derefOr(p.Department, "")},
		{"Team", derefOr(p.Team, "")},
		{"Location", derefOr(p.Location, "")},
		{"Employment type", derefOr(p.EmploymentType, "")},
		{"Workplace type", derefOr(p.WorkplaceType, "")},
		{"Status", p.ListingStatus},
		{"Job URL", derefOr(p.JobURL, "")},
		{"Application URL", derefOr(p.ApplicationURL, "")},
	}
	for _, f := range fields {
		if f.value == "" {
			continue
		}
		b.WriteString(fieldLabel.Render(f.label+":") + " " + f.value + "\n")
	}
	if desc := derefOr(p.DescriptionText, ""); desc != "" {
		b.WriteString("\n" + desc + "\n")
	}
	return b.String()
}

// showPostingDetail (re)sizes the detail viewport from the current window
// dimensions and loads the currently-selected posting's content into it,
// resetting scroll to the top -- called both when first entering the
// detail screen and when moving to a different posting within it.
func (a *App) showPostingDetail() {
	a.detailViewport.Width = a.width
	a.detailViewport.Height = a.listRows()
	if a.postingCursor < len(a.postings) {
		content := postingDetailContent(a.postings[a.postingCursor])
		a.detailViewport.SetContent(wrapToWidth(content, a.detailViewport.Width))
	}
	a.detailViewport.GotoTop()
}

type companiesLoadedMsg struct {
	companies []store.Company
	err       error
}

func loadCompanies(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		companies, err := s.ListActiveCompanies(context.Background())
		return companiesLoadedMsg{companies: companies, err: err}
	}
}

func (a *App) Init() tea.Cmd {
	return loadCompanies(a.store)
}

type companyCreatedMsg struct {
	company store.Company
	err     error
}

func createCompany(s *store.Store, name, sourceRef string) tea.Cmd {
	return func() tea.Msg {
		company, err := s.CreateCompany(context.Background(), name, "ashby", sourceRef)
		return companyCreatedMsg{company: company, err: err}
	}
}

type companyPausedMsg struct {
	companyID int64
	err       error
}

func pauseCompany(s *store.Store, companyID int64) tea.Cmd {
	return func() tea.Msg {
		err := s.SoftDeleteCompany(context.Background(), companyID)
		return companyPausedMsg{companyID: companyID, err: err}
	}
}

type companyRefreshedMsg struct {
	companyName string
	result      sync.Result
	err         error
}

func refreshCompany(syncer *sync.Syncer, companyID int64, companyName string) tea.Cmd {
	return func() tea.Msg {
		result, err := syncer.SyncCompany(context.Background(), companyID)
		return companyRefreshedMsg{companyName: companyName, result: result, err: err}
	}
}

type postingsLoadedMsg struct {
	postings []store.Posting
	err      error
}

func loadPostings(s *store.Store, companyID int64) tea.Cmd {
	return func() tea.Msg {
		postings, err := s.ListPostingsByCompany(context.Background(), companyID)
		return postingsLoadedMsg{postings: postings, err: err}
	}
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case companiesLoadedMsg:
		a.err = msg.err
		a.companies = msg.companies
	case companyCreatedMsg:
		a.err = msg.err
		if msg.err == nil {
			a.companies = append(a.companies, msg.company)
			a.screen = screenCompanyList
		}
	case companyPausedMsg:
		a.err = msg.err
		if msg.err == nil {
			for i, c := range a.companies {
				if c.ID == msg.companyID {
					a.companies = append(a.companies[:i], a.companies[i+1:]...)
					break
				}
			}
			if a.cursor >= len(a.companies) && a.cursor > 0 {
				a.cursor = len(a.companies) - 1
			}
		}
	case companyRefreshedMsg:
		a.err = msg.err
		if msg.err == nil {
			r := msg.result
			a.status = fmt.Sprintf("%s: fetched %d, created %d, updated %d, closed %d, reopened %d",
				msg.companyName, r.Fetched, r.Created, r.Updated, r.Closed, r.Reopened)
		}
	case postingsLoadedMsg:
		a.err = msg.err
		a.postings = msg.postings
		a.postingCursor = 0
	case browserOpenedMsg:
		a.err = msg.err
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		if a.screen == screenPostingDetail {
			// Re-wrap at the new width: viewport.View() truncates rather
			// than re-wrapping already-set content when Width shrinks, so
			// just resizing the fields isn't enough. showPostingDetail
			// also resizes detailViewport itself from a.width/a.height.
			// When not on the detail screen, sizing happens fresh the
			// next time it's entered, so nothing to do here.
			a.showPostingDetail()
		}
	case tea.KeyMsg:
		switch a.screen {
		case screenCompanyList:
			switch {
			case msg.Type == tea.KeyDown, msg.String() == "j":
				if a.cursor < len(a.companies)-1 {
					a.cursor++
				}
			case msg.Type == tea.KeyUp, msg.String() == "k":
				if a.cursor > 0 {
					a.cursor--
				}
			case msg.String() == "q":
				return a, tea.Quit
			case msg.String() == "p":
				if a.cursor < len(a.companies) {
					return a, pauseCompany(a.store, a.companies[a.cursor].ID)
				}
			case msg.String() == "r":
				if a.cursor < len(a.companies) {
					c := a.companies[a.cursor]
					return a, refreshCompany(a.syncer, c.ID, c.Name)
				}
			case msg.String() == "a":
				a.screen = screenCompanyForm
				a.formFocus = 0
				for i := range a.formInputs {
					a.formInputs[i].SetValue("")
					a.formInputs[i].Blur()
				}
				a.formInputs[0].Focus()
			case msg.Type == tea.KeyEnter:
				if a.cursor < len(a.companies) {
					a.selectedCompany = a.companies[a.cursor]
					a.screen = screenPostingList
					return a, loadPostings(a.store, a.selectedCompany.ID)
				}
			}
		case screenPostingList:
			switch {
			case msg.Type == tea.KeyDown, msg.String() == "j":
				if a.postingCursor < len(a.postings)-1 {
					a.postingCursor++
				}
			case msg.Type == tea.KeyUp, msg.String() == "k":
				if a.postingCursor > 0 {
					a.postingCursor--
				}
			case msg.Type == tea.KeyEsc, msg.String() == "b":
				a.screen = screenCompanyList
			case msg.Type == tea.KeyEnter:
				if a.postingCursor < len(a.postings) {
					a.screen = screenPostingDetail
					a.showPostingDetail()
				}
			case msg.String() == "o":
				if a.postingCursor < len(a.postings) {
					url := a.postings[a.postingCursor].JobURL
					if url != nil && *url != "" {
						return a, openInBrowser(*url)
					}
				}
			}
		case screenPostingDetail:
			switch {
			case msg.Type == tea.KeyRight, msg.String() == "l":
				if a.postingCursor < len(a.postings)-1 {
					a.postingCursor++
					a.showPostingDetail()
				}
			case msg.Type == tea.KeyLeft, msg.String() == "h":
				if a.postingCursor > 0 {
					a.postingCursor--
					a.showPostingDetail()
				}
			case msg.Type == tea.KeyEsc, msg.String() == "b":
				a.screen = screenPostingList
			case msg.String() == "o":
				if a.postingCursor < len(a.postings) {
					url := a.postings[a.postingCursor].JobURL
					if url != nil && *url != "" {
						return a, openInBrowser(*url)
					}
				}
			default:
				var cmd tea.Cmd
				a.detailViewport, cmd = a.detailViewport.Update(msg)
				return a, cmd
			}
		case screenCompanyForm:
			if msg.Type == tea.KeyEsc {
				a.screen = screenCompanyList
				return a, nil
			}
			if msg.Type == tea.KeyTab {
				a.formInputs[a.formFocus].Blur()
				a.formFocus = (a.formFocus + 1) % formFieldCount
				a.formInputs[a.formFocus].Focus()
				return a, nil
			}
			if msg.Type == tea.KeyEnter {
				name := a.formInputs[formFieldName].Value()
				sourceRef := a.formInputs[formFieldSourceRef].Value()
				if name == "" || sourceRef == "" {
					return a, nil
				}
				return a, createCompany(a.store, name, sourceRef)
			}
			var cmd tea.Cmd
			a.formInputs[a.formFocus], cmd = a.formInputs[a.formFocus].Update(msg)
			return a, cmd
		}
	}
	return a, nil
}

func (a *App) View() string {
	var b strings.Builder

	if a.err != nil {
		b.WriteString(errStyle.Render(fmt.Sprintf("error: %v", a.err)) + "\n\n")
	} else if a.status != "" {
		b.WriteString(helpStyle.Render(a.status) + "\n\n")
	}

	switch a.screen {
	case screenCompanyForm:
		b.WriteString(titleStyle.Render("Add company") + "\n")
		labels := []string{"Name", "Ashby slug"}
		for i, input := range a.formInputs {
			label := fieldLabel
			if i == a.formFocus {
				label = focusedLabel
			}
			b.WriteString(label.Render(labels[i]+":") + " " + input.View() + "\n")
		}
		b.WriteString(helpStyle.Render("tab: next field  enter: save  esc: cancel"))
	case screenPostingList:
		b.WriteString(titleStyle.Render(fmt.Sprintf("Postings: %s", a.selectedCompany.Name)) + "\n")
		if len(a.postings) == 0 {
			b.WriteString("No postings yet. Press 'r' from the company list to refresh.\n")
		}
		start, end := visibleWindow(a.postingCursor, len(a.postings), a.listRows())
		for i := start; i < end; i++ {
			p := a.postings[i]
			line := fmt.Sprintf("%s | %s | %s | %s", p.Title, derefOr(p.Department, ""), derefOr(p.Location, ""), p.ListingStatus)
			if i == a.postingCursor {
				b.WriteString(cursorStyle.Render("> "+line) + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
		b.WriteString(helpStyle.Render("↑/↓ (j/k): select  enter: view detail  o: open in browser  esc/b: back"))
	case screenPostingDetail:
		if a.postingCursor < len(a.postings) {
			b.WriteString(titleStyle.Render(a.postings[a.postingCursor].Title) + "\n")
		}
		b.WriteString(a.detailViewport.View() + "\n")
		b.WriteString(helpStyle.Render("↑/↓ (j/k): scroll  ←/→ (h/l): prev/next posting  o: open in browser  esc/b: back"))
	default:
		b.WriteString(titleStyle.Render("Companies") + "\n")
		if len(a.companies) == 0 {
			b.WriteString("No companies yet. Press 'a' to add one.\n")
		}
		start, end := visibleWindow(a.cursor, len(a.companies), a.listRows())
		for i := start; i < end; i++ {
			c := a.companies[i]
			line := fmt.Sprintf("%s (%s)", c.Name, c.SourceRef)
			if i == a.cursor {
				b.WriteString(cursorStyle.Render("> "+line) + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
		b.WriteString(helpStyle.Render("↑/↓ (j/k): select  enter: view postings  a: add  p: pause  r: refresh  q: quit"))
	}

	return b.String()
}
