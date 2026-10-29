package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

var testStatusApplication = message.Application{ID: 7, PostingID: 5, Status: message.ApplicationStarted}

// statusPickerOn is a picker opened on testStatusApplication at status.
func statusPickerOn(applications *service.ApplicationService, status application.Status) *applicationStatusModel {
	app := testStatusApplication
	app.Status = statusSnapshots[status]
	return newApplicationStatusModel(applications, app)
}

func TestApplicationStatusModel_OpensWithTheCursorOnTheNavsStatus(t *testing.T) {
	t.Parallel()

	m := statusPickerOn(nil, application.Submitted)
	if want := applicationStatusIndex(application.Submitted); m.cursor != want {
		t.Fatalf("cursor = %d, want %d (index of %s)", m.cursor, want, application.Submitted)
	}
}

func TestApplicationStatusModel_CursorMovement_ClampsToStatusCount(t *testing.T) {
	t.Parallel()

	m := statusPickerOn(nil, application.Started)
	for i := 0; i < len(applicationStatuses)+5; i++ {
		m.keyPressed(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.cursor != len(applicationStatuses)-1 {
		t.Fatalf("cursor after many downs = %d, want %d (clamped)", m.cursor, len(applicationStatuses)-1)
	}

	m.keyPressed(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != len(applicationStatuses)-2 {
		t.Fatalf("cursor after one up = %d, want %d", m.cursor, len(applicationStatuses)-2)
	}
}

func TestApplicationStatusModel_EscOrB_PopsAndClearsTheBanner(t *testing.T) {
	t.Parallel()

	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEsc}, runeKey('b')} {
		t.Run(key.String(), func(t *testing.T) {
			t.Parallel()
			m := statusPickerOn(nil, application.Started)

			got := runCmd(t, m.keyPressed(key))
			if !popsIn(got, m.Instance()) {
				t.Errorf("%s sent %#v, want a pop of the picker", key, got)
			}
			if status, ok := statusIn(got); !ok || status != "" {
				t.Errorf("%s sent %#v, want an empty Notify", key, got)
			}
		})
	}
}

func TestApplicationStatusModel_Enter_SavesTheStatusAtTheCursor(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	stored := mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer").ID)
	m := newApplicationStatusModel(s.Applications, applicationSnapshot(stored))
	for m.cursor != applicationStatusIndex(application.Interviewing) {
		m.keyPressed(runeKey('j'))
	}

	got := runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}))

	if len(got) != 1 {
		t.Fatalf("enter sent %#v, want one Changed", got)
	}
	changed, ok := got[0].(message.Event[message.Changed[message.Application]])
	if !ok {
		t.Fatalf("enter sent %T, want a Changed of the application", got[0])
	}
	assertEventFrom(t, changed.Meta, message.Source{Instance: m.Instance()})
	if changed.Payload.Before.Status != statusSnapshots[application.Started] || changed.Payload.After.Status != statusSnapshots[application.Interviewing] {
		t.Errorf("status %v -> %v, want %v -> %v", changed.Payload.Before.Status, changed.Payload.After.Status, statusSnapshots[application.Started], statusSnapshots[application.Interviewing])
	}
	saved, err := s.Applications.Get(t.Context(), stored.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if saved.Status != application.Interviewing {
		t.Errorf("saved status = %s, want %s", saved.Status, application.Interviewing)
	}
}

// The picker closes on its own save only. Its own failure leaves it open
// for another go, and so does a write from anywhere else.
func TestApplicationStatusModel_ClosesOnItsOwnSaveOnly(t *testing.T) {
	t.Parallel()

	before := testStatusApplication
	after := before
	after.Status = message.ApplicationInterviewing
	saved := func(instance message.ScreenInstance) tea.Msg {
		return message.Updated(message.Source{Instance: instance}, before.ID, time.Now().UTC(), before, after)
	}
	tests := []struct {
		name      string
		result    func(m *applicationStatusModel) tea.Msg
		wantClose bool
	}{
		{name: "its own, saved", result: func(m *applicationStatusModel) tea.Msg { return saved(m.Instance()) }, wantClose: true},
		{name: "its own, failed", result: func(m *applicationStatusModel) tea.Msg {
			return message.New(message.Source{Instance: m.Instance()}, message.Subject{Type: message.TypeApplication, ID: before.ID},
				message.OpUpdated, time.Now().UTC(), model.ErrorOf(errors.New("disk full")))
		}},
		{name: "another screen's", result: func(*applicationStatusModel) tea.Msg {
			return saved(statusPickerOn(nil, application.Started).Instance())
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := statusPickerOn(nil, application.Started)
			r := model.NewMsgRouter()
			if err := r.Register(m); err != nil {
				t.Fatalf("Register: %v", err)
			}

			got := runCmd(t, r.Dispatch(tt.result(m)))

			if closed := popsIn(got, m.Instance()); closed != tt.wantClose {
				t.Errorf("sent %#v, want a pop: %v", got, tt.wantClose)
			}
			if status, ok := statusIn(got); tt.wantClose && (!ok || status != "") {
				t.Errorf("sent %#v, want an empty Notify with the pop", got)
			}
		})
	}
}

