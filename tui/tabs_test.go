package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
)

func TestApp_Tab_SwitchesBetweenTheLists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		from message.Screen
		want message.Screen
	}{
		{"applications to companies", message.ScreenActiveApplications, message.ScreenCompanyList},
		{"companies to all postings", message.ScreenCompanyList, message.ScreenAllPostings},
		{"all postings to applications", message.ScreenAllPostings, message.ScreenActiveApplications},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := testutil.NewTestApp(t)
			app := newTestApp(t, s, newTestSyncer(s, nil))
			app.nav.tab = tt.from
			app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyTab})
			if app.onScreen() != tt.want {
				t.Errorf("screen after tab = %v, want %v", app.onScreen(), tt.want)
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
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyTab})
	for _, key := range []tea.KeyPressMsg{runeKey('/'), runeKey('g'), {Code: tea.KeyTab}} {
		app = sendKeyAndApply(t, app, key)
	}
	if app.onScreen() != message.ScreenAllPostings {
		t.Fatalf("screen after tab while searching = %v, want all postings", app.onScreen())
	}
	for range 2 {
		app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyTab})
	}
	if !companiesTabOf(app).searching || companiesTabOf(app).query != "g" {
		t.Errorf("search after switching back = %v %q, want open with %q", companiesTabOf(app).searching, companiesTabOf(app).query, "g")
	}
}

// newTabsTestApp has two companies and no applications, sized to width.
func newTabsTestApp(t *testing.T, width int) *App {
	t.Helper()
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustCreateCompany(t, s, "Globex", jobboard.Ashby, "globex")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: width, Height: 24})
	return app
}

// viewLines is the app's view without styling, one string per line.
func viewLines(app *App) []string {
	return strings.Split(ansi.Strip(viewText(app)), "\n")
}

func TestApp_TabBar_ShowsEveryTab(t *testing.T) {
	t.Parallel()

	// The all-postings count is right before that tab has been opened.
	for _, sc := range []message.Screen{message.ScreenActiveApplications, message.ScreenCompanyList, message.ScreenAllPostings} {
		s := testutil.NewTestApp(t)
		acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
		mustCreateCompany(t, s, "Globex", jobboard.Ashby, "globex")
		mustUpsertPosting(t, s, acme.ID, "1", "Engineer")
		app := newTestApp(t, s, newTestSyncer(s, nil))
		app, _ = sendKey(app, tea.WindowSizeMsg{Width: 100, Height: 24})
		app.nav.tab = sc
		if got := viewLines(app)[0]; !strings.Contains(got, "Applications 0") || !strings.Contains(got, "Companies") || !strings.Contains(got, "All postings 1") {
			t.Errorf("screen %v: first line = %q, want every tab, with counts on applications and all postings", sc, got)
		}
		if got := viewLines(app)[0]; regexp.MustCompile(`Companies \d`).MatchString(got) {
			t.Errorf("screen %v: first line = %q, want the companies tab without a count", sc, got)
		}
	}
}

func TestApp_TabBar_UnderlinesTheTabOnScreen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		screen message.Screen
		label  string
	}{
		{message.ScreenActiveApplications, "Applications 0"},
		{message.ScreenCompanyList, "Companies"},
	}
	for _, tt := range tests {
		app := newTabsTestApp(t, 120)
		app.nav.tab = tt.screen
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

	for _, sc := range []message.Screen{message.ScreenActiveApplications, message.ScreenCompanyList} {
		app := newTabsTestApp(t, 100)
		app.nav.tab = sc
		app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEsc})
		if app.onScreen() != sc {
			t.Errorf("screen after esc on %v = %v, want it unchanged", sc, app.onScreen())
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
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.onScreen() != message.ScreenApplicationDetail || shownDetail(app).Posting.Title != "Engineer 07" {
		t.Fatalf("after enter, enter: screen %v showing %q, want application detail for Engineer 07", app.onScreen(), shownDetail(app).Posting.Title)
	}
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEsc}, {Code: tea.KeyTab}, {Code: tea.KeyTab}, {Code: tea.KeyTab}} {
		app = sendKeyAndApply(t, app, key)
	}
	if app.onScreen() != message.ScreenActiveApplications || applicationsTabOf(app).searching || applicationsTabOf(app).query != "engineer 07" {
		t.Errorf("back on the home list: screen %v, searching %v, query %q; want the matches for %q still kept",
			app.onScreen(), applicationsTabOf(app).searching, applicationsTabOf(app).query, "engineer 07")
	}
}
