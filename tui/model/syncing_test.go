package model_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// fakeRouter records what owner registers, by message type.
type fakeRouter struct {
	owner  model.SubComponent
	routes map[model.RouteID]*model.Route
}

func (r *fakeRouter) Register(msg tea.Msg, m model.SubComponent, scope model.Scope, handler model.HandlerFunc) error {
	r.routes[model.RouteIDOf(msg)] = model.NewRoute(r.owner, scope, handler)
	return nil
}

// newSyncing is a Syncing with its routes registered, and send, which
// delivers a message as the router would. Syncing is never in focus, so a
// route only it can't get fails the test.
func newSyncing(t *testing.T) (*model.Syncing, func(tea.Msg) tea.Cmd) {
	t.Helper()
	m := model.NewSyncing(nil)
	r := &fakeRouter{owner: m, routes: map[model.RouteID]*model.Route{}}
	if err := m.RegisterRoutes(r); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	return m, func(msg tea.Msg) tea.Cmd {
		t.Helper()
		route, ok := r.routes[model.RouteIDOf(msg)]
		if !ok {
			t.Fatalf("nothing registered for %T", msg)
		}
		if !route.Accepts(nil) {
			t.Fatalf("%T is registered with a scope that never reaches Syncing, which is never in focus", msg)
		}
		return route.Handle(msg)
	}
}

func viewOf(m *model.Syncing) string {
	return ansi.Strip(m.View().Content)
}

var acmeAndGlobex = []company.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Globex"}}

func TestSyncing_ToggleSyncAll_StartsWithTheFirstCompany(t *testing.T) {
	t.Parallel()
	m, send := newSyncing(t)

	if cmd := send(message.ToggleSyncAllMsg{Companies: acmeAndGlobex}); cmd == nil {
		t.Fatal("ToggleSyncAllMsg returned nil Cmd, want the first company's sync")
	}

	view := viewOf(m)
	if !strings.Contains(view, "Syncing 1/2: Acme…") || !strings.Contains(view, "░") {
		t.Errorf("View = %q, want the bar and Syncing 1/2: Acme…", view)
	}
}

// A run may be syncing the company right now, so a refresh waits (#150).
func TestSyncing_RefreshDuringARun_IsRefused(t *testing.T) {
	t.Parallel()
	m, send := newSyncing(t)
	send(message.ToggleSyncAllMsg{Companies: acmeAndGlobex})

	if cmd := send(message.RefreshCompanyMsg{Company: acmeAndGlobex[1]}); cmd != nil {
		t.Error("RefreshCompanyMsg during a run returned a Cmd, want nil")
	}
	if view := viewOf(m); !strings.HasSuffix(view, "Sync all in progress: refresh unavailable") {
		t.Errorf("View = %q, want it to end with the refusal", view)
	}
}

// messagesOf runs cmd, and each command of a batch, returning their messages.
func messagesOf(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var msgs []tea.Msg
	for _, c := range batch {
		msgs = append(msgs, messagesOf(c)...)
	}
	return msgs
}

func TestSyncing_LastCompany_ReportsTheSummaryAndAnnouncesTheResults(t *testing.T) {
	t.Parallel()
	m, send := newSyncing(t)
	send(message.ToggleSyncAllMsg{Companies: acmeAndGlobex})
	send(message.SyncAllStepMsg{RunID: 1, Result: sync.Result{CompanyID: 1, Name: "Acme"}})

	msgs := messagesOf(send(message.SyncAllStepMsg{RunID: 1, Result: sync.Result{CompanyID: 2, Name: "Globex"}}))

	if view := viewOf(m); view != "" {
		t.Errorf("View after the last company = %q, want nothing", view)
	}
	var summary string
	var finished *message.SyncAllFinishedMsg
	for _, msg := range msgs {
		switch msg := msg.(type) {
		case message.BannerMsg:
			summary = msg.Status
		case message.SyncAllFinishedMsg:
			finished = &msg
		}
	}
	if summary != "2 companies, 0 failed" {
		t.Errorf("reported status = %q, want %q", summary, "2 companies, 0 failed")
	}
	if finished == nil || len(finished.Results) != 2 {
		t.Errorf("announced %+v, want a SyncAllFinishedMsg with both results", finished)
	}
}

// A result arriving late from a stopped run neither counts towards nor
// advances the run under way.
func TestSyncing_ResultFromAnEarlierRun_IsIgnored(t *testing.T) {
	t.Parallel()
	m, send := newSyncing(t)
	send(message.ToggleSyncAllMsg{Companies: acmeAndGlobex})
	send(message.ToggleSyncAllMsg{}) // stop after Acme
	send(message.SyncAllStepMsg{RunID: 1, Result: sync.Result{CompanyID: 1, Name: "Acme"}})
	send(message.ToggleSyncAllMsg{Companies: acmeAndGlobex}) // run 2
	before := viewOf(m)

	if cmd := send(message.SyncAllStepMsg{RunID: 1, Result: sync.Result{CompanyID: 2, Name: "Globex"}}); cmd != nil {
		t.Error("a result from run 1 returned a Cmd during run 2, want nil")
	}
	if view := viewOf(m); view != before {
		t.Errorf("View after a run-1 result = %q, want %q (unchanged)", view, before)
	}
}

// wrongMsg is a message Syncing doesn't register for.
type wrongMsg struct{}

// Every handler, handed a message of a type it wasn't registered for,
// sends an ErrMsg instead of panicking or ignoring it.
func TestSyncing_Handlers_FailOnTheWrongMessage(t *testing.T) {
	t.Parallel()
	m := model.NewSyncing(nil)
	r := &fakeRouter{owner: m, routes: map[model.RouteID]*model.Route{}}
	if err := m.RegisterRoutes(r); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	for id, route := range r.routes {
		t.Run(id.String(), func(t *testing.T) {
			cmd := route.Handle(wrongMsg{})
			if cmd == nil {
				t.Fatal("handler given wrongMsg returned no command, want one sending message.ErrMsg")
			}
			if got, ok := cmd().(message.ErrMsg); !ok || got.Err == nil {
				t.Errorf("handler given wrongMsg sent %#v, want a message.ErrMsg with an Err", got)
			}
		})
	}
}
