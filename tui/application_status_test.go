package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/domain/application"
)

func TestApplicationStatusModel_New_SeedsCursorFromCurrentStatus(t *testing.T) {
	t.Parallel()

	m := newApplicationStatusModel(nil, 7, application.Submitted)
	want := applicationStatusIndex(application.Submitted)
	if m.cursor != want {
		t.Fatalf("cursor = %d, want %d (index of %s)", m.cursor, want, application.Submitted)
	}
}

func TestApplicationStatusModel_CursorMovement_ClampsToStatusCount(t *testing.T) {
	t.Parallel()

	m := newApplicationStatusModel(nil, 7, application.Started)
	for i := 0; i < len(applicationStatuses)+5; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.cursor != len(applicationStatuses)-1 {
		t.Fatalf("cursor after many downs = %d, want %d (clamped)", m.cursor, len(applicationStatuses)-1)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != len(applicationStatuses)-2 {
		t.Fatalf("cursor after one up = %d, want %d", m.cursor, len(applicationStatuses)-2)
	}
}

func TestApplicationStatusModel_Esc_ReturnsCancelMsg(t *testing.T) {
	t.Parallel()

	m := newApplicationStatusModel(nil, 7, application.Started)
	cmd, intent := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(cancelApplicationStatusMsg); !ok {
		t.Fatalf("intent = %T, want cancelApplicationStatusMsg", intent)
	}
}

func TestApplicationStatusModel_Enter_ReturnsUpdateCmd(t *testing.T) {
	t.Parallel()

	m := newApplicationStatusModel(nil, 7, application.Started)
	cmd, intent := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that updates the application status")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
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

	m := newApplicationStatusModel(nil, 7, application.Started)
	got := m.View()

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

	m := newApplicationStatusModel(nil, 7, application.Started)

	if got := m.View(); !strings.Contains(got, "Withdrawn") {
		t.Errorf("View() = %q, want Withdrawn offered -- it's the only way to close an application by hand without claiming a rejection that never happened", got)
	}
}

// TestStatusForm_ShowsTheStatusAsItIsNow: another process (sync closing
// the posting, an agent) can change the status after the home list
// loaded. The form must start from the stored status, or saving it would
// silently undo that change.
func TestStatusForm_ShowsTheStatusAsItIsNow(t *testing.T) {
	t.Parallel()

	app, target := deleteTestApp(t)
	if _, err := updateApplicationStatusIn(context.Background(), storeOf(app), target.PostingID, application.Interviewing); err != nil {
		t.Fatalf("application.UpdateStatus: %v", err)
	}

	app, cmd := sendKey(app, runeKey('s'))
	app = applyCmd(t, app, cmd)

	if app.currentScreen != screenApplicationStatusSelect {
		t.Fatalf("screen after s = %v, want the status form", app.currentScreen)
	}
	if want := applicationStatusIndex(application.Interviewing); app.applicationStatus.cursor != want {
		t.Errorf("cursor = %d (%s), want %d (%s, the stored status)", app.applicationStatus.cursor, applicationStatuses[app.applicationStatus.cursor], want, application.Interviewing)
	}
}

func TestStatusForm_ApplicationDeletedMeanwhile_StaysAndSaysSo(t *testing.T) {
	t.Parallel()

	app, target := deleteTestApp(t)
	if err := storeOf(app).Applications.Delete(context.Background(), target.ID); err != nil {
		t.Fatalf("application.Delete: %v", err)
	}

	app = sendKeyAndApply(t, app, runeKey('s'))

	if app.currentScreen != screenActiveApplications {
		t.Errorf("screen = %v, want the home list (no status left to change)", app.currentScreen)
	}
	if app.err == nil || !strings.Contains(app.err.Error(), "deleted in the meantime") {
		t.Errorf("err = %v, want one saying it was deleted", app.err)
	}
}
