package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// header is the line App.View draws above every screen, with a rule
// under it: the tabs on a tab, and "" anywhere else. It ends in a
// newline, so screenRows can count its rows the way it counts the
// banner's.
func (a *App) header() string {
	switch a.screen {
	case screenActiveApplications, screenCompanyList:
		return a.tabBar()
	}
	return ""
}

// tabBar names both tabs with their counts, and underlines the one on
// screen.
func (a *App) tabBar() string {
	tabs := []struct {
		screen screen
		label  string
	}{
		{screenActiveApplications, fmt.Sprintf(" Applications %d ", len(a.activeApplications))},
		{screenCompanyList, fmt.Sprintf(" Companies %d ", len(a.companies))},
	}
	const gap = "  "
	var line, rule strings.Builder
	for i, tab := range tabs {
		if i > 0 {
			line.WriteString(gap)
			rule.WriteString(dimStyle.Render(strings.Repeat("─", len(gap))))
		}
		style, stroke := dimStyle, "─"
		if tab.screen == a.screen {
			style, stroke = sectionStyle, "━"
		}
		line.WriteString(style.Render(tab.label))
		rule.WriteString(style.Render(strings.Repeat(stroke, lipgloss.Width(tab.label))))
	}
	if rest := tableWidth(a.width) - lipgloss.Width(rule.String()); rest > 0 {
		rule.WriteString(dimStyle.Render(strings.Repeat("─", rest)))
	}
	return line.String() + "\n" + rule.String() + "\n"
}
