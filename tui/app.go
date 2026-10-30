// Package tui is the terminal UI: Bubble Tea model for browsing and
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
	"log"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/cellbuf"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/filter"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
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
	screenCompanyDelete
	screenAllPostings
)

type App struct {
	companyService     *service.CompanyService
	postingService     *service.PostingService
	applicationService *service.ApplicationService
	documentService    *service.DocumentService
	scoringService     *service.ScoringService
	changeLogService   *service.ChangeLog
	changeLog          bool
	origin             string
	// seen is each view's place in the change log (see cursor); a view
	// not in it is at since. latestSeen is the newest event read so far.
	seen          map[screen]int64
	since         int64
	latestSeen    int64
	syncer        *sync.Syncer
	companyList   *companyListModel
	allPostings   *allPostingsModel
	currentScreen screen
	stack         *screenStack
	status        string
	err           error
	// syncing runs sync all and refreshes, and draws their progress in the
	// banner.
	syncing               *model.Syncing
	activeApplicationList *activeApplicationListModel
	// exportDir is the destination the export screen prefills: the last
	// directory successfully exported to this session, falling back to
	// defaultExportDir. Ephemeral and in-memory only, like hideArchived
	// -- a remembered path is a within-session convenience, not
	// something worth a schema change to persist.
	exportDir string
	// openURL opens a URL in the browser: openInBrowser, swapped for a fake
	// in tests so they don't launch one.
	openURL func(src message.Source, about message.Subject, url string) tea.Cmd
	// documents resolves an application's document paths, hiding the
	// path convention and base directory the same way store hides
	// schema/SQL details -- threaded through from SWAMP_DOCUMENTS_PATH,
	// mirroring how SWAMP_DB_PATH configures the store.
	documents *documents.Store
	// hideArchived is what each posting list opens with: the last one's
	// choice, for the session only. Defaults true: the point of archiving
	// a posting is to declutter the list.
	hideArchived bool
	width        int
	height       int

	// router is a message router for Dispatching bubbletea tea.Msg's
	router model.Router
	// instance is the banner's, which App registers routes for.
	instance message.ScreenInstance

	// sized is the last size sent to the screens.
	sized message.ContentSize
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
		return model.CursorStyle.Render("> "+line) + "\n"
	}
	return "  " + line + "\n"
}

// renderCursorLine renders one line of a plain (non-checkbox) cursor
// list, highlighted when isCursor -- the shape applicationStatusModel's
// and documentReviewSelectModel's option lists both use.
func renderCursorLine(line string, isCursor bool) string {
	if isCursor {
		return model.CursorStyle.Render("> "+line) + "\n"
	}
	return "  " + line + "\n"
}

func New(
	companies *service.CompanyService,
	postings *service.PostingService,
	applications *service.ApplicationService,
	documentService *service.DocumentService,
	scores *service.ScoringService,
	changes *service.ChangeLog,
	syncer *sync.Syncer,
	docs *documents.Store,
) *App {
	a := &App{
		companyService:        companies,
		postingService:        postings,
		applicationService:    applications,
		documentService:       documentService,
		scoringService:        scores,
		changeLogService:      changes,
		seen:                  map[screen]int64{},
		syncer:                syncer,
		currentScreen:         screenActiveApplications,
		allPostings:           newAllPostingsModel(postings),
		activeApplicationList: newActiveApplicationListModel(applications, documentService, docs),
		hideArchived:          true,
		documents:             docs,
		exportDir:             defaultExportDir,
		openURL:               openInBrowser,
		companyList:           newCompanyListModel(companies, postings),
		syncing:               model.NewSyncing(syncer),
		router:                model.NewMsgRouter(),
		instance:              message.NewScreenInstance(),
	}
	a.stack = newScreenStack(a.router)

	modules := []model.Routable{
		a,
		a.activeApplicationList,
		a.companyList,
		a.allPostings,
		a.syncing,
	}

	for _, module := range modules {
		if err := a.router.Register(module); err != nil {
			panic(err)
		}
	}
	// The home screen is up first. What hearing so starts is a read, which
	// Init starts too.
	_ = a.router.Dispatch(a.focusSignal())

	return a
}

func (a *App) focusSignal() tea.Msg {
	return message.New(message.Source{Process: a.origin}, message.Subject{}, message.OpNone, time.Now().UTC(),
		message.Signal[message.FocusChanged]{Value: message.FocusChanged{Focused: a.focused()}})
}

