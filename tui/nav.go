package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/tui/message"
)

// openScreen asks App to push screen, about subject, showing v.
func openScreen[T message.Body](src message.Source, screen message.Screen, subject message.Subject, v T) tea.Cmd {
	return func() tea.Msg {
		return message.New(src, subject, message.OpNone, time.Now().UTC(), message.Nav[T]{Op: message.NavPush, Screen: screen, Value: v})
	}
}

// closeScreen asks App to pop src's screen.
func closeScreen(src message.Source) tea.Cmd {
	return func() tea.Msg {
		return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), message.Nav[message.None]{Op: message.NavPop})
	}
}
