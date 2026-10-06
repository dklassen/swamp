package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestApp_Tab_SwitchesBetweenTheLists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		from screen
		want screen
	}{
		{"applications to companies", screenActiveApplications, screenCompanyList},
		{"companies to applications", screenCompanyList, screenActiveApplications},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newTestStore(t)
			app := newTestApp(t, s, newTestSyncer(s, nil))
			app.screen = tt.from
			app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyTab})
			if app.screen != tt.want {
				t.Errorf("screen after tab = %v, want %v", app.screen, tt.want)
			}
		})
	}
}

// Each tab keeps its own state: the search survives a trip to the other
// tab, and tab isn't typed into it.
func TestApp_Tab_KeepsTheCompanySearch(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app.screen = screenCompanyList
	for _, key := range []tea.KeyMsg{runeKey('/'), runeKey('g'), {Type: tea.KeyTab}} {
		app = sendKeyAndApply(t, app, key)
	}
	if app.screen != screenActiveApplications {
		t.Fatalf("screen after tab while searching = %v, want active applications", app.screen)
	}
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyTab})
	if !app.companyList.searching || app.companyList.query != "g" {
		t.Errorf("search after switching back = %v %q, want open with %q", app.companyList.searching, app.companyList.query, "g")
	}
}

// newTabsTestApp has two companies and no applications, sized to width.
func newTabsTestApp(t *testing.T, width int) *App {
	t.Helper()
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	mustCreateCompany(t, s, "Globex", "ashby", "globex")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: width, Height: 24})
	return app
}

// viewLines is the app's view without styling, one string per line.
func viewLines(app *App) []string {
	return strings.Split(ansi.Strip(app.View()), "\n")
}

func TestApp_TabBar_ShowsBothTabsWithCounts(t *testing.T) {
	t.Parallel()

	for _, sc := range []screen{screenActiveApplications, screenCompanyList} {
		app := newTabsTestApp(t, 100)
		app.screen = sc
		if got := viewLines(app)[0]; !strings.Contains(got, "Applications 0") || !strings.Contains(got, "Companies 2") {
			t.Errorf("screen %v: first line = %q, want both tabs with their counts", sc, got)
		}
	}
}

func TestApp_TabBar_UnderlinesTheTabOnScreen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		screen screen
		label  string
	}{
		{screenActiveApplications, "Applications 0"},
		{screenCompanyList, "Companies 2"},
	}
	for _, tt := range tests {
		app := newTabsTestApp(t, 120)
		app.screen = tt.screen
		lines := viewLines(app)
		tabs, rule := []rune(lines[0]), []rune(lines[1])
		if got := len(rule); got != 120 {
			t.Errorf("%s: rule is %d wide, want 120", tt.label, got)
		}
		start := strings.Index(string(tabs), tt.label)
		start = len([]rune(string(tabs)[:start]))
		heavy := strings.Repeat("━", len([]rune(tt.label)))
		if got := string(rule[start : start+len([]rune(tt.label))]); got != heavy {
			t.Errorf("%s: rule under it = %q, want %q", tt.label, got, heavy)
		}
		if n := strings.Count(string(rule), "━"); n != len([]rune(tt.label))+2 {
			t.Errorf("%s: %d heavy strokes, want %d (the label and a space either side)", tt.label, n, len([]rune(tt.label))+2)
		}
	}
}

func TestApp_Esc_OnATab_StaysPut(t *testing.T) {
	t.Parallel()

	for _, sc := range []screen{screenActiveApplications, screenCompanyList} {
		app := newTabsTestApp(t, 100)
		app.screen = sc
		app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEsc})
		if app.screen != sc {
			t.Errorf("screen after esc on %v = %v, want it unchanged", sc, app.screen)
		}
	}
}
