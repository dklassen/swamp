package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

func TestPostingDetailModel_New_RendersTitleAndContent(t *testing.T) {
	t.Parallel()

	p := store.Posting{ID: 1, IngestedFields: store.IngestedFields{Title: "Engineer"}}
	m := newPostingDetailModel(nil, nil, 80, 20, p, store.Application{}, false)
	if !containsAll(m.View(), "Engineer", "No application started") {
		t.Fatalf("View() = %q, want it to contain title and no-application message", m.View())
	}
}

func TestPostingDetailModel_L_ReturnsNavigateForward(t *testing.T) {
	t.Parallel()

	m := newPostingDetailModel(nil, nil, 80, 20, store.Posting{ID: 5}, store.Application{}, false)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(navigatePostingMsg)
	if !ok {
		t.Fatalf("intent = %T, want navigatePostingMsg", intent)
	}
	if got.postingID != 5 || got.direction != 1 {
		t.Fatalf("navigatePostingMsg = %+v, want {postingID:5 direction:1}", got)
	}
}

func TestPostingDetailModel_H_ReturnsNavigateBackward(t *testing.T) {
	t.Parallel()

	m := newPostingDetailModel(nil, nil, 80, 20, store.Posting{ID: 5}, store.Application{}, false)
	_, intent := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	got, ok := intent.(navigatePostingMsg)
	if !ok || got.direction != -1 {
		t.Fatalf("intent = %+v, want navigatePostingMsg{direction: -1}", intent)
	}
}

func TestPostingDetailModel_Esc_ReturnsBackToPostingListMsg(t *testing.T) {
	t.Parallel()

	m := newPostingDetailModel(nil, nil, 80, 20, store.Posting{ID: 5}, store.Application{}, false)
	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(backToPostingListMsg); !ok {
		t.Fatalf("intent = %T, want backToPostingListMsg", intent)
	}
}

func TestPostingDetailModel_A_NoApplication_ReturnsCreateCmd(t *testing.T) {
	t.Parallel()

	m := newPostingDetailModel(nil, nil, 80, 20, store.Posting{ID: 5}, store.Application{}, false)
	cmd, intent := m.Update(runeKey('a'))
	if cmd == nil {
		t.Fatal("cmd = nil, want a command that creates the application")
	}
	if intent != nil {
		t.Fatalf("intent = %v, want nil", intent)
	}
}

func TestPostingDetailModel_A_HasApplication_NoOp(t *testing.T) {
	t.Parallel()

	m := newPostingDetailModel(nil, documents.NewStore(t.TempDir()), 80, 20, store.Posting{ID: 5}, store.Application{}, true)
	cmd, intent := m.Update(runeKey('a'))
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (application already exists)", cmd, intent)
	}
}

func TestPostingDetailModel_S_HasApplication_ReturnsEnterStatusMsg(t *testing.T) {
	t.Parallel()

	app := store.Application{PostingID: 5, Status: store.ApplicationStatusSubmitted}
	m := newPostingDetailModel(nil, documents.NewStore(t.TempDir()), 80, 20, store.Posting{ID: 5}, app, true)
	cmd, intent := m.Update(runeKey('s'))
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(enterApplicationStatusMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterApplicationStatusMsg", intent)
	}
	if got.postingID != 5 || got.currentStatus != store.ApplicationStatusSubmitted {
		t.Fatalf("enterApplicationStatusMsg = %+v, want postingID=5 currentStatus=%s", got, store.ApplicationStatusSubmitted)
	}
}

func TestPostingDetailModel_S_NoApplication_NoOp(t *testing.T) {
	t.Parallel()

	m := newPostingDetailModel(nil, nil, 80, 20, store.Posting{ID: 5}, store.Application{}, false)
	cmd, intent := m.Update(runeKey('s'))
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (no application yet)", cmd, intent)
	}
}

func TestPostingDetailModel_N_HasApplication_ReturnsEnterNotesMsg(t *testing.T) {
	t.Parallel()

	app := store.Application{PostingID: 5, Notes: "existing notes"}
	m := newPostingDetailModel(nil, documents.NewStore(t.TempDir()), 80, 20, store.Posting{ID: 5}, app, true)
	cmd, intent := m.Update(runeKey('n'))
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(enterApplicationNotesMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterApplicationNotesMsg", intent)
	}
	if got.postingID != 5 || got.currentNotes != "existing notes" {
		t.Fatalf("enterApplicationNotesMsg = %+v, want postingID=5 currentNotes=%q", got, "existing notes")
	}
}

func TestPostingDetailModel_R_HasApplication_ReturnsEnterDocumentReviewSelectMsg(t *testing.T) {
	t.Parallel()

	app := store.Application{ID: 9, PostingID: 5}
	m := newPostingDetailModel(nil, documents.NewStore(t.TempDir()), 80, 20, store.Posting{ID: 5}, app, true)
	cmd, intent := m.Update(runeKey('r'))
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(enterDocumentReviewSelectMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterDocumentReviewSelectMsg", intent)
	}
	if got.postingID != 5 || got.applicationID != 9 {
		t.Fatalf("enterDocumentReviewSelectMsg = %+v, want postingID=5 applicationID=9", got)
	}
}

func TestPostingDetailModel_R_NoApplication_NoOp(t *testing.T) {
	t.Parallel()

	m := newPostingDetailModel(nil, nil, 80, 20, store.Posting{ID: 5}, store.Application{}, false)
	cmd, intent := m.Update(runeKey('r'))
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil (no application yet)", cmd, intent)
	}
}

func TestPostingDetailModel_Resize_RebuildsViewportAtNewDimensions(t *testing.T) {
	t.Parallel()

	m := newPostingDetailModel(nil, nil, 80, 20, store.Posting{ID: 1, IngestedFields: store.IngestedFields{Title: "Engineer"}}, store.Application{}, false)
	m.resize(40, 10)
	if m.viewport.Width != 40 || m.viewport.Height != 10 {
		t.Fatalf("viewport dims = %dx%d, want 40x10", m.viewport.Width, m.viewport.Height)
	}
	if !strings.Contains(m.View(), "Engineer") {
		t.Fatalf("View() after resize = %q, want it to still contain the title", m.View())
	}
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