// stackedAs is the screen each kind a Nav pushes shows as, for the paths
// that still switch on currentScreen.
var stackedAs = map[message.Screen]screen{
	message.ScreenCompanyForm:          screenCompanyForm,
	message.ScreenCompanyEdit:          screenCompanyEdit,
	message.ScreenCompanyDelete:        screenCompanyDelete,
	message.ScreenCompanyPostings:      screenPostingList,
	message.ScreenFilterSelect:         screenFilterSelect,
	message.ScreenPostingDetail:        screenPostingDetail,
	message.ScreenApplicationStatus:    screenApplicationStatusSelect,
	message.ScreenApplicationNotes:     screenApplicationNotesEdit,
	message.ScreenApplicationDetail:    screenApplicationDetail,
	message.ScreenApplicationDelete:    screenApplicationDelete,
	message.ScreenDocumentReviewSelect: screenDocumentReviewSelect,
	message.ScreenDocumentReviewForm:   screenDocumentReviewForm,
	message.ScreenApplicationExport:    screenApplicationExport,
	message.ScreenApplicationSubmit:    screenApplicationSubmit,
	message.ScreenApplicationForm:      screenApplicationForm,
}

// push opens s over the screen up: routes, then Init, so nothing Init
// starts can report before s can hear it.
func (a *App) push(kind message.Screen, s stackScreen) tea.Cmd {
	as, ok := stackedAs[kind]
	if !ok {
		return a.fail(fmt.Errorf("tui: %v isn't a screen a Nav pushes", kind))
	}
	if err := a.stack.push(kind, s, a.currentScreen); err != nil {
		return a.fail(err)
	}
	a.currentScreen = as
	return s.Init()
}

func (a *App) replace(kind message.Screen, s stackScreen) tea.Cmd {
	as, ok := stackedAs[kind]
	if !ok {
		return a.fail(fmt.Errorf("tui: %v isn't a screen a Nav pushes", kind))
	}
	if err := a.stack.replace(kind, s); err != nil {
		return a.fail(err)
	}
	a.currentScreen = as
	return s.Init()
}

func (a *App) pop() tea.Cmd {
	under, err := a.stack.pop()
	if err != nil {
		return a.fail(err)
	}
	a.currentScreen = under
	return nil
}

func (a *App) fail(err error) tea.Cmd {
	return message.Fail(message.Source{Process: a.origin}, err)
}

// focused is the instance of the converted screen on screen, or 0 when
// the screen up hasn't registered routes yet, including one of App's own
// shown over a pushed screen.
func (a *App) focused() message.ScreenInstance {
	if a.stack.len() > 0 {
		if stackedAs[a.stack.topKind()] == a.currentScreen {
			return a.stack.top().Instance()
		}
		return 0
	}
	switch a.currentScreen {
	case screenActiveApplications:
		return a.activeApplicationList.Instance()
	case screenCompanyList:
		return a.companyList.Instance()
	case screenAllPostings:
		return a.allPostings.Instance()
	}
	return 0
}

// onTop is whether meta is from the screen on top. A Nav from any other,
// such as a second esc typed before the first one's close arrived, is
// dropped.
func (a *App) onTop(meta message.Meta) bool {
	return meta.Source.Instance != 0 && meta.Source.Instance == a.focused()
}

// unknownNav reports a Nav for a screen App doesn't open that way.
func (a *App) unknownNav(nav tea.Msg) tea.Cmd {
	return a.fail(fmt.Errorf("tui: no screen opens on %+v", nav))
}

// navNone pops, or opens a screen that shows nothing yet.
func (a *App) navNone(e message.Event[message.Nav[message.None]]) tea.Cmd {
	if !a.onTop(e.Meta) {
		return nil
	}
	switch {
	case e.Payload.Op == message.NavPop:
		return a.pop()
	case e.Payload.Op == message.NavPush && e.Payload.Screen == message.ScreenCompanyForm:
		return a.push(e.Payload.Screen, newCompanyFormModel(a.syncer))
	}
	return a.unknownNav(e)
}

func (a *App) navCompany(e message.Event[message.Nav[message.Company]]) tea.Cmd {
	if !a.onTop(e.Meta) {
		return nil
	}
	c := e.Payload.Value
	switch {
	case e.Payload.Op == message.NavPush && e.Payload.Screen == message.ScreenCompanyEdit:
		return a.push(e.Payload.Screen, newCompanyEditModel(a.companyService, c.ID, c.Name))
	case e.Payload.Op == message.NavPush && e.Payload.Screen == message.ScreenCompanyPostings:
		return a.push(e.Payload.Screen, newPostingListModel(a.companyService, a.postingService, c, a.hideArchived, a.sized))
	}
	return a.unknownNav(e)
}

func (a *App) navCompanyRow(e message.Event[message.Nav[message.CompanyRow]]) tea.Cmd {
	if !a.onTop(e.Meta) {
		return nil
	}
	row := e.Payload.Value
	if e.Payload.Op == message.NavPush && e.Payload.Screen == message.ScreenCompanyDelete {
		return a.push(e.Payload.Screen, newCompanyDeleteModel(a.companyService, companyOf(row.Company), row.OpenPostings, a.sized.Width))
	}
	return a.unknownNav(e)
}

