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
		{name: "changed", meta: message.New(src, message.Subject{}, message.OpUpdated, at, message.Changed[message.Company]{}).Meta, wantKind: message.KindChanged, wantType: message.TypeCompany},
		{name: "read", meta: message.New(src, message.Subject{}, message.OpNone, at, message.Read[message.ContentSize]{}).Meta, wantKind: message.KindRead, wantType: message.TypeContentSize},
		{name: "load", meta: message.New(src, message.Subject{}, message.OpNone, at, message.Load[message.CompanyList]{}).Meta, wantKind: message.KindLoad, wantType: message.TypeCompanyList},
		{name: "run", meta: message.New(src, message.Subject{}, message.OpNone, at, message.Run[message.ContentSize]{}).Meta, wantKind: message.KindRun, wantType: message.TypeContentSize},
		{name: "ran", meta: message.New(src, message.Subject{}, message.OpNone, at, message.Ran[message.ContentSize]{}).Meta, wantKind: message.KindRan, wantType: message.TypeContentSize},
		{name: "error", meta: message.New(src, message.Subject{}, message.OpDeleted, at, message.Error{Code: message.ErrNotFound}).Meta, wantKind: message.KindError, wantType: message.TypeError},
		{name: "nav", meta: message.New(src, message.Subject{}, message.OpNone, at, message.Nav[message.Company]{Op: message.NavPush, Screen: message.ScreenCompanyEdit}).Meta, wantKind: message.KindNav, wantType: message.TypeCompany},
		{name: "nav with nothing", meta: message.New(src, message.Subject{}, message.OpNone, at, message.Nav[message.None]{Op: message.NavPop}).Meta, wantKind: message.KindNav, wantType: message.TypeNone},
		{name: "notify", meta: message.New(src, message.Subject{}, message.OpNone, at, message.Notify{Status: "saved"}).Meta, wantKind: message.KindNotify, wantType: message.TypeStatus},
		{name: "signal", meta: message.New(src, message.Subject{}, message.OpNone, at, message.Signal[message.ContentSize]{}).Meta, wantKind: message.KindSignal, wantType: message.TypeContentSize},
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

