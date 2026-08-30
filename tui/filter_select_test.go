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

func TestFilterWindow_FitsEverything_ReturnsFullRangeForBothGroups(t *testing.T) {
	t.Parallel()

	deptStart, deptEnd, locStart, locEnd := filterWindow(1, 2, 2, 10)
	if deptStart != 0 || deptEnd != 2 {
		t.Fatalf("dept range = (%d, %d), want (0, 2)", deptStart, deptEnd)
	}
	if locStart != 0 || locEnd != 2 {
		t.Fatalf("loc range = (%d, %d), want (0, 2)", locStart, locEnd)
	}
}

func TestFilterWindow_ZeroRows_ReturnsFullRangeForBothGroups(t *testing.T) {
	t.Parallel()

	// Before the first tea.WindowSizeMsg arrives, height is 0 -- don't
	// hide anything (mirrors visibleWindow's own zero-rows behavior).
	deptStart, deptEnd, locStart, locEnd := filterWindow(4, 2, 3, 0)
	if deptStart != 0 || deptEnd != 2 {
		t.Fatalf("dept range = (%d, %d), want (0, 2)", deptStart, deptEnd)
	}
	if locStart != 0 || locEnd != 3 {
		t.Fatalf("loc range = (%d, %d), want (0, 3)", locStart, locEnd)
	}
}

func TestFilterWindow_CursorDeepInLocations_HidesDepartmentGroupEntirely(t *testing.T) {
	t.Parallel()

	// cursor 10 = location index 8 (10 - 2 departments) of 20 locations.
	deptStart, deptEnd, locStart, locEnd := filterWindow(10, 2, 20, 5)
	if deptStart != deptEnd {
		t.Fatalf("dept range = (%d, %d), want empty -- window has scrolled past all departments", deptStart, deptEnd)
	}
	if locStart != 6 || locEnd != 11 {
		t.Fatalf("loc range = (%d, %d), want (6, 11) -- 5 rows centered on location index 8", locStart, locEnd)
	}
}

func TestFilterWindow_CursorInDepartments_HidesLocationGroupEntirely(t *testing.T) {
	t.Parallel()

	deptStart, deptEnd, locStart, locEnd := filterWindow(1, 20, 5, 5)
	if locStart != locEnd {
		t.Fatalf("loc range = (%d, %d), want empty -- window hasn't reached locations yet", locStart, locEnd)
	}
	if deptEnd-deptStart != 5 {
		t.Fatalf("dept window size = %d, want 5", deptEnd-deptStart)
	}
}

func TestFilterWindow_WindowStraddlesBothGroups_ShowsPartOfEach(t *testing.T) {
	t.Parallel()

	// cursor 2 = last department (index 2) of 3 departments + 3 locations,
	// 4 rows -- window should show all 3 departments and the first
	// location, keeping the cursor visible without hiding either header
	// unnecessarily.
	deptStart, deptEnd, locStart, locEnd := filterWindow(2, 3, 3, 4)
	if deptStart != 0 || deptEnd != 3 {
		t.Fatalf("dept range = (%d, %d), want (0, 3) -- all departments visible", deptStart, deptEnd)
	}
	if locStart != 0 || locEnd != 1 {
		t.Fatalf("loc range = (%d, %d), want (0, 1) -- first location visible", locStart, locEnd)
	}
}

func TestFilterWindow_NoDepartments_TreatsGroupAsEmpty(t *testing.T) {
	t.Parallel()

	deptStart, deptEnd, locStart, locEnd := filterWindow(2, 0, 5, 3)
	if deptStart != 0 || deptEnd != 0 {
		t.Fatalf("dept range = (%d, %d), want (0, 0) -- no departments exist", deptStart, deptEnd)
	}
	if locStart != 1 || locEnd != 4 {
		t.Fatalf("loc range = (%d, %d), want (1, 4)", locStart, locEnd)
	}
}
