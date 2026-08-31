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
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dklassen/swamp/documents"
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
	screenCompanyEdit
	screenPostingList
	screenPostingDetail
	screenFilterSelect
	screenApplicationStatusSelect
	screenApplicationNotesEdit
)

type App struct {
	store           *store.Store
	syncer          *sync.Syncer
	companies       []store.Company
	companyList     companyListModel
	screen          screen
	companyForm     companyFormModel
	companyEdit     companyEditModel
	status          string
	err             error
	selectedCompany store.Company
	postings        []store.Posting
	postingMarkup   map[int64]store.PostingMarkup
	postingList     postingListModel
	postingDetail   postingDetailModel
	// applicationsByPosting holds the application for each posting_id that
	// has one (fetched async on entering posting detail -- see
	// loadApplication). A posting with no entry has no application yet
	// (Application is created lazily, unlike PostingMarkup).
	applicationsByPosting map[int64]store.Application
	applicationStatus     applicationStatusModel
	applicationNotes      applicationNotesModel
	// documents resolves an application's document paths, hiding the
	// path convention and base directory the same way store hides
	// schema/SQL details -- threaded through from SWAMP_DOCUMENTS_PATH,
	// mirroring how SWAMP_DB_PATH configures the store.
	documents *documents.Store
	// hideArchived is ephemeral, in-memory-only display state -- not
	// persisted (see decisions.log). Defaults true: the point of
	// archiving a posting is to declutter the list.
	hideArchived bool
	width        int
	height       int

	filterSelect filterSelectModel

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

func derefOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
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

func New(s *store.Store, syncer *sync.Syncer, docs *documents.Store) *App {
	return &App{
		store:        s,
		syncer:       syncer,
		companyList:  newCompanyListModel(s, syncer),
		companyForm:  newCompanyFormModel(s),
		postingList:  newPostingListModel(s),
		hideArchived: true,
		documents:    docs,
	}
}

// documentStatusLine renders a single "<label>: found (<path>)" or
// "<label>: not found (<path>)" line for the documents section.
func documentStatusLine(label string, exists bool, path string) string {
	status := "not found"
	if exists {
		status = "found"
	}
	return fieldLabel.Render(label+":") + " " + status + " (" + path + ")\n"
}

// postingDetailContent renders a posting's fields, application state, and
// description as the scrollable body of the detail view (title is rendered
// separately, as a fixed header outside the viewport). application/
// hasApplication are passed in rather than fetched here so this stays a
// pure function of already-loaded state -- the async fetch happens
// separately via loadApplication (see newPostingDetailModel).
//
// docs resolves the application's document paths and their presence via
// os.Stat -- done inline here rather than through a separate
// tea.Cmd/tea.Msg round trip like the rest of this file's store-backed
// state, since checking whether two local files exist is cheap/local
// enough that a second async fetch just to avoid it here would be
// over-applying that convention (see decisions.log). When hasApplication
// is false, no documents section is rendered at all -- "no application
// -> show nothing".
func postingDetailContent(p store.Posting, application store.Application, hasApplication bool, docs *documents.Store) string {
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
	if hasApplication {
		b.WriteString(fieldLabel.Render("Application status:") + " " + application.Status.String() + "\n")
		if application.Notes != "" {
			b.WriteString(fieldLabel.Render("Application notes:") + " " + application.Notes + "\n")
		}
		status := docs.Status(application.ID)
		b.WriteString("\n" + fieldLabel.Render("Documents") + "\n")
		b.WriteString(documentStatusLine("Cover Letter", status.CoverLetter.Exists, status.CoverLetter.Path))
		b.WriteString(documentStatusLine("Resume", status.Resume.Exists, status.Resume.Path))
	} else {
		b.WriteString(helpStyle.Render("No application started -- press 'a' to start one.") + "\n")
	}
	if desc := derefOr(p.DescriptionText, ""); desc != "" {
		b.WriteString("\n" + desc + "\n")
	}
	return b.String()
}

// lookupPosting finds id in a.postings, returning it along with its
// application (if any) from a.applicationsByPosting -- used to seed a
// fresh postingDetailModel by ID, since that model doesn't hold a
// reference to either collection itself.
func (a *App) lookupPosting(id int64) (store.Posting, store.Application, bool) {
	var p store.Posting
	for _, candidate := range a.postings {
		if candidate.ID == id {
			p = candidate
			break
		}
	}
	app, hasApp := a.applicationsByPosting[id]
	return p, app, hasApp
}

// indexOfPosting returns id's index in postings, or -1 if not present.
func indexOfPosting(postings []store.Posting, id int64) int {
	for i, p := range postings {
		if p.ID == id {
			return i
		}
	}
	return -1
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

// sortCompaniesByName keeps an in-memory companies slice in the same
// order ListActiveCompanies' `ORDER BY name` would return -- called after
// any in-place mutation (create, rename) that could change where an
// entry belongs, since Go's string `<` matches SQLite's default byte-wise
// collation.
func sortCompaniesByName(companies []store.Company) {
	sort.Slice(companies, func(i, j int) bool {
		return companies[i].Name < companies[j].Name
	})
}

func (a *App) Init() tea.Cmd {
	return loadCompanies(a.store)
}

type companyCreatedMsg struct {
	company store.Company
	err     error
}

func createCompany(s *store.Store, name, source, sourceRef string) tea.Cmd {
	return func() tea.Msg {
		company, err := s.CreateCompany(context.Background(), name, source, sourceRef)
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

type companyNameUpdatedMsg struct {
	company store.Company
	err     error
}

func updateCompanyName(s *store.Store, companyID int64, name string) tea.Cmd {
	return func() tea.Msg {
		company, err := s.UpdateCompanyName(context.Background(), companyID, name)
		return companyNameUpdatedMsg{company: company, err: err}
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

type applicationCreatedMsg struct {
	application store.Application
	err         error
}

func createApplication(s *store.Store, postingID int64) tea.Cmd {
	return func() tea.Msg {
		app, err := s.CreateApplication(context.Background(), postingID)
		return applicationCreatedMsg{application: app, err: err}
	}
}

type applicationLoadedMsg struct {
	postingID   int64
	application store.Application
	found       bool
	err         error
}

// loadApplication fetches the application for a posting, if one exists.
// GetApplication returns ErrNotFound when the posting has no application
// yet (the common case, since Application is created lazily via 'a' on the
// detail screen rather than alongside every posting) -- that's translated
// to found=false here rather than surfaced as an error.
func loadApplication(s *store.Store, postingID int64) tea.Cmd {
	return func() tea.Msg {
		app, err := s.GetApplication(context.Background(), postingID)
		if errors.Is(err, store.ErrNotFound) {
			return applicationLoadedMsg{postingID: postingID, found: false}
		}
		if err != nil {
			return applicationLoadedMsg{postingID: postingID, err: err}
		}
		return applicationLoadedMsg{postingID: postingID, application: app, found: true}
	}
}

type applicationStatusUpdatedMsg struct {
	application store.Application
	err         error
}

func updateApplicationStatus(s *store.Store, postingID int64, status store.ApplicationStatus) tea.Cmd {
	return func() tea.Msg {
		app, err := s.UpdateApplicationStatus(context.Background(), postingID, status)
		return applicationStatusUpdatedMsg{application: app, err: err}
	}
}

type applicationNotesUpdatedMsg struct {
	application store.Application
	err         error
}

func updateApplicationNotes(s *store.Store, postingID int64, notes string) tea.Cmd {
	return func() tea.Msg {
		app, err := s.UpdateApplicationNotes(context.Background(), postingID, notes)
		return applicationNotesUpdatedMsg{application: app, err: err}
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
//
// hideArchived additionally drops archived postings from the result --
// ephemeral TUI display state (see the App.hideArchived field), not a
// company_filters row, so it's applied here rather than at the
// ingestion-gating layer.
func loadPostings(s *store.Store, companyID int64, hideArchived bool) tea.Cmd {
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

		if hideArchived {
			postings = filterOutArchived(postings, markup)
		}

		return postingsLoadedMsg{postings: postings, markup: markup, departments: departments, locations: locations}
	}
}

// filterOutArchived drops postings whose markup has ArchivedAt set.
func filterOutArchived(postings []store.Posting, markup map[int64]store.PostingMarkup) []store.Posting {
	visible := make([]store.Posting, 0, len(postings))
	for _, p := range postings {
		if markup[p.ID].ArchivedAt != nil {
			continue
		}
		visible = append(visible, p)
	}
	return visible
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
			sortCompaniesByName(a.companies)
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
			a.companyList.clampCursor(len(a.companies))
		}
	case companyNameUpdatedMsg:
		a.err = msg.err
		if msg.err == nil {
			for i, c := range a.companies {
				if c.ID == msg.company.ID {
					a.companies[i] = msg.company
					break
				}
			}
			sortCompaniesByName(a.companies)
			a.screen = screenCompanyList
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
				return a, loadPostings(a.store, a.selectedCompany.ID, a.hideArchived)
			}
		}
	case postingsLoadedMsg:
		a.err = msg.err
		a.postings = msg.postings
		a.postingMarkup = msg.markup
		a.postingList.resetCursor()
		a.activeFilterDepartments = msg.departments
		a.activeFilterLocations = msg.locations
	case postingMarkupUpdatedMsg:
		a.err = msg.err
		if msg.err == nil {
			if a.postingMarkup == nil {
				a.postingMarkup = make(map[int64]store.PostingMarkup)
			}
			a.postingMarkup[msg.markup.PostingID] = msg.markup
			// Archiving a posting while hideArchived is on should remove
			// it from view immediately, not just once the list is next
			// reloaded -- consistent with the fact that unarchiving it
			// again requires pressing 'A' first (it's no longer
			// reachable by cursor once hidden).
			if a.hideArchived && msg.markup.ArchivedAt != nil {
				for i, p := range a.postings {
					if p.ID == msg.markup.PostingID {
						a.postings = append(a.postings[:i], a.postings[i+1:]...)
						break
					}
				}
				a.postingList.clampCursor(len(a.postings))
			}
		}
	case applicationLoadedMsg:
		a.err = msg.err
		if msg.err == nil {
			if a.applicationsByPosting == nil {
				a.applicationsByPosting = make(map[int64]store.Application)
			}
			if msg.found {
				a.applicationsByPosting[msg.postingID] = msg.application
			} else {
				delete(a.applicationsByPosting, msg.postingID)
			}
			if a.screen == screenPostingDetail {
				p, app, hasApp := a.lookupPosting(a.postingDetail.posting.ID)
				a.postingDetail = newPostingDetailModel(a.store, a.documents, a.width, a.listRows(), p, app, hasApp)
			}
		}
	case applicationCreatedMsg:
		a.err = msg.err
		if msg.err == nil {
			if a.applicationsByPosting == nil {
				a.applicationsByPosting = make(map[int64]store.Application)
			}
			a.applicationsByPosting[msg.application.PostingID] = msg.application
			if a.screen == screenPostingDetail {
				p, app, hasApp := a.lookupPosting(a.postingDetail.posting.ID)
				a.postingDetail = newPostingDetailModel(a.store, a.documents, a.width, a.listRows(), p, app, hasApp)
			}
		}
	case applicationStatusUpdatedMsg:
		a.err = msg.err
		if msg.err == nil {
			if a.applicationsByPosting == nil {
				a.applicationsByPosting = make(map[int64]store.Application)
			}
			a.applicationsByPosting[msg.application.PostingID] = msg.application
			a.screen = screenPostingDetail
			p, app, hasApp := a.lookupPosting(a.postingDetail.posting.ID)
			a.postingDetail = newPostingDetailModel(a.store, a.documents, a.width, a.listRows(), p, app, hasApp)
		}
	case applicationNotesUpdatedMsg:
		a.err = msg.err
		if msg.err == nil {
			if a.applicationsByPosting == nil {
				a.applicationsByPosting = make(map[int64]store.Application)
			}
			a.applicationsByPosting[msg.application.PostingID] = msg.application
			a.screen = screenPostingDetail
			p, app, hasApp := a.lookupPosting(a.postingDetail.posting.ID)
			a.postingDetail = newPostingDetailModel(a.store, a.documents, a.width, a.listRows(), p, app, hasApp)
		}
	case filterOptionsLoadedMsg:
		a.err = msg.err
		a.filterSelect = newFilterSelectModel(a.store, a.selectedCompany.ID, a.selectedCompany.Name, msg.departments, msg.locations, msg.existingFilters)
	case companyFiltersSavedMsg:
		a.err = msg.err
		if msg.err == nil {
			a.screen = screenPostingList
			a.postings = narrowPostingsToFilters(a.postings, msg.departments, msg.locations)
			a.activeFilterDepartments = msg.departments
			a.activeFilterLocations = msg.locations
			a.postingList.resetCursorIfOutOfBounds(len(a.postings))
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
			// just resizing the fields isn't enough. postingDetail.resize
			// rebuilds the viewport at the new dimensions. When not on the
			// detail screen, sizing happens fresh the next time it's
			// entered, so nothing to do here.
			a.postingDetail.resize(a.width, a.listRows())
		}
	case tea.KeyMsg:
		prevScreen := a.screen
		model, cmd := a.updateKeyMsg(msg)
		if a.screen != prevScreen {
			// A stale error from whatever screen the user just left (e.g.
			// a failed save they then cancelled out of) shouldn't keep
			// showing on the screen they land on next -- see #41.
			a.err = nil
		}
		return model, cmd
	}
	return a, nil
}

func (a *App) updateKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch a.screen {
	case screenCompanyList:
		cmd, intent := a.companyList.Update(msg, a.companies)
		switch v := intent.(type) {
		case enterCompanyFormMsg:
			a.screen = screenCompanyForm
			a.companyForm = newCompanyFormModel(a.store)
		case enterCompanyEditMsg:
			a.screen = screenCompanyEdit
			a.companyEdit = newCompanyEditModel(a.store, v.company.ID, v.company.Name)
		case selectCompanyMsg:
			a.selectedCompany = v.company
			a.screen = screenPostingList
			return a, loadPostings(a.store, a.selectedCompany.ID, a.hideArchived)
		}
		return a, cmd
	case screenPostingList:
		snap := postingListSnapshot{
			companyName:             a.selectedCompany.Name,
			postings:                a.postings,
			markup:                  a.postingMarkup,
			hideArchived:            a.hideArchived,
			activeFilterDepartments: a.activeFilterDepartments,
			activeFilterLocations:   a.activeFilterLocations,
		}
		cmd, intent := a.postingList.Update(msg, snap)
		switch v := intent.(type) {
		case backToCompanyListMsg:
			a.screen = screenCompanyList
		case enterPostingDetailMsg:
			p, app, hasApp := a.lookupPosting(v.postingID)
			a.postingDetail = newPostingDetailModel(a.store, a.documents, a.width, a.listRows(), p, app, hasApp)
			a.screen = screenPostingDetail
			return a, loadApplication(a.store, p.ID)
		case enterFilterSelectMsg:
			a.screen = screenFilterSelect
			return a, loadFilterOptions(a.store, a.selectedCompany.ID)
		case toggleHideArchivedMsg:
			a.hideArchived = !a.hideArchived
			return a, loadPostings(a.store, a.selectedCompany.ID, a.hideArchived)
		}
		return a, cmd
	case screenPostingDetail:
		cmd, intent := a.postingDetail.Update(msg)
		switch v := intent.(type) {
		case backToPostingListMsg:
			a.screen = screenPostingList
			a.postingList.setCursor(indexOfPosting(a.postings, a.postingDetail.posting.ID))
		case navigatePostingMsg:
			idx := indexOfPosting(a.postings, v.postingID)
			newIdx := idx + v.direction
			if idx >= 0 && newIdx >= 0 && newIdx < len(a.postings) {
				p := a.postings[newIdx]
				app, hasApp := a.applicationsByPosting[p.ID]
				a.postingDetail = newPostingDetailModel(a.store, a.documents, a.width, a.listRows(), p, app, hasApp)
				return a, loadApplication(a.store, p.ID)
			}
		case enterApplicationStatusMsg:
			a.screen = screenApplicationStatusSelect
			a.applicationStatus = newApplicationStatusModel(a.store, v.postingID, v.currentStatus)
		case enterApplicationNotesMsg:
			a.screen = screenApplicationNotesEdit
			a.applicationNotes = newApplicationNotesModel(a.store, v.postingID, v.currentNotes, a.width, a.listRows())
		}
		return a, cmd
	case screenApplicationStatusSelect:
		cmd, intent := a.applicationStatus.Update(msg)
		if _, ok := intent.(cancelApplicationStatusMsg); ok {
			a.screen = screenPostingDetail
		}
		return a, cmd
	case screenApplicationNotesEdit:
		cmd, intent := a.applicationNotes.Update(msg)
		if _, ok := intent.(cancelApplicationNotesMsg); ok {
			a.screen = screenPostingDetail
		}
		return a, cmd
	case screenFilterSelect:
		cmd, intent := a.filterSelect.Update(msg)
		if _, ok := intent.(cancelFilterSelectMsg); ok {
			a.screen = screenPostingList
		}
		return a, cmd
	case screenCompanyForm:
		cmd, intent := a.companyForm.Update(msg)
		if _, ok := intent.(cancelCompanyFormMsg); ok {
			a.screen = screenCompanyList
		}
		return a, cmd
	case screenCompanyEdit:
		cmd, intent := a.companyEdit.Update(msg)
		if _, ok := intent.(cancelCompanyEditMsg); ok {
			a.screen = screenCompanyList
		}
		return a, cmd
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
		b.WriteString(a.companyForm.View())
	case screenCompanyEdit:
		b.WriteString(a.companyEdit.View())
	case screenPostingList:
		snap := postingListSnapshot{
			companyName:             a.selectedCompany.Name,
			postings:                a.postings,
			markup:                  a.postingMarkup,
			hideArchived:            a.hideArchived,
			activeFilterDepartments: a.activeFilterDepartments,
			activeFilterLocations:   a.activeFilterLocations,
		}
		b.WriteString(a.postingList.View(snap, a.listRows()))
	case screenPostingDetail:
		b.WriteString(a.postingDetail.View())
	case screenApplicationStatusSelect:
		b.WriteString(a.applicationStatus.View())
	case screenApplicationNotesEdit:
		b.WriteString(a.applicationNotes.View())
	case screenFilterSelect:
		b.WriteString(a.filterSelect.View(a.listRows()))
	default:
		b.WriteString(a.companyList.View(a.companies, a.listRows()))
	}

	return b.String()
}