func (a *App) navFilterSelection(e message.Event[message.Nav[message.FilterSelection]]) tea.Cmd {
	if !a.onTop(e.Meta) {
		return nil
	}
	if e.Payload.Op == message.NavPush && e.Payload.Screen == message.ScreenFilterSelect {
		return a.push(e.Payload.Screen, newFilterSelectModel(a.companyService, e.Payload.Value, a.sized))
	}
	return a.unknownNav(e)
}

// postingList is the open posting list, or nil, for the screens over it
// that are still App's.
func (a *App) postingList() *postingListModel {
	l, _ := a.stack.open(message.ScreenCompanyPostings).(*postingListModel)
	return l
}

func (a *App) navPostingDetail(e message.Event[message.Nav[message.PostingDetail]]) tea.Cmd {
	if !a.onTop(e.Meta) {
		return nil
	}
	switch {
	case e.Payload.Op == message.NavPush && e.Payload.Screen == message.ScreenPostingDetail:
		// It reads in Init, so it's as up to date as the change log read so far.
		a.seen[screenPostingDetail] = a.latestSeen
		return a.push(e.Payload.Screen, a.newPostingDetail(e.Payload.Value))
	case e.Payload.Op == message.NavReplace && e.Payload.Screen == message.ScreenPostingDetail:
		return a.replace(e.Payload.Screen, a.newPostingDetail(e.Payload.Value))
	}
	return a.unknownNav(e)
}

func (a *App) newPostingDetail(d message.PostingDetail) *postingDetailModel {
	return newPostingDetailModel(a.postingService, a.documentService, a.scoringService, a.documents, d, a.sized)
}

func (a *App) navApplication(e message.Event[message.Nav[message.Application]]) tea.Cmd {
	if !a.onTop(e.Meta) {
		return nil
	}
	if e.Payload.Op == message.NavPush {
		switch e.Payload.Screen {
		case message.ScreenApplicationStatus:
			return a.push(e.Payload.Screen, newApplicationStatusModel(a.applicationService, e.Payload.Value))
		case message.ScreenApplicationNotes:
			return a.push(e.Payload.Screen, newApplicationNotesModel(a.applicationService, e.Payload.Value, a.sized))
		case message.ScreenDocumentReviewSelect:
			return a.push(e.Payload.Screen, newDocumentReviewSelectModel(a.documents, e.Payload.Value))
		case message.ScreenApplicationForm:
			return a.push(e.Payload.Screen, newApplicationFormModel(a.postingService, e.Payload.Value, a.sized))
		}
	}
	return a.unknownNav(e)
}

// navDocumentReview opens the review form, over application detail or in
// place of the picker.
func (a *App) navDocumentReview(e message.Event[message.Nav[message.DocumentReview]]) tea.Cmd {
	if !a.onTop(e.Meta) {
		return nil
	}
	switch {
	case e.Payload.Op == message.NavPush && e.Payload.Screen == message.ScreenDocumentReviewForm:
		return a.push(e.Payload.Screen, a.newDocumentReviewForm(e.Payload.Value))
	case e.Payload.Op == message.NavReplace && e.Payload.Screen == message.ScreenDocumentReviewForm:
		return a.replace(e.Payload.Screen, a.newDocumentReviewForm(e.Payload.Value))
	}
	return a.unknownNav(e)
}

func (a *App) newDocumentReviewForm(d message.DocumentReview) *documentReviewFormModel {
	return newDocumentReviewFormModel(a.documentService, a.documents, d, a.sized)
}

// postingDetail is the open posting detail, or nil, for the screens over
// it that are still App's.
func (a *App) postingDetail() *postingDetailModel {
	d, _ := a.stack.open(message.ScreenPostingDetail).(*postingDetailModel)
	return d
}

func (a *App) navApplicationDetail(e message.Event[message.Nav[message.ApplicationDetail]]) tea.Cmd {
	if !a.onTop(e.Meta) {
		return nil
	}
	if e.Payload.Op == message.NavPush {
		switch e.Payload.Screen {
		case message.ScreenApplicationDetail:
			// It reads in Init, so it's as up to date as the change log read so far.
			a.seen[screenApplicationDetail] = a.latestSeen
			return a.push(e.Payload.Screen, newApplicationDetailModel(a.applicationService, a.postingService, a.companyService, a.documentService, a.documents, e.Payload.Value))
		case message.ScreenApplicationDelete:
			return a.push(e.Payload.Screen, newApplicationDeleteModel(a.applicationService, e.Payload.Value, a.sized.Width))
		case message.ScreenApplicationExport:
			return a.push(e.Payload.Screen, newApplicationExportModel(a.documentService, a.documents, e.Payload.Value, a.exportDir, a.sized))
		case message.ScreenApplicationSubmit:
			return a.push(e.Payload.Screen, newApplicationSubmitModel(a.applicationService, a.documentService, a.documents, a.openURL, e.Payload.Value, a.exportDir))
		}
	}
	return a.unknownNav(e)
}

// applicationDetail is the open application detail, or nil.
func (a *App) applicationDetail() *applicationDetailModel {
	d, _ := a.stack.open(message.ScreenApplicationDetail).(*applicationDetailModel)
	return d
}

