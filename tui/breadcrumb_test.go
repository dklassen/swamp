package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/store"
)

// Below the tabs, the header names the way back to one: what esc steps
// through, last screen first.
func TestApp_Header_BreadcrumbIsThePathBack(t *testing.T) {
	t.Parallel()

	posting := store.Posting{ID: 5, IngestedFields: store.IngestedFields{Title: "Staff Engineer"}}
	application := store.ApplicationView{
		Application: store.Application{ID: 1},
		Posting:     store.Posting{ID: 9, IngestedFields: store.IngestedFields{Title: "Engineering Manager"}},
		CompanyName: "Initech",
	}
	tests := []struct {
		name   string
		screen screen
		stack  []screen
		want   string
	}{
		{"posting list", screenPostingList, nil, "Companies › Globex"},
		{"posting detail from the list", screenPostingDetail, []screen{screenPostingList}, "Companies › Globex › Staff Engineer"},
		{"application detail", screenApplicationDetail, nil, "Applications › Initech › Engineering Manager"},
		{"posting detail from an application", screenPostingDetail, []screen{screenApplicationDetail}, "Applications › Initech › Engineering Manager › Posting"},
		{"notes, under posting detail from the list", screenApplicationNotesEdit, []screen{screenPostingList}, "Companies › Globex › Staff Engineer › Edit notes"},
		{"status from the home list", screenApplicationStatusSelect, []screen{screenActiveApplications}, "Applications › Set status"},
		{"review form, picked under posting detail", screenDocumentReviewForm, []screen{screenPostingList, screenPostingDetail}, "Companies › Globex › Staff Engineer › Review " + documents.CoverLetter.Label()},
		{"add company", screenCompanyForm, nil, "Companies › Add company"},
		{"filters", screenFilterSelect, nil, "Companies › Globex › Filters"},
		{"export", screenApplicationExport, nil, "Applications › Export PDFs"},
		{"submit", screenApplicationSubmit, nil, "Applications › Initech › Engineering Manager › Submit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := New(nil, nil, documents.NewStore(t.TempDir()))
			app.width = 120
			app.selectedCompany = store.Company{Name: "Globex"}
			app.postingDetail.posting = posting
			app.applicationDetail.application = application
			app.documentReviewForm.documentType = documents.CoverLetter
			app.screen, app.returnStack = tt.screen, tt.stack

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
		"posting detail":                     func(app *App) string { return app.postingDetail.posting.Title },
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
			if app.screen != sc.screen {
				t.Fatalf("screen = %v, want %v", app.screen, sc.screen)
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
		app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
		return applyCmd(t, app, cmd)
	}
	keys := func(open func(*testing.T, *App) *App, keys ...tea.KeyMsg) func(*testing.T, *App) *App {
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
		{name: "add company", screen: screenCompanyForm, open: keys(home, tea.KeyMsg{Type: tea.KeyTab}, runeKey('a'))},
		{name: "edit company", screen: screenCompanyEdit, open: keys(home, tea.KeyMsg{Type: tea.KeyTab}, runeKey('e'))},
		{name: "application form", screen: screenApplicationForm, open: keys(openApplication, runeKey('f'))},
	}
}
