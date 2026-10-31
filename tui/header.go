package tui

import (
	"fmt"
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

// crumbs names each screen in path, the way its title used to.
func (a *App) crumbs(path []message.Screen) []string {
	var crumbs []string
	for i, s := range path {
		switch s {
		case message.ScreenActiveApplications:
			crumbs = append(crumbs, "Applications")
		case message.ScreenCompanyList:
			crumbs = append(crumbs, "Companies")
		case message.ScreenAllPostings:
			crumbs = append(crumbs, "All postings")
		case message.ScreenCompanyPostings:
			if l := a.postingList(); l != nil {
				crumbs = append(crumbs, l.company.Name)
			}
		case message.ScreenPostingDetail:
			// Application detail has already named it.
			if i > 0 && path[i-1] == message.ScreenApplicationDetail {
				crumbs = append(crumbs, "Posting")
			} else if d := a.postingDetail(); d != nil {
				crumbs = append(crumbs, d.posting.Title)
			}
		case message.ScreenApplicationDetail:
			if d := a.applicationDetail(); d != nil {
				crumbs = append(crumbs, d.detail.CompanyName, d.detail.Posting.Title)
			}
		case message.ScreenCompanyForm:
			crumbs = append(crumbs, "Add company")
		case message.ScreenCompanyEdit:
			crumbs = append(crumbs, "Edit company")
		case message.ScreenFilterSelect:
			crumbs = append(crumbs, "Filters")
		case message.ScreenApplicationStatus:
			crumbs = append(crumbs, "Set status")
		case message.ScreenApplicationNotes:
			crumbs = append(crumbs, "Edit notes")
		case message.ScreenDocumentReviewSelect:
			crumbs = append(crumbs, "Review a document")
		case message.ScreenDocumentReviewForm:
			if f, ok := a.stack.open(message.ScreenDocumentReviewForm).(*documentReviewFormModel); ok {
				crumbs = append(crumbs, "Review "+documentTypeOf(f.review.DocumentType).Label())
			}
		case message.ScreenApplicationExport:
			crumbs = append(crumbs, "Export PDFs")
		case message.ScreenApplicationSubmit:
			crumbs = append(crumbs, "Submit")
		case message.ScreenApplicationForm:
			crumbs = append(crumbs, "Application form")
		}
	}
	return crumbs
}

// tabBar names every tab, with a count on all but the companies tab, and
// underlines active.
func (a *App) tabBar(active message.Screen) string {
	tabs := []struct {
		screen message.Screen
		label  string
	}{
		{message.ScreenActiveApplications, fmt.Sprintf(" Applications %d ", len(a.activeApplicationList.apps))},
		{message.ScreenCompanyList, " Companies "},
		{message.ScreenAllPostings, fmt.Sprintf(" All postings %d ", a.allPostings.page.WithoutQuery)},
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
		line.WriteString(style.Render(tab.label))
		rule.WriteString(style.Render(strings.Repeat(stroke, lipgloss.Width(tab.label))))
	}
	if rest := tableWidth(a.width) - lipgloss.Width(rule.String()); rest > 0 {
		rule.WriteString(model.DimStyle.Render(strings.Repeat("─", rest)))
	}
	return line.String() + "\n" + rule.String() + "\n"
}
