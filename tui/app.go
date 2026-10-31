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
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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

type App struct {
	companyService     *service.CompanyService
	postingService     *service.PostingService
	applicationService *service.ApplicationService
	documentService    *service.DocumentService
	scoringService     *service.ScoringService
	changeLogService   *service.ChangeLog
	origin             string
	// changes reads the log's rows as events, nil without a change log,
	// on a tick every changePoll.
	changes     *changeReader
	changePoll  time.Duration
	syncer      *sync.Syncer
	companyList *companyListModel
	allPostings *allPostingsModel
	// tab is the tab up, which the stack's screens are pushed over.
	tab    message.Screen
	stack  *screenStack
	status string
	err    error
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
		syncer:                syncer,
		tab:                   message.ScreenActiveApplications,
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

// tabs are the persistent screens, in the order tab goes through them.
var tabs = []message.Screen{message.ScreenActiveApplications, message.ScreenCompanyList, message.ScreenAllPostings}

// path is the tab up, then each screen pushed over it: the way esc goes
// back, reversed.
func (a *App) path() []message.Screen {
	return append([]message.Screen{a.tab}, a.stack.kinds()...)
}

// onScreen is the kind of the screen on top.
func (a *App) onScreen() message.Screen {
	path := a.path()
	return path[len(path)-1]
}

// screenOf is the open screen of kind.
func (a *App) screenOf(kind message.Screen) stackScreen {
	switch kind {
	case message.ScreenActiveApplications:
		return a.activeApplicationList
	case message.ScreenCompanyList:
		return a.companyList
	case message.ScreenAllPostings:
		return a.allPostings
	}
	return a.stack.open(kind)
}

// push opens s over the screen up: routes, then Init, so nothing Init
// starts can report before s can hear it.
func (a *App) push(kind message.Screen, s stackScreen) tea.Cmd {
	if err := a.stack.push(kind, s); err != nil {
		return a.fail(err)
	}
	return s.Init()
}

func (a *App) replace(kind message.Screen, s stackScreen) tea.Cmd {
	if err := a.stack.replace(kind, s); err != nil {
		return a.fail(err)
	}
	return s.Init()
}

func (a *App) pop() tea.Cmd {
	if err := a.stack.pop(); err != nil {
		return a.fail(err)
	}
	return nil
}

func (a *App) fail(err error) tea.Cmd {
	return message.Fail(message.Source{Process: a.origin}, err)
}

// focused is the instance of the screen on top.
func (a *App) focused() message.ScreenInstance {
	return a.screenOf(a.onScreen()).Instance()
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

// postingList is the open posting list, or nil.
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

// postingDetail is the open posting detail, or nil.
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

// outcomeDisplay is how each review outcome looks: its style, shared by
// reviewBadge and reviewGlyphSummary, and its glyph. An outcome missing here
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
// Each document is abbreviated to its label's initials ("CL", "R"), then
// its outcome's glyph: a dim "-" with no review recorded yet.
func reviewGlyphSummary(reviews map[document.Type]document.Review) string {
	entries := make([]string, 0, len(document.Types()))
	for _, documentType := range document.Types() {
		var initials []rune
		for _, word := range strings.Fields(documentType.Label()) {
			initials = append(initials, unicode.ToUpper([]rune(word)[0]))
		}
		glyph := model.DimStyle.Render("-")
		if review, ok := reviews[documentType]; ok {
			glyph = model.DimStyle.Render("?")
			if display, ok := outcomeDisplay[review.Outcome]; ok {
				glyph = display.style.Render(display.glyph)
			}
		}
		entries = append(entries, string(initials)+":"+glyph)
	}
	return strings.Join(entries, " ")
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

// changePollInterval is how often the TUI reads the change log. Nothing is
// lost between reads, so this only decides how soon a change appears.
const changePollInterval = 500 * time.Millisecond

// WithChangeLog makes the App show changes other processes make, read
// from the change log after since. origin is this process's own, whose
// rows its commands already sent as events.
func (a *App) WithChangeLog(since int64, origin string) *App {
	a.origin = origin
	a.changes = newChangeReader(a.companyService, a.postingService, a.applicationService, a.documentService, a.scoringService, a.changeLogService, origin, since)
	if err := a.router.Register(a.changes); err != nil {
		panic(err)
	}
	a.changePoll = changePollInterval
	return a
}

// pollChanges asks for the next tick; nil without a change log.
func (a *App) pollChanges() tea.Cmd {
	if a.changes == nil {
		return nil
	}
	src := message.Source{Instance: a.instance}
	return tea.Tick(a.changePoll, func(at time.Time) tea.Msg {
		return message.New(src, message.Subject{}, message.OpNone, at.UTC(), message.Signal[message.ChangeTick]{})
	})
}

// changeTick asks for the next tick; the reader reads on this one.
func (a *App) changeTick(message.Event[message.Signal[message.ChangeTick]]) tea.Cmd {
	return a.pollChanges()
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

// handle does what's App's alone, ahead of the screens: quitting, the
// window's size and switching tabs.
func (a *App) handle(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case msg.String() == "ctrl+c":
			return tea.Quit, true
		case msg.String() == "tab" && a.stack.len() == 0:
			a.tab = tabs[(slices.Index(tabs, a.tab)+1)%len(tabs)]
			// A stale error from the tab just left shouldn't show on the next.
			a.err = nil
			return nil, true
		}
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		// The terminal rewraps the old frame when it narrows, pushing rows
		// below where the new one ends. The renderer only clears below a
		// frame that got shorter, so without this they stay on screen.
		return tea.ClearScreen, true
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
		model.Always(a.changeTick, model.From(a.instance)),
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
	// Before Dispatch, since a Nav's route moves the screen.
	focusedBefore := a.focused()
	cmd, handled := a.handle(msg)
	if !handled {
		cmd = a.router.Dispatch(msg)
	}
	if a.focused() != focusedBefore {
		// Dispatched now, not as a Cmd, so keys already queued see it.
		cmd = tea.Batch(cmd, a.router.Dispatch(a.focusSignal()))
	}
	// The window or the banner may have changed size, on any message.
	if size := (message.ContentSize{Width: a.width, Rows: a.screenRows()}); size != a.sized {
		a.sized = size
		cmd = tea.Batch(cmd, a.router.Dispatch(message.New(message.Source{Process: a.origin}, message.Subject{}, message.OpNone, time.Now().UTC(),
			message.Signal[message.ContentSize]{Value: size})))
	}
	return a, cmd
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
	path := a.path()
	top := path[len(path)-1]
	body := a.screenOf(top).View().Content
	if top == message.ScreenCompanyDelete || top == message.ScreenApplicationDelete {
		body = overlay(a.screenOf(path[len(path)-2]).View().Content, body, a.width)
	}
	v := tea.NewView(a.header() + body + a.banner())
	v.AltScreen = true
	return v
}

// beneath is the path to the screen drawn on screen: to the one on top,
// or to the one a confirmation's box is drawn over.
func (a *App) beneath() []message.Screen {
	path := a.path()
	switch path[len(path)-1] {
	case message.ScreenCompanyDelete, message.ScreenApplicationDelete:
		return path[:len(path)-1]
	}
	return path
}
