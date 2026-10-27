package message

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"time"
)

// Meta is what every event carries, and all that routing and filtering read.
type Meta struct {
	ID      string    `json:"id"`
	Kind    Kind      `json:"kind"`
	Type    Type      `json:"type"`
	Source  Source    `json:"source"`
	Subject Subject   `json:"subject,omitzero"`
	Op      Op        `json:"op,omitzero"`
	At      time.Time `json:"at"`
}

// Source is who an event is from: the writer's "kind:pid", and the screen
// that asked, 0 for none.
type Source struct {
	Process  string         `json:"process"`
	Instance ScreenInstance `json:"instance,omitzero"`
}

// Subject is the entity an event is about, zero for none.
type Subject struct {
	Type Type  `json:"type"`
	ID   int64 `json:"id"`
}

// Body is what a primitive holds: a snapshot, a query's result, a job.
type Body interface{ Type() Type }

// Primitive is one of the kinds of payload.
type Primitive interface {
	Kind() Kind
	Type() Type
}

type Snapshot interface {
	comparable
	Body
}

type Event[P Primitive] struct {
	Meta    Meta `json:"meta"`
	Payload P    `json:"payload"`
}

func (e Event[P]) Metadata() Meta { return e.Meta }

// New is the only way to make an event, so Kind and Type match the payload.
func New[P Primitive](src Source, subject Subject, op Op, at time.Time, p P) Event[P] {
	return Event[P]{
		Meta:    Meta{ID: NewID(), Kind: p.Kind(), Type: p.Type(), Source: src, Subject: subject, Op: op, At: at},
		Payload: p,
	}
}

// NewID is 128 random bits as hex, so a consumer can drop an event it has
// already seen.
func NewID() string {
	var b [16]byte
	// crypto/rand.Read never returns an error; it crashes the program instead.
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// NewScreenInstance is 63 random bits, never 0, so a model can name itself
// without a counter shared with every other model.
func NewScreenInstance() ScreenInstance {
	for {
		var b [8]byte
		_, _ = rand.Read(b[:])
		if n := ScreenInstance(binary.BigEndian.Uint64(b[:]) >> 1); n != 0 {
			return n
		}
	}
}

// Changed is a write that has happened. Before is zero for OpCreated and
// After for OpDeleted.
type Changed[T Snapshot] struct {
	Before T `json:"before,omitzero"`
	After  T `json:"after,omitzero"`
}

func (Changed[T]) Kind() Kind { return KindChanged }
func (Changed[T]) Type() Type { var t T; return t.Type() }

func Created[T Snapshot](src Source, id int64, at time.Time, after T) Event[Changed[T]] {
	return changed(src, id, OpCreated, at, Changed[T]{After: after})
}

func Updated[T Snapshot](src Source, id int64, at time.Time, before, after T) Event[Changed[T]] {
	return changed(src, id, OpUpdated, at, Changed[T]{Before: before, After: after})
}

func Deleted[T Snapshot](src Source, id int64, at time.Time, before T) Event[Changed[T]] {
	return changed(src, id, OpDeleted, at, Changed[T]{Before: before})
}

func changed[T Snapshot](src Source, id int64, op Op, at time.Time, c Changed[T]) Event[Changed[T]] {
	return New(src, Subject{Type: c.Type(), ID: id}, op, at, c)
}

// Read is a read's result, for the model that asked.
type Read[T Body] struct {
	Value T `json:"value"`
}

func (Read[T]) Kind() Kind { return KindRead }
func (Read[T]) Type() Type { var t T; return t.Type() }

// Load asks the model that shows T to read it again. Its Read is the reply.
type Load[T Body] struct{}

func (Load[T]) Kind() Kind { return KindLoad }
func (Load[T]) Type() Type { var t T; return t.Type() }

// Run asks for a job whose state another model owns.
type Run[T Body] struct {
	Value T `json:"value"`
}

func (Run[T]) Kind() Kind { return KindRun }
func (Run[T]) Type() Type { var t T; return t.Type() }

// Ran is that a job ran, with its report: counts and what a responding
// model acts on, never the rows the job wrote.
type Ran[T Body] struct {
	Value T `json:"value"`
}

func (Ran[T]) Kind() Kind { return KindRan }
func (Ran[T]) Type() Type { var t T; return t.Type() }

// Error is a write, load or run that didn't happen, or a fault. Meta says
// by whom, of which entity, and the op.
type Error struct {
	Code ErrorCode `json:"code"`
	Text string    `json:"text"`
}

func (Error) Kind() Kind { return KindError }
func (Error) Type() Type { return TypeError }

// Nav is a request to the screen stack. The entity to open on is the
// event's Subject.
type Nav struct {
	Op     NavOp  `json:"op"`
	Screen Screen `json:"screen,omitzero"`
}

func (Nav) Kind() Kind { return KindNav }
func (Nav) Type() Type { return TypeNav }

// Notify is a status line for the banner.
type Notify struct {
	Status string `json:"status"`
}

func (Notify) Kind() Kind { return KindNotify }
func (Notify) Type() Type { return TypeStatus }

// Signal is a UI fact with no entity, such as the content area's size.
type Signal[T Body] struct {
	Value T `json:"value"`
}

func (Signal[T]) Kind() Kind { return KindSignal }
func (Signal[T]) Type() Type { var t T; return t.Type() }
