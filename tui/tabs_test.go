package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/dklassen/swamp/internal/testutil"
)

func TestApp_Tab_SwitchesBetweenTheLists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		from screen
		want screen
	}{
		{"applications to companies", screenActiveApplications, screenCompanyList},
		{"companies to all postings", screenCompanyList, screenAllPostings},
		{"all postings to applications", screenAllPostings, screenActiveApplications},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := testutil.NewTestApp(t)
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

	s := testutil.NewTestApp(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app.screen = screenCompanyList
	for _, key := range []tea.KeyMsg{runeKey('/'), runeKey('g'), {Type: tea.KeyTab}} {
		app = sendKeyAndApply(t, app, key)
	}
	if app.screen != screenAllPostings {
		t.Fatalf("screen after tab while searching = %v, want all postings", app.screen)
	}
	for range 2 {
		app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyTab})
	}
	if !app.companyList.searching || app.companyList.query != "g" {
		t.Errorf("search after switching back = %v %q, want open with %q", app.companyList.searching, app.companyList.query, "g")
	}
}

// newTabsTestApp has two companies and no applications, sized to width.
func newTabsTestApp(t *testing.T, width int) *App {
	t.Helper()
	s := testutil.NewTestApp(t)
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

func TestApp_TabBar_ShowsEveryTabWithItsCount(t *testing.T) {
	t.Parallel()

	// The all-postings count is right before that tab has been opened.
	for _, sc := range []screen{screenActiveApplications, screenCompanyList, screenAllPostings} {
		s := testutil.NewTestApp(t)
		acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
		mustCreateCompany(t, s, "Globex", "ashby", "globex")
		mustUpsertPosting(t, s, acme.ID, "1", "Engineer")
		app := newTestApp(t, s, newTestSyncer(s, nil))
		app, _ = sendKey(app, tea.WindowSizeMsg{Width: 100, Height: 24})
		app.screen = sc
		if got := viewLines(app)[0]; !strings.Contains(got, "Applications 0") || !strings.Contains(got, "Companies 2") || !strings.Contains(got, "All postings 1") {
			t.Errorf("screen %v: first line = %q, want every tab with its count", sc, got)
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

// Opening a match and coming back finds the search as it was, and so does
// a trip to the other tab.
func TestApp_ActiveApplications_SearchSurvivesDetailAndTab(t *testing.T) {
	t.Parallel()

	app := newFullTestApp(t)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 100, Height: 24})
	for _, r := range "/engineer 07" {
		app = sendKeyAndApply(t, app, runeKey(r))
	}
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	if app.screen != screenApplicationDetail || app.applicationDetail.application.Posting.Title != "Engineer 07" {
		t.Fatalf("after enter, enter: screen %v showing %q, want application detail for Engineer 07", app.screen, app.applicationDetail.application.Posting.Title)
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyTab}, {Type: tea.KeyTab}, {Type: tea.KeyTab}} {
		app = sendKeyAndApply(t, app, key)
	}
	if app.screen != screenActiveApplications || app.activeApplicationList.searching || app.activeApplicationList.query != "engineer 07" {
		t.Errorf("back on the home list: screen %v, searching %v, query %q; want the matches for %q still kept",
			app.screen, app.activeApplicationList.searching, app.activeApplicationList.query, "engineer 07")
	}
}
