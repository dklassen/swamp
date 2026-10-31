package tui

import (
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

// Navigation is the tabs, one of them up. Each keeps its instance, and
// hears its routes, whether it's up or not.
type Navigation struct {
	instance message.ScreenInstance
	tabs     []Tab
	// tab is the kind of the tab up.
	tab message.Screen
}

var (
	_ model.SubComponent = (*Navigation)(nil)
	_ model.Routable     = (*Navigation)(nil)
)

// NewNavigation is tabs, with the first up.
func NewNavigation(tabs []Tab) *Navigation {
	return &Navigation{
		instance: message.NewScreenInstance(),
		tabs:     tabs,
		tab:      tabs[0].Kind,
	}
}

func (n *Navigation) Instance() message.ScreenInstance { return n.instance }

// Routes are every tab's.
func (n *Navigation) Routes() []*model.Route {
	var routes []*model.Route
	for _, tab := range n.tabs {
		routes = append(routes, tab.Screen.Routes()...)
	}
	return routes
}

// Init starts each tab.
func (n *Navigation) Init() tea.Cmd {
	cmds := make([]tea.Cmd, len(n.tabs))
	for i, tab := range n.tabs {
		cmds[i] = tab.Screen.Init()
	}
	return tea.Batch(cmds...)
}

func (n *Navigation) Update(tea.Msg) (tea.Model, tea.Cmd) { return n, nil }

// View is the tab up's.
func (n *Navigation) View() tea.View {
	return n.Screen(n.tab).View()
}

// Focused is the instance of the tab up.
func (n *Navigation) Focused() message.ScreenInstance {
	return n.Screen(n.tab).Instance()
}

// NextTab puts the next tab up.
func (n *Navigation) NextTab() {
	i := slices.IndexFunc(n.tabs, func(t Tab) bool { return t.Kind == n.tab })
	n.tab = n.tabs[(i+1)%len(n.tabs)].Kind
}

// Tab is the kind of the tab up.
func (n *Navigation) Tab() message.Screen { return n.tab }

// Screen is the tab of kind, or nil if none is.
func (n *Navigation) Screen(kind message.Screen) stackScreen {
	for _, tab := range n.tabs {
		if tab.Kind == kind {
			return tab.Screen
		}
	}
	return nil
}
