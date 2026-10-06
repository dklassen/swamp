package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// tableWidthOf is how wide the table in view is, from its top border.
func tableWidthOf(t *testing.T, view string) int {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "╭") {
			return lipgloss.Width(line)
		}
	}
	t.Fatalf("no table in view:\n%s", view)
	return 0
}
