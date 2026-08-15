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

	"github.com/dklassen/swamp/filter"
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
	screenFilterSelect
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
	postingMarkup   map[int64]store.PostingMarkup
	postingCursor   int
	detailViewport  viewport.Model
	width           int
	height          int

	filterDepartmentOptions   []string
	filterLocationOptions     []string
	filterCursor              int
	filterSelectedDepartments map[string]bool
	filterSelectedLocations   map[string]bool

	// activeFilterDepartments/activeFilterLocations are the company's
	// currently-applied filter values, kept in sync with whatever
	// loadPostings last applied (or, immediately on save, what was just
	// saved) -- displayed in the posting list so the filter state isn't
	// invisible.
	activeFilterDepartments []string
	activeFilterLocations   []string
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

// filterItemAtCursor maps filterCursor into the combined
// departments-then-locations list, returning which field/value it points
// at. ok is false if the cursor is out of range (e.g. no options loaded
// yet).
func (a *App) filterItemAtCursor() (field, value string, ok bool) {
	if a.filterCursor < len(a.filterDepartmentOptions) {
		return "department", a.filterDepartmentOptions[a.filterCursor], true
	}
	idx := a.filterCursor - len(a.filterDepartmentOptions)
	if idx < len(a.filterLocationOptions) {
		return "location", a.filterLocationOptions[idx], true
	}
	return "", "", false
}

// postingMarker renders a posting's markup state as a single-character
// column for the posting list. Archived takes precedence over interested
// when both are set, matching the same precedence the 00003 migration's
// Down path uses when collapsing both flags back into one enum value.
func postingMarker(m store.PostingMarkup) string {
	switch {
	case m.ArchivedAt != nil:
		return "✕"
	case m.InterestedAt != nil:
		return "★"
	default:
		return " "
	}
}

func derefOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
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

func renderFilterOption(label string, checked, isCursor bool) string {
	box := "[ ]"
	if checked {
		box = "[x]"
	}
	line := box + " " + label
	if isCursor {
		return cursorStyle.Render("> "+line) + "\n"
	}
	return "  " + line + "\n"
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

type companyDeletedMsg struct {
	companyID int64
	err       error
}

func deleteCompany(s *store.Store, companyID int64) tea.Cmd {
	return func() tea.Msg {
		err := s.SoftDeleteCompany(context.Background(), companyID)
		return companyDeletedMsg{companyID: companyID, err: err}
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

type postingMarkupUpdatedMsg struct {
	markup store.PostingMarkup
	err    error
}

func toggleInterested(s *store.Store, postingID int64, currentlyInterested bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var (
			m   store.PostingMarkup
			err error
		)
		if currentlyInterested {
			m, err = s.UnmarkPostingInterested(ctx, postingID)
		} else {
			m, err = s.SetPostingInterested(ctx, postingID)
		}
		return postingMarkupUpdatedMsg{markup: m, err: err}
	}
}

func toggleArchived(s *store.Store, postingID int64, currentlyArchived bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var (
			m   store.PostingMarkup
			err error
		)
		if currentlyArchived {
			m, err = s.UnarchivePosting(ctx, postingID)
		} else {
			m, err = s.SetPostingArchived(ctx, postingID)
		}
		return postingMarkupUpdatedMsg{markup: m, err: err}
	}
}

type postingsLoadedMsg struct {
	postings    []store.Posting
	markup      map[int64]store.PostingMarkup
	departments []string
	locations   []string
	err         error
}

// loadPostings re-applies the company's currently-saved filters after
// fetching, since ListPostingsByCompany itself has no notion of
// company_filters (filters gate ingestion only, per the store/sync
// design -- narrowing the displayed list is a TUI-side concern). Without
// this, any reload (including the one triggered right after saving a new
// filter selection) would silently undo the narrowing by loading
// everything unfiltered.
//
// Also loads each visible posting's markup (interested/archived) so the
// list can render it inline -- one GetPostingMarkup call per posting.
// Every posting always has exactly one markup row (created alongside it
// in UpsertPosting), so N+1 here is N cheap local sqlite reads, not N
// round trips to a remote service.
func loadPostings(s *store.Store, companyID int64) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		postings, err := s.ListPostingsByCompany(ctx, companyID)
		if err != nil {
			return postingsLoadedMsg{err: err}
		}
		companyFilters, err := s.ListCompanyFilters(ctx, companyID)
		if err != nil {
			return postingsLoadedMsg{err: err}
		}
		departments, locations := splitCompanyFilters(companyFilters)
		postings = narrowPostingsToFilters(postings, departments, locations)

		markup := make(map[int64]store.PostingMarkup, len(postings))
		for _, p := range postings {
			m, err := s.GetPostingMarkup(ctx, p.ID)
			if err != nil {
				return postingsLoadedMsg{err: err}
			}
			markup[p.ID] = m
		}

		return postingsLoadedMsg{postings: postings, markup: markup, departments: departments, locations: locations}
	}
}