// A model draws its own instance, so with no counter to share, draws must
// not repeat, and none may be 0, which is none.
func TestNewScreenInstance_IsNonzeroAndDoesNotRepeat(t *testing.T) {
	t.Parallel()

	seen := map[message.ScreenInstance]bool{}
	for range 1000 {
		got := message.NewScreenInstance()
		if got == 0 {
			t.Fatal("NewScreenInstance() = 0, want a number: 0 is none")
		}
		if seen[got] {
			t.Fatalf("NewScreenInstance() gave %d twice", got)
		}
		seen[got] = true
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

	before, after := message.Company{ID: 3, Name: "Acme"}, message.Company{ID: 3, Name: "Acme Corp"}
	subject := message.Subject{Type: message.TypeCompany, ID: 3}
	tests := []struct {
		name string
		got  message.Event[message.Changed[message.Company]]
		want message.Event[message.Changed[message.Company]]
	}{
		{
			name: "created",
			got:  message.Created(src, 3, at, after),
			want: message.Event[message.Changed[message.Company]]{
				Meta:    message.Meta{Kind: message.KindChanged, Type: message.TypeCompany, Source: src, Subject: subject, Op: message.OpCreated, At: at},
				Payload: message.Changed[message.Company]{After: after},
			},
		},
		{
			name: "updated",
			got:  message.Updated(src, 3, at, before, after),
			want: message.Event[message.Changed[message.Company]]{
				Meta:    message.Meta{Kind: message.KindChanged, Type: message.TypeCompany, Source: src, Subject: subject, Op: message.OpUpdated, At: at},
				Payload: message.Changed[message.Company]{Before: before, After: after},
			},
		},
		{
			name: "deleted",
			got:  message.Deleted(src, 3, at, before),
			want: message.Event[message.Changed[message.Company]]{
				Meta:    message.Meta{Kind: message.KindChanged, Type: message.TypeCompany, Source: src, Subject: subject, Op: message.OpDeleted, At: at},
				Payload: message.Changed[message.Company]{Before: before},
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

	e := message.Company{ID: 3, Name: "Acme"}
	tests := []struct {
		name    string
		changed message.Changed[message.Company]
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
				var got message.Company
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

	subject := message.Subject{Type: message.TypeCompany, ID: 3}
	r := message.ContentSize{Width: 80, Rows: 24}
	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{name: "changed", check: roundTrip(message.Updated(src, 3, at,
			message.Company{ID: 3, Name: "Acme", Description: "Makes anvils", CreatedAt: at.Add(-time.Hour), UpdatedAt: at.Add(-time.Hour)},
			message.Company{ID: 3, Name: "Acme Corp", Description: "Makes anvils", CreatedAt: at.Add(-time.Hour), UpdatedAt: at}))},
		{name: "read", check: roundTrip(message.New(src, subject, message.OpNone, at, message.Read[message.ContentSize]{Value: r}))},
		{name: "company list read", check: roundTrip(message.New(src, message.Subject{}, message.OpNone, at, message.Read[message.CompanyList]{Value: message.CompanyList{
			Companies:    []message.Company{{ID: 3, Name: "Acme", CreatedAt: at, UpdatedAt: at}},
			OpenPostings: map[int64]int{3: 2},
		}}))},
		{name: "load", check: roundTrip(message.New(src, message.Subject{}, message.OpNone, at, message.Load[message.CompanyList]{}))},
		{name: "run", check: roundTrip(message.New(src, subject, message.OpNone, at, message.Run[message.ContentSize]{Value: r}))},
		{name: "ran", check: roundTrip(message.New(src, subject, message.OpNone, at, message.Ran[message.ContentSize]{Value: r}))},
		{name: "sync company run", check: roundTrip(message.New(src, subject, message.OpNone, at, message.Run[message.SyncCompany]{Value: message.SyncCompany{
			Company: message.Company{ID: 3, Name: "Acme", CreatedAt: at, UpdatedAt: at},
		}}))},
		{name: "sync all run", check: roundTrip(message.New(src, message.Subject{}, message.OpNone, at, message.Run[message.SyncAll]{Value: message.SyncAll{
			Companies: []message.Company{{ID: 3, Name: "Acme", CreatedAt: at, UpdatedAt: at}},
		}}))},
		{name: "sync report ran", check: roundTrip(message.New(src, subject, message.OpNone, at, message.Ran[message.SyncReport]{Value: message.SyncReport{
			RunID: 2, Fetched: 40, Created: 3, Updated: 2, Closed: 1, Reopened: 1,
		}}))},
		{name: "skipped sync report ran", check: roundTrip(message.New(src, subject, message.OpNone, at, message.Ran[message.SyncReport]{Value: message.SyncReport{
			RunID: 2, Code: message.ErrSyncInProgress,
		}}))},
		{name: "sync all report ran", check: roundTrip(message.New(src, message.Subject{}, message.OpNone, at, message.Ran[message.SyncAllReport]{Value: message.SyncAllReport{
			Companies: 3, Failed: []string{"Globex"}, Skipped: []string{"Initech"},
		}}))},
		{name: "error", check: roundTrip(message.New(src, subject, message.OpUpdated, at, message.Error{Code: message.ErrSyncInProgress, Text: "a sync is already running"}))},
		{name: "nav", check: roundTrip(message.New(src, subject, message.OpNone, at, message.Nav[message.Company]{Op: message.NavReplace, Screen: message.ScreenCompanyEdit,
			Value: message.Company{ID: 3, Name: "Acme", CreatedAt: at, UpdatedAt: at}}))},
		{name: "company row nav", check: roundTrip(message.New(src, subject, message.OpNone, at, message.Nav[message.CompanyRow]{Op: message.NavPush, Screen: message.ScreenCompanyDelete,
			Value: message.CompanyRow{Company: message.Company{ID: 3, Name: "Acme", CreatedAt: at, UpdatedAt: at}, OpenPostings: 2}}))},
		{name: "pop", check: roundTrip(message.New(src, message.Subject{}, message.OpNone, at, message.Nav[message.None]{Op: message.NavPop}))},
		{name: "notify", check: roundTrip(message.New(src, message.Subject{}, message.OpNone, at, message.Notify{Status: "saved"}))},
		{name: "signal", check: roundTrip(message.New(src, message.Subject{}, message.OpNone, at, message.Signal[message.ContentSize]{Value: r}))},
		{name: "focus signal", check: roundTrip(message.New(src, message.Subject{}, message.OpNone, at, message.Signal[message.FocusChanged]{Value: message.FocusChanged{Focused: 7}}))},
		{name: "posting list read", check: roundTrip(message.New(src, subject, message.OpNone, at, message.Read[message.PostingList]{Value: message.PostingList{
			Postings:    []message.Posting{{ID: 5, CompanyID: 3, Title: "Engineer", Department: "Platform", Location: "Remote", JobURL: "https://example.com/5", ListingStatus: message.ListingOpen}},
			Markup:      map[int64]message.Markup{5: {PostingID: 5, InterestedAt: at}},
			Departments: []string{"Platform"},
		}}))},
		{name: "markup changed", check: roundTrip(message.New(src, message.Subject{Type: message.TypePosting, ID: 5}, message.OpUpdated, at, message.Changed[message.Markup]{
			Before: message.Markup{PostingID: 5},
			After:  message.Markup{PostingID: 5, ArchivedAt: at},
		}))},
		{name: "application changed", check: roundTrip(message.New(src, message.Subject{Type: message.TypeApplication, ID: 4}, message.OpDeleted, at, message.Changed[message.Application]{
			Before: message.Application{ID: 4, PostingID: 5, Status: message.ApplicationInterviewing, Notes: "call Tuesday", CreatedAt: at, UpdatedAt: at},
			After:  message.Application{ID: 4, PostingID: 5, Status: message.ApplicationInterviewing, Notes: "call Tuesday", CreatedAt: at, UpdatedAt: at, DeletedAt: at},
		}))},
		{name: "posting closed error", check: roundTrip(message.New(src, message.Subject{Type: message.TypePosting, ID: 5}, message.OpCreated, at, message.Error{Code: message.ErrPostingClosed, Text: "posting is closed"}))},
		{name: "hide archived signal", check: roundTrip(message.New(src, message.Subject{}, message.OpNone, at, message.Signal[message.HideArchived]{Value: message.HideArchived{Hidden: true}}))},
		{name: "filter selection nav", check: roundTrip(message.New(src, subject, message.OpNone, at, message.Nav[message.FilterSelection]{Op: message.NavPush, Screen: message.ScreenFilterSelect,
			Value: message.FilterSelection{Company: message.Company{ID: 3, Name: "Acme", CreatedAt: at, UpdatedAt: at}, Departments: []string{"Platform"}, Locations: []string{"Remote"}}}))},
		{name: "filter options read", check: roundTrip(message.New(src, subject, message.OpNone, at, message.Read[message.FilterOptions]{Value: message.FilterOptions{
			Departments: []string{"Platform", "Sales"}, Locations: []string{"Remote"},
		}}))},
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
