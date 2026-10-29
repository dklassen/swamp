package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/tui/message"
)

// Below the tabs, the header names the way back to one: what esc steps
// through, last screen first.
func TestApp_Header_BreadcrumbIsThePathBack(t *testing.T) {
	t.Parallel()

	p := message.Posting{ID: 5, Title: "Staff Engineer"}
	view := posting.ActiveApplication{
		Application: application.Application{ID: 1},
		Posting:     posting.Posting{ID: 9, IngestedFields: posting.IngestedFields{Title: "Engineering Manager"}},
		CompanyName: "Initech",
	}
	// noDetail is a detailOver for a path with no posting detail on it.
	const noDetail screen = -1
	tests := []struct {
		name   string
		screen screen
		// detailOver is the screen posting detail was pushed over.
		detailOver screen
		stack      []screen
		want       string
	}{
		{"posting list", screenPostingList, noDetail, nil, "Companies › Globex"},
		{"posting detail from the list", screenPostingDetail, screenPostingList, nil, "Companies › Globex › Staff Engineer"},
		{"posting detail from all postings", screenPostingDetail, screenAllPostings, nil, "All postings › Staff Engineer"},
		{"notes, under posting detail from all postings", screenApplicationNotesEdit, screenAllPostings, nil, "All postings › Staff Engineer › Edit notes"},
		{"application detail", screenApplicationDetail, noDetail, nil, "Applications › Initech › Engineering Manager"},
		{"posting detail from an application", screenPostingDetail, screenApplicationDetail, nil, "Applications › Initech › Engineering Manager › Posting"},
		{"notes, under posting detail from the list", screenApplicationNotesEdit, screenPostingList, nil, "Companies › Globex › Staff Engineer › Edit notes"},
		{"status from the home list", screenApplicationStatusSelect, noDetail, []screen{screenActiveApplications}, "Applications › Set status"},
		{"review form, picked under posting detail", screenDocumentReviewForm, screenPostingList, []screen{screenPostingDetail}, "Companies › Globex › Staff Engineer › Review " + document.CoverLetter.Label()},
		{"add company", screenCompanyForm, noDetail, nil, "Companies › Add company"},
		{"filters", screenFilterSelect, noDetail, nil, "Companies › Globex › Filters"},
		{"export", screenApplicationExport, noDetail, nil, "Applications › Export PDFs"},
		{"submit", screenApplicationSubmit, noDetail, nil, "Applications › Initech › Engineering Manager › Submit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := New(
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				documents.NewStore(t.TempDir()),
			)
			app.width = 120
			if err := app.stack.push(message.ScreenCompanyPostings, newPostingListModel(nil, nil, message.Company{ID: 1, Name: "Globex"}, false, message.ContentSize{}), screenCompanyList); err != nil {
				t.Fatalf("push: %v", err)
			}
			if tt.detailOver != noDetail {
				d := newPostingDetailModel(nil, nil, nil, nil, message.PostingDetail{Posting: p}, message.ContentSize{})
				if err := app.stack.push(message.ScreenPostingDetail, d, tt.detailOver); err != nil {
					t.Fatalf("push: %v", err)
				}
			}
			app.applicationDetail.application = view
			app.documentReviewForm.documentType = document.CoverLetter
			app.currentScreen, app.returnStack = tt.screen, tt.stack

			if got := strings.TrimSpace(strings.Split(ansi.Strip(app.header()), "\n")[0]); got != tt.want {
				t.Errorf("breadcrumb = %q, want %q", got, tt.want)
			}
		})
	}
}