func (a *App) hideArchivedChanged(e message.Event[message.Signal[message.HideArchived]]) tea.Cmd {
	a.hideArchived = e.Payload.Value.Hidden
	return nil
}

// WithStatus starts the App with status on the status line.
func (a *App) WithStatus(status string) *App {
	a.status = status
	return a
}

// documentStatusLine renders a single "<label>: found (<path>)" or
// "<label>: not found (<path>)" line for the documents section, followed
// by that document's latest review outcome (see reviewBadge) and, when
// the review left notes, an indented "Notes:" line -- the reviewer's
// record of what specifically to fix before the next cycle (see
// #83).
func documentStatusLine(label string, exists bool, path string, review document.Review, hasReview bool) string {
	status := "not found"
	if exists {
		status = "found"
	}
	line := model.FieldLabel.Render(label+":") + " " + status + " (" + path + ") " + reviewBadge(review, hasReview) + "\n"
	if hasReview && review.Notes != "" {
		line += "  " + model.DimStyle.Render("Notes: "+review.Notes) + "\n"
	}
	return line
}

// outcomeDisplay is how each review outcome looks: its style, shared by
// reviewBadge and reviewGlyph, and its glyph. An outcome missing here
// renders dim, as "[<outcome>]" and "?"; a test checks every
// document.Outcomes() has an entry.
var outcomeDisplay = map[document.Outcome]struct {
	style lipgloss.Style
	glyph string
}{
	document.Passed:  {model.PassStyle, "✓"},
	document.Flagged: {model.ErrStyle, "✗"},
}

// reviewBadge renders a document's latest review outcome as a short
// styled tag: dim "[not reviewed]" when hasReview is false (no review
// recorded yet), otherwise the outcome's name in capitals, styled per
// outcomeDisplay: green "[PASSED]", red "[FLAGGED]".
func reviewBadge(review document.Review, hasReview bool) string {
	if !hasReview {
		return model.DimStyle.Render("[not reviewed]")
	}
	display, ok := outcomeDisplay[review.Outcome]
	if !ok {
		return model.DimStyle.Render("[" + review.Outcome.String() + "]")
	}
	return display.style.Render("[" + strings.ToUpper(review.Outcome.String()) + "]")
}

// reviewGlyphSummary renders a compact, single-line summary of an
// application's latest cover-letter/resume review outcomes, for list
// rows (e.g. the active-applications table) that don't have room for
// reviewBadge's full "[FLAGGED]"/notes rendering (#83).
// A document with no entry in reviews (no review recorded yet)
// renders as a dim "-".
func reviewGlyphSummary(reviews map[document.Type]document.Review) string {
	entries := make([]string, 0, len(document.Types()))
	for _, documentType := range document.Types() {
		review, ok := reviews[documentType]
		entries = append(entries, documentAbbreviation(documentType)+":"+reviewGlyph(review, ok))
	}
	return strings.Join(entries, " ")
}

// documentAbbreviation is the initials of documentType's label, for the
// review column: "CL" for cover letter, "R" for resume.
func documentAbbreviation(documentType document.Type) string {
	var initials []rune
	for _, word := range strings.Fields(documentType.Label()) {
		initials = append(initials, unicode.ToUpper([]rune(word)[0]))
	}
	return string(initials)
}

// reviewGlyph is reviewBadge's one-character form, for list rows: dim "-"
// with no review, otherwise the outcome's glyph from outcomeDisplay.
func reviewGlyph(review document.Review, hasReview bool) string {
	if !hasReview {
		return model.DimStyle.Render("-")
	}
	display, ok := outcomeDisplay[review.Outcome]
	if !ok {
		return model.DimStyle.Render("?")
	}
	return display.style.Render(display.glyph)
}

