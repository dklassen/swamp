package tui

import (
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