// splitCompanyFilters separates a company's saved filter rows by field.
func splitCompanyFilters(filters []store.CompanyFilter) (departments, locations []string) {
	for _, f := range filters {
		switch f.Field {
		case "department":
			departments = append(departments, f.Value)
		case "location":
			locations = append(locations, f.Value)
		}
	}
	return departments, locations
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set
}

type filterOptionsLoadedMsg struct {
	departments     []string
	locations       []string
	existingFilters []store.CompanyFilter
	err             error
}

func loadFilterOptions(s *store.Store, companyID int64) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		departments, err := s.ListDistinctDepartmentsForCompany(ctx, companyID)
		if err != nil {
			return filterOptionsLoadedMsg{err: err}
		}
		locations, err := s.ListDistinctLocationsForCompany(ctx, companyID)
		if err != nil {
			return filterOptionsLoadedMsg{err: err}
		}
		existing, err := s.ListCompanyFilters(ctx, companyID)
		if err != nil {
			return filterOptionsLoadedMsg{err: err}
		}
		return filterOptionsLoadedMsg{departments: departments, locations: locations, existingFilters: existing}
	}
}

type companyFiltersSavedMsg struct {
	departments []string
	locations   []string
	err         error
}

func saveCompanyFilters(s *store.Store, companyID int64, departments, locations []string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		if err := s.DeleteCompanyFilters(ctx, companyID); err != nil {
			return companyFiltersSavedMsg{err: err}
		}
		for _, d := range departments {
			if _, err := s.CreateCompanyFilter(ctx, companyID, "department", d); err != nil {
				return companyFiltersSavedMsg{err: err}
			}
		}
		for _, l := range locations {
			if _, err := s.CreateCompanyFilter(ctx, companyID, "location", l); err != nil {
				return companyFiltersSavedMsg{err: err}
			}
		}
		return companyFiltersSavedMsg{departments: departments, locations: locations}
	}
}

// selectedFilterValues returns the currently checked department and
// location values from the filter-select screen, in the same order as
// filterDepartmentOptions/filterLocationOptions.
func (a *App) selectedFilterValues() (departments, locations []string) {
	for _, d := range a.filterDepartmentOptions {
		if a.filterSelectedDepartments[d] {
			departments = append(departments, d)
		}
	}
	for _, l := range a.filterLocationOptions {
		if a.filterSelectedLocations[l] {
			locations = append(locations, l)
		}
	}
	return departments, locations
}

