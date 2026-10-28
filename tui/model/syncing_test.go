package model_test

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"

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

// syncAll asks for a run over companies, or for the run under way to stop.
func syncAll(companies []message.Company) tea.Msg {
	return message.New(message.Source{}, message.Subject{}, message.OpNone, time.Now().UTC(),
		message.Run[message.SyncAll]{Value: message.SyncAll{Companies: companies}})
}

// syncCompany asks for c to be refreshed.
func syncCompany(c message.Company) tea.Msg {
	return message.New(message.Source{}, message.Subject{Type: message.TypeCompany, ID: c.ID}, message.OpNone, time.Now().UTC(),
		message.Run[message.SyncCompany]{Value: message.SyncCompany{Company: c}})
}

func viewOf(m *model.Syncing) string {
	return ansi.Strip(m.View().Content)
}

var acmeAndGlobex = []message.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Globex"}}

func TestSyncing_RunSyncAll_StartsWithTheFirstCompany(t *testing.T) {
	t.Parallel()
	m, send := newSyncing(t)

	if cmd := send(syncAll(acmeAndGlobex)); cmd == nil {
		t.Fatal("Run[SyncAll] returned nil Cmd, want the first company's sync")
	}

	view := viewOf(m)
	if !strings.Contains(view, "Syncing 1/2: Acme…") || !strings.Contains(view, "░") {
		t.Errorf("View = %q, want the bar and Syncing 1/2: Acme…", view)
	}
}

// failed is c's sync failing, from m, with code.
func failed(m *model.Syncing, c message.Company, code message.ErrorCode) tea.Msg {
	return message.New(message.Source{Instance: m.Instance()}, message.Subject{Type: message.TypeCompany, ID: c.ID}, message.OpNone, time.Now().UTC(),
		message.Error{Code: code, Text: "it failed"})
}

// A refresh's outcome goes to the banner by company name. A failure other
// than a sync elsewhere is an Error the banner already shows.
func TestSyncing_RefreshReportingBack_StopsTheSpinnerAndSaysHowItWent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		outcome    func(m *model.Syncing) tea.Msg
		wantStatus []string
	}{
		{
			name:       "synced",
			outcome:    func(m *model.Syncing) tea.Msg { return synced(m, 0, acmeAndGlobex[0], 0) },
			wantStatus: []string{"Acme: fetched 0, created 0, updated 0, closed 0, reopened 0"},
		},
		{
			name:       "syncing elsewhere",
			outcome:    func(m *model.Syncing) tea.Msg { return failed(m, acmeAndGlobex[0], message.ErrSyncInProgress) },
			wantStatus: []string{"Acme is already syncing elsewhere; try again in a moment"},
		},
		{
			name:    "failed",
			outcome: func(m *model.Syncing) tea.Msg { return failed(m, acmeAndGlobex[0], message.ErrOther) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, send := newSyncing(t)
			send(syncCompany(acmeAndGlobex[0]))

			var status []string
			for _, msg := range messagesOf(send(tt.outcome(m))) {
				if notify, ok := msg.(message.Event[message.Notify]); ok {
					status = append(status, notify.Payload.Status)
				}
			}

			if diff := cmp.Diff(tt.wantStatus, status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
			if view := viewOf(m); view != "" {
				t.Errorf("View = %q, want nothing once the refresh is back", view)
			}
		})
	}
}

// With two refreshes in flight, each outcome names its own company, and
// the spinner turns until both are back.
func TestSyncing_TwoRefreshes_EachReportsItsOwnCompany(t *testing.T) {
	t.Parallel()
	m, send := newSyncing(t)
	send(syncCompany(acmeAndGlobex[0]))
	send(syncCompany(acmeAndGlobex[1]))

	msgs := messagesOf(send(failed(m, acmeAndGlobex[0], message.ErrSyncInProgress)))

	if len(msgs) != 1 || msgs[0].(message.Event[message.Notify]).Payload.Status != "Acme is already syncing elsewhere; try again in a moment" {
		t.Errorf("Acme's outcome sent %+v, want its status", msgs)
	}
	if view := viewOf(m); !strings.Contains(view, "Refreshing Globex…") {
		t.Errorf("View = %q, want the spinner still turning for Globex", view)
	}
}

