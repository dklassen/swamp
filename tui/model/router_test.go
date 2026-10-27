package model_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

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

// component is a SubComponent that does nothing, to own routes. It draws
// its instance, as a model does, unless the test sets one.
type component struct {
	name     string
	routes   []*model.Route
	instance message.ScreenInstance
}

func (c *component) Instance() message.ScreenInstance {
	if c.instance == 0 {
		c.instance = message.NewScreenInstance()
	}
	return c.instance
}

func (c *component) Init() tea.Cmd                       { return nil }
func (c *component) Update(tea.Msg) (tea.Model, tea.Cmd) { return c, nil }
func (c *component) View() tea.View                      { return tea.NewView(c.name) }
func (c *component) Routes() []*model.Route              { return c.routes }

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
// router's fault; it's reported as a fault, not a panic.
func TestRoute_WrongMessageSendsAFault(t *testing.T) {
	t.Parallel()
	route := model.Always(func(message.CompanyDeletedMsg) tea.Cmd { return nil })

	cmd := route.Handle(message.CompanyCreatedMsg{})

	if cmd == nil {
		t.Fatal("handler given the wrong message returned no command, want one sending a fault")
	}
	if got, ok := cmd().(message.Event[message.Error]); !ok || got.Payload.Code != message.ErrFault || got.Payload.Text == "" {
		t.Errorf("handler given the wrong message sent %#v, want an Event[Error] with code %v and a Text", got, message.ErrFault)
	}
}

// A route is a concrete type, and an interface's zero value has none, so
// a handler that takes an interface would never be called.
func TestMsgRouter_Register_RefusesAnInterfaceMessageType(t *testing.T) {
	t.Parallel()

	owner := &component{routes: []*model.Route{model.Always(func(tea.KeyMsg) tea.Cmd { return nil })}}

	err := model.NewMsgRouter().Register(owner)

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
			owner.routes = []*model.Route{tt.route(owner, func(message.CompanyDeletedMsg) tea.Cmd { got++; return nil })}
			if err := r.Register(owner); err != nil {
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

	owner := &component{routes: []*model.Route{{}}}

	if err := model.NewMsgRouter().Register(owner); err == nil {
		t.Error("Register of an owner with &model.Route{} returned no error, want one: it has no type or handler")
	}
}

func TestMsgRouter_Register_RefusesANilRoute(t *testing.T) {
	t.Parallel()
	owner := &component{routes: []*model.Route{nil}}

	if err := model.NewMsgRouter().Register(owner); err == nil {
		t.Error("Register of an owner with a nil Route returned no error, want one")
	}
}

func TestMsgRouter_Register_RefusesANilOwner(t *testing.T) {
	t.Parallel()

	if err := model.NewMsgRouter().Register(nil); err == nil {
		t.Error("Register(nil) returned no error, want one")
	}
}

// unnamed is a component that never drew an instance.
type unnamed struct{ component }

func (*unnamed) Instance() message.ScreenInstance { return 0 }

// 0 is an event's "no instance", so an owner with it couldn't be named.
func TestMsgRouter_Register_RefusesInstanceZero(t *testing.T) {
	t.Parallel()
	owner := &unnamed{component{routes: []*model.Route{model.Always(func(message.CompanyDeletedMsg) tea.Cmd { return nil })}}}

	if err := model.NewMsgRouter().Register(owner); err == nil {
		t.Error("Register of an owner with instance 0 returned no error, want one")
	}
}

// An event names its model by instance, so two models with one number, or
// a new model with a closed one's, would each hear the other's events.
func TestMsgRouter_Register_RefusesAnInstanceItHasSeen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		deregister bool
	}{
		{name: "still registered", deregister: false},
		{name: "deregistered since", deregister: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := model.NewMsgRouter()
			first := &component{name: "first", instance: 5, routes: []*model.Route{model.Always(func(message.CompanyDeletedMsg) tea.Cmd { return nil })}}
			if err := r.Register(first); err != nil {
				t.Fatalf("Register first: %v", err)
			}
			if tt.deregister {
				if err := r.Deregister(first); err != nil {
					t.Fatalf("Deregister first: %v", err)
				}
			}
			got := 0
			second := &component{name: "second", instance: 5, routes: []*model.Route{model.Always(func(message.CompanyDeletedMsg) tea.Cmd { got++; return nil })}}

			err := r.Register(second)

			if err == nil || !strings.Contains(err.Error(), "5") {
				t.Errorf("Register of a second owner with instance 5 = %v, want an error naming the instance", err)
			}
			r.Dispatch(message.CompanyDeletedMsg{})
			if got != 0 {
				t.Errorf("deliveries to the refused owner = %d, want 0", got)
			}
		})
	}
}