// The breadcrumb takes the place of each screen's title, so a screen
// doesn't name itself twice or spend two rows on it.
func TestApp_Screens_BreadcrumbReplacesTheTitle(t *testing.T) {
	t.Parallel()

	// What each screen's own title said.
	formerTitles := map[string]func(app *App) string{
		"posting list": func(*App) string { return "Postings: Acme" },
		"posting list with description and filters": func(*App) string { return "Postings: Acme" },
		"posting detail":                     func(app *App) string { return app.postingDetail().posting.Title },
		"filter select":                      func(*App) string { return "Filters: Acme" },
		"filter select across both sections": func(*App) string { return "Filters: Acme" },
		"application notes":                  func(*App) string { return "Edit application notes" },
		"review form from application detail": func(app *App) string {
			return "Review " + app.documentReviewForm.documentType.Label()
		},
		"application submit": func(app *App) string {
			return "Submit: " + app.applicationDetail.application.Posting.Title
		},
		"application delete": func(app *App) string {
			return "Delete application: " + app.applicationDetail.application.Posting.Title
		},
		"application detail": func(app *App) string { return app.applicationDetail.application.Posting.Title },
		"export":             func(*App) string { return "Export PDFs" },
		"status":             func(*App) string { return "Set application status" },
		"review select":      func(*App) string { return "Review a document" },
		"add company":        func(*App) string { return "Add company" },
		"edit company":       func(*App) string { return "Edit company" },
		"application form":   func(*App) string { return "Application form" },
		"review form from review select": func(app *App) string {
			return "Review " + app.documentReviewForm.documentType.Label()
		},
	}
	for _, sc := range append(sizedScreens(), unsizedScreens()...) {
		if isTab(sc.screen) {
			continue
		}
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()

			formerTitle, ok := formerTitles[sc.name]
			if !ok {
				t.Fatalf("no former title for %q; add it to formerTitles", sc.name)
			}
			app := newFullTestApp(t)
			app, _ = sendKey(app, tea.WindowSizeMsg{Width: 100, Height: 24})
			app = clearBanner(sc.open(t, app))
			if app.currentScreen != sc.screen {
				t.Fatalf("screen = %v, want %v", app.currentScreen, sc.screen)
			}
			lines := viewLines(app)
			if got := strings.TrimSpace(lines[2]); got == formerTitle(app) {
				t.Errorf("first line under the header is still the title %q:\n%s", got, strings.Join(lines, "\n"))
			}
		})
	}
}

// unsizedScreens are the screens below the tabs that sizedScreens leaves
// out, opened the same way.
func unsizedScreens() []sizedScreen {
	openApplication := func(t *testing.T, app *App) *App {
		app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
		return applyCmd(t, app, cmd)
	}
	keys := func(open func(*testing.T, *App) *App, keys ...tea.KeyPressMsg) func(*testing.T, *App) *App {
		return func(t *testing.T, app *App) *App {
			app = open(t, app)
			for _, key := range keys {
				app = sendKeyAndApply(t, app, key)
			}
			return app
		}
	}
	home := func(_ *testing.T, app *App) *App { return app }
	return []sizedScreen{
		{name: "application detail", screen: screenApplicationDetail, open: openApplication},
		{name: "export", screen: screenApplicationExport, open: keys(home, runeKey('e'))},
		{name: "status", screen: screenApplicationStatusSelect, open: keys(home, runeKey('s'))},
		{name: "review select", screen: screenDocumentReviewSelect, open: keys(func(t *testing.T, app *App) *App {
			return clearBanner(openPostingDetail(t, openPostingList(t, app)))
		}, runeKey('r'))},
		{name: "add company", screen: screenCompanyForm, open: keys(home, tea.KeyPressMsg{Code: tea.KeyTab}, runeKey('a'))},
		{name: "edit company", screen: screenCompanyEdit, open: keys(home, tea.KeyPressMsg{Code: tea.KeyTab}, runeKey('e'))},
		{name: "application form", screen: screenApplicationForm, open: keys(openApplication, runeKey('f'))},
	}
}

// A path too long for the line loses its middle first, oldest crumb
// first, so the tab and the screen you're on stay readable.
func TestApp_Header_LongBreadcrumbLosesItsMiddleFirst(t *testing.T) {
	t.Parallel()

	view := posting.ActiveApplication{
		Posting:     posting.Posting{IngestedFields: posting.IngestedFields{Title: "Engineering Manager, Payments Reliability"}},
		CompanyName: "Initech Global Holdings",
	}
	tests := []struct {
		name  string
		width int
		want  string
	}{
		{"fits", 120, "Applications › Initech Global Holdings › Engineering Manager, Payments Reliability › Posting"},
		{"drops the oldest middle crumb", 80, "Applications › … › Engineering Manager, Payments Reliability › Posting"},
		{"drops every middle crumb", 40, "Applications › … › Posting"},
		{"then cuts off the last crumb", 24, "Applications › … › Pos…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := New(
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				documents.NewStore(t.TempDir()),
			)
			app.width = tt.width
			app.applicationDetail.application = view
			app.currentScreen, app.returnStack = screenPostingDetail, []screen{screenApplicationDetail}

			line := strings.Split(ansi.Strip(app.header()), "\n")[0]
			if got := strings.TrimSpace(line); got != tt.want {
				t.Errorf("breadcrumb = %q, want %q", got, tt.want)
			}
			if w := ansi.StringWidth(line); w > tt.width {
				t.Errorf("breadcrumb is %d wide, want at most %d", w, tt.width)
			}
		})
	}
}