// postingDetailContent renders a posting's fields, application state, and
// description as the scrollable body of the detail view (title is rendered
// separately, as a fixed header outside the viewport). application/
// hasApplication are passed in rather than fetched here so this stays a
// pure function of already-loaded state.
//
// docs resolves the application's document paths and their presence via
// os.Stat -- done inline here rather than through a separate
// tea.Cmd/tea.Msg round trip like the rest of this file's store-backed
// state, since checking whether two local files exist is cheap/local
// enough that a second async fetch just to avoid it here would be
// over-applying that convention. When hasApplication
// is false, no documents section is rendered at all -- "no application
// -> show nothing".
//
// latestReviews is, like application, read by posting detail and passed
// in rather than queried here -- a DB read, unlike docs.Status's local
// os.Stat, follows the same tea.Cmd/tea.Msg convention as the rest of
// this file's store-backed state (#83). A document with no entry in the
// map renders as "not reviewed".
func postingDetailContent(p posting.Posting, application application.Application, hasApplication bool, docs *documents.Store, latestReviews map[document.Type]document.Review, width int) string {
	var b strings.Builder
	b.WriteString(sectionHeading("Posting", width) + "\n")
	fields := []struct{ label, value string }{
		{"Department", p.Department},
		{"Team", p.Team},
		{"Location", p.Location},
		{"Employment type", p.EmploymentType},
		{"Workplace type", p.WorkplaceType},
		{"Status", p.ListingStatus.String()},
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
		for _, documentType := range document.Types() {
			doc, err := status.Doc(documentType)
			if err != nil {
				continue
			}
			review, hasReview := latestReviews[documentType]
			b.WriteString(detailDocumentField(documentTitle(documentType), doc.Exists, doc.Path, review, hasReview, width))
		}
	} else {
		b.WriteString(model.HelpStyle.Render("No application started -- press 'a' to start one.") + "\n")
	}
	if desc := p.DescriptionText; desc != "" {
		b.WriteString("\n" + sectionHeading("Description", width) + "\n" + desc + "\n")
	}
	return b.String()
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

// indexOfCompany returns id's index in companies, or -1 if not present.
func indexOfCompany(companies []message.Company, id int64) int {
	for i, c := range companies {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// reloadCompanies asks the company list to read its companies again.
func (a *App) reloadCompanies() tea.Cmd {
	return requestLoad[message.CompanyList](message.Source{Instance: a.instance})
}

// requestLoad asks the model that shows T to read it again.
func requestLoad[T message.Body](src message.Source) tea.Cmd {
	return func() tea.Msg {
		return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), message.Load[T]{})
	}
}

// sortCompaniesByName keeps an in-memory companies slice in the same
// order ListActiveCompanies' `ORDER BY name` would return -- called after
// any in-place mutation (create, rename) that could change where an
// entry belongs, since Go's string `<` matches SQLite's default byte-wise
// collation.
func sortCompaniesByName(companies []message.Company) {
	sort.Slice(companies, func(i, j int) bool {
		return companies[i].Name < companies[j].Name
	})
}

func (a *App) Init() tea.Cmd {
	return tea.Batch(a.activeApplicationList.Init(), a.companyList.Init(), a.allPostings.Init(), a.pollChanges())
}

// boardCheckTimeout bounds createCompany's live board check, so an
// unreachable API fails the add (closed) instead of leaving the form
// waiting (#36). Syncer already gives up on any fetch
// after sync.Config.FetchTimeout (#142); this is tighter because someone
// is sitting at the form waiting for it.
const boardCheckTimeout = 15 * time.Second

// createCompany saves a company only once its source ref resolves to a
// real board (see sync.Syncer.CreateCompany).
func createCompany(syncer *sync.Syncer, src message.Source, name, source, sourceRef string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), boardCheckTimeout)
		defer cancel()
		company, err := syncer.CreateCompany(ctx, name, source, sourceRef)
		if err != nil {
			return message.New(src, message.Subject{}, message.OpCreated, time.Now().UTC(), model.ErrorOf(err))
		}
		return message.Created(src, company.ID, company.CreatedAt, companySnapshot(*company))
	}
}

func deleteCompany(companies *service.CompanyService, src message.Source, companyID int64) tea.Cmd {
	return func() tea.Msg {
		deleted, err := companies.Delete(context.Background(), companyID)
		if err != nil {
			return message.New(src, message.Subject{Type: message.TypeCompany, ID: companyID}, message.OpDeleted, time.Now().UTC(), model.ErrorOf(err))
		}
		return message.Deleted(src, companyID, deleted.At, companySnapshot(deleted.Before))
	}
}

func updateCompanyName(companies *service.CompanyService, src message.Source, companyID int64, name string) tea.Cmd {
	return func() tea.Msg {
		renamed, err := companies.SetName(context.Background(), companyID, name)
		if err != nil {
			return message.New(src, message.Subject{Type: message.TypeCompany, ID: companyID}, message.OpUpdated, time.Now().UTC(), model.ErrorOf(err))
		}
		return message.Updated(src, companyID, renamed.At, companySnapshot(renamed.Before), companySnapshot(renamed.After))
	}
}

func toggleInterested(postings *service.PostingService, src message.Source, postingID int64, currentlyInterested bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var (
			m   *change.Of[posting.Markup]
			err error
		)
		if currentlyInterested {
			m, err = postings.UnmarkInterested(ctx, postingID)
		} else {
			m, err = postings.SetInterested(ctx, postingID)
		}
		return markupChanged(src, postingID, m, err)
	}
}

func toggleArchived(postings *service.PostingService, src message.Source, postingID int64, currentlyArchived bool) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var (
			m   *change.Of[posting.Markup]
			err error
		)
		if currentlyArchived {
			m, err = postings.Unarchive(ctx, postingID)
		} else {
			m, err = postings.SetArchived(ctx, postingID)
		}
		return markupChanged(src, postingID, m, err)
	}
}

