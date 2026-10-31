package tui

import (
	"fmt"
	"slices"

	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// stackScreen is a screen a Nav opens: routed while it's open, drawn while
// it's on top, and closed by a pop.
type stackScreen interface {
	model.SubComponent
	model.Routable
	// crumbs names the screen in the header, over a screen of kind
	// beneath, or over none if it's a tab.
	crumbs(beneath message.Screen) []string
}

// stacked is a screen pushed over the one before it, or over the tab.
type stacked struct {
	kind   message.Screen
	screen stackScreen
}

// screenStack is the screens a Nav has pushed over the tab, top last. A
// screen's routes are registered for exactly as long as it's on the stack.
type screenStack struct {
	router  model.Router
	entries []stacked
}

func newScreenStack(router model.Router) *screenStack {
	return &screenStack{router: router}
}

// push registers s's routes and puts it on top. A kind is open at most
// once, so pushing one that's open is a bug in the caller.
func (st *screenStack) push(kind message.Screen, s stackScreen) error {
	if slices.ContainsFunc(st.entries, func(e stacked) bool { return e.kind == kind }) {
		panic(fmt.Sprintf("tui: %v is already open", kind))
	}
	if err := st.router.Register(s); err != nil {
		return err
	}
	st.entries = append(st.entries, stacked{kind: kind, screen: s})
	return nil
}

// pop deregisters the screen on top.
func (st *screenStack) pop() error {
	n := len(st.entries)
	if n == 0 {
		return fmt.Errorf("tui: no screen is pushed to pop")
	}
	if err := st.router.Deregister(st.entries[n-1].screen); err != nil {
		return err
	}
	st.entries = st.entries[:n-1]
	return nil
}

// replace pops the screen on top and pushes s in its place, in one step.
func (st *screenStack) replace(kind message.Screen, s stackScreen) error {
	if len(st.entries) == 0 {
		return fmt.Errorf("tui: no screen is pushed to replace")
	}
	if err := st.pop(); err != nil {
		return err
	}
	return st.push(kind, s)
}

// open is the screen of kind on the stack, or nil if none is.
func (st *screenStack) open(kind message.Screen) stackScreen {
	for _, e := range st.entries {
		if e.kind == kind {
			return e.screen
		}
	}
	return nil
}

// kinds is the kind of each screen pushed, bottom first.
func (st *screenStack) kinds() []message.Screen {
	kinds := make([]message.Screen, len(st.entries))
	for i, e := range st.entries {
		kinds[i] = e.kind
	}
	return kinds
}

func (st *screenStack) len() int { return len(st.entries) }
