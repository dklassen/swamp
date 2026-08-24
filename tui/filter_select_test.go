package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
)

func newTestFilterSelectModel() *filterSelectModel {
	m := newFilterSelectModel(nil, 1, "Acme",
		[]string{"Engineering", "Sales"},
		[]string{"Remote", "Onsite"},
		[]store.CompanyFilter{{Field: "department", Value: "Sales"}},
	)
	return &m
}

func TestFilterSelectModel_New_SeedsExistingSelections(t *testing.T) {
	t.Parallel()

	m := newTestFilterSelectModel()
	if !m.selectedDepartments["Sales"] {
		t.Fatal("Sales should be pre-selected from existing filters")
	}
	if m.selectedDepartments["Engineering"] {
		t.Fatal("Engineering should not be pre-selected")
	}
}

func TestFilterSelectModel_ItemAtCursor_SpansDepartmentsThenLocations(t *testing.T) {
	t.Parallel()

	m := newTestFilterSelectModel()

	field, value, ok := m.itemAtCursor()
	if !ok || field != "department" || value != "Engineering" {
		t.Fatalf("itemAtCursor() at 0 = %q, %q, %v, want department, Engineering, true", field, value, ok)
	}

	m.cursor = 2 // first location
	field, value, ok = m.itemAtCursor()
	if !ok || field != "location" || value != "Remote" {
		t.Fatalf("itemAtCursor() at 2 = %q, %q, %v, want location, Remote, true", field, value, ok)
	}

	m.cursor = 4 // out of range
	if _, _, ok = m.itemAtCursor(); ok {
		t.Fatal("itemAtCursor() out of range should return ok=false")
	}
}

func TestFilterSelectModel_CursorMovement_ClampsToTotal(t *testing.T) {
	t.Parallel()

	m := newTestFilterSelectModel()
	for i := 0; i < 10; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.cursor != 3 { // 2 departments + 2 locations - 1
		t.Fatalf("cursor after many downs = %d, want 3 (clamped)", m.cursor)
	}
}

func TestFilterSelectModel_Space_TogglesSelection(t *testing.T) {
	t.Parallel()

	m := newTestFilterSelectModel()
	m.Update(tea.KeyMsg{Type: tea.KeySpace}) // cursor 0 = Engineering
	if !m.selectedDepartments["Engineering"] {
		t.Fatal("Engineering should be selected after space")
	}
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if m.selectedDepartments["Engineering"] {
		t.Fatal("Engineering should be unselected after second space")
	}
}

func TestFilterSelectModel_Esc_ReturnsCancelMsg(t *testing.T) {
	t.Parallel()

	m := newTestFilterSelectModel()
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(cancelFilterSelectMsg); !ok {
		t.Fatalf("intent = %T, want cancelFilterSelectMsg", intent)
	}
}

func TestFilterSelectModel_Enter_ReturnsSaveCmd(t *testing.T) {
	t.Parallel()

	m := newTestFilterSelectModel()
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that saves company filters")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}