func markupChanged(src message.Source, postingID int64, m *change.Of[posting.Markup], err error) tea.Msg {
	subject := message.Subject{Type: message.TypePosting, ID: postingID}
	if err != nil {
		return message.New(src, subject, message.OpUpdated, time.Now().UTC(), model.ErrorOf(err))
	}
	return message.New(src, subject, message.OpUpdated, m.At,
		message.Changed[message.Markup]{Before: markupSnapshot(m.Before), After: markupSnapshot(m.After)})
}

// createApplication's error is about the posting, since there's no
// application yet.
func createApplication(postings *service.PostingService, src message.Source, postingID int64) tea.Cmd {
	return func() tea.Msg {
		app, err := postings.ApplyTo(context.Background(), postingID)
		if err != nil {
			return message.New(src, message.Subject{Type: message.TypePosting, ID: postingID}, message.OpCreated, time.Now().UTC(), model.ErrorOf(err))
		}
		return message.Created(src, app.ID, app.CreatedAt, applicationSnapshot(*app))
	}
}

func applicationWritten(src message.Source, applicationID int64, op message.Op, a *change.Of[application.Application], err error) tea.Msg {
	if err != nil {
		return message.New(src, message.Subject{Type: message.TypeApplication, ID: applicationID}, op, time.Now().UTC(), model.ErrorOf(err))
	}
	if op == message.OpDeleted {
		return message.Deleted(src, applicationID, a.At, applicationSnapshot(a.Before))
	}
	return message.Updated(src, applicationID, a.At, applicationSnapshot(a.Before), applicationSnapshot(a.After))
}

// currentReviews is the latest review of each of an application's
// documents, leaving out one whose document has changed since.
func currentReviews(documentService *service.DocumentService, docs *documents.Store, applicationID int64) (map[document.Type]document.Review, error) {
	reviews, err := documentService.LatestReviews(context.Background(), applicationID)
	if err != nil {
		return nil, err
	}
	return documents.Current(docs.Status(applicationID), reviews, document.Review.IsCurrent)
}

func updateApplicationStatus(applications *service.ApplicationService, src message.Source, applicationID int64, status application.Status) tea.Cmd {
	return func() tea.Msg {
		a, err := applications.SetStatus(context.Background(), applicationID, status)
		return applicationWritten(src, applicationID, message.OpUpdated, a, err)
	}
}

func updateApplicationNotes(applications *service.ApplicationService, src message.Source, applicationID int64, notes string) tea.Cmd {
	return func() tea.Msg {
		a, err := applications.SetNotes(context.Background(), applicationID, notes)
		return applicationWritten(src, applicationID, message.OpUpdated, a, err)
	}
}

// loadPostings re-applies the company's currently-saved filters after
// fetching, since ListPostingsByCompany returns every stored posting:
// sync stores them all, and filters only decide what's shown. Without
// this, any reload (including the one triggered right after saving a new
// filter selection) would silently undo the narrowing by loading
// everything unfiltered.
//
// Also loads each visible posting's markup (interested/archived) so the
// list can render it inline -- one PostingService.Markup call per posting.
// Every posting always has exactly one markup row (created alongside it
// in UpsertPosting), so N+1 here is N cheap local sqlite reads, not N
// round trips to a remote service.
//
// Closed postings are always dropped: a listing that has closed on the job
// board can't be applied to, so it has no place in a list for finding
// postings to act on. They stay in the db (never deleted),
// applications on them stay reachable from the
// active-applications screen, and a posting that reopens reappears on the
// next load. Filtered here rather than in ListPostingsByCompany because
// sync also uses that query and needs every posting, closed ones included.
//
// hideArchived additionally drops archived postings from the result --
// ephemeral TUI display state (see the App.hideArchived field), not a
// company_filters row, so it's applied here rather than at the
// ingestion-gating layer.
func loadPostings(companies *service.CompanyService, postings *service.PostingService, src message.Source, companyID int64, hideArchived bool) tea.Cmd {
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
		list.Departments, list.Locations = splitCompanyFilters(companyFilters)
		list.Postings, err = filterPostingsByCompanyFilters(openSnapshots(stored), companyFilters)
		if err != nil {
			return failed(err)
		}

		list.Markup = make(map[int64]message.Markup, len(list.Postings))
		for _, p := range list.Postings {
			m, err := postings.Markup(ctx, p.ID)
			if err != nil {
				return failed(err)
			}
			list.Markup[p.ID] = markupSnapshot(*m)
		}

		if hideArchived {
			list.Postings = filterOutArchived(list.Postings, list.Markup)
		}

		return message.New(src, subject, message.OpNone, time.Now().UTC(), message.Read[message.PostingList]{Value: list})
	}
}

// openSnapshots is the postings whose listing hasn't closed on the job
// board.
func openSnapshots(postings []posting.Posting) []message.Posting {
	visible := make([]message.Posting, 0, len(postings))
	for _, p := range postings {
		if p.ListingStatus == posting.ListingClosed {
			continue
		}
		visible = append(visible, postingSnapshot(p))
	}
	return visible
}

