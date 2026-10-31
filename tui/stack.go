package tui

import (
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// stackScreen is a screen a Nav opens: routed while it's open, drawn while
// it's on top, and closed by a pop.
type stackScreen interface {
	model.SubComponent
	model.Routable
}

// Table is what one Nav payload opens, as a route on a Stack.
type Table func(*Stack) *model.Route

// Opens is the Table for Nav[T]: each kind in table opens the screen its
// function builds from the Nav's value.
func Opens[T message.Body](table map[message.Screen]func(T) stackScreen) Table {
	return func(st *Stack) *model.Route {
		return model.Always(navTo(st, table))
	}
}

// stacked is a screen pushed over the one before it, or over the tab.
type stacked struct {
	kind   message.Screen
	screen stackScreen
}

// Stack is the screens a Nav has pushed over the tab, top last. It opens
// what its tables have for each Nav from the screen in focus; any kind in
// a table opens on a push or a replace. Each screen is new when it opens
// and gone when it closes, so whoever routes the stack's screens takes
// them from Moved.
type Stack struct {
	instance message.ScreenInstance
	entries  []stacked
	routes   []*model.Route
	// focused is the screen in focus, the only one whose Nav is taken.
	focused message.ScreenInstance
	// closed and opened are the screens moved since Moved was last called.
	closed, opened []stackScreen
}

// NewStack is an empty stack that opens what tables have.
func NewStack(tables ...Table) *Stack {
	st := &Stack{instance: message.NewScreenInstance()}
	for _, table := range tables {
		st.routes = append(st.routes, table(st))
	}
	st.routes = append(st.routes, model.Always(st.focusChanged))
	return st
}

func (st *Stack) Instance() message.ScreenInstance { return st.instance }

func (st *Stack) Routes() []*model.Route { return st.routes }

func (st *Stack) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
	st.focused = e.Payload.Value.Focused
	return nil
}

// Moved is the screens closed and opened since it was last called.
func (st *Stack) Moved() (closed, opened []stackScreen) {
	closed, opened = st.closed, st.opened
	st.closed, st.opened = nil, nil
	return closed, opened
}

// Top is the screen on top, or nil if none is pushed.
func (st *Stack) Top() stackScreen {
	if len(st.entries) == 0 {
		return nil
	}
	return st.entries[len(st.entries)-1].screen
}

// Screen is the screen of kind on the stack, or nil if none is.
func (st *Stack) Screen(kind message.Screen) stackScreen {
	for _, e := range st.entries {
		if e.kind == kind {
			return e.screen
		}
	}
	return nil
}

// Kinds is the kind of each screen pushed, bottom first.
func (st *Stack) Kinds() []message.Screen {
	kinds := make([]message.Screen, len(st.entries))
	for i, e := range st.entries {
		kinds[i] = e.kind
	}
	return kinds
}

// push puts s on top. A kind is open at most once, so pushing one that's
// open is a bug in the caller.
func (st *Stack) push(kind message.Screen, s stackScreen) {
	if slices.ContainsFunc(st.entries, func(e stacked) bool { return e.kind == kind }) {
		panic(fmt.Sprintf("tui: %v is already open", kind))
	}
	st.entries = append(st.entries, stacked{kind: kind, screen: s})
	st.opened = append(st.opened, s)
}

// pop closes the screen on top.
func (st *Stack) pop() error {
	n := len(st.entries)
	if n == 0 {
		return fmt.Errorf("tui: no screen is pushed to pop")
	}
	st.closed = append(st.closed, st.entries[n-1].screen)
	st.entries = st.entries[:n-1]
	return nil
}

// replace pops the screen on top and pushes s in its place, in one step.
func (st *Stack) replace(kind message.Screen, s stackScreen) error {
	if err := st.pop(); err != nil {
		return fmt.Errorf("tui: no screen is pushed to replace")
	}
	st.push(kind, s)
	return nil
}

func (st *Stack) fail(err error) tea.Cmd {
	return message.Fail(message.Source{Instance: st.instance}, err)
}

// onTop is whether meta is from the screen in focus. A Nav from any other,
// such as a second esc typed before the first one's close arrived, is
// dropped.
func (st *Stack) onTop(meta message.Meta) bool {
	return meta.Source.Instance != 0 && meta.Source.Instance == st.focused
}

// navTo opens what table has for each Nav[T] from the screen in focus.
func navTo[T message.Body](st *Stack, table map[message.Screen]func(T) stackScreen) func(message.Event[message.Nav[T]]) tea.Cmd {
	return func(e message.Event[message.Nav[T]]) tea.Cmd {
		if !st.onTop(e.Meta) {
			return nil
		}
		nav := e.Payload
		if nav.Op == message.NavPop {
			if err := st.pop(); err != nil {
				return st.fail(err)
			}
			return nil
		}
		open, ok := table[nav.Screen]
		switch {
		case !ok:
			return st.unknownNav(e)
		case nav.Op == message.NavPush:
			st.push(nav.Screen, open(nav.Value))
			return nil
		case nav.Op == message.NavReplace:
			if err := st.replace(nav.Screen, open(nav.Value)); err != nil {
				return st.fail(err)
			}
			return nil
		}
		return st.unknownNav(e)
	}
}

// unknownNav reports a Nav for a screen none of the tables opens that way.
func (st *Stack) unknownNav(nav tea.Msg) tea.Cmd {
	return st.fail(fmt.Errorf("tui: no screen opens on %+v", nav))
}
