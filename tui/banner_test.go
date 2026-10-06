package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Notifications sit under the screen, after its help line, until they have
// a place of their own; the header stays on top.
func TestApp_Banner_ShowsBelowTheScreen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		set  func(app *App)
		want string
	}{
		{"status", func(app *App) { app.status = "2 PDFs exported" }, "2 PDFs exported"},
		{"error", func(app *App) { app.err = errors.New("could not open browser") }, "error: could not open browser"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := newFullTestApp(t)
			app, _ = sendKey(app, tea.WindowSizeMsg{Width: 100, Height: 24})
			app = clearBanner(app)
			tt.set(app)

			lines := strings.Split(ansi.Strip(app.View()), "\n")
			if !strings.Contains(lines[0], "Applications") {
				t.Errorf("first line = %q, want the tab bar", lines[0])
			}
			if got := strings.TrimSpace(lines[len(lines)-1]); got != tt.want {
				t.Errorf("last line = %q, want %q:\n%s", got, tt.want, strings.Join(lines, "\n"))
			}
		})
	}
}