// filterOutArchived drops postings whose markup has ArchivedAt set.
func filterOutArchived(postings []message.Posting, markup map[int64]message.Markup) []message.Posting {
	visible := make([]message.Posting, 0, len(postings))
	for _, p := range postings {
		if !markup[p.ID].ArchivedAt.IsZero() {
			continue
		}
		visible = append(visible, p)
	}
	return visible
}

// filterPostingsByCompanyFilters keeps only postings matching
// companyFilters. An unsupported field is returned as an error rather
// than ignored.
func filterPostingsByCompanyFilters(postings []message.Posting, companyFilters []filter.Filter) ([]message.Posting, error) {
	narrowed := make([]message.Posting, 0, len(postings))
	for _, p := range postings {
		match, err := filter.Match(filter.Posting{Department: p.Department, Location: p.Location}, companyFilters)
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
func splitCompanyFilters(filters []filter.Filter) (departments, locations []string) {
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

func companyFiltersFrom(departments, locations []string) []filter.Filter {
	filters := make([]filter.Filter, 0, len(departments)+len(locations))
	for _, d := range departments {
		filters = append(filters, filter.Filter{Field: filter.FieldDepartment, Value: d})
	}
	for _, l := range locations {
		filters = append(filters, filter.Filter{Field: filter.FieldLocation, Value: l})
	}
	return filters
}

// errInternal is the banner for a fault: the error itself means nothing
// to the user, so it says where the details went.
var errInternal = errors.New("internal error, see swamp.log")

// handle is the routers handler for global control messages
func (a *App) handle(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return tea.Quit, true
		}
	}
	return nil, false
}

// Routes are the banner's, which hears every error whoever it's for. An
// error stays up until the next error or notice, since a load that
// succeeds sends neither and loads finish in any order.
func (a *App) Routes() []*model.Route {
	return []*model.Route{
		model.Always(a.errorReported),
		model.Always(a.notified),
		model.Always(a.navNone),
		model.Always(a.navCompany),
		model.Always(a.navCompanyRow),
		model.Always(a.navFilterSelection),
		model.Always(a.navPostingDetail),
		model.Always(a.navApplication),
		model.Always(a.navApplicationDetail),
		model.Always(a.navDocumentReview),
		model.Always(a.hideArchivedChanged),
		model.Always(a.applicationChanged),
		model.Always(a.browserOpened),
		model.Always(a.exported),
	}
}

// browserOpened clears an earlier failure from the banner.
func (a *App) browserOpened(message.Event[message.Ran[message.OpenBrowser]]) tea.Cmd {
	a.err = nil
	return nil
}

// exported remembers where the export went, for the next export or submit
// to start from, and clears an earlier failure from the banner.
func (a *App) exported(e message.Event[message.Ran[message.ExportReport]]) tea.Cmd {
	a.err, a.exportDir = nil, e.Payload.Value.Dir
	return nil
}

// applicationChanged clears its asker's earlier error, which the write
// going through has made stale.
func (a *App) applicationChanged(e message.Event[message.Changed[message.Application]]) tea.Cmd {
	if e.Meta.Source.Instance != 0 {
		a.err = nil
	}
	return nil
}

func (a *App) Instance() message.ScreenInstance { return a.instance }

func (a *App) errorReported(e message.Event[message.Error]) tea.Cmd {
	if e.Payload.Code == message.ErrFault {
		// The TUI owns the terminal, so cmd/swamp points log at swamp.log.
		log.Printf("tui: %s", e.Payload.Text)
		a.err = errInternal
		return nil
	}
	if e.Payload.Code == message.ErrPostingClosed {
		// Refused, not failed: the posting is no longer listed, so
		// there's no form left to submit.
		a.err, a.status = nil, "This posting is closed; an application can't be started on it"
		return nil
	}
	a.err, a.status = errors.New(e.Payload.Text), ""
	return nil
}

func (a *App) notified(e message.Event[message.Notify]) tea.Cmd {
	a.err, a.status = nil, e.Payload.Status
	return nil
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd, handled := a.handle(msg)
	if handled {
		return a, cmd
	}

	// Before Dispatch, since a Nav's route moves the screen.
	before, focusedBefore := a.currentScreen, a.focused()
	routed := a.router.Dispatch(msg)
	model, cmd := a.update(msg)
	cmd = tea.Batch(routed, cmd)
	// A replace changes the screen in focus but not currentScreen.
	if a.currentScreen != before || a.focused() != focusedBefore {
		// Dispatched now, not as a Cmd, so keys already queued see it.
		focused := a.router.Dispatch(a.focusSignal())
		cmd = tea.Batch(cmd, focused, a.activate())
	}
	// The window or the banner may have changed size, on any message.
	if size := (message.ContentSize{Width: a.width, Rows: a.screenRows()}); size != a.sized {
		a.sized = size
		cmd = tea.Batch(cmd, a.router.Dispatch(message.New(message.Source{Process: a.origin}, message.Subject{}, message.OpNone, time.Now().UTC(),
			message.Signal[message.ContentSize]{Value: size})))
	}
	return model, cmd
}

func (a *App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case changeTickMsg:
		return a, a.handleTick()
	case changesMsg:
		return a, a.handleChanges(msg)
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		// The terminal rewraps the old frame when it narrows, pushing rows
		// below where the new one ends. The renderer only clears below a
		// frame that got shorter, so without this they stay on screen.
		return a, tea.ClearScreen
	case tea.KeyPressMsg:
		prevScreen := a.currentScreen
		model, cmd := a.updateKeyMsg(msg)
		if a.currentScreen != prevScreen {
			// A stale error from whatever screen the user just left (e.g.
			// a failed save they then cancelled out of) shouldn't keep
			// showing on the screen they land on next -- see #41.
			a.err = nil
		}
		return model, cmd
	}
	return a, nil
}