// A deregistered owner's routes get no more messages.
func TestMsgRouter_Deregister_StopsTheOwnersRoutes(t *testing.T) {
	t.Parallel()
	got := 0
	owner := &component{routes: []*model.Route{model.Always(func(message.CompanyDeletedMsg) tea.Cmd { got++; return nil })}}
	r := model.NewMsgRouter()
	if err := r.Register(owner); err != nil {
		t.Fatalf("Register: %v", err)
	}
	r.Dispatch(message.CompanyDeletedMsg{})
	if got != 1 {
		t.Fatalf("deliveries before Deregister = %d, want 1", got)
	}

	if err := r.Deregister(owner); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	r.Dispatch(message.CompanyDeletedMsg{})

	if got != 1 {
		t.Errorf("deliveries after Deregister = %d, want still 1", got)
	}
}

// Owners are instances, not types: deregistering one leaves another of
// the same type routed.
func TestMsgRouter_Deregister_LeavesOtherInstancesOfTheSameType(t *testing.T) {
	t.Parallel()
	gone, kept := 0, 0
	leaving := &component{routes: []*model.Route{model.Always(func(message.CompanyDeletedMsg) tea.Cmd { gone++; return nil })}}
	staying := &component{routes: []*model.Route{model.Always(func(message.CompanyDeletedMsg) tea.Cmd { kept++; return nil })}}
	r := model.NewMsgRouter()
	for _, owner := range []*component{leaving, staying} {
		if err := r.Register(owner); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}
	r.Dispatch(message.CompanyDeletedMsg{})
	if gone != 1 || kept != 1 {
		t.Fatalf("deliveries before Deregister: leaving = %d, staying = %d, want 1 and 1", gone, kept)
	}

	if err := r.Deregister(leaving); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	r.Dispatch(message.CompanyDeletedMsg{})

	if gone != 1 || kept != 2 {
		t.Errorf("deliveries after Deregister: leaving = %d, staying = %d, want 1 and 2", gone, kept)
	}
}

// Deregistering what was never registered is a mistake, so it's an error.
func TestMsgRouter_Deregister_RefusesAnOwnerItDoesNotHave(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		owner model.Routable
	}{
		{name: "nil", owner: nil},
		{name: "never registered", owner: &component{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := model.NewMsgRouter().Deregister(tt.owner); err == nil {
				t.Errorf("Deregister(%v) returned no error, want one", tt.owner)
			}
		})
	}
}

