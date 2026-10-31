package tui

import (
	"slices"
	"testing"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/tui/message"
)

// newAppWith is an App without services that opens screens from screens.
func newAppWith(t *testing.T, screens Navigation) *App {
	t.Helper()
	return New(nil, nil, nil, nil, nil, nil, nil, documents.NewStore(t.TempDir()), screens)
}

func TestApp_Nav_ToAScreenMissingFromTheTable_IsReported(t *testing.T) {
	t.Parallel()
	app := newAppWith(t, Navigation{})
	from := message.Source{Instance: app.focused()}

	app, cmd := sendKey(app, message.Push(from, message.ScreenCompanyForm, message.Subject{}, message.None{})())

	if app.onScreen() != tabs[0] {
		t.Errorf("screen = %v, want %v left up", app.onScreen(), tabs[0])
	}
	if cmd == nil {
		t.Fatal("no command, want an Error")
	}
	if got, ok := cmd().(message.Event[message.Error]); !ok || got.Payload.Code != message.ErrFault {
		t.Errorf("command sent %#v, want an Event[Error] with code %v", got, message.ErrFault)
	}
}

func TestApp_Nav_ReplaceOntoAScreenInTheTable_OpensIt(t *testing.T) {
	t.Parallel()
	app := newAppWith(t, Screens)
	app, _ = sendKey(app, message.Push(message.Source{Instance: app.focused()}, message.ScreenCompanyForm, message.Subject{}, message.None{})())
	from := message.Source{Instance: app.focused()}

	app, _ = sendKey(app, message.Replace(from, message.ScreenCompanyEdit, message.Subject{}, message.Company{ID: 1, Name: "Acme"})())

	if got, want := app.path(), []message.Screen{tabs[0], message.ScreenCompanyEdit}; !slices.Equal(got, want) {
		t.Errorf("path = %v, want %v", got, want)
	}
}
