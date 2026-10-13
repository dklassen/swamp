package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ctrl+c quits wherever the user is, including on a search line or a
// form, where letters are typed rather than acted on.
func TestApp_CtrlC_QuitsFromAnyScreen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		open func(t *testing.T, app *App) *App
	}{
		{"applications", func(t *testing.T, app *App) *App { return app }},
		{"companies, searching", func(t *testing.T, app *App) *App {
			app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyTab})
			return sendKeyAndApply(t, app, runeKey('/'))
		}},
		{"all postings, searching", func(t *testing.T, app *App) *App {
			app = openAllPostings(t, app)
			return sendKeyAndApply(t, app, runeKey('/'))
		}},
		{"posting detail", func(t *testing.T, app *App) *App {
			app = openAllPostings(t, app)
			return sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEnter})
		}},
		{"add company form", func(t *testing.T, app *App) *App {
			app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyTab})
			return sendKeyAndApply(t, app, runeKey('a'))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newTestStore(t)
			acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
			mustUpsertPosting(t, s, acme.ID, "1", "Engineer")
			app := newTestApp(t, s, newTestSyncer(s, nil))
			app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
			app = tt.open(t, app)
			on := app.screen

			_, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyCtrlC})

			if cmd == nil {
				t.Fatalf("ctrl+c on screen %v did nothing", on)
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Errorf("ctrl+c on screen %v didn't quit", on)
			}
		})
	}
}
