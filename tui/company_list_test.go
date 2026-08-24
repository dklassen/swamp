package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
)

func TestCompanyListModel_CursorMovement(t *testing.T) {
	t.Parallel()

	companies := []store.Company{{ID: 1, Name: "Acme"}, {ID: 2, Name: "Globex"}}
	m := &companyListModel{}

	m.Update(tea.KeyMsg{Type: tea.KeyDown}, companies)
	if m.cursor != 1 {
		t.Fatalf("cursor after down = %d, want 1", m.cursor)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyDown}, companies)
	if m.cursor != 1 {
		t.Fatalf("cursor after down at bottom = %d, want 1 (clamped)", m.cursor)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyUp}, companies)
	if m.cursor != 0 {
		t.Fatalf("cursor after up = %d, want 0", m.cursor)
	}
}

func TestCompanyListModel_Enter_ReturnsSelectCompanyMsg(t *testing.T) {
	t.Parallel()

	companies := []store.Company{{ID: 1, Name: "Acme"}}
	m := &companyListModel{}

	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter}, companies)
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	sel, ok := intent.(selectCompanyMsg)
	if !ok {
		t.Fatalf("intent = %T, want selectCompanyMsg", intent)
	}
	if sel.company.ID != 1 {
		t.Fatalf("selectCompanyMsg.company.ID = %d, want 1", sel.company.ID)
	}
}

func TestCompanyListModel_Enter_NoCompanies_ReturnsNothing(t *testing.T) {
	t.Parallel()

	m := &companyListModel{}

	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter}, nil)
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil", cmd, intent)
	}
}

func TestCompanyListModel_A_ReturnsEnterCompanyFormMsg(t *testing.T) {
	t.Parallel()

	m := &companyListModel{}

	cmd, intent := m.Update(runeKey('a'), nil)
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(enterCompanyFormMsg); !ok {
		t.Fatalf("intent = %T, want enterCompanyFormMsg", intent)
	}
}

func TestCompanyListModel_ClampCursor(t *testing.T) {
	t.Parallel()

	m := &companyListModel{cursor: 2}
	m.clampCursor(1)
	if m.cursor != 0 {
		t.Fatalf("cursor after clamp = %d, want 0", m.cursor)
	}
}