// The picker opens on the status the home list shows, which follows other
// processes' writes, so saving can't put back a status they changed.
func TestStatusPicker_OpensOnAStatusChangedElsewhere(t *testing.T) {
	t.Parallel()

	app, target := deleteTestApp(t)
	app.WithChangeLog(0, "tui:1")
	if err := updateApplicationStatusIn(t.Context(), storeOf(app), target.PostingID, application.Interviewing); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	app = sendKeyAndApply(t, app, changesMsg{view: app.currentScreen, events: []change.Event{{Table: "applications", RowID: target.ID, Op: "update", Origin: "mcp:7"}}})

	app = sendKeyAndApply(t, app, runeKey('s'))

	if want := applicationStatusIndex(application.Interviewing); statusPicker(t, app).cursor != want {
		t.Errorf("cursor = %d (%s), want %d (%s, as the other process left it)", statusPicker(t, app).cursor, applicationStatuses[statusPicker(t, app).cursor], want, application.Interviewing)
	}
}

// A status the store refuses, here for an application deleted since the
// picker opened, shows why and leaves the picker open.
func TestStatusPicker_SaveFails_ShowsWhyAndStaysOpen(t *testing.T) {
	t.Parallel()

	app, target := deleteTestApp(t)
	app = sendKeyAndApply(t, app, runeKey('s'))
	if _, err := storeOf(app).Applications.Delete(t.Context(), target.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})

	if app.currentScreen != screenApplicationStatusSelect {
		t.Errorf("screen = %v, want the status picker, still open", app.currentScreen)
	}
	if app.err == nil {
		t.Error("err = nil, want why the save failed")
	}
}

func TestApplicationStatusModel_Handlers_FailOnTheWrongMessage(t *testing.T) {
	t.Parallel()
	assertHandlersFailOnWrongMessage(t, statusPickerOn(nil, application.Started))
}

// TestApplicationStatusLabel pins the display text for every status in
// one place. Tested directly rather than only through the screens that
// render it: this mapping is the single source of display truth for four
// separate call sites, and covering all seven values through each of
// them would say the same thing four times over.
func TestApplicationStatusLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status application.Status
		want   string
	}{
		{application.Started, "Started"},
		{application.Submitted, "Submitted"},
		{application.Interviewing, "Interviewing"},
		{application.Rejected, "Rejected"},
		{application.OfferReceived, "Offer received"},
		{application.OfferAccepted, "Offer accepted"},
		{application.OfferDeclined, "Offer declined"},
		{application.PostingClosed, "Posting closed"},
		{application.Withdrawn, "Withdrawn"},
	}

	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			t.Parallel()
			if got := applicationStatusLabel(tt.status); got != tt.want {
				t.Errorf("applicationStatusLabel(%s) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

// TestApplicationStatusLabel_CoversEveryStatus fails if a status is
// added to the enum without a label, rather than letting it fall through
// to the raw DB string in the UI.
func TestApplicationStatusLabel_CoversEveryStatus(t *testing.T) {
	t.Parallel()

	for _, status := range application.All() {
		if got := applicationStatusLabel(status); got == status.String() {
			t.Errorf("applicationStatusLabel(%s) returned the raw enum value %q -- needs a display label", status, got)
		}
	}
}

func TestApplicationStatusModel_View_ShowsLabelsNotEnumValues(t *testing.T) {
	t.Parallel()

	m := statusPickerOn(nil, application.Started)
	got := m.View().Content

	if !strings.Contains(got, "Offer received") {
		t.Errorf("View() = %q, want human-readable status labels", got)
	}
	if strings.Contains(got, "offer_received") {
		t.Errorf("View() leaks the raw enum value \"offer_received\" into the picker")
	}
}

// TestApplicationStatusModel_View_OffersWithdrawn is the whole manual
// close path: the picker renders every status in the enum, so adding
// withdrawn is what gives the user a way to end an application they've
// changed their mind about. If the picker ever stops offering the full
// set, this is the test that says the path is gone.
func TestApplicationStatusModel_View_OffersWithdrawn(t *testing.T) {
	t.Parallel()

	m := statusPickerOn(nil, application.Started)

	if got := m.View().Content; !strings.Contains(got, "Withdrawn") {
		t.Errorf("View() = %q, want Withdrawn offered -- it's the only way to close an application by hand without claiming a rejection that never happened", got)
	}
}