func (a *App) updateKeyMsg(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Ahead of the screen's own keys, so it works with the company search
	// open too.
	if msg.String() == "tab" {
		switch a.currentScreen {
		case screenActiveApplications:
			a.currentScreen = screenCompanyList
			return a, nil
		case screenCompanyList:
			a.currentScreen = screenAllPostings
			return a, nil
		case screenAllPostings:
			a.currentScreen = screenActiveApplications
			return a, nil
		}
	}
	return a, nil
}

// banner is the error or status message View() draws below the active
// screen, including the blank line separating the two, or "" when there's
// nothing to show. It's at the bottom until notifications get a place of
// their own, so one coming and going doesn't move the screen.
func (a *App) banner() string {
	if a.err != nil {
		return "\n\n" + model.ErrStyle.Render(fmt.Sprintf("error: %v", a.err))
	}
	if syncing := a.syncing.View().Content; syncing != "" {
		return "\n\n" + syncing
	}
	if a.status != "" {
		return "\n\n" + model.DimStyle.Render(a.status)
	}
	return ""
}

// screenRows is the terminal height left for the active screen once
// View() has drawn the header above it and the banner below. Every
// newline in them takes one row. A line wider than the terminal doesn't
// take extra rows: bubbletea truncates lines to the window width rather
// than letting them wrap.
func (a *App) screenRows() int {
	return max(a.height-strings.Count(a.header()+a.banner(), "\n"), 0)
}

func (a *App) View() tea.View {
	body := a.body(a.beneath())
	switch a.currentScreen {
	case screenCompanyDelete, screenApplicationDelete:
		body = overlay(body, a.stack.top().View().Content, a.width)
	}
	v := tea.NewView(a.header() + body + a.banner())
	v.AltScreen = true
	return v
}

// beneath is the screen drawn on screen: a.screen, or the one a
// confirmation's box is drawn over.
func (a *App) beneath() screen {
	switch a.currentScreen {
	case screenCompanyDelete:
		return screenCompanyList
	case screenApplicationDelete:
		under, _ := a.stack.under(message.ScreenApplicationDelete)
		return under
	}
	return a.currentScreen
}

// body draws s, between the header and the banner.
func (a *App) body(s screen) string {
	var b strings.Builder
	switch s {
	case screenActiveApplications:
		b.WriteString(a.activeApplicationList.View().Content)
	case screenApplicationDetail:
		if d := a.applicationDetail(); d != nil {
			b.WriteString(d.View().Content)
		}
	case screenAllPostings:
		b.WriteString(a.allPostings.View().Content)
	case screenCompanyList:
		b.WriteString(a.companyList.View().Content)
	case screenCompanyForm:
		b.WriteString(a.stack.top().View().Content)
	case screenCompanyEdit, screenFilterSelect, screenApplicationStatusSelect, screenApplicationNotesEdit,
		screenDocumentReviewSelect, screenDocumentReviewForm, screenApplicationExport, screenApplicationSubmit, screenApplicationForm:
		b.WriteString(a.stack.top().View().Content)
	case screenPostingList:
		if l := a.postingList(); l != nil {
			b.WriteString(l.View().Content)
		}
	case screenPostingDetail:
		if d := a.postingDetail(); d != nil {
			b.WriteString(d.View().Content)
		}
	}
	return b.String()
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
	return model.SectionStyle.Render(name) + " " + model.DimStyle.Render(strings.Repeat("─", ruleWidth))
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
	return model.FieldLabel.Render(label) + gap + strings.Join(lines, "\n") + "\n"
}

// detailDocumentField is detailField for a drafted document: its presence
// and path, the latest review's badge, and the review's notes on their
// own line under the value column -- the aligned counterpart of
// documentStatusLine, which application detail still uses.
func detailDocumentField(label string, exists bool, path string, review document.Review, hasReview bool, width int) string {
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
		row += detailField("", model.DimStyle.Render("Notes: "+review.Notes), width)
	}
	return row
}
