package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/filter"
	"github.com/dklassen/swamp/store"
)

func newTestFilterSelectModel() *filterSelectModel {
	m := newFilterSelectModel(1, "Acme",
		[]string{"Engineering", "Sales"},
		[]string{"Remote", "Onsite"},
		[]store.CompanyFilter{{Field: filter.FieldDepartment, Value: "Sales"}},
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
	if !ok || field != filter.FieldDepartment || value != "Engineering" {
		t.Fatalf("itemAtCursor() at 0 = %q, %q, %v, want department, Engineering, true", field, value, ok)
	}

	m.cursor = 2 // first location
	field, value, ok = m.itemAtCursor()
	if !ok || field != filter.FieldLocation || value != "Remote" {
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

func TestFilterSelectModel_Enter_ReturnsSaveFilterSelectionMsg(t *testing.T) {
	t.Parallel()

	m := newTestFilterSelectModel()
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil (saving is App's job, not this model's)", cmd)
	}
	got, ok := intent.(saveFilterSelectionMsg)
	if !ok {
		t.Fatalf("intent = %T, want saveFilterSelectionMsg", intent)
	}
	if len(got.departments) != 1 || got.departments[0] != "Sales" {
		t.Fatalf("departments = %v, want [Sales] (pre-seeded existing filter)", got.departments)
	}
	if len(got.locations) != 0 {
		t.Fatalf("locations = %v, want empty", got.locations)
	}
}

func TestFilterWindow(t *testing.T) {
	tests := []struct {
		name                                                 string
		cursor, numDepts, numLocs, rows                      int
		wantDeptStart, wantDeptEnd, wantLocStart, wantLocEnd int
	}{
		{
			name:          "fits everything returns full range for both groups",
			cursor:        1,
			numDepts:      2,
			numLocs:       2,
			rows:          10,
			wantDeptStart: 0, wantDeptEnd: 2,
			wantLocStart: 0, wantLocEnd: 2,
		},
		{
			// Before the first tea.WindowSizeMsg arrives, height is 0 --
			// don't hide anything (mirrors visibleWindow's own
			// zero-rows behavior).
			name:          "zero rows returns full range for both groups",
			cursor:        4,
			numDepts:      2,
			numLocs:       3,
			rows:          0,
			wantDeptStart: 0, wantDeptEnd: 2,
			wantLocStart: 0, wantLocEnd: 3,
		},
		{
			// cursor 10 = location index 8 (10 - 2 departments) of 20
			// locations -- window has scrolled past all departments
			// (empty dept range), 5 rows centered on location index 8.
			name:          "cursor deep in locations hides department group entirely",
			cursor:        10,
			numDepts:      2,
			numLocs:       20,
			rows:          5,
			wantDeptStart: 2, wantDeptEnd: 2,
			wantLocStart: 6, wantLocEnd: 11,
		},
		{
			// Window hasn't reached locations yet (empty loc range); 5
			// rows starting at the top of 20 departments.
			name:          "cursor in departments hides location group entirely",
			cursor:        1,
			numDepts:      20,
			numLocs:       5,
			rows:          5,
			wantDeptStart: 0, wantDeptEnd: 5,
			wantLocStart: 0, wantLocEnd: 0,
		},
		{
			// cursor 2 = last department (index 2) of 3 departments + 3
			// locations, 4 rows -- window should show all 3 departments
			// and the first location, keeping the cursor visible
			// without hiding either header unnecessarily.
			name:          "window straddles both groups shows part of each",
			cursor:        2,
			numDepts:      3,
			numLocs:       3,
			rows:          4,
			wantDeptStart: 0, wantDeptEnd: 3,
			wantLocStart: 0, wantLocEnd: 1,
		},
		{
			name:          "no departments treats group as empty",
			cursor:        2,
			numDepts:      0,
			numLocs:       5,
			rows:          3,
			wantDeptStart: 0, wantDeptEnd: 0,
			wantLocStart: 1, wantLocEnd: 4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			deptStart, deptEnd, locStart, locEnd := filterWindow(tt.cursor, tt.numDepts, tt.numLocs, tt.rows)
			if deptStart != tt.wantDeptStart || deptEnd != tt.wantDeptEnd {
				t.Errorf("dept range = (%d, %d), want (%d, %d)", deptStart, deptEnd, tt.wantDeptStart, tt.wantDeptEnd)
			}
			if locStart != tt.wantLocStart || locEnd != tt.wantLocEnd {
				t.Errorf("loc range = (%d, %d), want (%d, %d)", locStart, locEnd, tt.wantLocStart, tt.wantLocEnd)
			}
		})
	}
}
