package tui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/tui/message"
)

// opened is the company msg pushes screen about, 0 if it isn't such a push.
func opened(msg tea.Msg, screen message.Screen) int64 {
	nav, ok := msg.(message.Event[message.Nav[message.Company]])
	if !ok || nav.Payload.Op != message.NavPush || nav.Payload.Screen != screen || nav.Payload.Value.ID != nav.Meta.Subject.ID {
		return 0
	}
	return nav.Meta.Subject.ID
}

// pops is whether msg asks App to pop from's screen.
func pops(msg tea.Msg, from message.ScreenInstance) bool {
	nav, ok := msg.(message.Event[message.Nav[message.None]])
	return ok && nav.Payload.Op == message.NavPop && nav.Meta.Source.Instance == from
}

// popsIn is whether any of msgs asks App to pop from's screen.
func popsIn(msgs []tea.Msg, from message.ScreenInstance) bool {
	return slices.ContainsFunc(msgs, func(msg tea.Msg) bool { return pops(msg, from) })
}

// statusPicker is the open status picker, failing the test if none is.
func statusPicker(t *testing.T, app *App) *applicationStatusModel {
	t.Helper()
	m, ok := app.stack.open(message.ScreenApplicationStatus).(*applicationStatusModel)
	if !ok {
		t.Fatal("no status picker is open")
	}
	return m
}

// notesEditor is the open notes editor, failing the test if none is.
func notesEditor(t *testing.T, app *App) *applicationNotesModel {
	t.Helper()
	m, ok := app.stack.open(message.ScreenApplicationNotes).(*applicationNotesModel)
	if !ok {
		t.Fatal("no notes editor is open")
	}
	return m
}
