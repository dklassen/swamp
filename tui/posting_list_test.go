package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dklassen/swamp/store"
)

func testPostingListSnapshot() postingListSnapshot {
	return postingListSnapshot{
		postings: []store.Posting{
			{ID: 1, IngestedFields: store.IngestedFields{Title: "Engineer"}},
			{ID: 2, IngestedFields: store.IngestedFields{Title: "Designer"}},
		},
		markup: map[int64]store.PostingMarkup{},
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

func TestTruncateCol(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"shorter than max is unchanged", "Engineer", 10, "Engineer"},
		{"equal to max is unchanged", "Engineer", 8, "Engineer"},
		{"longer than max is truncated with ellipsis", "Senior Backend Engineer", 10, "Senior Ba…"},
		{"empty string is unchanged", "", 10, ""},
		{"wide (double-width) runes truncate by display width, not rune count", "シニアコンサルティングエンジニア", 10, "シニアコ…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncateCol(tt.s, tt.max)
			if got != tt.want {
				t.Fatalf("truncateCol(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
			if w := lipgloss.Width(got); w > tt.max {
				t.Fatalf("truncateCol(%q, %d) width = %d, want <= %d", tt.s, tt.max, w, tt.max)
			}
		})
	}
}

func TestPostingListModel_View_ShowsCompanyDescription(t *testing.T) {
	t.Parallel()

	m := newPostingListModel(nil)
	snap := testPostingListSnapshot()
	snap.companyDescription = "Acme builds rockets for roadrunner enthusiasts."

	got := m.View(snap, 0, 40)
	if !strings.Contains(got, snap.companyDescription) {
		t.Errorf("View missing company description %q:\n%s", snap.companyDescription, got)
	}
}

// The description line costs the table exactly one row, so it must render
// as exactly one line.
func TestPostingListModel_View_DescriptionAddsExactlyOneLine(t *testing.T) {
	t.Parallel()

	m := newPostingListModel(nil)
	snap := testPostingListSnapshot()
	without := strings.Count(m.View(snap, 0, 40), "\n")

	snap.companyDescription = "Acme builds rockets for roadrunner enthusiasts."
	with := strings.Count(m.View(snap, 0, 40), "\n")

	if with != without+1 {
		t.Errorf("View with description = %d lines, want %d (one more than without)", with, without+1)
	}
}

// The table spans the terminal, so every list is as wide as the others
// and long titles get the room there is.
func TestPostingListModel_View_TableSpansTerminalWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		width int
		want  int
	}{
		{"narrow terminal", 80, 80},
		{"wide terminal", 160, 160},
		{"before the terminal reports its size", 0, fallbackTableWidth},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := newPostingListModel(nil)
			snap := testPostingListSnapshot()
			if got := tableWidthOf(t, m.View(snap, tt.width, 40)); got != tt.want {
				t.Errorf("table width = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPostingListModel_View_LongTitleFitsWhenThereIsRoom(t *testing.T) {
	t.Parallel()

	title := "Senior Software Engineer, Payments Infrastructure and Reliability (Remote)"
	tests := []struct {
		name     string
		width    int
		wantFull bool
	}{
		{"wide terminal shows it in full", 160, true},
		{"narrow terminal cuts it off", 80, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := newPostingListModel(nil)
			snap := testPostingListSnapshot()
			snap.postings[0].Title = title
			view := m.View(snap, tt.width, 40)
			if got := strings.Contains(view, title); got != tt.wantFull {
				t.Errorf("full title in view = %v, want %v:\n%s", got, tt.wantFull, view)
			}
			if !tt.wantFull && !strings.Contains(view, "…") {
				t.Errorf("cut-off title has no ellipsis:\n%s", view)
			}
		})
	}
}

// The description line is cut off at the table's width, not short of it.
func TestPostingListModel_View_DescriptionUsesTableWidth(t *testing.T) {
	t.Parallel()

	m := newPostingListModel(nil)
	snap := testPostingListSnapshot()
	snap.companyDescription = strings.Repeat("Acme builds rockets. ", 7)
	snap.companyDescription = strings.TrimSpace(snap.companyDescription)

	view := m.View(snap, 160, 40)
	if !strings.Contains(view, snap.companyDescription) {
		t.Errorf("View at width 160 missing the %d-column description in full:\n%s", len(snap.companyDescription), view)
	}
}

func TestPostingListModel_R_AsksToRefreshTheCompany(t *testing.T) {
	t.Parallel()

	m := newPostingListModel(nil)
	cmd, intent := m.Update(runeKey('r'), testPostingListSnapshot())
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(refreshSelectedCompanyMsg); !ok {
		t.Fatalf("intent = %T, want refreshSelectedCompanyMsg", intent)
	}
}

func TestPostingListModel_View_AdvertisesRefresh(t *testing.T) {
	t.Parallel()

	m := newPostingListModel(nil)
	if got := m.View(testPostingListSnapshot(), 100, 40); !strings.Contains(got, "r: refresh") {
		t.Errorf("View() doesn't advertise r: refresh:\n%s", got)
	}
	empty := testPostingListSnapshot()
	empty.postings = nil
	if got := m.View(empty, 100, 40); !strings.Contains(got, "No postings yet. Press 'r' to refresh.") {
		t.Errorf("empty View() = %q, want it to point at r on this screen", got)
	}
}
