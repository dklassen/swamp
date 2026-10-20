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

// Handle registers under the type the handler takes, and hands it that
// message as that type.
func TestHandle_RoutesByTheHandlersMessageType(t *testing.T) {
	t.Parallel()
	owner := &component{name: "owner"}
	r := &fakeRouter{owner: owner, routes: map[model.RouteID]*model.Route{}}
	var got message.CompanyDeletedMsg

	err := model.Handle(r, owner, model.AlwaysScope, nil, func(msg message.CompanyDeletedMsg) tea.Cmd { got = msg; return nil })
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	route, ok := r.routes[model.RouteIDOf(message.CompanyDeletedMsg{})]
	if !ok {
		t.Fatalf("Handle registered %v, want a route for message.CompanyDeletedMsg", r.routes)
	}
	route.Handle(message.CompanyDeletedMsg{CompanyID: 7})
	if got.CompanyID != 7 {
		t.Errorf("handler got %+v, want CompanyID 7", got)
	}
}

// The router only calls a route with its own type, so a mismatch is the
// router's fault; it's reported as an ErrMsg, not a panic.
func TestHandle_WrongMessageSendsAnErrMsg(t *testing.T) {
	t.Parallel()

	owner := &component{name: "owner"}
	r := &fakeRouter{owner: owner, routes: map[model.RouteID]*model.Route{}}
	if err := model.Handle(r, owner, model.AlwaysScope, nil, func(message.CompanyDeletedMsg) tea.Cmd { return nil }); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	cmd := r.routes[model.RouteIDOf(message.CompanyDeletedMsg{})].Handle(message.CompanyCreatedMsg{})

	if cmd == nil {
		t.Fatal("handler given the wrong message returned no command, want one sending message.ErrMsg")
	}
	if got, ok := cmd().(message.ErrMsg); !ok || got.Err == nil {
		t.Errorf("handler given the wrong message sent %#v, want a message.ErrMsg with an Err", got)
	}
}

// A route is a concrete type, and an interface's zero value has none, so
// a handler that takes an interface would never be called.
func TestHandle_RefusesAnInterfaceMessageType(t *testing.T) {
	t.Parallel()
	m := &component{name: "owner"}
	r := &fakeRouter{owner: m, routes: map[model.RouteID]*model.Route{}}

	err := model.Handle(r, m, model.FocusScope, func() bool { return true }, func(tea.KeyMsg) tea.Cmd { return nil })

	if err == nil {
		t.Error("Handle with a tea.KeyMsg handler returned no error, want one: an interface can't be routed")
	}
	if len(r.routes) != 0 {
		t.Errorf("Handle registered %v, want nothing", r.routes)
	}
}
