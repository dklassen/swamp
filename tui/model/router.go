package model

import (
	"fmt"
	"reflect"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/tui/message"
)

// Scope is when a registered handler gets the messages it registered for.
type Scope int

const (
	// AlwaysScope: every message of the type, whether or not the owner is
	// in focus, such as a load's result.
	AlwaysScope Scope = iota
	// FocusScope: only while the owner is in focus, such as typed input.
	FocusScope
)

// HandlerFunc handles a message of the type it was registered for.
type HandlerFunc func(tea.Msg) tea.Cmd

// Router is what a component registers its routes with.
type Router interface {
	// Register calls handler for every msg of the same concrete type that
	// scope lets through.
	Register(msg tea.Msg, s SubComponent, scope Scope, handler HandlerFunc) error
}

// Handle registers handle for every T that scope lets through. The route
// is T itself, so a handler can't be registered for a message it doesn't
// take.
func Handle[T tea.Msg](r Router, s SubComponent, scope Scope, handle func(T) tea.Cmd) error {
	var zero T
	if any(zero) == nil {
		return fmt.Errorf("model.Handle: the handler takes %v, an interface, which has no type to route by; take a concrete message type", reflect.TypeFor[T]())
	}
	return r.Register(zero, s, scope, func(msg tea.Msg) tea.Cmd {
		m, ok := msg.(T)
		if !ok {
			return message.Fail(fmt.Errorf("handler for %T got %T", zero, msg))
		}
		return handle(m)
	})
}

// SubComponent is a screen, or anything else the router sends messages
// to. Implementations are pointer types, since the handlers they register
// change them in place.
type SubComponent interface {
	tea.Model
	// RegisterRoutes runs when the router opens the component, before Init.
	RegisterRoutes(r Router) error
}

// Route is one registered handler and the component that registered it.
type Route struct {
	Owner  SubComponent
	Scope  Scope
	Handle HandlerFunc
}

func NewRoute(owner SubComponent, scope Scope, handle HandlerFunc) *Route {
	return &Route{
		owner,
		scope,
		handle,
	}
}

// Accepts reports whether r's handler gets a message while focused is the
// component in focus; focused is nil when none is.
func (r *Route) Accepts(focused SubComponent) bool {
	return r.Scope == AlwaysScope || r.Owner == focused
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
