package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
)

func TestApplicationStatusModel_New_SeedsCursorFromCurrentStatus(t *testing.T) {
	t.Parallel()

	m := newApplicationStatusModel(nil, 7, store.ApplicationStatusSubmitted)
	want := applicationStatusIndex(store.ApplicationStatusSubmitted)
	if m.cursor != want {
		t.Fatalf("cursor = %d, want %d (index of %s)", m.cursor, want, store.ApplicationStatusSubmitted)
	}
}

func TestApplicationStatusModel_CursorMovement_ClampsToStatusCount(t *testing.T) {
	t.Parallel()

	m := newApplicationStatusModel(nil, 7, store.ApplicationStatusStarted)
	for i := 0; i < len(applicationStatuses)+5; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.cursor != len(applicationStatuses)-1 {
		t.Fatalf("cursor after many downs = %d, want %d (clamped)", m.cursor, len(applicationStatuses)-1)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != len(applicationStatuses)-2 {
		t.Fatalf("cursor after one up = %d, want %d", m.cursor, len(applicationStatuses)-2)
	}
}

func TestApplicationStatusModel_Esc_ReturnsCancelMsg(t *testing.T) {
	t.Parallel()

	m := newApplicationStatusModel(nil, 7, store.ApplicationStatusStarted)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(cancelApplicationStatusMsg); !ok {
		t.Fatalf("intent = %T, want cancelApplicationStatusMsg", intent)
	}
}

func TestApplicationStatusModel_Enter_ReturnsUpdateCmd(t *testing.T) {
	t.Parallel()

	m := newApplicationStatusModel(nil, 7, store.ApplicationStatusStarted)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
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
		status store.ApplicationStatus
		want   string
	}{
		{store.ApplicationStatusStarted, "Started"},
		{store.ApplicationStatusSubmitted, "Submitted"},
		{store.ApplicationStatusInterviewing, "Interviewing"},
		{store.ApplicationStatusRejected, "Rejected"},
		{store.ApplicationStatusOfferReceived, "Offer received"},
		{store.ApplicationStatusOfferAccepted, "Offer accepted"},
		{store.ApplicationStatusOfferDeclined, "Offer declined"},
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

	for _, status := range store.ApplicationStatuses() {
		if got := applicationStatusLabel(status); got == status.String() {
			t.Errorf("applicationStatusLabel(%s) returned the raw enum value %q -- needs a display label", status, got)
		}
	}
}

func TestApplicationStatusModel_View_ShowsLabelsNotEnumValues(t *testing.T) {
	t.Parallel()

	m := newApplicationStatusModel(nil, 7, store.ApplicationStatusStarted)
	got := m.View()

	if !strings.Contains(got, "Offer received") {
		t.Errorf("View() = %q, want human-readable status labels", got)
	}
	if strings.Contains(got, "offer_received") {
		t.Errorf("View() leaks the raw enum value \"offer_received\" into the picker")
	}
}
