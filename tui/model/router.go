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

// HandlerFunc handles a message of the type it was registered for.
type HandlerFunc func(tea.Msg) tea.Cmd

// Router is what a component registers its routes with.
type Router interface {
	// Register calls handler for every msg of the same concrete type.
	Register(msg tea.Msg, s SubComponent, handler HandlerFunc) error
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

func (mr *MsgRouter) Register(msg tea.Msg, s SubComponent, handler HandlerFunc) error {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	id := RouteIDOf(msg)
	if id.typ == nil {
		return fmt.Errorf("router: cannot register a nil message type")
	}

	route := NewRoute(handler)
	mr.routes[id] = append(mr.routes[id], route)
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

// HandleAlways registers handle for every T, whether or not owner is in
// focus, such as a load's result. The route is T itself, so a handler
// can't be registered for a message it doesn't take.
func HandleAlways[T tea.Msg](r Router, owner SubComponent, handle func(T) tea.Cmd) error {
	return register(r, owner, func() bool { return true }, handle)
}

// HandleFocused registers handle for every T that arrives while owner is
// in focus, such as typed input.
func HandleFocused[T tea.Msg](r Router, owner Focusable, handle func(T) tea.Cmd) error {
	return register(r, owner, owner.Focused, handle)
}

func register[T tea.Msg](r Router, owner SubComponent, accepts func() bool, handle func(T) tea.Cmd) error {
	var zero T
	if any(zero) == nil {
		return fmt.Errorf("model: the handler takes %v, an interface, which has no type to route by; take a concrete message type", reflect.TypeFor[T]())
	}
	return r.Register(zero, owner, func(msg tea.Msg) tea.Cmd {
		m, ok := msg.(T)
		if !ok {
			return message.Fail(fmt.Errorf("handler for %T got %T", zero, msg))
		}
		if !accepts() {
			return nil
		}
		return handle(m)
	})
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

// Route is one registered handler.
type Route struct {
	Handle HandlerFunc
}

func NewRoute(handle HandlerFunc) *Route {
	return &Route{
		handle,
	}
}

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
