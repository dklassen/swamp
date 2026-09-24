package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

func TestCompanyListModel_E_ReturnsEnterCompanyEditMsg(t *testing.T) {
	t.Parallel()

	companies := []store.Company{{ID: 1, Name: "Acme"}}
	m := &companyListModel{}

	cmd, intent := m.Update(runeKey('e'), companies)
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	edit, ok := intent.(enterCompanyEditMsg)
	if !ok {
		t.Fatalf("intent = %T, want enterCompanyEditMsg", intent)
	}
	if edit.company.ID != 1 {
		t.Fatalf("enterCompanyEditMsg.company.ID = %d, want 1", edit.company.ID)
	}
}

func TestCompanyListModel_E_NoCompanies_ReturnsNothing(t *testing.T) {
	t.Parallel()

	m := &companyListModel{}

	cmd, intent := m.Update(runeKey('e'), nil)
	if cmd != nil || intent != nil {
		t.Fatalf("cmd, intent = %v, %v, want nil, nil", cmd, intent)
	}
}

func TestCompanyListModel_Esc_ReturnsBackToActiveApplicationsMsg(t *testing.T) {
	t.Parallel()

	m := &companyListModel{}

	cmd, intent := m.Update(tea.KeyMsg{Type: tea.KeyEsc}, nil)
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if _, ok := intent.(backToActiveApplicationsMsg); !ok {
		t.Fatalf("intent = %T, want backToActiveApplicationsMsg", intent)
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

func TestCompanyListModel_View_ShowsOpenPostingsAndLastFetched(t *testing.T) {
	t.Parallel()

	fetched := time.Date(2026, 9, 24, 0, 52, 0, 0, time.UTC)
	companies := []store.Company{
		{ID: 1, Name: "Mattermost", Source: "greenhouse", SourceRef: "mattermost", LastFetchedAt: fetched},
		{ID: 2, Name: "Runway", Source: "ashby", SourceRef: "runway-ml"},
	}
	openPostings := map[int64]int{1: 14}

	m := newCompanyListModel(nil, nil)
	got := m.View(companies, openPostings, 0, 40)

	for _, want := range []string{
		"Mattermost", "14", fetched.Local().Format("2006-01-02 15:04"),
		"Runway", "never",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("View missing %q:\n%s", want, got)
		}
	}
	// Board and slug are left out of the list on purpose: they don't help
	// decide where to look next.
	for _, notWant := range []string{"greenhouse", "runway-ml", "Board", "Slug"} {
		if strings.Contains(got, notWant) {
			t.Errorf("View unexpectedly contains %q:\n%s", notWant, got)
		}
	}
}

// Descriptions live in the info box (i), not the table.
func TestCompanyListModel_View_TableHasNoDescription(t *testing.T) {
	t.Parallel()

	companies := []store.Company{{ID: 1, Name: "Mattermost", Description: "Open-core collaboration platform."}}

	m := newCompanyListModel(nil, nil)
	got := m.View(companies, nil, 120, 40)
	for _, notWant := range []string{"Description", "Open-core collaboration platform."} {
		if strings.Contains(got, notWant) {
			t.Errorf("View with the info box closed contains %q:\n%s", notWant, got)
		}
	}
}

// Moving between companies with no description, a short one or a long one
// never changes the list's height, with the info box open or closed.
func TestCompanyListModel_View_SameHeightWhateverTheSelectedDescription(t *testing.T) {
	t.Parallel()

	companies := []store.Company{
		{ID: 1, Name: "Blank"},
		{ID: 2, Name: "Short", Description: "Makes widgets."},
		{ID: 3, Name: "Long", Description: strings.Repeat("A company with a very long description that goes on and on. ", 20)},
	}

	for _, infoOpen := range []bool{false, true} {
		m := newCompanyListModel(nil, nil)
		if infoOpen {
			m.Update(runeKey('i'), companies)
		}
		var heights []int
		for range companies {
			heights = append(heights, strings.Count(m.View(companies, nil, 120, 30), "\n"))
			m.Update(tea.KeyMsg{Type: tea.KeyDown}, companies)
		}
		for i, h := range heights {
			if h != heights[0] {
				t.Errorf("info open=%v: View height with %s selected = %d lines, want %d (same as %s)", infoOpen, companies[i].Name, h, heights[0], companies[0].Name)
			}
		}
	}
}

// Column widths are fixed, so scrolling to a window of shorter names (or
// only never-fetched companies) doesn't make the table change width.
func TestCompanyListModel_View_SameWidthWhateverIsScrolledIntoView(t *testing.T) {
	t.Parallel()

	companies := []store.Company{
		{ID: 1, Name: "Wikimedia Foundation", LastFetchedAt: time.Date(2026, 9, 24, 1, 9, 0, 0, time.UTC)},
		{ID: 2, Name: "Rewind"},
		{ID: 3, Name: "Zapier"},
	}

	m := newCompanyListModel(nil, nil)
	// A tiny budget shows one company at a time, so each cursor position
	// scrolls a different name into view.
	listRows := postingTableChromeLines + 1
	var widths []int
	for range companies {
		firstLine := strings.SplitN(m.View(companies, nil, 0, listRows), "╭", 2)[1]
		firstLine = strings.SplitN(firstLine, "\n", 2)[0]
		widths = append(widths, lipgloss.Width(firstLine))
		m.Update(tea.KeyMsg{Type: tea.KeyDown}, companies)
	}
	for i, w := range widths {
		if w != widths[0] {
			t.Errorf("table width with %s in view = %d, want %d (same as %s)", companies[i].Name, w, widths[0], companies[0].Name)
		}
	}
}

func TestCompanyListModel_I_TogglesInfoBoxWithFullDescription(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("Open-core collaboration and workflow automation. ", 3)
	companies := []store.Company{
		{ID: 1, Name: "Mattermost", Source: "greenhouse", SourceRef: "mattermost", Description: long},
	}
	// The box wraps the description, so compare with whitespace squashed.
	squash := func(s string) string {
		return strings.Join(strings.Fields(strings.NewReplacer("│", " ", "╭", " ", "╮", " ", "─", " ").Replace(s)), " ")
	}

	m := newCompanyListModel(nil, nil)
	if got := m.View(companies, nil, 120, 40); strings.Contains(got, "greenhouse/mattermost") {
		t.Fatalf("info box should start closed:\n%s", got)
	}

	cmd, intent := m.Update(runeKey('i'), companies)
	if cmd != nil || intent != nil {
		t.Fatalf("Update on 'i' = (%v, %v), want (nil, nil): the box is this screen's own state", cmd, intent)
	}
	got := m.View(companies, nil, 120, 40)
	for _, want := range []string{"Mattermost · greenhouse/mattermost", strings.TrimSpace(long)} {
		if !strings.Contains(squash(got), want) {
			t.Errorf("open info box missing %q:\n%s", want, got)
		}
	}

	m.Update(runeKey('i'), companies)
	if got := m.View(companies, nil, 120, 40); strings.Contains(got, "greenhouse/mattermost") {
		t.Errorf("second 'i' should close the info box:\n%s", got)
	}
}

// With more companies than fit, opening the info box takes its lines from
// the table rather than making the view taller than the space it's given.
func TestCompanyListModel_View_InfoBoxTakesItsSpaceFromTheTable(t *testing.T) {
	t.Parallel()

	var companies []store.Company
	for i := range 30 {
		companies = append(companies, store.Company{ID: int64(i + 1), Name: fmt.Sprintf("Company %02d", i), Description: "Makes widgets."})
	}

	m := newCompanyListModel(nil, nil)
	closed := strings.Count(m.View(companies, nil, 120, 20), "\n")
	m.Update(runeKey('i'), companies)
	open := strings.Count(m.View(companies, nil, 120, 20), "\n")

	if open != closed {
		t.Errorf("View with info box open = %d lines, want %d (same as closed)", open, closed)
	}
}
