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
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

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
	// changes reads the log's rows as events, nil without a change log.
	changes *changeReader
	// nav is the tabs, and stack the screens pushed over the tab up.
	nav   *Navigation
	stack *Stack
	// navRoutes open what the tables have for each Nav.
	navRoutes []*model.Route
	status    string
	err       error
	// syncing runs sync all and refreshes, and draws their progress in the
	// banner.
	syncing *model.Syncing
	width   int
	height  int

	// router is a message router for Dispatching bubbletea tea.Msg's
	router model.Router
	// instance is the banner's, which App registers routes for.
	instance message.ScreenInstance

	// sized is the last size sent to the screens.
	sized message.ContentSize
}

// New is the App over nav and stack, routed by router, opening what
// tables have for each Nav.
func New(
	router model.Router,
	companies *service.CompanyService,
	postings *service.PostingService,
	applications *service.ApplicationService,
	documentService *service.DocumentService,
	scores *service.ScoringService,
	changes *service.ChangeLog,
	syncer *sync.Syncer,
	nav *Navigation,
	stack *Stack,
	tables ...Table,
) *App {
	a := &App{
		companyService:     companies,
		postingService:     postings,
		applicationService: applications,
		documentService:    documentService,
		scoringService:     scores,
		changeLogService:   changes,
		nav:                nav,
		stack:              stack,
		syncing:            model.NewSyncing(syncer),
		router:             router,
		instance:           message.NewScreenInstance(),
	}
	for _, table := range tables {
		a.navRoutes = append(a.navRoutes, table(a))
	}
	for _, module := range []model.Routable{a, a.nav, a.syncing} {
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

// top is the screen on top: the stack's, or the tab up when nothing is
// pushed.
func (a *App) top() stackScreen {
	if s := a.stack.Top(); s != nil {
		return s
	}
	return a.nav.Screen(a.nav.Tab())
}

func (a *App) focused() message.ScreenInstance { return a.top().Instance() }

// path is the tab up, then each screen pushed over it: the way esc goes
// back, reversed.
func (a *App) path() []message.Screen {
	return append([]message.Screen{a.nav.Tab()}, a.stack.Kinds()...)
}

// onScreen is the kind of the screen on top.
func (a *App) onScreen() message.Screen {
	path := a.path()
	return path[len(path)-1]
}

// screen is the open screen of kind, or nil if none is.
func (a *App) screen(kind message.Screen) stackScreen {
	if s := a.nav.Screen(kind); s != nil {
		return s
	}
	return a.stack.Screen(kind)
}

// WithStatus starts the App with status on the status line.
func (a *App) WithStatus(status string) *App {
	a.status = status
	return a
}

func (a *App) Init() tea.Cmd {
	if a.changes == nil {
		return a.nav.Init()
	}
	return tea.Batch(a.nav.Init(), a.changes.Init())
}

// changePollInterval is how often the TUI reads the change log. Nothing is
// lost between reads, so this only decides how soon a change appears.
const changePollInterval = 500 * time.Millisecond

// WithChangeLog makes the App show changes other processes make, read
// from the change log after since. origin is this process's own, whose
// rows its commands already sent as events.
func (a *App) WithChangeLog(since int64, origin string) *App {
	a.origin = origin
	a.changes = newChangeReader(a.companyService, a.postingService, a.applicationService, a.documentService, a.scoringService, a.changeLogService, origin, since, changePollInterval)
	if err := a.router.Register(a.changes); err != nil {
		panic(err)
	}
	return a
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
		case msg.String() == "tab" && a.stack.Top() == nil:
			a.nav.NextTab()
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

// Routes are the Navs', and the banner's, which hears every error whoever
// it's for. An error stays up until the next error or notice, since a
// load that succeeds sends neither and loads finish in any order.
func (a *App) Routes() []*model.Route {
	return append([]*model.Route{
		model.Always(a.errorReported),
		model.Always(a.notified),
		model.Always(a.applicationChanged),
		model.Always(a.browserOpened),
		model.Always(a.exported),
	}, a.navRoutes...)
}

// Table is what one Nav payload opens, as a route on an App.
type Table func(*App) *model.Route

// Opens is the Table for Nav[T]: each kind in table opens the screen its
// function builds from the Nav's value.
func Opens[T message.Body](table map[message.Screen]func(T) stackScreen) Table {
	return func(a *App) *model.Route {
		return model.Always(navTo(a, table))
	}
}

// navTo opens what table has for each Nav[T] from the screen on top, and
// closes the top on a pop or a replace. Any kind in table opens on a push
// or a replace. A Nav from any other screen, such as a second esc typed
// before the first one's close arrived, is dropped.
func navTo[T message.Body](a *App, table map[message.Screen]func(T) stackScreen) func(message.Event[message.Nav[T]]) tea.Cmd {
	return func(e message.Event[message.Nav[T]]) tea.Cmd {
		if e.Meta.Source.Instance == 0 || e.Meta.Source.Instance != a.focused() {
			return nil
		}
		nav := e.Payload
		var opened stackScreen
		if nav.Op != message.NavPop {
			open, ok := table[nav.Screen]
			if !ok || (nav.Op != message.NavPush && nav.Op != message.NavReplace) {
				return a.fail(fmt.Errorf("tui: no screen opens on %+v", e))
			}
			opened = open(nav.Value)
		}
		if nav.Op != message.NavPush {
			closed, err := a.stack.Pop()
			if err != nil {
				return a.fail(err)
			}
			if err := a.router.Deregister(closed); err != nil {
				return a.fail(err)
			}
		}
		if opened == nil {
			return nil
		}
		// Routed before Init, so nothing Init starts can report before the
		// screen can hear it.
		if err := a.router.Register(opened); err != nil {
			return a.fail(err)
		}
		a.stack.Push(nav.Screen, opened)
		return opened.Init()
	}
}

func (a *App) fail(err error) tea.Cmd {
	return message.Fail(message.Source{Instance: a.instance}, err)
}

// browserOpened clears an earlier failure from the banner.
func (a *App) browserOpened(message.Event[message.Ran[message.OpenBrowser]]) tea.Cmd {
	a.err = nil
	return nil
}

// exported clears an earlier failure from the banner.
func (a *App) exported(message.Event[message.Ran[message.ExportReport]]) tea.Cmd {
	a.err = nil
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
	v := tea.NewView(a.header() + a.top().View().Content + a.banner())
	v.AltScreen = true
	return v
}
