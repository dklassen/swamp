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
}

// stacked is a screen pushed over the one under named; popping it puts
// under back.
type stacked struct {
	kind   message.Screen
	screen stackScreen
	under  screen
}

// screenStack is the screens a Nav has pushed, top last. A screen's routes
// are registered for exactly as long as it's on the stack.
type screenStack struct {
	router  model.Router
	entries []stacked
}

func newScreenStack(router model.Router) *screenStack {
	return &screenStack{router: router}
}

// push registers s's routes and puts it on top, over under. A kind is open
// at most once, so pushing one that's open is a bug in the caller.
func (st *screenStack) push(kind message.Screen, s stackScreen, under screen) error {
	if slices.ContainsFunc(st.entries, func(e stacked) bool { return e.kind == kind }) {
		panic(fmt.Sprintf("tui: %v is already open", kind))
	}
	if err := st.router.Register(s); err != nil {
		return err
	}
	st.entries = append(st.entries, stacked{kind: kind, screen: s, under: under})
	return nil
}

// pop deregisters the screen on top and returns the one it was pushed over.
func (st *screenStack) pop() (screen, error) {
	n := len(st.entries)
	if n == 0 {
		return 0, fmt.Errorf("tui: no screen is pushed to pop")
	}
	top := st.entries[n-1]
	if err := st.router.Deregister(top.screen); err != nil {
		return 0, err
	}
	st.entries = st.entries[:n-1]
	return top.under, nil
}

// top is the screen pushed last. Only call it while len is above 0.
func (st *screenStack) top() stackScreen {
	return st.entries[len(st.entries)-1].screen
}

func (st *screenStack) len() int { return len(st.entries) }
