package message_test

import (
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dklassen/swamp/tui/message"
)

const (
	entityType message.Type = "entity"
	reportType message.Type = "report"
)

// entity stands in for a snapshot such as message.Company.
type entity struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (entity) Type() message.Type { return entityType }

// report stands in for a query's result, a job or a signal's content.
type report struct {
	Count int       `json:"count"`
	At    time.Time `json:"at"`
}

func (report) Type() message.Type { return reportType }

var (
	src = message.Source{Process: "tui:42", Instance: 7}
	at  = time.Date(2026, 10, 26, 9, 30, 0, 0, time.UTC)
)

func TestNew_SetsKindAndTypeFromThePayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		meta     message.Meta
		wantKind message.Kind
		wantType message.Type
	}{
		{name: "changed", meta: message.New(src, message.Subject{}, message.OpUpdated, at, message.Changed[entity]{}).Meta, wantKind: message.KindChanged, wantType: entityType},
		{name: "read", meta: message.New(src, message.Subject{}, message.OpNone, at, message.Read[report]{}).Meta, wantKind: message.KindRead, wantType: reportType},
		{name: "run", meta: message.New(src, message.Subject{}, message.OpNone, at, message.Run[report]{}).Meta, wantKind: message.KindRun, wantType: reportType},
		{name: "ran", meta: message.New(src, message.Subject{}, message.OpNone, at, message.Ran[report]{}).Meta, wantKind: message.KindRan, wantType: reportType},
		{name: "error", meta: message.New(src, message.Subject{}, message.OpDeleted, at, message.Error{Code: message.ErrNotFound}).Meta, wantKind: message.KindError, wantType: message.Type(message.ErrNotFound.String())},
		{name: "nav", meta: message.New(src, message.Subject{}, message.OpNone, at, message.Nav{Op: message.NavPush, Screen: message.ScreenCompanyEdit}).Meta, wantKind: message.KindNav, wantType: message.Type(message.ScreenCompanyEdit.String())},
		{name: "notify", meta: message.New(src, message.Subject{}, message.OpNone, at, message.Notify{Status: "saved"}).Meta, wantKind: message.KindNotify, wantType: message.TypeStatus},
		{name: "signal", meta: message.New(src, message.Subject{}, message.OpNone, at, message.Signal[report]{}).Meta, wantKind: message.KindSignal, wantType: reportType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.meta.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", tt.meta.Kind, tt.wantKind)
			}
			if tt.meta.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", tt.meta.Type, tt.wantType)
			}
		})
	}
}

func TestNewID_Is128RandomBitsAsHex(t *testing.T) {
	t.Parallel()

	a, b := message.NewID(), message.NewID()
	for _, id := range []string{a, b} {
		raw, err := hex.DecodeString(id)
		if err != nil {
			t.Fatalf("NewID() = %q, not hex: %v", id, err)
		}
		if got, want := len(raw)*8, 128; got != want {
			t.Errorf("NewID() = %q, %d bits, want %d", id, got, want)
		}
	}
	if a == b {
		t.Errorf("two NewID() calls both gave %q", a)
	}
}

func TestNew_GivesEachEventItsOwnID(t *testing.T) {
	t.Parallel()

	a := message.New(src, message.Subject{}, message.OpNone, at, message.Notify{})
	b := message.New(src, message.Subject{}, message.OpNone, at, message.Notify{})
	if a.Meta.ID == "" || a.Meta.ID == b.Meta.ID {
		t.Errorf("IDs %q and %q, want two different non-empty IDs", a.Meta.ID, b.Meta.ID)
	}
}

func TestCreatedUpdatedDeleted_SetOpAndLeaveTheMissingSideZero(t *testing.T) {
	t.Parallel()

	before, after := entity{ID: 3, Name: "Acme"}, entity{ID: 3, Name: "Acme Corp"}
	subject := message.Subject{Type: entityType, ID: 3}
	tests := []struct {
		name string
		got  message.Event[message.Changed[entity]]
		want message.Event[message.Changed[entity]]
	}{
		{
			name: "created",
			got:  message.Created(src, 3, at, after),
			want: message.Event[message.Changed[entity]]{
				Meta:    message.Meta{Kind: message.KindChanged, Type: entityType, Source: src, Subject: subject, Op: message.OpCreated, At: at},
				Payload: message.Changed[entity]{After: after},
			},
		},
		{
			name: "updated",
			got:  message.Updated(src, 3, at, before, after),
			want: message.Event[message.Changed[entity]]{
				Meta:    message.Meta{Kind: message.KindChanged, Type: entityType, Source: src, Subject: subject, Op: message.OpUpdated, At: at},
				Payload: message.Changed[entity]{Before: before, After: after},
			},
		},
		{
			name: "deleted",
			got:  message.Deleted(src, 3, at, before),
			want: message.Event[message.Changed[entity]]{
				Meta:    message.Meta{Kind: message.KindChanged, Type: entityType, Source: src, Subject: subject, Op: message.OpDeleted, At: at},
				Payload: message.Changed[entity]{Before: before},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, tt.got, cmpopts.IgnoreFields(message.Meta{}, "ID")); diff != "" {
				t.Errorf("event mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestChanged_JSONLeavesOutTheMissingSide(t *testing.T) {
	t.Parallel()

	e := entity{ID: 3, Name: "Acme"}
	tests := []struct {
		name    string
		changed message.Changed[entity]
	}{
		{name: "created", changed: message.Created(src, 3, at, e).Payload},
		{name: "deleted", changed: message.Deleted(src, 3, at, e).Payload},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, err := json.Marshal(tt.changed)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(b, &fields); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if len(fields) != 1 {
				t.Fatalf("%s has %d fields, want only the side that's set", b, len(fields))
			}
			for _, raw := range fields {
				var got entity
				if err := json.Unmarshal(raw, &got); err != nil {
					t.Fatalf("Unmarshal %s: %v", raw, err)
				}
				if diff := cmp.Diff(e, got); diff != "" {
					t.Errorf("the side that's set (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestEvent_EachPrimitiveRoundTripsThroughJSON(t *testing.T) {
	t.Parallel()

	subject := message.Subject{Type: entityType, ID: 3}
	r := report{Count: 2, At: at}
	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{name: "changed", check: roundTrip(message.Updated(src, 3, at, entity{ID: 3, Name: "Acme"}, entity{ID: 3, Name: "Acme Corp"}))},
		{name: "read", check: roundTrip(message.New(src, subject, message.OpNone, at, message.Read[report]{Value: r}))},
		{name: "run", check: roundTrip(message.New(src, subject, message.OpNone, at, message.Run[report]{Value: r}))},
		{name: "ran", check: roundTrip(message.New(src, subject, message.OpNone, at, message.Ran[report]{Value: r}))},
		{name: "error", check: roundTrip(message.New(src, subject, message.OpUpdated, at, message.Error{Code: message.ErrSyncInProgress, Text: "a sync is already running"}))},
		{name: "nav", check: roundTrip(message.New(src, subject, message.OpNone, at, message.Nav{Op: message.NavReplace, Screen: message.ScreenCompanyEdit}))},
		{name: "notify", check: roundTrip(message.New(src, message.Subject{}, message.OpNone, at, message.Notify{Status: "saved"}))},
		{name: "signal", check: roundTrip(message.New(src, message.Subject{}, message.OpNone, at, message.Signal[report]{Value: r}))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t)
		})
	}
}

func roundTrip[P message.Primitive](want message.Event[P]) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()
		b, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var got message.Event[P]
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("Unmarshal %s: %v", b, err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("%s round trip (-want +got):\n%s", b, diff)
		}
	}
}
