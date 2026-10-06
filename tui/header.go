package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// header is the line App.View draws above every screen, with a rule
// under it: the tabs on a tab, and the path back to one anywhere else.
// It ends in a newline, so screenRows can count its rows the way it
// counts the banner's.
func (a *App) header() string {
	if isTab(a.screen) {
		return a.tabBar()
	}
	crumbs := a.crumbs(a.path())
	for i, crumb := range crumbs {
		style := dimStyle
		if i == len(crumbs)-1 {
			style = sectionStyle
		}
		crumbs[i] = style.Render(crumb)
	}
	line := " " + strings.Join(crumbs, dimStyle.Render(" › "))
	return line + "\n" + dimStyle.Render(strings.Repeat("─", tableWidth(a.width))) + "\n"
}

func isTab(s screen) bool {
	return s == screenActiveApplications || s == screenCompanyList
}

// parents maps each screen entered from only one place to that place,
// which is where esc goes. The screens missing from it are entered from
// more than one, and push where they came from onto returnStack.
var parents = map[screen]screen{
	screenCompanyForm:          screenCompanyList,
	screenCompanyEdit:          screenCompanyList,
	screenPostingList:          screenCompanyList,
	screenFilterSelect:         screenPostingList,
	screenApplicationDetail:    screenActiveApplications,
	screenApplicationExport:    screenActiveApplications,
	screenApplicationSubmit:    screenApplicationDetail,
	screenApplicationDelete:    screenApplicationDetail,
	screenApplicationForm:      screenApplicationDetail,
	screenApplicationNotesEdit: screenPostingDetail,
}

// path is the screens from a tab to the one on screen: the way esc goes
// back, reversed.
func (a *App) path() []screen {
	path := []screen{a.screen}
	next := len(a.returnStack) - 1
	for s := a.screen; !isTab(s); path = append(path, s) {
		if p, ok := parents[s]; ok {
			s = p
		} else if next >= 0 {
			s = a.returnStack[next]
			next--
		} else {
			// Where returnBack goes when a push was missed.
			s = screenActiveApplications
		}
	}
	slices.Reverse(path)
	return path
}

// crumbs names each screen in path, the way its title used to.
func (a *App) crumbs(path []screen) []string {
	var crumbs []string
	for i, s := range path {
		switch s {
		case screenActiveApplications:
			crumbs = append(crumbs, "Applications")
		case screenCompanyList:
			crumbs = append(crumbs, "Companies")
		case screenPostingList:
			crumbs = append(crumbs, a.selectedCompany.Name)
		case screenPostingDetail:
			// Application detail has already named it.
			if i > 0 && path[i-1] == screenApplicationDetail {
				crumbs = append(crumbs, "Posting")
			} else {
				crumbs = append(crumbs, a.postingDetail.posting.Title)
			}
		case screenApplicationDetail:
			view := a.applicationDetail.application
			crumbs = append(crumbs, view.CompanyName, view.Posting.Title)
		case screenCompanyForm:
			crumbs = append(crumbs, "Add company")
		case screenCompanyEdit:
			crumbs = append(crumbs, "Edit company")
		case screenFilterSelect:
			crumbs = append(crumbs, "Filters")
		case screenApplicationStatusSelect:
			crumbs = append(crumbs, "Set status")
		case screenApplicationNotesEdit:
			crumbs = append(crumbs, "Edit notes")
		case screenDocumentReviewSelect:
			crumbs = append(crumbs, "Review a document")
		case screenDocumentReviewForm:
			crumbs = append(crumbs, "Review "+a.documentReviewForm.documentType.Label())
		case screenApplicationExport:
			crumbs = append(crumbs, "Export PDFs")
		case screenApplicationSubmit:
			crumbs = append(crumbs, "Submit")
		case screenApplicationDelete:
			crumbs = append(crumbs, "Delete application")
		case screenApplicationForm:
			crumbs = append(crumbs, "Application form")
		}
	}
	return crumbs
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
