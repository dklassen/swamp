package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