// A run may be syncing the company right now, so a refresh waits (#150).
func TestSyncing_RefreshDuringARun_IsRefused(t *testing.T) {
	t.Parallel()
	m, send := newSyncing(t)
	send(syncAll(acmeAndGlobex))

	if cmd := send(syncCompany(acmeAndGlobex[1])); cmd != nil {
		t.Error("Run[SyncCompany] during a run returned a Cmd, want nil")
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

// synced is c's step in run runID, from m, with code saying why it didn't
// sync, zero if it did.
func synced(m *model.Syncing, runID int, c message.Company, code message.ErrorCode) tea.Msg {
	return message.New(message.Source{Instance: m.Instance()}, message.Subject{Type: message.TypeCompany, ID: c.ID}, message.OpNone, time.Now().UTC(),
		message.Ran[message.SyncReport]{Value: message.SyncReport{RunID: runID, Code: code}})
}

// The summary is the line `swamp fetch` prints, and the report has the
// same totals, with no Error for a company that failed.
func TestSyncing_LastCompany_ReportsTheRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		codes       [2]message.ErrorCode
		wantSummary string
		wantReport  message.SyncAllReport
	}{
		{name: "both synced", wantSummary: "2 companies, 0 failed", wantReport: message.SyncAllReport{Companies: 2}},
		{
			name: "one failed, one skipped", codes: [2]message.ErrorCode{message.ErrOther, message.ErrSyncInProgress},
			wantSummary: "2 companies, 1 failed (Acme), 1 skipped (Globex)",
			wantReport:  message.SyncAllReport{Companies: 2, Failed: []string{"Acme"}, Skipped: []string{"Globex"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, send := newSyncing(t)
			send(syncAll(acmeAndGlobex))
			send(synced(m, 1, acmeAndGlobex[0], tt.codes[0]))

			msgs := messagesOf(send(synced(m, 1, acmeAndGlobex[1], tt.codes[1])))

			if view := viewOf(m); view != "" {
				t.Errorf("View after the last company = %q, want nothing", view)
			}
			var summary string
			var reports []message.SyncAllReport
			for _, msg := range msgs {
				switch msg := msg.(type) {
				case message.Event[message.Notify]:
					if msg.Meta.Source.Instance != m.Instance() {
						t.Errorf("status sent from instance %d, want Syncing's, %d", msg.Meta.Source.Instance, m.Instance())
					}
					summary = msg.Payload.Status
				case message.Event[message.Ran[message.SyncAllReport]]:
					if msg.Meta.Source.Instance != m.Instance() {
						t.Errorf("report sent from instance %d, want Syncing's, %d", msg.Meta.Source.Instance, m.Instance())
					}
					reports = append(reports, msg.Payload.Value)
				case message.Event[message.Error]:
					t.Errorf("sent %+v, want a failed company in the summary, not an Error", msg)
				}
			}
			if summary != tt.wantSummary {
				t.Errorf("reported status = %q, want %q", summary, tt.wantSummary)
			}
			if diff := cmp.Diff([]message.SyncAllReport{tt.wantReport}, reports); diff != "" {
				t.Errorf("reports (-want +got):\n%s", diff)
			}
		})
	}
}

// A result arriving late from a stopped run neither counts towards nor
// advances the run under way.
func TestSyncing_ResultFromAnEarlierRun_IsIgnored(t *testing.T) {
	t.Parallel()
	m, send := newSyncing(t)
	send(syncAll(acmeAndGlobex))
	send(syncAll(nil)) // stop after Acme
	send(synced(m, 1, acmeAndGlobex[0], 0))
	send(syncAll(acmeAndGlobex)) // run 2
	before := viewOf(m)

	if cmd := send(synced(m, 1, acmeAndGlobex[1], 0)); cmd != nil {
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
