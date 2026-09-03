package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
)

func testActiveApplications() []store.ApplicationView {
	return []store.ApplicationView{
		{
			Application: store.Application{ID: 10, Status: store.ApplicationStatusStarted},
			Posting:     store.Posting{ID: 1, IngestedFields: store.IngestedFields{Title: "Engineer"}},
			CompanyName: "Acme",
		},
		{
			Application: store.Application{ID: 20, Status: store.ApplicationStatusInterviewing},
			Posting:     store.Posting{ID: 2, IngestedFields: store.IngestedFields{Title: "Designer"}},
			CompanyName: "Globex",
		},
	}
}

func TestActiveApplicationListModel_CursorMovement_ClampsToApps(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel()
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

func TestActiveApplicationListModel_View_ShowsReviewGlyphsPerApplication(t *testing.T) {
	t.Parallel()

	apps := testActiveApplications()
	apps[0].LatestReviews = map[store.DocumentType]store.DocumentReview{
		store.DocumentTypeCoverLetter: {Outcome: store.ReviewOutcomeFlagged},
		store.DocumentTypeResume:      {Outcome: store.ReviewOutcomePassed},
	}
	// apps[1] is left with no LatestReviews -- neither document reviewed yet.

	m := newActiveApplicationListModel()
	got := m.View(apps, 20)

	if !containsAll(got, "CL:✗ R:✓", "CL:- R:-") {
		t.Fatalf("View() = %q, want CL:✗ R:✓ for the reviewed application and CL:- R:- for the unreviewed one", got)
	}
}

func TestActiveApplicationListModel_C_ReturnsBackToCompanyListMsg(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel()
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

	m := newActiveApplicationListModel()
	cmd, _ := m.Update(runeKey('q'), testActiveApplications())
	if cmd == nil {
		t.Fatal("Update on 'q' returned nil Cmd, want tea.Quit")
	}
}

func TestActiveApplicationListModel_S_ReturnsEnterApplicationStatusMsg(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel()
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

func TestActiveApplicationListModel_Enter_ReturnsEnterApplicationDetailMsg(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel()
	apps := testActiveApplications()

	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEnter}, apps)
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	got, ok := intent.(enterApplicationDetailMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterApplicationDetailMsg", intent)
	}
	if got.application.ID != apps[0].ID {
		t.Fatalf("application.ID = %d, want %d (application at cursor 0)", got.application.ID, apps[0].ID)
	}
}

func TestActiveApplicationListModel_EmptyList_KeysDoNotPanic(t *testing.T) {
	t.Parallel()

	m := newActiveApplicationListModel()
	for _, key := range []tea.KeyMsg{runeKey('s'), {Type: tea.KeyEnter}} {
		if cmd, intent := m.Update(key, nil); cmd != nil || intent != nil {
			t.Fatalf("Update(%v) on empty list = %v, %v, want nil, nil", key, cmd, intent)
		}
	}
}
