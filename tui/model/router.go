// Package model provides components for sharing data via Messages: a Router that
// dispatches each message to the handlers registered for its type, a
// Syncing model that runs company syncs in the background, and common
// styles.
package model

import (
	"fmt"
	"reflect"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/tui/message"
)

// Router is what a component's routes are registered with.
type Router interface {
	// Register calls each of owner's routes for every message of its type
	// that the route lets through.
	Register(owner Routable) error
	Deregister(owner Routable) error
	Dispatch(msg tea.Msg) tea.Cmd
}

// Routable is a component with routes for a Router. Instance is the
// number events name it by, which it draws itself.
type Routable interface {
	Routes() []*Route
	Instance() message.ScreenInstance
}

type MsgRouter struct {
	// Keyed by RouteID (the reflect.Type of the message)
	routes map[RouteID][]*Route
	// Never forgotten on Deregister: a late event for a closed owner still
	// names its instance, so a new owner with it would hear that event.
	seen map[message.ScreenInstance]bool
}

func NewMsgRouter() *MsgRouter {
	return &MsgRouter{
		routes: make(map[RouteID][]*Route),
		seen:   make(map[message.ScreenInstance]bool),
	}
}

// Register adds owner's routes, or none of them if any is invalid or its
// instance is 0 or one Register has seen before.
func (mr *MsgRouter) Register(owner Routable) error {
	if owner == nil {
		return fmt.Errorf("router: cannot register a nil owner")
	}
	instance := owner.Instance()
	if instance == 0 {
		return fmt.Errorf("router: registering %T: instance 0 names nothing; draw one with message.NewScreenInstance", owner)
	}
	if mr.seen[instance] {
		return fmt.Errorf("router: registering %T: instance %d was already registered; each model draws its own", owner, instance)
	}

	routes := owner.Routes()

	owned := make([]*Route, 0, len(routes))
	for _, route := range routes {
		if route == nil {
			return fmt.Errorf("router: registering %T: a nil Route; make one with Always or WhenFocused", owner)
		}
		if route.err != nil {
			return fmt.Errorf("router: registering %T: %w", owner, route.err)
		}
		if route.id.typ == nil {
			return fmt.Errorf("router: registering %T: a zero Route; make one with Always or WhenFocused", owner)
		}
		r := *route
		r.owner = owner
		owned = append(owned, &r)
	}

	for _, r := range owned {
		mr.routes[r.id] = append(mr.routes[r.id], r)
	}
	mr.seen[instance] = true
	return nil
}

// Deregister removes every route owner registered.
func (mr *MsgRouter) Deregister(owner Routable) error {
	if owner == nil {
		return fmt.Errorf("router: cannot deregister a nil owner")
	}

	// A new map and slices, never edited in place: a Dispatch already
	// running still walks the old ones.
	next := make(map[RouteID][]*Route, len(mr.routes))
	removed := false
	for id, routes := range mr.routes {
		for _, r := range routes {
			if r.owner == owner {
				removed = true
				continue
			}
			next[id] = append(next[id], r)
		}
	}

	if !removed {
		return fmt.Errorf("router: deregistering %T: it has no routes registered", owner)
	}

	mr.routes = next
	return nil
}

func (mr *MsgRouter) Dispatch(msg tea.Msg) tea.Cmd {
	id := RouteIDOf(msg)
	routes, exists := mr.routes[id]

	if !exists {
		return nil
	}

	var cmds []tea.Cmd
	for _, route := range routes {
		// Execute the registered handler function
		if cmd := route.Handle(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return tea.Batch(cmds...)
}

// SubComponent is a screen, or anything else the router sends messages
// to. Implementations are pointer types, since the handlers they register
// change them in place.
type SubComponent interface {
	tea.Model
}

// Focusable is a SubComponent that knows whether it's in focus.
type Focusable interface {
	SubComponent
	Focused() bool
}

// Route is one handler, when it gets its messages, and, once registered,
// its owner. Its fields are unexported so Always and WhenFocused are the
// only ways to make one.
type Route struct {
	id     RouteID
	handle func(tea.Msg) tea.Cmd
	owner  Routable
	err    error
}

// Always is a route to handle for every T, whether or not its owner is in
// focus, such as a load's result. The route is T itself, so a handler
// can't be registered for a message it doesn't take.
func Always[T tea.Msg](handle func(T) tea.Cmd, filters ...Filter) *Route {
	return newRoute(func() bool { return true }, handle, filters)
}

// WhenFocused is a route to handle for every T that arrives while owner is
// in focus, such as typed input.
func WhenFocused[T tea.Msg](owner Focusable, handle func(T) tea.Cmd, filters ...Filter) *Route {
	return newRoute(owner.Focused, handle, filters)
}

// Filter is whether a route's handler gets an event, from its Meta.
type Filter func(message.Meta) bool

// About passes events whose Subject is that entity.
func About(typ message.Type, id int64) Filter {
	return func(m message.Meta) bool { return m.Subject == message.Subject{Type: typ, ID: id} }
}

// From passes events that screen instance asked for.
func From(instance message.ScreenInstance) Filter {
	return func(m message.Meta) bool { return m.Source.Instance == instance }
}

// OnOp passes events with one of ops, never one with OpNone, since that's
// a request rather than something that happened.
func OnOp(ops ...message.Op) Filter {
	return func(m message.Meta) bool {
		return m.Op != message.OpNone && slices.Contains(ops, m.Op)
	}
}

type event interface{ Metadata() message.Meta }

func newRoute[T tea.Msg](accepts func() bool, handle func(T) tea.Cmd, filters []Filter) *Route {
	var zero T
	if any(zero) == nil {
		return &Route{err: fmt.Errorf("model: the handler takes %v, an interface, which has no type to route by; take a concrete message type", reflect.TypeFor[T]())}
	}
	if _, ok := any(zero).(event); len(filters) > 0 && !ok {
		return &Route{err: fmt.Errorf("model: the route for %T has filters, but %T has no Metadata() for them to read; filter only an event", zero, zero)}
	}
	return &Route{
		id: RouteIDOf(zero),
		handle: func(msg tea.Msg) tea.Cmd {
			m, ok := msg.(T)
			if !ok {
				return message.Fail(fmt.Errorf("handler for %T got %T", zero, msg))
			}
			if !accepts() {
				return nil
			}
			for _, f := range filters {
				if !f(any(m).(event).Metadata()) {
					return nil
				}
			}
			return handle(m)
		},
	}
}

// ID is the type of message r is for.
func (r *Route) ID() RouteID { return r.id }

// Handle hands msg to r's handler if r lets it through.
func (r *Route) Handle(msg tea.Msg) tea.Cmd { return r.handle(msg) }

// RouteID names the messages one set of handlers is for. It's opaque, so
// only RouteIDOf knows how it is made.
type RouteID struct {
	// Keyed by type identity, not name: %T prints the package's name, not
	// its path, so two packages' same-named types would share a string.
	typ reflect.Type
}

// RouteIDOf is msg's concrete type. A value and a pointer to it are
// different IDs, so a message is always sent and registered as a value.
func RouteIDOf(msg tea.Msg) RouteID {
	return RouteID{typ: reflect.TypeOf(msg)}
}

// String is the type's name as %T prints it, such as
// "message.CompanyCreatedMsg".
func (id RouteID) String() string {
	if id.typ == nil {
		return "<nil>"
	}
	return id.typ.String()
}
