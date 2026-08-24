package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
)

func testPostingListSnapshot() postingListSnapshot {
	return postingListSnapshot{
		companyName: "Acme",
		postings:    []store.Posting{{ID: 1, Title: "Engineer"}, {ID: 2, Title: "Designer"}},
		markup:      map[int64]store.PostingMarkup{},
	}
}

func TestPostingListModel_CursorMovement_ClampsToPostings(t *testing.T) {
	t.Parallel()

	m := newPostingListModel(nil)
	snap := testPostingListSnapshot()

	m.Update(tea.KeyMsg{Type: tea.KeyDown}, snap)
	m.Update(tea.KeyMsg{Type: tea.KeyDown}, snap)
	if m.cursor != 1 {
		t.Fatalf("cursor after two downs = %d, want 1 (clamped)", m.cursor)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyUp}, snap)
	m.Update(tea.KeyMsg{Type: tea.KeyUp}, snap)
	if m.cursor != 0 {
		t.Fatalf("cursor after two ups = %d, want 0 (clamped)", m.cursor)
	}
}

func TestPostingListModel_Enter_ReturnsEnterDetailMsg(t *testing.T) {
	t.Parallel()

	m := newPostingListModel(nil)
	snap := testPostingListSnapshot()

	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter}, snap)
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(enterPostingDetailMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterPostingDetailMsg", intent)
	}
	if got.postingID != 1 {
		t.Fatalf("postingID = %d, want 1 (posting at cursor 0)", got.postingID)
	}
}

func TestPostingListModel_Esc_ReturnsBackToCompanyListMsg(t *testing.T) {
	t.Parallel()

	m := newPostingListModel(nil)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc}, testPostingListSnapshot())
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(backToCompanyListMsg); !ok {
		t.Fatalf("intent = %T, want backToCompanyListMsg", intent)
	}
}

func TestPostingListModel_F_ReturnsEnterFilterSelectMsg(t *testing.T) {
	t.Parallel()

	m := newPostingListModel(nil)
	cmd, intent := m.Update(runeKey('f'), testPostingListSnapshot())
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(enterFilterSelectMsg); !ok {
		t.Fatalf("intent = %T, want enterFilterSelectMsg", intent)
	}
}

func TestPostingListModel_A_ReturnsToggleHideArchivedMsg(t *testing.T) {
	t.Parallel()

	m := newPostingListModel(nil)
	cmd, intent := m.Update(runeKey('A'), testPostingListSnapshot())
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(toggleHideArchivedMsg); !ok {
		t.Fatalf("intent = %T, want toggleHideArchivedMsg", intent)
	}
}

func TestPostingListModel_I_ReturnsToggleInterestedCmd(t *testing.T) {
	t.Parallel()

	m := newPostingListModel(nil)
	cmd, intent := m.Update(runeKey('i'), testPostingListSnapshot())
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that toggles interested")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}

func TestPostingListModel_ClampCursor(t *testing.T) {
	t.Parallel()

	m := &postingListModel{cursor: 1}
	m.clampCursor(1)
	if m.cursor != 0 {
		t.Fatalf("cursor after clamp = %d, want 0", m.cursor)
	}
}

func TestPostingListModel_ResetCursor(t *testing.T) {
	t.Parallel()

	m := &postingListModel{cursor: 3}
	m.resetCursor()
	if m.cursor != 0 {
		t.Fatalf("cursor after reset = %d, want 0", m.cursor)
	}
}
