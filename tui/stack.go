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

// stacked is a screen pushed over the one before it, or over the tab.
type stacked struct {
	kind   message.Screen
	screen stackScreen
}

// Stack is the screens pushed over the tab, top last.
type Stack struct {
	entries []stacked
}

func NewStack() *Stack { return &Stack{} }

// Push puts s on top. A kind is open at most once, so pushing one that's
// open is a bug in the caller.
func (st *Stack) Push(kind message.Screen, s stackScreen) {
	if slices.ContainsFunc(st.entries, func(e stacked) bool { return e.kind == kind }) {
		panic(fmt.Sprintf("tui: %v is already open", kind))
	}
	st.entries = append(st.entries, stacked{kind: kind, screen: s})
}

// Pop takes the screen on top off, and is it.
func (st *Stack) Pop() (stackScreen, error) {
	n := len(st.entries)
	if n == 0 {
		return nil, fmt.Errorf("tui: no screen is pushed to pop")
	}
	top := st.entries[n-1].screen
	st.entries = st.entries[:n-1]
	return top, nil
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
