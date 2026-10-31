package tui

import (
	"slices"
	"testing"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// newBareApp is an App without services, with the Screens it opens.
func newBareApp(t *testing.T) (*App, *Screens) {
	t.Helper()
	return newAppWithScreensOn(testutil.TestApp{}, nil, documents.NewStore(t.TempDir()))
}

func TestApp_Nav_ToAScreenMissingFromTheTables_IsReported(t *testing.T) {
	t.Parallel()
	router := model.NewMsgRouter()
	screens := NewScreens(nil, nil, nil, nil, nil, nil, documents.NewStore(t.TempDir()))
	noForm := Opens(map[message.Screen]func(message.None) stackScreen{})
	app := New(router, nil, nil, nil, nil, nil, nil, nil, NewNavigation(router, screens.Tabs(), noForm))
	home := app.nav.onScreen()
	from := message.Source{Instance: app.nav.Focused()}

	app, cmd := sendKey(app, message.Push(from, message.ScreenCompanyForm, message.Subject{}, message.None{})())

	if app.nav.onScreen() != home {
		t.Errorf("screen = %v, want %v left up", app.nav.onScreen(), home)
	}
	if cmd == nil {
		t.Fatal("no command, want an Error")
	}
	if got, ok := cmd().(message.Event[message.Error]); !ok || got.Payload.Code != message.ErrFault {
		t.Errorf("command sent %#v, want an Event[Error] with code %v", got, message.ErrFault)
	}
}

func TestApp_Nav_ReplaceOntoAScreenInTheTables_OpensIt(t *testing.T) {
	t.Parallel()
	app, _ := newBareApp(t)
	home := app.nav.onScreen()
	app, _ = sendKey(app, message.Push(message.Source{Instance: app.nav.Focused()}, message.ScreenCompanyForm, message.Subject{}, message.None{})())
	from := message.Source{Instance: app.nav.Focused()}

	app, _ = sendKey(app, message.Replace(from, message.ScreenCompanyEdit, message.Subject{}, message.Company{ID: 1, Name: "Acme"})())

	if got, want := app.nav.Path(), []message.Screen{home, message.ScreenCompanyEdit}; !slices.Equal(got, want) {
		t.Errorf("path = %v, want %v", got, want)
	}
}
