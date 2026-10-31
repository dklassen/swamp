// Package tui is the terminal UI: Bubble Tea model for browsing and
// managing companies and their postings. Update stays side-effect-free --
// store calls are wrapped in tea.Cmd so the message-passing logic is
// testable without a real terminal; View
// (rendering) is not held to automated test coverage, per this project's
// testing decisions.
package tui

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/documents"
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
