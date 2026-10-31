package tui

import (
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// Tab is a screen that's always open, one of those tab goes through.
type Tab struct {
	Kind   message.Screen
	Screen stackScreen
}

// Table is what one Nav payload opens, as a route on a Navigation.
type Table func(*Navigation) *model.Route

// Opens is the Table for Nav[T]: each kind in table opens the screen its
// function builds from the Nav's value.
func Opens[T message.Body](table map[message.Screen]func(T) stackScreen) Table {
	return func(n *Navigation) *model.Route {
		return model.Always(navTo(n, table))
	}
}

// Navigation is the screens open: the tabs, and the stack pushed over the
// tab up. It opens what its tables have for each Nav from the screen on
// top. Any kind in a table opens on a push or a replace.
type Navigation struct {
	instance message.ScreenInstance
	tabs     []Tab
	// tab is the kind of the tab up, which the stack's screens are pushed
	// over.
	tab    message.Screen
	stack  *screenStack
	routes []*model.Route
}

// NewNavigation is tabs, registered with router, with the first up.
// Screens pushed over them are registered while they're open.
func NewNavigation(router model.Router, tabs []Tab, tables ...Table) *Navigation {
	n := &Navigation{
		instance: message.NewScreenInstance(),
		tabs:     tabs,
		tab:      tabs[0].Kind,
		stack:    newScreenStack(router),
	}
	for _, tab := range tabs {
		if err := router.Register(tab.Screen); err != nil {
			panic(err)
		}
	}
	for _, table := range tables {
		n.routes = append(n.routes, table(n))
	}
	return n
}

func (n *Navigation) Instance() message.ScreenInstance { return n.instance }

func (n *Navigation) Routes() []*model.Route { return n.routes }

// Init starts each tab.
func (n *Navigation) Init() tea.Cmd {
	cmds := make([]tea.Cmd, len(n.tabs))
	for i, tab := range n.tabs {
		cmds[i] = tab.Screen.Init()
	}
	return tea.Batch(cmds...)
}

// View is the screen on top's.
func (n *Navigation) View() tea.View {
	return n.screenOf(n.onScreen()).View()
}

// Focused is the instance of the screen on top.
func (n *Navigation) Focused() message.ScreenInstance {
	return n.screenOf(n.onScreen()).Instance()
}

// NextTab puts the next tab up, unless a screen is pushed over this one.
func (n *Navigation) NextTab() bool {
	if n.stack.len() > 0 {
		return false
	}
	i := slices.IndexFunc(n.tabs, func(t Tab) bool { return t.Kind == n.tab })
	n.tab = n.tabs[(i+1)%len(n.tabs)].Kind
	return true
}

// Path is the tab up, then each screen pushed over it: the way esc goes
// back, reversed.
func (n *Navigation) Path() []message.Screen {
	return append([]message.Screen{n.tab}, n.stack.kinds()...)
}

// Screen is the open screen of kind, or nil if none is.
func (n *Navigation) Screen(kind message.Screen) stackScreen {
	return n.screenOf(kind)
}

// onScreen is the kind of the screen on top.
func (n *Navigation) onScreen() message.Screen {
	path := n.Path()
	return path[len(path)-1]
}

func (n *Navigation) screenOf(kind message.Screen) stackScreen {
	for _, tab := range n.tabs {
		if tab.Kind == kind {
			return tab.Screen
		}
	}
	return n.stack.open(kind)
}

// push opens s over the screen up: routes, then Init, so nothing Init
// starts can report before s can hear it.
func (n *Navigation) push(kind message.Screen, s stackScreen) tea.Cmd {
	if err := n.stack.push(kind, s); err != nil {
		return n.fail(err)
	}
	return s.Init()
}

func (n *Navigation) replace(kind message.Screen, s stackScreen) tea.Cmd {
	if err := n.stack.replace(kind, s); err != nil {
		return n.fail(err)
	}
	return s.Init()
}

func (n *Navigation) pop() tea.Cmd {
	if err := n.stack.pop(); err != nil {
		return n.fail(err)
	}
	return nil
}

func (n *Navigation) fail(err error) tea.Cmd {
	return message.Fail(message.Source{Instance: n.instance}, err)
}

// onTop is whether meta is from the screen on top. A Nav from any other,
// such as a second esc typed before the first one's close arrived, is
// dropped.
func (n *Navigation) onTop(meta message.Meta) bool {
	return meta.Source.Instance != 0 && meta.Source.Instance == n.Focused()
}

// navTo opens what table has for each Nav[T] from the screen on top.
func navTo[T message.Body](n *Navigation, table map[message.Screen]func(T) stackScreen) func(message.Event[message.Nav[T]]) tea.Cmd {
	return func(e message.Event[message.Nav[T]]) tea.Cmd {
		if !n.onTop(e.Meta) {
			return nil
		}
		nav := e.Payload
		if nav.Op == message.NavPop {
			return n.pop()
		}
		open, ok := table[nav.Screen]
		switch {
		case !ok:
			return n.unknownNav(e)
		case nav.Op == message.NavPush:
			return n.push(nav.Screen, open(nav.Value))
		case nav.Op == message.NavReplace:
			return n.replace(nav.Screen, open(nav.Value))
		}
		return n.unknownNav(e)
	}
}

// unknownNav reports a Nav for a screen none of the tables opens that way.
func (n *Navigation) unknownNav(nav tea.Msg) tea.Cmd {
	return n.fail(fmt.Errorf("tui: no screen opens on %+v", nav))
}
