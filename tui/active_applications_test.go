package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

func testActiveApplications() []store.ApplicationView {
	return []store.ApplicationView{
		{
			Application: store.Application{ID: 10, Status: store.ApplicationStatusStarted},
			Posting:     store.Posting{ID: 1, Title: "Engineer"},
			CompanyName: "Acme",
		},
		{
			Application: store.Application{ID: 20, Status: store.ApplicationStatusInterviewing},
			Posting:     store.Posting{ID: 2, Title: "Designer"},
			CompanyName: "Globex",
		},
	}
}

func TestActiveApplicationListModel_CursorMovement_ClampsToApps(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel(nil, nil)
	apps := testActiveApplications()

	m.Update(tea.KeyMsg{Type: tea.KeyDown}, apps)
	m.Update(tea.KeyMsg{Type: tea.KeyDown}, apps)
	if m.cursor != 1 {
		t.Fatalf("cursor after two downs = %d, want 1 (clamped)", m.cursor)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyUp}, apps)
	m.Update(tea.KeyMsg{Type: tea.KeyUp}, apps)
	if m.cursor != 0 {
		t.Fatalf("cursor after two ups = %d, want 0 (clamped)", m.cursor)
	}
}

func TestActiveApplicationListModel_C_ReturnsBackToCompanyListMsg(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel(nil, nil)
	cmd, intent := m.Update(runeKey('c'), testActiveApplications())
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(backToCompanyListMsg); !ok {
		t.Fatalf("intent = %T, want backToCompanyListMsg", intent)
	}
}

func TestActiveApplicationListModel_Q_ReturnsQuitCmd(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel(nil, nil)
	cmd, _ := m.Update(runeKey('q'), testActiveApplications())
	if cmd == nil {
		t.Fatal("Update on 'q' returned nil Cmd, want tea.Quit")
	}
}

func TestActiveApplicationListModel_S_ReturnsEnterApplicationStatusMsg(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel(nil, nil)
	apps := testActiveApplications()

	cmd, intent := m.Update(runeKey('s'), apps)
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(enterApplicationStatusMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterApplicationStatusMsg", intent)
	}
	if got.postingID != 1 {
		t.Fatalf("postingID = %d, want 1 (posting at cursor 0)", got.postingID)
	}
	if got.currentStatus != store.ApplicationStatusStarted {
		t.Fatalf("currentStatus = %s, want %s", got.currentStatus, store.ApplicationStatusStarted)
	}
}

func TestActiveApplicationListModel_L_ReturnsOpenCoverLetterCmd(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel(nil, documents.NewStore(t.TempDir()))
	cmd, intent := m.Update(runeKey('l'), testActiveApplications())
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that opens the cover letter in $EDITOR")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}

func TestActiveApplicationListModel_R_ReturnsOpenResumeCmd(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel(nil, documents.NewStore(t.TempDir()))
	cmd, intent := m.Update(runeKey('r'), testActiveApplications())
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that opens the resume in $EDITOR")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}

func TestActiveApplicationListModel_EmptyList_LSRDoNotPanic(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel(nil, documents.NewStore(t.TempDir()))
	for _, key := range []tea.KeyMsg{runeKey('l'), runeKey('r'), runeKey('s')} {
		if cmd, intent := m.Update(key, nil); cmd != nil || intent != nil {
			t.Fatalf("Update(%v) on empty list = %v, %v, want nil, nil", key, cmd, intent)
		}
	}
}