// A handler can deregister its own owner, such as a screen closing
// itself; the dispatch it's part of still reaches every other owner.
func TestMsgRouter_Deregister_DuringDispatchStillDeliversToOthers(t *testing.T) {
	t.Parallel()
	r := model.NewMsgRouter()
	closing := &component{name: "closing"}
	closing.routes = []*model.Route{model.Always(func(message.CompanyDeletedMsg) tea.Cmd {
		if err := r.Deregister(closing); err != nil {
			t.Errorf("Deregister: %v", err)
		}
		return nil
	})}
	got := 0
	other := &component{name: "other", routes: []*model.Route{model.Always(func(message.CompanyDeletedMsg) tea.Cmd { got++; return nil })}}
	for _, owner := range []*component{closing, other} {
		if err := r.Register(owner); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	r.Dispatch(message.CompanyDeletedMsg{})

	if got != 1 {
		t.Errorf("deliveries to the other owner = %d, want 1", got)
	}
}

type companyChanged = message.Event[message.Changed[message.Company]]

// A route's filters read the event's Meta, and an event one rejects never
// reaches the handler.
func TestRoute_About_PassesOnlyThatSubject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event companyChanged
		want  int
	}{
		{name: "that company", event: message.Created(message.Source{}, 7, time.Time{}, message.Company{ID: 7}), want: 1},
		{name: "another company", event: message.Created(message.Source{}, 8, time.Time{}, message.Company{ID: 8}), want: 0},
		{name: "no subject", event: message.New(message.Source{}, message.Subject{}, message.OpNone, time.Time{}, message.Changed[message.Company]{}), want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := 0
			route := model.Always(func(companyChanged) tea.Cmd { got++; return nil }, model.About(message.TypeCompany, 7))

			route.Handle(tt.event)

			if got != tt.want {
				t.Errorf("deliveries = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRoute_From_PassesOnlyThatInstance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		instance message.ScreenInstance
		want     int
	}{
		{name: "that instance", instance: 3, want: 1},
		{name: "an earlier instance", instance: 2, want: 0},
		{name: "no instance", instance: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := 0
			route := model.Always(func(companyChanged) tea.Cmd { got++; return nil }, model.From(3))

			route.Handle(message.Created(message.Source{Instance: tt.instance}, 7, time.Time{}, message.Company{ID: 7}))

			if got != tt.want {
				t.Errorf("deliveries = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRoute_TwoFilters_BothMustPass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event companyChanged
		want  int
	}{
		{name: "both pass", event: message.Deleted(message.Source{}, 7, time.Time{}, message.Company{ID: 7}), want: 1},
		{name: "only About passes", event: message.Created(message.Source{}, 7, time.Time{}, message.Company{ID: 7}), want: 0},
		{name: "only OnOp passes", event: message.Deleted(message.Source{}, 8, time.Time{}, message.Company{ID: 8}), want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := 0
			route := model.Always(func(companyChanged) tea.Cmd { got++; return nil }, model.About(message.TypeCompany, 7), model.OnOp(message.OpDeleted))

			route.Handle(tt.event)

			if got != tt.want {
				t.Errorf("deliveries = %d, want %d", got, tt.want)
			}
		})
	}
}

// A filter reads Meta, which only an event has, so a filter on any other
// message would never pass; Register refuses it rather than drop them all.
func TestMsgRouter_Register_RefusesAFilterOnAMessageWithoutMeta(t *testing.T) {
	t.Parallel()
	got := 0
	owner := &component{routes: []*model.Route{
		model.Always(func(companyChanged) tea.Cmd { got++; return nil }),
		model.Always(func(tea.KeyPressMsg) tea.Cmd { return nil }, model.From(1)),
	}}
	r := model.NewMsgRouter()

	err := r.Register(owner)

	if err == nil {
		t.Fatal("Register of a filtered tea.KeyPressMsg route returned no error, want one")
	}
	for _, want := range []string{fmt.Sprintf("%T", owner), fmt.Sprintf("%T", tea.KeyPressMsg{})} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Register error %q doesn't name %s", err, want)
		}
	}
	r.Dispatch(message.Created(message.Source{}, 7, time.Time{}, message.Company{ID: 7}))
	if got != 0 {
		t.Errorf("deliveries to the owner's other route = %d, want 0: Register should register none of its routes", got)
	}
}

// A request has no Op, so OnOp never passes one, even when asked for
// OpNone.
func TestRoute_OnOp_PassesOnlyThoseOps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ops  []message.Op
		op   message.Op
		want int
	}{
		{name: "one of them", ops: []message.Op{message.OpCreated, message.OpDeleted}, op: message.OpDeleted, want: 1},
		{name: "another op", ops: []message.Op{message.OpCreated, message.OpDeleted}, op: message.OpUpdated, want: 0},
		{name: "no op", ops: []message.Op{message.OpCreated}, op: message.OpNone, want: 0},
		{name: "no op, asked for", ops: []message.Op{message.OpNone}, op: message.OpNone, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := 0
			route := model.Always(func(companyChanged) tea.Cmd { got++; return nil }, model.OnOp(tt.ops...))

			route.Handle(message.New(message.Source{}, message.Subject{}, tt.op, time.Time{}, message.Changed[message.Company]{}))

			if got != tt.want {
				t.Errorf("deliveries of a %v event to OnOp(%v) = %d, want %d", tt.op, tt.ops, got, tt.want)
			}
		})
	}
}