// narrowPostingsToFilters keeps only postings matching the given
// department/location values (OR within a field, AND across fields, same
// semantics as filter.Match), for the optimistic client-side narrowing
// shown immediately after saving a filter selection, before the
// background re-sync completes.
func narrowPostingsToFilters(postings []store.Posting, departments, locations []string) []store.Posting {
	rules := make([]filter.Filter, 0, len(departments)+len(locations))
	for _, d := range departments {
		rules = append(rules, filter.Filter{Field: "department", Value: d})
	}
	for _, l := range locations {
		rules = append(rules, filter.Filter{Field: "location", Value: l})
	}

	narrowed := make([]store.Posting, 0, len(postings))
	for _, p := range postings {
		fp := filter.Posting{Department: derefOr(p.Department, ""), Location: derefOr(p.Location, "")}
		match, err := filter.Match(fp, rules)
		if err == nil && match {
			narrowed = append(narrowed, p)
		}
	}
	return narrowed
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
	case companyDeletedMsg:
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
			if r.CompanyID == a.selectedCompany.ID {
				// The company whose postings are currently being viewed
				// just finished a re-sync (e.g. triggered by saving a
				// filter selection) -- reload from the DB so the view
				// becomes authoritative instead of just the optimistic
				// client-side narrowing applied at save time.
				return a, loadPostings(a.store, a.selectedCompany.ID)
			}
		}
	case postingsLoadedMsg:
		a.err = msg.err
		a.postings = msg.postings
		a.postingMarkup = msg.markup
		a.postingCursor = 0
		a.activeFilterDepartments = msg.departments
		a.activeFilterLocations = msg.locations
	case postingMarkupUpdatedMsg:
		a.err = msg.err
		if msg.err == nil {
			if a.postingMarkup == nil {
				a.postingMarkup = make(map[int64]store.PostingMarkup)
			}
			a.postingMarkup[msg.markup.PostingID] = msg.markup
		}
	case filterOptionsLoadedMsg:
		a.err = msg.err
		a.filterDepartmentOptions = msg.departments
		a.filterLocationOptions = msg.locations
		a.filterCursor = 0
		existingDepartments, existingLocations := splitCompanyFilters(msg.existingFilters)
		a.filterSelectedDepartments = toSet(existingDepartments)
		a.filterSelectedLocations = toSet(existingLocations)
	case companyFiltersSavedMsg:
		a.err = msg.err
		if msg.err == nil {
			a.screen = screenPostingList
			a.postings = narrowPostingsToFilters(a.postings, msg.departments, msg.locations)
			a.activeFilterDepartments = msg.departments
			a.activeFilterLocations = msg.locations
			if a.postingCursor >= len(a.postings) {
				a.postingCursor = 0
			}
			return a, refreshCompany(a.syncer, a.selectedCompany.ID, a.selectedCompany.Name)
		}
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
			case msg.String() == "d":
				if a.cursor < len(a.companies) {
					return a, deleteCompany(a.store, a.companies[a.cursor].ID)
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
			case msg.String() == "f":
				a.screen = screenFilterSelect
				return a, loadFilterOptions(a.store, a.selectedCompany.ID)
			case msg.String() == "i":
				if a.postingCursor < len(a.postings) {
					p := a.postings[a.postingCursor]
					currentlyInterested := a.postingMarkup[p.ID].InterestedAt != nil
					return a, toggleInterested(a.store, p.ID, currentlyInterested)
				}
			case msg.String() == "x":
				if a.postingCursor < len(a.postings) {
					p := a.postings[a.postingCursor]
					currentlyArchived := a.postingMarkup[p.ID].ArchivedAt != nil
					return a, toggleArchived(a.store, p.ID, currentlyArchived)
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
		case screenFilterSelect:
			total := len(a.filterDepartmentOptions) + len(a.filterLocationOptions)
			switch {
			case msg.Type == tea.KeyDown, msg.String() == "j":
				if a.filterCursor < total-1 {
					a.filterCursor++
				}
			case msg.Type == tea.KeyUp, msg.String() == "k":
				if a.filterCursor > 0 {
					a.filterCursor--
				}
			case msg.Type == tea.KeySpace:
				field, value, ok := a.filterItemAtCursor()
				if ok {
					switch field {
					case "department":
						a.filterSelectedDepartments[value] = !a.filterSelectedDepartments[value]
					case "location":
						a.filterSelectedLocations[value] = !a.filterSelectedLocations[value]
					}
				}
			case msg.Type == tea.KeyEsc, msg.String() == "b":
				a.screen = screenPostingList
			case msg.Type == tea.KeyEnter:
				departments, locations := a.selectedFilterValues()
				return a, saveCompanyFilters(a.store, a.selectedCompany.ID, departments, locations)
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
		if summary := filterSummaryLine(a.activeFilterDepartments, a.activeFilterLocations); summary != "" {
			b.WriteString(helpStyle.Render(summary) + "\n")
		}
		if len(a.postings) == 0 {
			b.WriteString("No postings yet. Press 'r' from the company list to refresh.\n")
		}
		start, end := visibleWindow(a.postingCursor, len(a.postings), a.listRows())
		for i := start; i < end; i++ {
			p := a.postings[i]
			marker := postingMarker(a.postingMarkup[p.ID])
			line := fmt.Sprintf("%s %s | %s | %s | %s", marker, p.Title, derefOr(p.Department, ""), derefOr(p.Location, ""), p.ListingStatus)
			if i == a.postingCursor {
				b.WriteString(cursorStyle.Render("> "+line) + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
		b.WriteString(helpStyle.Render("↑/↓ (j/k): select  enter: view detail  o: open in browser  f: filters  i: interested  x: archive  esc/b: back"))
	case screenPostingDetail:
		if a.postingCursor < len(a.postings) {
			b.WriteString(titleStyle.Render(a.postings[a.postingCursor].Title) + "\n")
		}
		b.WriteString(a.detailViewport.View() + "\n")
		b.WriteString(helpStyle.Render("↑/↓ (j/k): scroll  ←/→ (h/l): prev/next posting  o: open in browser  esc/b: back"))
	case screenFilterSelect:
		b.WriteString(titleStyle.Render(fmt.Sprintf("Filters: %s", a.selectedCompany.Name)) + "\n")
		if len(a.filterDepartmentOptions) == 0 && len(a.filterLocationOptions) == 0 {
			b.WriteString("No department/location values discovered yet -- refresh the company first.\n")
		}
		idx := 0
		if len(a.filterDepartmentOptions) > 0 {
			b.WriteString(fieldLabel.Render("Department") + "\n")
			for _, d := range a.filterDepartmentOptions {
				b.WriteString(renderFilterOption(d, a.filterSelectedDepartments[d], idx == a.filterCursor))
				idx++
			}
		}
		if len(a.filterLocationOptions) > 0 {
			b.WriteString(fieldLabel.Render("Location") + "\n")
			for _, l := range a.filterLocationOptions {
				b.WriteString(renderFilterOption(l, a.filterSelectedLocations[l], idx == a.filterCursor))
				idx++
			}
		}
		b.WriteString(helpStyle.Render("↑/↓ (j/k): select  space: toggle  enter: save  esc/b: cancel"))
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
		b.WriteString(helpStyle.Render("↑/↓ (j/k): select  enter: view postings  a: add  d: delete  r: refresh  q: quit"))
	}

	return b.String()
}
