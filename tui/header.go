package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
)

// header is the line App.View draws above every screen, with a rule
// under it: the tabs on a tab, and the path back to one anywhere else.
// It ends in a newline, so screenRows can count its rows the way it
// counts the banner's.
func (a *App) header() string {
	path := a.path()
	if len(path) == 1 {
		return a.tabBar(path[0])
	}
	// Less the leading space.
	crumbs := fitCrumbs(a.crumbs(path), tableWidth(a.width)-1)
	for i, crumb := range crumbs {
		style := model.DimStyle
		if i == len(crumbs)-1 {
			style = model.SectionStyle
		}
		crumbs[i] = style.Render(crumb)
	}
	line := " " + strings.Join(crumbs, model.DimStyle.Render(crumbSeparator))
	return line + "\n" + model.DimStyle.Render(strings.Repeat("─", tableWidth(a.width))) + "\n"
}

const crumbSeparator = " › "

// fitCrumbs shortens crumbs to fit width once joined. The middle ones go
// first, oldest first, behind a single "…", so the tab and the screen
// you're on stay; if that's still too long, the last is cut off.
func fitCrumbs(crumbs []string, width int) []string {
	if len(crumbs) < 2 {
		return crumbs
	}
	first, last := crumbs[0], crumbs[len(crumbs)-1]
	for dropped := 0; dropped <= len(crumbs)-2; dropped++ {
		fitted := crumbs
		if dropped > 0 {
			fitted = append([]string{first, "…"}, crumbs[1+dropped:]...)
		}
		if lipgloss.Width(strings.Join(fitted, crumbSeparator)) <= width {
			return fitted
		}
	}
	fitted := []string{first, last}
	if len(crumbs) > 2 {
		fitted = []string{first, "…", last}
	}
	before := lipgloss.Width(strings.Join(fitted[:len(fitted)-1], crumbSeparator) + crumbSeparator)
	fitted[len(fitted)-1] = truncateCol(last, max(width-before, 0))
	return fitted
}

// crumbs is what each screen in path names itself.
func (a *App) crumbs(path []message.Screen) []string {
	var crumbs []string
	beneath := message.ScreenNone
	for _, kind := range path {
		crumbs = append(crumbs, a.screenOf(kind).crumbs(beneath)...)
		beneath = kind
	}
	return crumbs
}

// tabBar names every tab and underlines active.
func (a *App) tabBar(active message.Screen) string {
	tabs := []struct {
		screen message.Screen
		label  string
	}{
		{message.ScreenActiveApplications, a.activeApplicationList.tabLabel()},
		{message.ScreenCompanyList, a.companyList.tabLabel()},
		{message.ScreenAllPostings, a.allPostings.tabLabel()},
	}
	const gap = "  "
	var line, rule strings.Builder
	for i, tab := range tabs {
		if i > 0 {
			line.WriteString(gap)
			rule.WriteString(model.DimStyle.Render(strings.Repeat("─", len(gap))))
		}
		style, stroke := model.DimStyle, "─"
		if tab.screen == active {
			style, stroke = model.SectionStyle, "━"
		}
		label := " " + tab.label + " "
		line.WriteString(style.Render(label))
		rule.WriteString(style.Render(strings.Repeat(stroke, lipgloss.Width(label))))
	}
	if rest := tableWidth(a.width) - lipgloss.Width(rule.String()); rest > 0 {
		rule.WriteString(model.DimStyle.Render(strings.Repeat("─", rest)))
	}
	return line.String() + "\n" + rule.String() + "\n"
}
