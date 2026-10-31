package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// Table is what one Nav payload opens, as a route on an App.
type Table func(*App) *model.Route

// Opens is the Table for Nav[T]: each kind in table opens the screen its
// function builds from the Nav's value.
func Opens[T message.Body](table map[message.Screen]func(T) stackScreen) Table {
	return func(a *App) *model.Route {
		return model.Always(navTo(a, table))
	}
}

// navTo opens what table has for each Nav[T] from the screen on top. Any
// kind in table opens on a push or a replace.
func navTo[T message.Body](a *App, table map[message.Screen]func(T) stackScreen) func(message.Event[message.Nav[T]]) tea.Cmd {
	return func(e message.Event[message.Nav[T]]) tea.Cmd {
		if !a.onTop(e.Meta) {
			return nil
		}
		nav := e.Payload
		if nav.Op == message.NavPop {
			return a.pop()
		}
		open, ok := table[nav.Screen]
		switch {
		case !ok:
			return a.unknownNav(e)
		case nav.Op == message.NavPush:
			return a.push(nav.Screen, open(nav.Value))
		case nav.Op == message.NavReplace:
			return a.replace(nav.Screen, open(nav.Value))
		}
		return a.unknownNav(e)
	}
}

// push opens s over the screen on top: routes, then Init, so nothing Init
// starts can report before s can hear it.
func (a *App) push(kind message.Screen, s stackScreen) tea.Cmd {
	if err := a.router.Register(s); err != nil {
		return a.fail(err)
	}
	a.stack.Push(kind, s)
	return s.Init()
}

// pop closes the screen on top and stops its routes.
func (a *App) pop() tea.Cmd {
	s, err := a.stack.Pop()
	if err != nil {
		return a.fail(err)
	}
	if err := a.router.Deregister(s); err != nil {
		return a.fail(err)
	}
	return nil
}

// replace pops the screen on top and pushes s in its place, in one step.
func (a *App) replace(kind message.Screen, s stackScreen) tea.Cmd {
	if cmd := a.pop(); cmd != nil {
		return cmd
	}
	return a.push(kind, s)
}

func (a *App) fail(err error) tea.Cmd {
	return message.Fail(message.Source{Instance: a.instance}, err)
}

// onTop is whether meta is from the screen on top. A Nav from any other,
// such as a second esc typed before the first one's close arrived, is
// dropped.
func (a *App) onTop(meta message.Meta) bool {
	return meta.Source.Instance != 0 && meta.Source.Instance == a.focused()
}

// unknownNav reports a Nav for a screen none of the tables opens that way.
func (a *App) unknownNav(nav tea.Msg) tea.Cmd {
	return a.fail(fmt.Errorf("tui: no screen opens on %+v", nav))
}
