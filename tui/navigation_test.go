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

// newFakesApp is an App whose None table opens form and edit as the
// company form and company edit.
func newFakesApp(t *testing.T, form, edit *stackedFake) *App {
	t.Helper()
	router := model.NewMsgRouter()
	screens := NewScreens(nil, nil, nil, nil, nil, nil, documents.NewStore(t.TempDir()))
	fakes := Opens(map[message.Screen]func(message.None) stackScreen{
		message.ScreenCompanyForm: func(message.None) stackScreen { return form },
		message.ScreenCompanyEdit: func(message.None) stackScreen { return edit },
	})
	return New(router, nil, NewNavigation(screens.Tabs()), NewStack(), fakes)
}

func TestNavigation_Routes_AreItsTabs(t *testing.T) {
	t.Parallel()
	up, other := newStackedFake(), newStackedFake()
	router := model.NewMsgRouter()
	nav := NewNavigation([]Tab{{Kind: message.ScreenActiveApplications, Screen: up}, {Kind: message.ScreenCompanyList, Screen: other}})
	if err := router.Register(nav); err != nil {
		t.Fatalf("register: %v", err)
	}

	router.Dispatch(stackPing{})

	if up.heard != 1 || other.heard != 1 {
		t.Errorf("pings heard: tab up %d, the other %d; want 1 and 1", up.heard, other.heard)
	}
}

func TestApp_Nav_Push_RoutesToTheScreen(t *testing.T) {
	t.Parallel()
	form := newStackedFake()
	app := newFakesApp(t, form, newStackedFake())

	app, _ = sendKey(app, message.Push(message.Source{Instance: app.focused()}, message.ScreenCompanyForm, message.Subject{}, message.None{})())
	_, _ = sendKey(app, stackPing{})

	if form.heard != 1 {
		t.Errorf("pings the pushed screen heard = %d, want 1", form.heard)
	}
}

func TestApp_Nav_Pop_StopsTheScreensRoutes(t *testing.T) {
	t.Parallel()
	form := newStackedFake()
	app := newFakesApp(t, form, newStackedFake())
	app, _ = sendKey(app, message.Push(message.Source{Instance: app.focused()}, message.ScreenCompanyForm, message.Subject{}, message.None{})())

	app, _ = sendKey(app, message.Pop(message.Source{Instance: form.instance})())
	_, _ = sendKey(app, stackPing{})

	if form.heard != 0 {
		t.Errorf("pings the popped screen heard = %d, want 0", form.heard)
	}
}

func TestApp_Nav_Replace_SwapsWhichScreenHears(t *testing.T) {
	t.Parallel()
	form, edit := newStackedFake(), newStackedFake()
	app := newFakesApp(t, form, edit)
	app, _ = sendKey(app, message.Push(message.Source{Instance: app.focused()}, message.ScreenCompanyForm, message.Subject{}, message.None{})())

	app, _ = sendKey(app, message.Replace(message.Source{Instance: form.instance}, message.ScreenCompanyEdit, message.Subject{}, message.None{})())
	_, _ = sendKey(app, stackPing{})

	if form.heard != 0 || edit.heard != 1 {
		t.Errorf("pings heard after the replace: replaced %d, replacement %d; want 0 and 1", form.heard, edit.heard)
	}
}

func TestApp_Nav_ReplaceWithNothingPushed_IsReported(t *testing.T) {
	t.Parallel()
	form := newStackedFake()
	app := newFakesApp(t, form, newStackedFake())
	home := app.onScreen()

	app, cmd := sendKey(app, message.Replace(message.Source{Instance: app.focused()}, message.ScreenCompanyForm, message.Subject{}, message.None{})())

	if app.onScreen() != home {
		t.Errorf("screen = %v, want %v left up", app.onScreen(), home)
	}
	if cmd == nil {
		t.Fatal("no command, want an Error")
	}
	if got, ok := cmd().(message.Event[message.Error]); !ok || got.Payload.Code != message.ErrFault {
		t.Errorf("command sent %#v, want an Event[Error] with code %v", got, message.ErrFault)
	}
}

func TestApp_Nav_ToAScreenMissingFromTheTables_IsReported(t *testing.T) {
	t.Parallel()
	router := model.NewMsgRouter()
	screens := NewScreens(nil, nil, nil, nil, nil, nil, documents.NewStore(t.TempDir()))
	noForm := Opens(map[message.Screen]func(message.None) stackScreen{})
	app := New(router, nil, NewNavigation(screens.Tabs()), NewStack(), noForm)
	home := app.onScreen()
	from := message.Source{Instance: app.focused()}

	app, cmd := sendKey(app, message.Push(from, message.ScreenCompanyForm, message.Subject{}, message.None{})())

	if app.onScreen() != home {
		t.Errorf("screen = %v, want %v left up", app.onScreen(), home)
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
	home := app.onScreen()
	app, _ = sendKey(app, message.Push(message.Source{Instance: app.focused()}, message.ScreenCompanyForm, message.Subject{}, message.None{})())
	from := message.Source{Instance: app.focused()}

	app, _ = sendKey(app, message.Replace(from, message.ScreenCompanyEdit, message.Subject{}, message.Company{ID: 1, Name: "Acme"})())

	if got, want := app.path(), []message.Screen{home, message.ScreenCompanyEdit}; !slices.Equal(got, want) {
		t.Errorf("path = %v, want %v", got, want)
	}
}
