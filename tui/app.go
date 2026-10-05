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
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/cellbuf"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/filter"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).MarginBottom(1)
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).MarginTop(1)
	// dimStyle is helpStyle's same muted color without its MarginTop(1) --
	// that margin renders a leading blank line, which is right for the
	// screen-bottom help line helpStyle exists for but breaks any inline
	// use (a table cell, a line that isn't the very start of the view).
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	passStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	fieldLabel   = lipgloss.NewStyle().Bold(true)
	focusedLabel = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	sectionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
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
	screenDocumentReviewSelect
	screenDocumentReviewForm
	screenActiveApplications
	screenApplicationDetail
	screenApplicationExport
	screenApplicationSubmit
	screenApplicationForm
	screenApplicationDelete
)

type App struct {
	store     *store.Store
	syncer    *sync.Syncer
	companies []store.Company
	// companyOpenPostings is each company's open, unarchived posting count
	// (store.CountOpenPostingsByCompany), loaded alongside companies.
	companyOpenPostings map[int64]int
	companyList         companyListModel
	screen              screen
	companyForm         companyFormModel
	companyEdit         companyEditModel
	status              string
	err                 error
	// syncAll is the sync-all run, if one is or was under way (#153).
	syncAll         syncAllState
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
	applicationForm       applicationFormModel
	documentReviewSelect  documentReviewSelectModel
	documentReviewForm    documentReviewFormModel
	// returnStack records where the user came from for each screen that
	// can be entered from more than one place: screenPostingDetail,
	// screenApplicationStatusSelect, and the document review screens
	// (select and form count as one step). Entering one pushes the
	// screen being left; backing out or finishing pops it. It's a stack
	// rather than one field per screen because these nest (e.g. posting
	// list -> posting detail -> status select), and one push/pop pair
	// per transition keeps a new entry point from needing its own field.
	// See decisions.log, issue #89.
	returnStack        []screen
	lastScreenInstance screenInstance
	// activeApplications backs the home screen: every application not at
	// a terminal dead-end status, across every company (see
	// store.ListActiveApplications, decisions.log #43).
	activeApplications []store.ApplicationView
	// activeApplicationProgress is each active application's document
	// progress, by application ID, loaded with activeApplications. The
	// home screen derives each row's next step from it and the row's
	// LatestReviews when it renders, so patching one row's reviews (#91)
	// moves its next step on too.
	activeApplicationProgress map[int64]map[documents.Type]documentProgress
	activeApplicationList     activeApplicationListModel
	applicationDetail         applicationDetailModel
	applicationExport         applicationExportModel
	// exportDir is the destination the export screen prefills: the last
	// directory successfully exported to this session, falling back to
	// defaultExportDir. Ephemeral and in-memory only, like hideArchived
	// -- a remembered path is a within-session convenience, not
	// something worth a schema change to persist.
	exportDir string
	// openURL opens a URL in the browser: openInBrowser, swapped for a fake
	// in tests so they don't launch one.
	openURL           func(url string) tea.Cmd
	applicationSubmit applicationSubmitModel
	applicationDelete applicationDeleteModel
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

// defaultExportDir is where the export screen points before anything has
// been exported this session. Configured here rather than via the
// environment or a flag: it's a starting point the user edits in the
// prompt whenever they want elsewhere, so a code-level default is the
// whole configuration surface it needs. A leading "~" is expanded when
// the export actually runs (see expandPath).
const defaultExportDir = "~/Desktop"

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

// renderCursorLine renders one line of a plain (non-checkbox) cursor
// list, highlighted when isCursor -- the shape applicationStatusModel's
// and documentReviewSelectModel's option lists both use.
func renderCursorLine(line string, isCursor bool) string {
	if isCursor {
		return cursorStyle.Render("> "+line) + "\n"
	}
	return "  " + line + "\n"
}

func New(s *store.Store, syncer *sync.Syncer, docs *documents.Store) *App {
	return &App{
		store:                 s,
		syncer:                syncer,
		screen:                screenActiveApplications,
		companyList:           newCompanyListModel(s),
		companyForm:           newCompanyFormModel(syncer),
		postingList:           newPostingListModel(s),
		activeApplicationList: newActiveApplicationListModel(),
		hideArchived:          true,
		documents:             docs,
		exportDir:             defaultExportDir,
		openURL:               openInBrowser,
	}
}

// documentStatusLine renders a single "<label>: found (<path>)" or
// "<label>: not found (<path>)" line for the documents section, followed
// by that document's latest review outcome (see reviewBadge) and, when
// the review left notes, an indented "Notes:" line -- the reviewer's
// record of what specifically to fix before the next cycle (see
// decisions.log #83).
func documentStatusLine(label string, exists bool, path string, review store.DocumentReview, hasReview bool) string {
	status := "not found"
	if exists {
		status = "found"
	}
	line := fieldLabel.Render(label+":") + " " + status + " (" + path + ") " + reviewBadge(review, hasReview) + "\n"
	if hasReview && review.Notes != "" {
		line += "  " + dimStyle.Render("Notes: "+review.Notes) + "\n"
	}
	return line
}

// outcomeDisplay is how each review outcome looks: its style, shared by
// reviewBadge and reviewGlyph, and its glyph. An outcome missing here
// renders dim, as "[<outcome>]" and "?"; a test checks every
// store.ReviewOutcomes() has an entry (RFC 0005).
var outcomeDisplay = map[store.ReviewOutcome]struct {
	style lipgloss.Style
	glyph string
}{
	store.ReviewOutcomePassed:  {passStyle, "✓"},
	store.ReviewOutcomeFlagged: {errStyle, "✗"},
}

// reviewBadge renders a document's latest review outcome as a short
// styled tag: dim "[not reviewed]" when hasReview is false (no review
// recorded yet), otherwise the outcome's name in capitals, styled per
// outcomeDisplay: green "[PASSED]", red "[FLAGGED]".
func reviewBadge(review store.DocumentReview, hasReview bool) string {
	if !hasReview {
		return dimStyle.Render("[not reviewed]")
	}
	display, ok := outcomeDisplay[review.Outcome]
	if !ok {
		return dimStyle.Render("[" + review.Outcome.String() + "]")
	}
	return display.style.Render("[" + strings.ToUpper(review.Outcome.String()) + "]")
}

// reviewGlyphSummary renders a compact, single-line summary of an
// application's latest cover-letter/resume review outcomes, for list
// rows (e.g. the active-applications table) that don't have room for
// reviewBadge's full "[FLAGGED]"/notes rendering -- see decisions.log
// #83. A document with no entry in reviews (no review recorded yet)
// renders as a dim "-".
func reviewGlyphSummary(reviews map[documents.Type]store.DocumentReview) string {
	entries := make([]string, 0, len(documents.Types()))
	for _, documentType := range documents.Types() {
		review, ok := reviews[documentType]
		entries = append(entries, documentAbbreviation(documentType)+":"+reviewGlyph(review, ok))
	}
	return strings.Join(entries, " ")
}

// documentAbbreviation is the initials of documentType's label, for the
// review column: "CL" for cover letter, "R" for resume.
func documentAbbreviation(documentType documents.Type) string {
	var initials []rune
	for _, word := range strings.Fields(documentType.Label()) {
		initials = append(initials, unicode.ToUpper([]rune(word)[0]))
	}
	return string(initials)
}

// reviewGlyph is reviewBadge's one-character form, for list rows: dim "-"
// with no review, otherwise the outcome's glyph from outcomeDisplay.
func reviewGlyph(review store.DocumentReview, hasReview bool) string {
	if !hasReview {
		return dimStyle.Render("-")
	}
	display, ok := outcomeDisplay[review.Outcome]
	if !ok {
		return dimStyle.Render("?")
	}
	return display.style.Render(display.glyph)
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
//
// latestReviews is, like application, loaded async and passed in (see
// loadDocumentReviews/documentReviewsLoadedMsg) rather than queried here
// -- a DB read, unlike docs.Status's local os.Stat, follows the same
// tea.Cmd/tea.Msg convention as the rest of this file's store-backed
// state (see decisions.log #83). A document with no entry in the map
// renders as "not reviewed".
func postingDetailContent(p store.Posting, application store.Application, hasApplication bool, docs *documents.Store, latestReviews map[documents.Type]store.DocumentReview, width int) string {
	var b strings.Builder
	b.WriteString(sectionHeading("Posting", width) + "\n")
	fields := []struct{ label, value string }{
		{"Department", p.Department},
		{"Team", p.Team},
		{"Location", p.Location},
		{"Employment type", p.EmploymentType},
		{"Workplace type", p.WorkplaceType},
		{"Status", p.ListingStatus},
		{"Job URL", p.JobURL},
		{"Application URL", p.ApplicationURL},
	}
	for _, f := range fields {
		if f.value == "" {
			continue
		}
		b.WriteString(detailField(f.label, f.value, width))
	}
	b.WriteString("\n" + sectionHeading("Application", width) + "\n")
	if hasApplication {
		b.WriteString(detailField("Status", applicationStatusLabel(application.Status), width))
		if application.Notes != "" {
			b.WriteString(detailField("Notes", application.Notes, width))
		}
		status := docs.Status(application.ID)
		b.WriteString("\n")
		for _, documentType := range documents.Types() {
			doc, err := status.Doc(documentType)
			if err != nil {
				continue
			}
			review, hasReview := latestReviews[documentType]
			b.WriteString(detailDocumentField(documentTitle(documentType), doc.Exists, doc.Path, review, hasReview, width))
		}
	} else {
		b.WriteString(helpStyle.Render("No application started -- press 'a' to start one.") + "\n")
	}
	if desc := p.DescriptionText; desc != "" {
		b.WriteString("\n" + sectionHeading("Description", width) + "\n" + desc + "\n")
	}
	return b.String()
}

// screenInstance tells apart two openings of the same screen (#115).
// Bubble Tea doesn't say which screen a Cmd's result is for, so without
// it a save from a screen the user closed and reopened would act on the
// new one. Zero is never handed out, so it never matches a real result.
type screenInstance int

func (a *App) newScreenInstance() screenInstance {
	a.lastScreenInstance++
	return a.lastScreenInstance
}

// enterFrom switches to next, pushing the current screen onto
// returnStack so returnBack can come back to it.
func (a *App) enterFrom(next screen) {
	a.returnStack = append(a.returnStack, a.screen)
	a.screen = next
}

// openDocumentReviewForm builds the review form from msg, or records
// msg.err and reports false so the caller stays where it is. Navigation
// is left to the caller: application detail pushes the form, the picker
// replaces itself with it.
func (a *App) openDocumentReviewForm(msg enterDocumentReviewFormMsg) bool {
	a.err = msg.err
	if msg.err != nil {
		return false
	}
	a.documentReviewForm = newDocumentReviewFormModel(a.store, a.documents, msg.applicationID, msg.documentType, msg.content, a.width, a.screenRows(), a.newScreenInstance())
	return true
}

// returnBack pops returnStack and switches to the screen on top,
// returning it. An empty stack means a push was missed somewhere; falling
// back to the home screen beats panicking or staying stuck.
func (a *App) returnBack() screen {
	a.screen = screenActiveApplications
	if n := len(a.returnStack); n > 0 {
		a.screen = a.returnStack[n-1]
		a.returnStack = a.returnStack[:n-1]
	}
	return a.screen
}

// rebuildPostingDetailApplication refreshes a.postingDetail after a
// change to the application-side data (status, notes, a review) while
// still on screenPostingDetail, keeping whichever posting is already
// displayed rather than re-deriving it via lookupPosting. lookupPosting's
// Posting result depends on a.postings, which is never populated when
// posting detail was entered via application detail's fast path (see
// decisions.log #87 follow-up) -- re-deriving it here would silently
// clobber a correct posting with a zero value. These handlers only ever
// change application data, never which posting is shown, so the
// already-displayed posting is always still the right one; only
// app/hasApp (from a.applicationsByPosting, independent of a.postings)
// need refreshing.
func (a *App) rebuildPostingDetailApplication() tea.Cmd {
	_, app, hasApp := a.lookupPosting(a.postingDetail.posting.ID)
	a.postingDetail = newPostingDetailModel(a.store, a.documents, a.width, a.screenRows(), a.postingDetail.posting, app, hasApp, nil, a.canNavigateSiblings(a.postingDetail.posting.ID))
	return maybeLoadDocumentReviews(a.store, a.documents, hasApp, app.ID)
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

// indexOfApplication returns id's index in apps, or -1 if not present.
func indexOfApplication(apps []store.ApplicationView, id int64) int {
	for i, app := range apps {
		if app.ID == id {
			return i
		}
	}
	return -1
}

// indexOfCompany returns id's index in companies, or -1 if not present.
func indexOfCompany(companies []store.Company, id int64) int {
	for i, c := range companies {
		if c.ID == id {
			return i
		}
	}
	return -1
}

type companiesLoadedMsg struct {
	companies    []store.Company
	openPostings map[int64]int
	err          error
}

// loadCompanies loads the active companies along with each one's open posting
// count for the company list's Open column.
func loadCompanies(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		companies, err := s.ListActiveCompanies(ctx)
		if err != nil {
			return companiesLoadedMsg{err: err}
		}
		openPostings, err := s.CountOpenPostingsByCompany(ctx)
		return companiesLoadedMsg{companies: companies, openPostings: openPostings, err: err}
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
	return tea.Batch(loadCompanies(a.store), loadActiveApplications(a.store, a.documents))
}

type activeApplicationsLoadedMsg struct {
	applications []store.ApplicationView
	progress     map[int64]map[documents.Type]documentProgress
	err          error
}

// loadActiveApplications fetches every active application, filtering
// each one's LatestReviews through documents.Current -- store has
// no filesystem access (see documents.go's own doc comment) so
// ListActiveApplications itself can't do this, and every screen this
// list feeds (the active-applications glyph column, application detail,
// and posting detail reached via application detail's 'p' fast path,
// which skips its own separate load -- see decisions.log) would
// otherwise show a stale review as if it still described the current
// document.
func loadActiveApplications(s *store.Store, docs *documents.Store) tea.Cmd {
	return func() tea.Msg {
		apps, err := s.ListActiveApplications(context.Background())
		if err != nil {
			return activeApplicationsLoadedMsg{err: err}
		}
		progress := make(map[int64]map[documents.Type]documentProgress, len(apps))
		for i, app := range apps {
			reviews, err := documents.Current(docs.Status(app.ID), app.LatestReviews, store.DocumentReview.IsCurrent)
			if err != nil {
				return activeApplicationsLoadedMsg{err: err}
			}
			apps[i].LatestReviews = reviews
			progress[app.ID], err = documentProgressOf(s, docs, app.ID)
			if err != nil {
				return activeApplicationsLoadedMsg{err: err}
			}
		}
		orderForHome(apps)
		return activeApplicationsLoadedMsg{applications: apps, progress: progress}
	}
}

type companyCreatedMsg struct {
	company store.Company
	err     error
}

// boardCheckTimeout bounds createCompany's live board check, so an
// unreachable API fails the add (closed) instead of leaving the form
// waiting (see decisions.log, #36). Syncer already gives up on any fetch
// after sync.Config.FetchTimeout (#142); this is tighter because someone
// is sitting at the form waiting for it.
const boardCheckTimeout = 15 * time.Second

// createCompany saves a company only once its source ref resolves to a
// real board (see sync.Syncer.CreateCompany).
func createCompany(syncer *sync.Syncer, name, source, sourceRef string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), boardCheckTimeout)
		defer cancel()
		company, err := syncer.CreateCompany(ctx, name, source, sourceRef)
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
	result sync.Result
	err    error
}

func refreshCompany(syncer *sync.Syncer, companyID int64) tea.Cmd {
	return func() tea.Msg {
		result, err := syncer.SyncCompany(context.Background(), companyID)
		return companyRefreshedMsg{result: result, err: err}
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

type documentReviewsLoadedMsg struct {
	applicationID int64
	reviews       map[documents.Type]store.DocumentReview
	err           error
}

// loadDocumentReviews fetches the most recent review, if any, of each
// document type for applicationID -- the data behind postingDetailContent's
// inline review-status section (see decisions.log #83). A document type
// with no review yet is simply absent from the returned map, not an
// error. Reviews whose content no longer matches what's currently on
// disk (the document was revised since being reviewed, whether in
// direct response to that review or independently) are filtered out by
// documents.Current -- a stale review shouldn't render as if it
// still described the current draft (see decisions.log,
// store.DocumentReview.IsCurrent).
func loadDocumentReviews(s *store.Store, docs *documents.Store, applicationID int64) tea.Cmd {
	return func() tea.Msg {
		reviews, err := s.LatestDocumentReviews(context.Background(), applicationID)
		if err != nil {
			return documentReviewsLoadedMsg{applicationID: applicationID, err: err}
		}
		reviews, err = documents.Current(docs.Status(applicationID), reviews, store.DocumentReview.IsCurrent)
		if err != nil {
			return documentReviewsLoadedMsg{applicationID: applicationID, err: err}
		}
		return documentReviewsLoadedMsg{applicationID: applicationID, reviews: reviews}
	}
}

// maybeLoadDocumentReviews returns the Cmd to (re)load applicationID's
// latest document reviews when hasApp is true, or nil otherwise -- used
// at every point a.postingDetail's underlying application changes, so
// the inline review status reloads through the same async round trip the
// rest of this screen's store-backed state uses (see
// postingDetailContent's own doc comment).
func maybeLoadDocumentReviews(s *store.Store, docs *documents.Store, hasApp bool, applicationID int64) tea.Cmd {
	if !hasApp {
		return nil
	}
	return loadDocumentReviews(s, docs, applicationID)
}

type applicationStatusUpdatedMsg struct {
	application store.Application
	from        screenInstance
	err         error
}

func updateApplicationStatus(s *store.Store, postingID int64, status store.ApplicationStatus, from screenInstance) tea.Cmd {
	return func() tea.Msg {
		app, err := s.UpdateApplicationStatus(context.Background(), postingID, status)
		return applicationStatusUpdatedMsg{application: app, from: from, err: err}
	}
}

type applicationNotesUpdatedMsg struct {
	application store.Application
	from        screenInstance
	err         error
}

func updateApplicationNotes(s *store.Store, postingID int64, notes string, from screenInstance) tea.Cmd {
	return func() tea.Msg {
		app, err := s.UpdateApplicationNotes(context.Background(), postingID, notes)
		return applicationNotesUpdatedMsg{application: app, from: from, err: err}
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
// Closed postings are always dropped: a listing that has closed on the job
// board can't be applied to, so it has no place in a list for finding
// postings to act on. They stay in the db (never deleted, see
// decisions.log), applications on them stay reachable from the
// active-applications screen, and a posting that reopens reappears on the
// next load. Filtered here rather than in ListPostingsByCompany because
// sync also uses that query and needs every posting, closed ones included.
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
		postings = filterOutClosed(postings)
		companyFilters, err := s.ListCompanyFilters(ctx, companyID)
		if err != nil {
			return postingsLoadedMsg{err: err}
		}
		departments, locations := splitCompanyFilters(companyFilters)
		postings, err = filterPostingsByCompanyFilters(postings, companyFilters)
		if err != nil {
			return postingsLoadedMsg{err: err}
		}

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

// filterOutClosed drops postings whose listing has closed on the job board.
func filterOutClosed(postings []store.Posting) []store.Posting {
	visible := make([]store.Posting, 0, len(postings))
	for _, p := range postings {
		if p.ListingStatus == "closed" {
			continue
		}
		visible = append(visible, p)
	}
	return visible
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

// filterPostingsByCompanyFilters keeps only postings matching
// companyFilters, via sync.FilterRules + filter.Match -- the same path
// SyncCompany uses to gate ingestion, so display-time filtering (both
// loadPostings' authoritative DB-driven pass and narrowPostingsToFilters'
// optimistic post-save pass below, which both call this) can't silently
// disagree with what was actually ingested, or with each other (see
// decisions.log, #61). A filter.Match error (an unsupported field name)
// is propagated to the caller rather than swallowed here; loadPostings
// surfaces it as a load error, while narrowPostingsToFilters treats it
// as unreachable given the fields it always passes.
func filterPostingsByCompanyFilters(postings []store.Posting, companyFilters []store.CompanyFilter) ([]store.Posting, error) {
	rules := sync.FilterRules(companyFilters)
	narrowed := make([]store.Posting, 0, len(postings))
	for _, p := range postings {
		match, err := filter.Match(filter.Posting{Department: p.Department, Location: p.Location}, rules)
		if err != nil {
			return nil, err
		}
		if match {
			narrowed = append(narrowed, p)
		}
	}
	return narrowed, nil
}

// splitCompanyFilters separates a company's saved filter rows by field.
func splitCompanyFilters(filters []store.CompanyFilter) (departments, locations []string) {
	for _, f := range filters {
		switch f.Field {
		case filter.FieldDepartment:
			departments = append(departments, f.Value)
		case filter.FieldLocation:
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

// companyFiltersAppliedMsg carries the result of applying a new filter
// selection: sync.ApplyCompanyFilters' replace-then-resync as one
// round trip, not the three-step save/resync/reload chain this used to
// be (see decisions.log, #56).
type companyFiltersAppliedMsg struct {
	result sync.Result
	err    error
}

func applyCompanyFilters(syncer *sync.Syncer, companyID int64, departments, locations []string) tea.Cmd {
	return func() tea.Msg {
		result, err := syncer.ApplyCompanyFilters(context.Background(), companyID, departments, locations)
		return companyFiltersAppliedMsg{result: result, err: err}
	}
}

// narrowPostingsToFilters keeps only postings matching the given
// department/location values (OR within a field, AND across fields, same
// semantics as filter.Match), for the optimistic client-side narrowing
// shown immediately after saving a filter selection, before the
// background re-sync completes. Builds synthetic store.CompanyFilter
// rows -- sync.FilterRules only reads Field/Value, so no DB round trip
// is needed -- and delegates to filterPostingsByCompanyFilters, so this
// shares the exact same rule-construction-and-match path as loadPostings'
// authoritative filtering rather than a second hand-copy of it (see
// decisions.log, #61).
func narrowPostingsToFilters(postings []store.Posting, departments, locations []string) []store.Posting {
	filters := make([]store.CompanyFilter, 0, len(departments)+len(locations))
	for _, d := range departments {
		filters = append(filters, store.CompanyFilter{Field: filter.FieldDepartment, Value: d})
	}
	for _, l := range locations {
		filters = append(filters, store.CompanyFilter{Field: filter.FieldLocation, Value: l})
	}

	narrowed, err := filterPostingsByCompanyFilters(postings, filters)
	if err != nil {
		// Unreachable: filters is built entirely from filter.FieldDepartment/
		// FieldLocation above, both always-supported by filter.Match.
		// Matches the old behavior on a hypothetical Match error here --
		// treat as no matches rather than propagate, since this optimistic
		// pass was never authoritative to begin with.
		return nil
	}
	return narrowed
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := a.update(msg)
	// The banner above the screen can appear, change, or clear on any
	// message -- a sync finishing, a failed browser open -- including
	// while a screen is up, so refit the screens that keep their size in
	// state to the rows left under the banner every time, rather than only
	// when they're built. The list screens are sized as they render, so
	// they need nothing here.
	switch a.screen {
	case screenPostingDetail:
		a.postingDetail.setHeight(a.screenRows())
	case screenApplicationNotesEdit:
		a.applicationNotes.setHeight(a.screenRows())
	case screenApplicationForm:
		a.applicationForm.setHeight(a.screenRows())
	case screenDocumentReviewForm:
		a.documentReviewForm.setHeight(a.screenRows())
	}
	return model, cmd
}

func (a *App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case companiesLoadedMsg:
		a.err = msg.err
		a.companies = msg.companies
		a.companyOpenPostings = msg.openPostings
	case activeApplicationsLoadedMsg:
		a.err = msg.err
		a.activeApplications = msg.applications
		a.activeApplicationProgress = msg.progress
		a.activeApplicationList.resetCursorIfOutOfBounds(len(a.activeApplications))
	case companyCreatedMsg:
		a.err = msg.err
		a.companyForm.checkResolved()
		if msg.err == nil {
			a.companies = append(a.companies, msg.company)
			sortCompaniesByName(a.companies)
			a.screen = screenCompanyList
		}
	case companyDeletedMsg:
		a.err = msg.err
		if msg.err == nil {
			if i := indexOfCompany(a.companies, msg.companyID); i != -1 {
				a.companies = append(a.companies[:i], a.companies[i+1:]...)
			}
			a.companyList.clampCursor(len(a.companies))
		}
	case companyNameUpdatedMsg:
		a.err = msg.err
		a.companyEdit.saveResolved()
		if msg.err == nil {
			if i := indexOfCompany(a.companies, msg.company.ID); i != -1 {
				a.companies[i] = msg.company
			}
			sortCompaniesByName(a.companies)
			a.screen = screenCompanyList
		}
	case syncAllStepMsg:
		return a, a.handleSyncAllStep(msg)
	case companyRefreshedMsg:
		if errors.Is(msg.err, sync.ErrSyncInProgress) {
			// Another sync of this company (e.g. a scheduled `swamp fetch`)
			// is running; this one fetched and wrote nothing (#150).
			a.err = nil
			a.status = msg.result.Name + " is already syncing elsewhere; try again in a moment"
			break
		}
		a.err = msg.err
		if msg.err == nil {
			r := msg.result
			a.status = fmt.Sprintf("%s: fetched %d, created %d, updated %d, closed %d, reopened %d",
				r.Name, r.Fetched, r.Created, r.Updated, r.Closed, r.Reopened)
			// The company list's Open and Last fetched columns just changed.
			reload := loadCompanies(a.store)
			if r.CompanyID == a.selectedCompany.ID {
				// The company whose postings are currently being viewed
				// just finished a re-sync (e.g. triggered by saving a
				// filter selection) -- reload from the DB so the view
				// becomes authoritative instead of just the optimistic
				// client-side narrowing applied at save time.
				return a, tea.Batch(reload, loadPostings(a.store, a.selectedCompany.ID, a.hideArchived))
			}
			return a, reload
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
				if i := indexOfPosting(a.postings, msg.markup.PostingID); i != -1 {
					a.postings = append(a.postings[:i], a.postings[i+1:]...)
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
				a.postingDetail = newPostingDetailModel(a.store, a.documents, a.width, a.screenRows(), p, app, hasApp, nil, a.canNavigateSiblings(p.ID))
				return a, maybeLoadDocumentReviews(a.store, a.documents, hasApp, app.ID)
			}
		}
	case applicationCreatedMsg:
		if errors.Is(msg.err, store.ErrPostingClosed) {
			// Refused, not failed (#175): the posting is no longer listed,
			// so there's no form left to submit.
			a.err = nil
			a.status = "This posting is closed; an application can't be started on it"
			break
		}
		a.err = msg.err
		if msg.err == nil {
			if a.applicationsByPosting == nil {
				a.applicationsByPosting = make(map[int64]store.Application)
			}
			a.applicationsByPosting[msg.application.PostingID] = msg.application
			var reviewsCmd tea.Cmd
			if a.screen == screenPostingDetail {
				p, app, hasApp := a.lookupPosting(a.postingDetail.posting.ID)
				a.postingDetail = newPostingDetailModel(a.store, a.documents, a.width, a.screenRows(), p, app, hasApp, nil, a.canNavigateSiblings(p.ID))
				reviewsCmd = maybeLoadDocumentReviews(a.store, a.documents, hasApp, app.ID)
			}
			// A freshly-started application should show up in the active-
			// applications list without needing a restart.
			return a, tea.Batch(loadActiveApplications(a.store, a.documents), reviewsCmd)
		}
	case applicationDeletedMsg:
		a.err = msg.err
		if a.screen == screenApplicationDelete {
			if msg.err != nil {
				// Back to detail rather than leaving a confirmation whose
				// y is already spent; the error shows there.
				a.screen = screenApplicationDetail
				return a, nil
			}
			a.screen = screenActiveApplications
		}
		if msg.err == nil {
			delete(a.applicationsByPosting, msg.application.Posting.ID)
			a.status = "Deleted the application for " + msg.application.Posting.Title
			return a, loadActiveApplications(a.store, a.documents)
		}
	case applicationStatusUpdatedMsg:
		a.err = msg.err
		if msg.err == nil {
			if a.applicationsByPosting == nil {
				a.applicationsByPosting = make(map[int64]store.Application)
			}
			a.applicationsByPosting[msg.application.PostingID] = msg.application
			// Nothing blocks esc while the save is in flight, so the user
			// may already have left -- only navigate if they're still here.
			if a.screen == screenApplicationStatusSelect && a.applicationStatus.instance == msg.from {
				a.returnBack()
			}
			if a.screen == screenApplicationSubmit && a.applicationSubmit.instance == msg.from {
				a.status = "Marked " + a.applicationSubmit.application.Posting.Title + " submitted"
				a.screen = screenActiveApplications
			}
			var reviewsCmd tea.Cmd
			if a.screen == screenPostingDetail {
				reviewsCmd = a.rebuildPostingDetailApplication()
			}
			// The new status may have moved this application to/from a
			// terminal status (rejected/offer_declined), changing whether
			// it belongs in the active-applications list at all -- reload
			// rather than patch in place so that's always correct,
			// regardless of which screen triggered the change.
			return a, tea.Batch(loadActiveApplications(a.store, a.documents), reviewsCmd)
		}
	case editorClosedMsg:
		a.err = msg.err
		// The edit may have made the latest review stale, so reload the
		// application's reviews the way u does (RFC 0007, step 6). Not
		// after an error: the editor didn't start or exited abnormally,
		// and the reload's result would clear the error before it's seen.
		if msg.err == nil && a.screen == screenApplicationDetail {
			return a, loadDocumentReviews(a.store, a.documents, a.applicationDetail.application.ID)
		}
	case applicationReloadedForDeleteMsg:
		a.err = msg.err
		// Same guard as applicationFormLoadedMsg: only if the user is still
		// on that application.
		if msg.err == nil && a.screen == screenApplicationDetail && a.applicationDetail.application.ID == msg.application.ID {
			a.applicationDelete = newApplicationDeleteModel(a.documents, msg.application)
			a.screen = screenApplicationDelete
		}
	case applicationStatusLoadedMsg:
		a.err = msg.err
		// Open the form only if the user is still where they asked for it.
		if msg.err == nil && a.screen == msg.from {
			a.enterFrom(screenApplicationStatusSelect)
			a.applicationStatus = newApplicationStatusModel(a.store, msg.postingID, msg.status, a.newScreenInstance())
		}
	case applicationFormLoadedMsg:
		a.err = msg.err
		// Open the form only if the user is still on the application it
		// was loaded for.
		if msg.err == nil && a.screen == screenApplicationDetail && a.applicationDetail.application.Posting.ID == msg.postingID {
			a.screen = screenApplicationForm
			a.applicationForm = newApplicationFormModel(a.store, msg.postingID, msg.form, a.width, a.screenRows())
		}
	case applicationFormSavedMsg:
		a.err = msg.err
		if msg.err == nil {
			a.status = "Application form saved"
			// Same late-save guard as applicationNotesUpdatedMsg.
			if a.screen == screenApplicationForm {
				a.screen = screenApplicationDetail
			}
		}
	case applicationNotesUpdatedMsg:
		a.err = msg.err
		if msg.err == nil {
			if a.applicationsByPosting == nil {
				a.applicationsByPosting = make(map[int64]store.Application)
			}
			a.applicationsByPosting[msg.application.PostingID] = msg.application
			// Same late-save guard as applicationStatusUpdatedMsg: yanking
			// the user back to posting detail after their own esc already
			// popped its returnStack entry would strand the next esc.
			if a.screen == screenApplicationNotesEdit && a.applicationNotes.instance == msg.from {
				a.screen = screenPostingDetail
			}
			if a.screen == screenPostingDetail {
				return a, a.rebuildPostingDetailApplication()
			}
		}
	case applicationExportedMsg:
		a.err = msg.err
		if a.screen == screenApplicationSubmit {
			// The submit screen shows the result itself, failure included,
			// and stays put: the user still decides whether they submitted.
			a.applicationSubmit.exported = &msg
			if msg.err == nil {
				a.exportDir = msg.dir
			}
			return a, nil
		}
		if msg.err != nil {
			// Stay on the export screen: the overwhelmingly likely
			// failure is a mistyped destination, and returning to the
			// list would make the user re-select the application just to
			// fix a path they can still see.
			return a, nil
		}
		a.exportDir = msg.dir
		a.status = exportStatusLine(msg)
		a.screen = screenActiveApplications
		return a, nil
	case documentChangedDuringReviewMsg:
		if a.screen == screenDocumentReviewForm && a.documentReviewForm.instance == msg.from {
			a.documentReviewForm.reload(msg.current)
		}
	case documentReviewCreatedMsg:
		a.err = msg.err
		if msg.err == nil {
			// Nothing blocks esc while the save is in flight, so the user
			// may already have left -- only navigate if they're still here.
			if a.screen == screenDocumentReviewForm && a.documentReviewForm.instance == msg.from {
				a.returnBack()
			}
			// Reload so the freshly-submitted review's outcome/notes show up
			// immediately wherever it's displayed, without having to
			// navigate away and back (see decisions.log #83) -- which
			// reload depends on which screen the user is now on.
			switch a.screen {
			case screenApplicationDetail:
				// documentReviewsLoadedMsg also patches this application's
				// home-screen row, so its glyph and next step aren't stale
				// once the user backs out -- no full reload needed (#91).
				return a, loadDocumentReviews(a.store, a.documents, a.applicationDetail.application.ID)
			case screenPostingDetail:
				return a, a.rebuildPostingDetailApplication()
			}
		}
	case documentReviewsLoadedMsg:
		a.err = msg.err
		// A response for a posting/application the user has since
		// navigated away from is simply discarded -- msg.applicationID no
		// longer matching what's on screen means this result is stale.
		if msg.err == nil && a.screen == screenPostingDetail && a.postingDetail.application.ID == msg.applicationID {
			a.postingDetail = newPostingDetailModel(a.store, a.documents, a.width, a.screenRows(), a.postingDetail.posting, a.postingDetail.application, a.postingDetail.hasApplication, msg.reviews, a.canNavigateSiblings(a.postingDetail.posting.ID))
		}
		if msg.err == nil && a.screen == screenApplicationDetail && a.applicationDetail.application.ID == msg.applicationID {
			a.applicationDetail.application.LatestReviews = msg.reviews
		}
		// The reviews are fresh whichever screen asked for them, so the
		// application's home-screen row takes them too (#91).
		if msg.err == nil {
			if i := indexOfApplication(a.activeApplications, msg.applicationID); i != -1 {
				a.activeApplications[i].LatestReviews = msg.reviews
			}
		}
	case filterOptionsLoadedMsg:
		a.err = msg.err
		a.filterSelect = newFilterSelectModel(a.selectedCompany.ID, a.selectedCompany.Name, msg.departments, msg.locations, msg.existingFilters)
	case companyFiltersAppliedMsg:
		if errors.Is(msg.err, sync.ErrSyncInProgress) {
			// The filters saved; only the re-sync was skipped, because
			// another sync of this company is running (#150). Reload so
			// the view reflects the saved filters.
			a.err = nil
			a.status = msg.result.Name + " filters saved; sync skipped, it's already syncing elsewhere"
			return a, loadPostings(a.store, a.selectedCompany.ID, a.hideArchived)
		}
		a.err = msg.err
		if msg.err == nil {
			r := msg.result
			a.status = fmt.Sprintf("%s: fetched %d, created %d, updated %d, closed %d, reopened %d",
				r.Name, r.Fetched, r.Created, r.Updated, r.Closed, r.Reopened)
			// Reload from the DB so the view becomes authoritative instead
			// of just the optimistic narrowing applied synchronously when
			// the filter selection was saved (see screenFilterSelect's
			// saveFilterSelectionMsg handling in updateKeyMsg).
			return a, loadPostings(a.store, a.selectedCompany.ID, a.hideArchived)
		}
	case browserOpenedMsg:
		a.err = msg.err
		if a.screen == screenApplicationSubmit {
			// Also shown next to the link, so the step doesn't claim the
			// link opened when it didn't.
			a.applicationSubmit.opened = &msg
		}
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
			a.postingDetail.resize(a.width, a.screenRows())
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
	case screenActiveApplications:
		cmd, intent := a.activeApplicationList.Update(msg, a.activeApplications)
		switch v := intent.(type) {
		case backToCompanyListMsg:
			a.screen = screenCompanyList
			// Archiving or unarchiving may have changed open counts.
			return a, loadCompanies(a.store)
		case enterApplicationStatusMsg:
			return a, loadApplicationStatus(a.store, v.postingID, a.screen)
		case enterApplicationDetailMsg:
			a.screen = screenApplicationDetail
			a.applicationDetail = newApplicationDetailModel(a.documents, v.application)
		case enterApplicationExportMsg:
			a.screen = screenApplicationExport
			a.applicationExport = newApplicationExportModel(a.store, a.documents, v.application, a.exportDir, a.width)
		}
		return a, cmd
	case screenApplicationDetail:
		cmd, intent := a.applicationDetail.Update(msg)
		switch v := intent.(type) {
		case enterApplicationFormMsg:
			return a, loadApplicationForm(a.store, v.postingID)
		case enterApplicationSubmitMsg:
			return a, a.startApplicationSubmit(v.application)
		case enterApplicationDeleteMsg:
			return a, reloadApplicationForDelete(a.store, v.application)
		case backToActiveApplicationsMsg:
			a.screen = screenActiveApplications
		case enterPostingDetailMsg:
			// Unlike screenPostingList's own enterPostingDetailMsg handler,
			// this can't use lookupPosting -- a.postings is only populated
			// by browsing that list, which application detail (reached via
			// active-applications) may never have done. applicationDetail's
			// own ApplicationView already carries the full posting,
			// application, and latest reviews (freshly loaded for the
			// active-applications screen), so render from that directly
			// rather than triggering loadApplication's own rebuild, which
			// would immediately clobber this with a zero-value Posting via
			// the same lookupPosting gap.
			//
			// a.applicationsByPosting must also be seeded here, the same way
			// screenPostingList's enterPostingDetailMsg handler gets it seeded
			// via loadApplication's applicationLoadedMsg: later handlers that
			// change application-side data while still on screenPostingDetail
			// (documentReviewCreatedMsg, applicationStatusUpdatedMsg, etc.) go
			// through rebuildPostingDetailApplication -> lookupPosting, which
			// reads only from this map, not from appView. Without seeding it
			// here, that lookup misses and hasApp wrongly flips to false even
			// though the application is unchanged.
			appView := a.applicationDetail.application
			if a.applicationsByPosting == nil {
				a.applicationsByPosting = make(map[int64]store.Application)
			}
			a.applicationsByPosting[appView.Posting.ID] = appView.Application
			a.postingDetail = newPostingDetailModel(a.store, a.documents, a.width, a.screenRows(), appView.Posting, appView.Application, true, appView.LatestReviews, a.canNavigateSiblings(appView.Posting.ID))
			a.enterFrom(screenPostingDetail)
		case enterDocumentReviewFormMsg:
			if a.openDocumentReviewForm(v) {
				a.enterFrom(screenDocumentReviewForm)
			}
		case refreshApplicationDetailMsg:
			return a, loadDocumentReviews(a.store, a.documents, a.applicationDetail.application.ID)
		}
		return a, cmd
	case screenCompanyList:
		cmd, intent := a.companyList.Update(msg, a.companies)
		switch v := intent.(type) {
		case enterCompanyFormMsg:
			a.screen = screenCompanyForm
			a.companyForm = newCompanyFormModel(a.syncer)
		case enterCompanyEditMsg:
			a.screen = screenCompanyEdit
			a.companyEdit = newCompanyEditModel(a.store, v.company.ID, v.company.Name)
		case refreshCompanyMsg:
			if a.syncAll.running {
				// The run may be syncing this company right now (#150).
				a.status = "Sync all in progress: refresh unavailable"
				return a, nil
			}
			return a, refreshCompany(a.syncer, v.company.ID)
		case syncAllKeyMsg:
			return a, a.toggleSyncAll()
		case selectCompanyMsg:
			a.selectedCompany = v.company
			a.screen = screenPostingList
			return a, loadPostings(a.store, a.selectedCompany.ID, a.hideArchived)
		case backToActiveApplicationsMsg:
			a.screen = screenActiveApplications
		}
		return a, cmd
	case screenPostingList:
		snap := postingListSnapshot{
			companyName:             a.selectedCompany.Name,
			companyDescription:      a.selectedCompany.Description,
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
			// Archiving or unarchiving may have changed open counts.
			return a, loadCompanies(a.store)
		case enterPostingDetailMsg:
			p, app, hasApp := a.lookupPosting(v.postingID)
			a.postingDetail = newPostingDetailModel(a.store, a.documents, a.width, a.screenRows(), p, app, hasApp, nil, a.canNavigateSiblings(p.ID))
			a.enterFrom(screenPostingDetail)
			return a, tea.Batch(loadApplication(a.store, p.ID), maybeLoadDocumentReviews(a.store, a.documents, hasApp, app.ID))
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
			if a.returnBack() == screenPostingList {
				a.postingList.setCursor(indexOfPosting(a.postings, a.postingDetail.posting.ID))
			}
		case navigatePostingMsg:
			idx := indexOfPosting(a.postings, v.postingID)
			newIdx := idx + v.direction
			if idx >= 0 && newIdx >= 0 && newIdx < len(a.postings) {
				p := a.postings[newIdx]
				app, hasApp := a.applicationsByPosting[p.ID]
				a.postingDetail = newPostingDetailModel(a.store, a.documents, a.width, a.screenRows(), p, app, hasApp, nil, a.canNavigateSiblings(p.ID))
				return a, tea.Batch(loadApplication(a.store, p.ID), maybeLoadDocumentReviews(a.store, a.documents, hasApp, app.ID))
			}
		case enterApplicationStatusMsg:
			return a, loadApplicationStatus(a.store, v.postingID, a.screen)
		case enterApplicationNotesMsg:
			a.screen = screenApplicationNotesEdit
			a.applicationNotes = newApplicationNotesModel(a.store, v.postingID, v.currentNotes, a.width, a.screenRows(), a.newScreenInstance())
		case enterDocumentReviewSelectMsg:
			a.enterFrom(screenDocumentReviewSelect)
			a.documentReviewSelect = newDocumentReviewSelectModel(a.documents, v.applicationID)
		case refreshPostingDetailMsg:
			return a, a.rebuildPostingDetailApplication()
		}
		return a, cmd
	case screenApplicationStatusSelect:
		cmd, intent := a.applicationStatus.Update(msg)
		if _, ok := intent.(cancelApplicationStatusMsg); ok {
			a.returnBack()
		}
		return a, cmd
	case screenApplicationExport:
		cmd, intent := a.applicationExport.Update(msg)
		if _, ok := intent.(cancelApplicationExportMsg); ok {
			a.screen = screenActiveApplications
		}
		return a, cmd
	case screenApplicationSubmit:
		cmd, intent := a.applicationSubmit.Update(msg)
		switch v := intent.(type) {
		case cancelApplicationSubmitMsg:
			a.screen = screenApplicationDetail
		case confirmApplicationSubmitMsg:
			return a, updateApplicationStatus(a.store, v.postingID, store.ApplicationStatusSubmitted, a.applicationSubmit.instance)
		}
		return a, cmd
	case screenApplicationDelete:
		cmd, intent := a.applicationDelete.Update(msg)
		switch v := intent.(type) {
		case cancelApplicationDeleteMsg:
			a.screen = screenApplicationDetail
		case confirmApplicationDeleteMsg:
			return a, deleteApplication(a.store, v.application)
		}
		return a, cmd
	case screenApplicationNotesEdit:
		cmd, intent := a.applicationNotes.Update(msg)
		if _, ok := intent.(cancelApplicationNotesMsg); ok {
			a.screen = screenPostingDetail
		}
		return a, cmd
	case screenApplicationForm:
		cmd, intent := a.applicationForm.Update(msg)
		if _, ok := intent.(cancelApplicationFormMsg); ok {
			a.screen = screenApplicationDetail
		}
		return a, cmd
	case screenDocumentReviewSelect:
		cmd, intent := a.documentReviewSelect.Update(msg)
		switch v := intent.(type) {
		case cancelDocumentReviewSelectMsg:
			a.returnBack()
		case enterDocumentReviewFormMsg:
			// Replaces the picker rather than pushing onto it, so leaving
			// the form goes straight back to posting detail.
			if a.openDocumentReviewForm(v) {
				a.screen = screenDocumentReviewForm
			}
		}
		return a, cmd
	case screenDocumentReviewForm:
		cmd, intent := a.documentReviewForm.Update(msg)
		if _, ok := intent.(cancelDocumentReviewFormMsg); ok {
			a.returnBack()
		}
		return a, cmd
	case screenFilterSelect:
		cmd, intent := a.filterSelect.Update(msg)
		switch v := intent.(type) {
		case cancelFilterSelectMsg:
			a.screen = screenPostingList
		case saveFilterSelectionMsg:
			if a.syncAll.running {
				// Saving re-syncs the company, which the run may be syncing
				// right now (#150). Refuse rather than queue: nothing is saved
				// or narrowed, and the selection stays on screen to save later.
				a.status = "Sync all in progress: save filters when it finishes"
				return a, nil
			}
			// Narrowed synchronously, before applyCompanyFilters' Cmd even
			// runs -- this needs nothing the async save/resync produces (it's
			// the same in-memory narrowing loadPostings' own authoritative
			// pass uses, just working off what's already loaded), so there's
			// no reason to wait on a round trip for it the way the old
			// three-step save/resync/reload chain did.
			a.screen = screenPostingList
			a.postings = narrowPostingsToFilters(a.postings, v.departments, v.locations)
			a.activeFilterDepartments = v.departments
			a.activeFilterLocations = v.locations
			a.postingList.resetCursorIfOutOfBounds(len(a.postings))
			return a, applyCompanyFilters(a.syncer, a.selectedCompany.ID, v.departments, v.locations)
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

// banner is the error or status message View() draws above the active
// screen, including the blank line separating the two, or "" when there's
// nothing to show.
func (a *App) banner() string {
	if a.err != nil {
		return errStyle.Render(fmt.Sprintf("error: %v", a.err)) + "\n\n"
	}
	if a.status != "" {
		return helpStyle.Render(a.status) + "\n\n"
	}
	return ""
}

// screenRows is the terminal height left for the active screen once
// View() has drawn the banner above it. Every newline in the banner ends
// one of its rows, and the screen starts on the row after the last. A
// banner line wider than the terminal doesn't take extra rows: bubbletea
// truncates lines to the window width rather than letting them wrap.
func (a *App) screenRows() int {
	return max(a.height-strings.Count(a.banner(), "\n"), 0)
}

func (a *App) View() string {
	var b strings.Builder

	b.WriteString(a.banner())

	switch a.screen {
	case screenActiveApplications:
		b.WriteString(a.activeApplicationList.View(a.activeApplications, a.activeApplicationProgress, time.Now(), a.screenRows()))
	case screenApplicationDetail:
		b.WriteString(a.applicationDetail.View())
	case screenCompanyList:
		b.WriteString(a.companyList.View(a.companies, a.companyOpenPostings, a.width, a.screenRows()))
	case screenCompanyForm:
		b.WriteString(a.companyForm.View())
	case screenCompanyEdit:
		b.WriteString(a.companyEdit.View())
	case screenPostingList:
		snap := postingListSnapshot{
			companyName:             a.selectedCompany.Name,
			companyDescription:      a.selectedCompany.Description,
			postings:                a.postings,
			markup:                  a.postingMarkup,
			hideArchived:            a.hideArchived,
			activeFilterDepartments: a.activeFilterDepartments,
			activeFilterLocations:   a.activeFilterLocations,
		}
		b.WriteString(a.postingList.View(snap, a.screenRows()))
	case screenPostingDetail:
		b.WriteString(a.postingDetail.View())
	case screenApplicationStatusSelect:
		b.WriteString(a.applicationStatus.View())
	case screenApplicationExport:
		b.WriteString(a.applicationExport.View())
	case screenApplicationSubmit:
		b.WriteString(a.applicationSubmit.View())
	case screenApplicationDelete:
		b.WriteString(a.applicationDelete.View())
	case screenApplicationNotesEdit:
		b.WriteString(a.applicationNotes.View())
	case screenApplicationForm:
		b.WriteString(a.applicationForm.View())
	case screenDocumentReviewSelect:
		b.WriteString(a.documentReviewSelect.View())
	case screenDocumentReviewForm:
		b.WriteString(a.documentReviewForm.View())
	case screenFilterSelect:
		b.WriteString(a.filterSelect.View(a.screenRows()))
	}

	return b.String()
}

// canNavigateSiblings reports whether prev/next posting navigation can
// actually move anywhere from postingID. It needs a.postings to contain
// that posting, which holds when the user reached posting detail by
// browsing a company's list, but not via application detail's 'p' fast
// path, where a.postings was never loaded (see issue #93).
//
// Recomputed at every posting-detail construction rather than carried
// along, so it always reflects what a.postings holds now rather than
// what it held when the screen was first entered.
func (a *App) canNavigateSiblings(postingID int64) bool {
	return indexOfPosting(a.postings, postingID) >= 0
}

// sectionHeading renders a posting-detail section heading: name, then a
// dim horizontal rule filling the rest of width, so each section reads as
// its own block. width <= 0 (before the first tea.WindowSizeMsg) gets a
// short fixed rule rather than none.
func sectionHeading(name string, width int) string {
	ruleWidth := 3
	if rest := width - lipgloss.Width(name) - 1; rest > ruleWidth {
		ruleWidth = rest
	}
	return sectionStyle.Render(name) + " " + dimStyle.Render(strings.Repeat("─", ruleWidth))
}

// detailLabelWidth is the width of posting detail's label column, sized
// to its longest label ("Application URL") plus a gap, so every value in
// the Posting and Application sections starts in the same column.
const detailLabelWidth = len("Application URL") + 2

// detailField renders one posting-detail row: label in a fixed-width
// column, then value word-wrapped to the rest of width, with continuation
// lines indented to the value column instead of falling back under the
// label. width <= 0 (before the first tea.WindowSizeMsg) leaves the value
// unwrapped.
func detailField(label, value string, width int) string {
	return detailRow(label, wrapDetailValue(value, width))
}

// detailValueWidth is the width left for a value after the label column,
// or 0 (unconstrained) when width is unknown or too narrow to fit one.
func detailValueWidth(width int) int {
	return max(width-detailLabelWidth, 0)
}

// wrapDetailValue word-wraps value to the value column's width, as lines.
func wrapDetailValue(value string, width int) []string {
	if valueWidth := detailValueWidth(width); valueWidth > 0 {
		value = cellbuf.Wrap(value, valueWidth, "")
	}
	return strings.Split(value, "\n")
}

// detailRow lays out a label and its already-wrapped value lines: the
// label padded to detailLabelWidth, then the value, with continuation
// lines indented to the value column.
func detailRow(label string, lines []string) string {
	indent := strings.Repeat(" ", detailLabelWidth)
	for i := 1; i < len(lines); i++ {
		lines[i] = indent + lines[i]
	}
	gap := strings.Repeat(" ", detailLabelWidth-lipgloss.Width(label))
	return fieldLabel.Render(label) + gap + strings.Join(lines, "\n") + "\n"
}

// detailDocumentField is detailField for a drafted document: its presence
// and path, the latest review's badge, and the review's notes on their
// own line under the value column -- the aligned counterpart of
// documentStatusLine, which application detail still uses.
func detailDocumentField(label string, exists bool, path string, review store.DocumentReview, hasReview bool, width int) string {
	status := "not found"
	if exists {
		status = "found"
	}
	// The badge is placed after wrapping rather than wrapped with the path:
	// "[not reviewed]" contains a space, and word wrapping could split it.
	lines := wrapDetailValue(status+" ("+path+")", width)
	badge := reviewBadge(review, hasReview)
	last := len(lines) - 1
	var row string
	switch valueWidth := detailValueWidth(width); {
	case width > 0 && valueWidth < lipgloss.Width(badge):
		// The value column is narrower than the badge (or there's no room
		// for one at all), so indenting the badge to it would overrun the
		// width and get it wrapped anyway. Give it a line of its own from
		// the left edge instead: out of alignment, but in one piece.
		row = detailRow(label, lines) + badge + "\n"
	case valueWidth > 0 && lipgloss.Width(lines[last])+1+lipgloss.Width(badge) > valueWidth:
		row = detailRow(label, append(lines, badge))
	default:
		lines[last] += " " + badge
		row = detailRow(label, lines)
	}
	if hasReview && review.Notes != "" {
		row += detailField("", dimStyle.Render("Notes: "+review.Notes), width)
	}
	return row
}
