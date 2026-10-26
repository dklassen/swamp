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

// routesOf is m's routes by message type, failing t if the router
// refuses them.
func routesOf(t *testing.T, m *model.Syncing) map[model.RouteID]*model.Route {
	t.Helper()
	if err := model.NewMsgRouter().Register(m); err != nil {
		t.Fatalf("Register: %v", err)
	}
	routes := map[model.RouteID]*model.Route{}
	for _, route := range m.Routes() {
		routes[route.ID()] = route
	}
	return routes
}

// newSyncing is a Syncing whose routes the router accepts, and send,
// which delivers a message as the router would.
func newSyncing(t *testing.T) (*model.Syncing, func(tea.Msg) tea.Cmd) {
	t.Helper()
	m := model.NewSyncing(nil)
	routes := routesOf(t, m)
	return m, func(msg tea.Msg) tea.Cmd {
		t.Helper()
		route, ok := routes[model.RouteIDOf(msg)]
		if !ok {
			t.Fatalf("nothing registered for %T", msg)
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
		case message.Event[message.Notify]:
			if msg.Meta.Source.Instance != m.Instance() {
				t.Errorf("status sent from instance %d, want Syncing's, %d", msg.Meta.Source.Instance, m.Instance())
			}
			summary = msg.Payload.Status
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
// sends a fault instead of panicking or ignoring it.
func TestSyncing_Handlers_FailOnTheWrongMessage(t *testing.T) {
	t.Parallel()
	for id, route := range routesOf(t, model.NewSyncing(nil)) {
		t.Run(id.String(), func(t *testing.T) {
			cmd := route.Handle(wrongMsg{})
			if cmd == nil {
				t.Fatal("handler given wrongMsg returned no command, want one sending a fault")
			}
			if got, ok := cmd().(message.Event[message.Error]); !ok || got.Payload.Code != message.ErrFault || got.Payload.Text == "" {
				t.Errorf("handler given wrongMsg sent %#v, want an Event[Error] with code %v and a Text", got, message.ErrFault)
			}
		})
	}
}
