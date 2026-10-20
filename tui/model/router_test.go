package model_test

import (
	"errors"
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

func TestRouteID_String_IsTheMessagesTypeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  tea.Msg
	}{
		{name: "value", msg: message.CompanyCreatedMsg{}},
		{name: "pointer", msg: &message.CompanyCreatedMsg{}},
		{name: "bubble tea message", msg: tea.WindowSizeMsg{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got, want := model.RouteIDOf(tt.msg).String(), fmt.Sprintf("%T", tt.msg); got != want {
				t.Errorf("RouteIDOf(%T).String() = %q, want %q", tt.msg, got, want)
			}
		})
	}
}

// The router keys its table by RouteID, so two messages share handlers
// exactly when their IDs are equal.
func TestRouteIDOf_EqualExactlyForTheSameConcreteType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b tea.Msg
		want bool
	}{
		{name: "same type, different values", a: message.CompanyCreatedMsg{}, b: message.CompanyCreatedMsg{Err: errors.New("boom")}, want: true},
		{name: "different types", a: message.CompanyCreatedMsg{}, b: message.CompanyDeletedMsg{}, want: false},
		{name: "a value and a pointer to it", a: message.CompanyCreatedMsg{}, b: &message.CompanyCreatedMsg{}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := model.RouteIDOf(tt.a) == model.RouteIDOf(tt.b); got != tt.want {
				t.Errorf("RouteIDOf(%T) == RouteIDOf(%T) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// component is a SubComponent that does nothing, to own routes.
type component struct{ name string }

func (c *component) Init() tea.Cmd                       { return nil }
func (c *component) Update(tea.Msg) (tea.Model, tea.Cmd) { return c, nil }
func (c *component) View() tea.View                      { return tea.NewView(c.name) }
func (c *component) RegisterRoutes(model.Router) error {
	return nil
}

// Always routes the type the handler takes, and hands it that message as
// that type.
func TestAlways_RoutesByTheHandlersMessageType(t *testing.T) {
	t.Parallel()
	var got message.CompanyDeletedMsg

	route := model.Always(func(msg message.CompanyDeletedMsg) tea.Cmd { got = msg; return nil })

	if want := model.RouteIDOf(message.CompanyDeletedMsg{}); route.ID() != want {
		t.Fatalf("route.ID() = %v, want %v", route.ID(), want)
	}
	route.Handle(message.CompanyDeletedMsg{CompanyID: 7})
	if got.CompanyID != 7 {
		t.Errorf("handler got %+v, want CompanyID 7", got)
	}
}

// The router only calls a route with its own type, so a mismatch is the
// router's fault; it's reported as an ErrMsg, not a panic.
func TestRoute_WrongMessageSendsAnErrMsg(t *testing.T) {
	t.Parallel()
	route := model.Always(func(message.CompanyDeletedMsg) tea.Cmd { return nil })

	cmd := route.Handle(message.CompanyCreatedMsg{})

	if cmd == nil {
		t.Fatal("handler given the wrong message returned no command, want one sending message.ErrMsg")
	}
	if got, ok := cmd().(message.ErrMsg); !ok || got.Err == nil {
		t.Errorf("handler given the wrong message sent %#v, want a message.ErrMsg with an Err", got)
	}
}

// A route is a concrete type, and an interface's zero value has none, so
// a handler that takes an interface would never be called.
func TestMsgRouter_Register_RefusesAnInterfaceMessageType(t *testing.T) {
	t.Parallel()

	err := model.NewMsgRouter().Register(model.Always(func(tea.KeyMsg) tea.Cmd { return nil }))

	if err == nil {
		t.Error("Register of a tea.KeyMsg handler returned no error, want one: an interface can't be routed")
	}
}

// focusable is a component whose focus the test sets.
type focusable struct {
	component
	focused bool
}

func (f *focusable) Focused() bool { return f.focused }

// An Always route gets every message of its type; a WhenFocused one only
// while its owner is in focus.
func TestMsgRouter_Dispatch_DeliversByScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		route   func(*focusable, func(message.CompanyDeletedMsg) tea.Cmd) *model.Route
		focused bool
		want    int
	}{
		{name: "always, in focus", route: always, focused: true, want: 1},
		{name: "always, out of focus", route: always, focused: false, want: 1},
		{name: "when focused, in focus", route: whenFocused, focused: true, want: 1},
		{name: "when focused, out of focus", route: whenFocused, focused: false, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			owner := &focusable{component: component{name: "owner"}, focused: tt.focused}
			r := model.NewMsgRouter()
			got := 0
			if err := r.Register(tt.route(owner, func(message.CompanyDeletedMsg) tea.Cmd { got++; return nil })); err != nil {
				t.Fatalf("Register: %v", err)
			}

			r.Dispatch(message.CompanyDeletedMsg{})

			if got != tt.want {
				t.Errorf("deliveries = %d, want %d", got, tt.want)
			}
		})
	}
}

func always(_ *focusable, h func(message.CompanyDeletedMsg) tea.Cmd) *model.Route {
	return model.Always(h)
}

func whenFocused(owner *focusable, h func(message.CompanyDeletedMsg) tea.Cmd) *model.Route {
	return model.WhenFocused(owner, h)
}

// A Route made without Always or WhenFocused has no type or handler.
func TestMsgRouter_Register_RefusesAZeroRoute(t *testing.T) {
	t.Parallel()

	if err := model.NewMsgRouter().Register(&model.Route{}); err == nil {
		t.Error("Register(&model.Route{}) returned no error, want one: it has no type or handler")
	}
}

func TestMsgRouter_Register_RefusesANilRoute(t *testing.T) {
	t.Parallel()

	if err := model.NewMsgRouter().Register(nil); err == nil {
		t.Error("Register(nil) returned no error, want one")
	}
}
