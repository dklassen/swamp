package message

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

type ScreenInstance int

// Fail reports err as a fault in the program, such as a handler given a
// message it wasn't registered for, which nothing the user does can fix.
func Fail(src Source, err error) tea.Cmd {
	return func() tea.Msg {
		return New(src, Subject{}, OpNone, time.Now().UTC(), Error{Code: ErrFault, Text: err.Error()})
	}
}

// ReportStatus puts s in the banner, or clears it if s is "".
func ReportStatus(src Source, s string) tea.Cmd {
	return func() tea.Msg {
		return New(src, Subject{}, OpNone, time.Now().UTC(), Notify{Status: s})
	}
}
