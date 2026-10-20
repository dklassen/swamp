// Package model provides components for sharing data via Messages: a Router that
// dispatches each message to the handlers registered for its type, a
// Syncing model that runs company syncs in the background, and common
// styles.
package model

import (
	"fmt"
	"reflect"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/tui/message"
)

// Router is what a component registers its routes with.
type Router interface {
	// Register calls route's handler for every message of its type that
	// route lets through.
	Register(route *Route) error
}

// Routable interface describes the ability to register its messages
// with a Router
type Routable interface {
	RegisterRoutes(r Router) error
}

type MsgRouter struct {
	mu sync.RWMutex
	// Keyed by RouteID (the reflect.Type of the message)
	routes map[RouteID][]*Route
}

func NewMsgRouter() *MsgRouter {
	return &MsgRouter{
		routes: make(map[RouteID][]*Route),
	}
}

func (mr *MsgRouter) Register(route *Route) error {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	if route == nil {
		return fmt.Errorf("router: cannot register a nil Route; make one with Always or WhenFocused")
	}
	if route.err != nil {
		return route.err
	}
	if route.id.typ == nil {
		return fmt.Errorf("router: cannot register a zero Route; make one with Always or WhenFocused")
	}

	mr.routes[route.id] = append(mr.routes[route.id], route)
	return nil
}

func (mr *MsgRouter) Dispatch(msg tea.Msg) tea.Cmd {
	mr.mu.RLock()
	id := RouteIDOf(msg)
	routes, exists := mr.routes[id]
	mr.mu.RUnlock()

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

// Route is one handler and when it gets its messages. Its fields are
// unexported so Always and WhenFocused are the only ways to make one.
type Route struct {
	id     RouteID
	handle func(tea.Msg) tea.Cmd
	err    error
}

// Always is a route to handle for every T, whether or not its owner is in
// focus, such as a load's result. The route is T itself, so a handler
// can't be registered for a message it doesn't take.
func Always[T tea.Msg](handle func(T) tea.Cmd) *Route {
	return newRoute(func() bool { return true }, handle)
}

// WhenFocused is a route to handle for every T that arrives while owner is
// in focus, such as typed input.
func WhenFocused[T tea.Msg](owner Focusable, handle func(T) tea.Cmd) *Route {
	return newRoute(owner.Focused, handle)
}

func newRoute[T tea.Msg](accepts func() bool, handle func(T) tea.Cmd) *Route {
	var zero T
	if any(zero) == nil {
		return &Route{err: fmt.Errorf("model: the handler takes %v, an interface, which has no type to route by; take a concrete message type", reflect.TypeFor[T]())}
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
