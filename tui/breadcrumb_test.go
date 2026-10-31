package tui

import (
	"slices"
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
	// screens builds each kind a case pushes, showing p, view or Globex.
	screens := map[message.Screen]func() stackScreen{
		message.ScreenCompanyPostings: func() stackScreen {
			return newPostingListModel(nil, nil, message.Company{ID: 1, Name: "Globex"}, false, message.ContentSize{})
		},
		message.ScreenPostingDetail: func() stackScreen {
			return newPostingDetailModel(nil, nil, nil, nil, message.PostingDetail{Posting: p}, message.ContentSize{})
		},
		message.ScreenApplicationDetail: func() stackScreen {
			return newApplicationDetailModel(nil, nil, nil, nil, nil, applicationDetailOf(view), 0)
		},
		message.ScreenApplicationNotes: func() stackScreen {
			return newApplicationNotesModel(nil, message.Application{ID: 1}, message.ContentSize{})
		},
		message.ScreenApplicationStatus: func() stackScreen {
			return newApplicationStatusModel(nil, message.Application{ID: 1})
		},
		message.ScreenDocumentReviewForm: func() stackScreen {
			return newDocumentReviewFormModel(nil, nil, message.DocumentReview{ApplicationID: 1, DocumentType: documentTypeSnapshots[document.CoverLetter]}, message.ContentSize{})
		},
		message.ScreenCompanyForm: func() stackScreen { return newCompanyFormModel(nil) },
		message.ScreenFilterSelect: func() stackScreen {
			return newFilterSelectModel(nil, message.FilterSelection{}, message.ContentSize{})
		},
		message.ScreenApplicationExport: func() stackScreen {
			return newApplicationExportModel(nil, nil, applicationDetailOf(view), "", message.ContentSize{})
		},
		message.ScreenApplicationSubmit: func() stackScreen {
			return newApplicationSubmitModel(nil, nil, nil, nil, applicationDetailOf(view), "")
		},
	}
	tests := []struct {
		name   string
		tab    message.Screen
		pushed []message.Screen
		want   string
	}{
		{"posting list", message.ScreenCompanyList, []message.Screen{message.ScreenCompanyPostings}, "Companies › Globex"},
		{"posting detail from the list", message.ScreenCompanyList, []message.Screen{message.ScreenCompanyPostings, message.ScreenPostingDetail}, "Companies › Globex › Staff Engineer"},
		{"posting detail from all postings", message.ScreenAllPostings, []message.Screen{message.ScreenPostingDetail}, "All postings › Staff Engineer"},
		{"notes, under posting detail from all postings", message.ScreenAllPostings, []message.Screen{message.ScreenPostingDetail, message.ScreenApplicationNotes}, "All postings › Staff Engineer › Edit notes"},
		{"application detail", message.ScreenActiveApplications, []message.Screen{message.ScreenApplicationDetail}, "Applications › Initech › Engineering Manager"},
		{"posting detail from an application", message.ScreenActiveApplications, []message.Screen{message.ScreenApplicationDetail, message.ScreenPostingDetail}, "Applications › Initech › Engineering Manager › Posting"},
		{"notes, under posting detail from the list", message.ScreenCompanyList, []message.Screen{message.ScreenCompanyPostings, message.ScreenPostingDetail, message.ScreenApplicationNotes}, "Companies › Globex › Staff Engineer › Edit notes"},
		{"status from the home list", message.ScreenActiveApplications, []message.Screen{message.ScreenApplicationStatus}, "Applications › Set status"},
		{"review form, picked under posting detail", message.ScreenCompanyList, []message.Screen{message.ScreenCompanyPostings, message.ScreenPostingDetail, message.ScreenDocumentReviewForm}, "Companies › Globex › Staff Engineer › Review " + document.CoverLetter.Label()},
		{"add company", message.ScreenCompanyList, []message.Screen{message.ScreenCompanyForm}, "Companies › Add company"},
		{"filters", message.ScreenCompanyList, []message.Screen{message.ScreenCompanyPostings, message.ScreenFilterSelect}, "Companies › Globex › Filters"},
		{"export", message.ScreenActiveApplications, []message.Screen{message.ScreenApplicationExport}, "Applications › Export PDFs"},
		{"submit", message.ScreenActiveApplications, []message.Screen{message.ScreenApplicationDetail, message.ScreenApplicationSubmit}, "Applications › Initech › Engineering Manager › Submit"},
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
			app.tab = tt.tab
			for _, kind := range tt.pushed {
				if err := app.stack.push(kind, screens[kind]()); err != nil {
					t.Fatalf("push %v: %v", kind, err)
				}
			}

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
			return "Review " + documentTypeOf(reviewForm(app).review.DocumentType).Label()
		},
		"application submit": func(app *App) string {
			return "Submit: " + shownDetail(app).Posting.Title
		},
		"application delete": func(app *App) string {
			return "Delete application: " + shownDetail(app).Posting.Title
		},
		"application detail": func(app *App) string { return shownDetail(app).Posting.Title },
		"export":             func(*App) string { return "Export PDFs" },
		"status":             func(*App) string { return "Set application status" },
		"review select":      func(*App) string { return "Review a document" },
		"add company":        func(*App) string { return "Add company" },
		"edit company":       func(*App) string { return "Edit company" },
		"application form":   func(*App) string { return "Application form" },
		"review form from review select": func(app *App) string {
			return "Review " + documentTypeOf(reviewForm(app).review.DocumentType).Label()
		},
	}
	for _, sc := range append(sizedScreens(), unsizedScreens()...) {
		if slices.Contains(tabs, sc.screen) {
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
			if app.onScreen() != sc.screen {
				t.Fatalf("screen = %v, want %v", app.onScreen(), sc.screen)
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
		{name: "application detail", screen: message.ScreenApplicationDetail, open: openApplication},
		{name: "export", screen: message.ScreenApplicationExport, open: keys(home, runeKey('e'))},
		{name: "status", screen: message.ScreenApplicationStatus, open: keys(home, runeKey('s'))},
		{name: "review select", screen: message.ScreenDocumentReviewSelect, open: keys(func(t *testing.T, app *App) *App {
			return clearBanner(openPostingDetail(t, openPostingList(t, app)))
		}, runeKey('r'))},
		{name: "add company", screen: message.ScreenCompanyForm, open: keys(home, tea.KeyPressMsg{Code: tea.KeyTab}, runeKey('a'))},
		{name: "edit company", screen: message.ScreenCompanyEdit, open: keys(home, tea.KeyPressMsg{Code: tea.KeyTab}, runeKey('e'))},
		{name: "application form", screen: message.ScreenApplicationForm, open: keys(openApplication, runeKey('f'))},
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
			pushApplicationDetail(t, app, view)
			d := newPostingDetailModel(nil, nil, nil, nil, message.PostingDetail{Posting: message.Posting{ID: 9}}, message.ContentSize{})
			if err := app.stack.push(message.ScreenPostingDetail, d); err != nil {
				t.Fatalf("push: %v", err)
			}

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

// pushApplicationDetail opens detail on a over the home list, as enter
// there would.
func pushApplicationDetail(t *testing.T, app *App, a posting.ActiveApplication) {
	t.Helper()
	d := newApplicationDetailModel(nil, nil, nil, nil, app.documents, applicationDetailOf(a), 0)
	if err := app.stack.push(message.ScreenApplicationDetail, d); err != nil {
		t.Fatalf("push: %v", err)
	}
}

// shownDetail is what the open application detail shows, zero if none is
// open.
func shownDetail(app *App) message.ApplicationDetail {
	if d := app.applicationDetail(); d != nil {
		return d.detail
	}
	return message.ApplicationDetail{}
}
