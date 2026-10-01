package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/filter"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
)

// newFullTestApp returns an App with enough data to fill every list
// screen at any reasonable terminal height: 40 companies, and 40 postings
// for Acme, each with its own department and location (filling the
// filter screen) and an application with both documents on disk.
func newFullTestApp(t *testing.T) *App {
	t.Helper()
	ctx := context.Background()

	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	for i := 1; i < 40; i++ {
		mustCreateCompany(t, s, fmt.Sprintf("Company %02d", i), "ashby", fmt.Sprintf("company-%02d", i))
	}

	var board []jobboard.Posting
	var applications []store.Application
	for i := 1; i <= 40; i++ {
		p := jobboard.Posting{
			SourceID:   fmt.Sprintf("job-%02d", i),
			Title:      fmt.Sprintf("Engineer %02d", i),
			Department: fmt.Sprintf("Department %02d", i),
			Location:   fmt.Sprintf("City %02d", i),
		}
		board = append(board, p)
		posting, err := s.UpsertPosting(ctx, store.CreatePostingParams{
			CompanyID: acme.ID,
			Source:    "ashby",
			SourceID:  p.SourceID,
			IngestedFields: store.IngestedFields{
				Title:      p.Title,
				Department: p.Department,
				Location:   p.Location,
			},
		})
		if err != nil {
			t.Fatalf("UpsertPosting: %v", err)
		}
		applications = append(applications, mustCreateApplication(t, s, posting.ID))
	}

	app := newTestApp(t, s, newTestSyncer(s, map[string][]jobboard.Posting{"acme": board}))
	for _, a := range applications {
		status := app.documents.Status(a.ID)
		for _, path := range []string{mustDoc(t, status, documents.CoverLetter).Path, mustDoc(t, status, documents.Resume).Path} {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			if err := os.WriteFile(path, []byte("# Draft"), 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
		}
	}
	return app
}

// TestApp_Screens_FitTheTerminalUnderTheBanner checks every screen that
// sizes itself to the terminal fits in it, rendered through App.View()
// with and without the status/error banner App draws above every screen.
// Only posting detail used to leave room for the banner; the rest were
// sized as if it weren't there, so the renderer dropped the top rows --
// the banner itself -- and a sync's result or a failure never showed
// (issue #138).
func TestApp_Screens_FitTheTerminalUnderTheBanner(t *testing.T) {
	const width, height = 100, 24

	screens := []struct {
		name   string
		open   func(t *testing.T, app *App) *App
		screen screen
	}{
		{name: "active applications", screen: screenActiveApplications, open: func(t *testing.T, app *App) *App {
			return app
		}},
		{name: "company list", screen: screenCompanyList, open: func(t *testing.T, app *App) *App {
			app, _ = sendKey(app, runeKey('c'))
			return app
		}},
		{name: "posting list", screen: screenPostingList, open: func(t *testing.T, app *App) *App {
			return openPostingList(t, app)
		}},
		{name: "posting list with description and filters", screen: screenPostingList, open: func(t *testing.T, app *App) *App {
			// Every line the posting list can draw above its table: the
			// company description, the filter summary and the archived
			// notice (on by default). Filtering on every department
			// keeps all 40 postings.
			ctx := context.Background()
			acme := app.companies[0]
			if _, err := app.store.UpdateCompanyDescription(ctx, acme.ID, "Acme makes everything."); err != nil {
				t.Fatalf("UpdateCompanyDescription: %v", err)
			}
			var filters []store.CompanyFilter
			for i := 1; i <= 40; i++ {
				filters = append(filters, store.CompanyFilter{Field: filter.FieldDepartment, Value: fmt.Sprintf("Department %02d", i)})
			}
			if err := app.store.ReplaceCompanyFilters(ctx, acme.ID, filters); err != nil {
				t.Fatalf("ReplaceCompanyFilters: %v", err)
			}
			app = applyCmd(t, app, loadCompanies(app.store))
			app = openPostingList(t, app)
			if app.selectedCompany.Description == "" || len(app.activeFilterDepartments) == 0 {
				t.Fatalf("posting list isn't showing the description and filters (description %q, %d department filters)", app.selectedCompany.Description, len(app.activeFilterDepartments))
			}
			return app
		}},
		{name: "posting detail", screen: screenPostingDetail, open: func(t *testing.T, app *App) *App {
			return openPostingDetail(t, openPostingList(t, app))
		}},
		{name: "filter select", screen: screenFilterSelect, open: func(t *testing.T, app *App) *App {
			app, cmd := sendKey(openPostingList(t, app), runeKey('f'))
			return applyCmd(t, app, cmd)
		}},
		{name: "filter select across both sections", screen: screenFilterSelect, open: func(t *testing.T, app *App) *App {
			// With the cursor on the first location, the window spans the
			// end of the departments and the start of the locations, so
			// both section labels take a row.
			app, cmd := sendKey(openPostingList(t, app), runeKey('f'))
			app = applyCmd(t, app, cmd)
			for range 40 {
				app, _ = sendKey(app, runeKey('j'))
			}
			view := ansi.Strip(app.View())
			if !strings.Contains(view, "Department\n") || !strings.Contains(view, "Location\n") {
				t.Fatalf("filter screen isn't showing both section labels:\n%s", view)
			}
			return app
		}},
		{name: "application notes", screen: screenApplicationNotesEdit, open: func(t *testing.T, app *App) *App {
			// The editors keep their size from when they're opened, so
			// open them with no banner up: one that appears afterwards
			// has to be fitted by App's refit, not by luck.
			app = clearBanner(openPostingDetail(t, openPostingList(t, app)))
			app, _ = sendKey(app, runeKey('n'))
			return app
		}},
		{name: "review form from application detail", screen: screenDocumentReviewForm, open: func(t *testing.T, app *App) *App {
			app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
			app = applyCmd(t, app, cmd)
			app, cmd = sendKey(app, runeKey('R'))
			return applyCmd(t, app, cmd)
		}},
		{name: "application submit", screen: screenApplicationSubmit, open: func(t *testing.T, app *App) *App {
			app.openURL = func(string) tea.Cmd { return nil }
			app.exportDir = t.TempDir()
			app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
			app = applyCmd(t, app, cmd)
			app, cmd = sendKey(app, runeKey('S'))
			return applyCmd(t, app, cmd)
		}},
		{name: "review form from review select", screen: screenDocumentReviewForm, open: func(t *testing.T, app *App) *App {
			app = clearBanner(openPostingDetail(t, openPostingList(t, app)))
			app, cmd := sendKey(app, runeKey('r'))
			app = applyCmd(t, app, cmd)
			app, cmd = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
			return applyCmd(t, app, cmd)
		}},
	}
	banners := []struct {
		name string
		msg  tea.Msg
		text string
	}{
		{name: "no banner"},
		{name: "status", msg: companyRefreshedMsg{result: sync.Result{Name: "Acme", Fetched: 40}}, text: "Acme: fetched 40"},
		{name: "error", msg: browserOpenedMsg{err: errors.New("could not open browser")}, text: "error: could not open browser"},
	}
	for _, sc := range screens {
		for _, bn := range banners {
			t.Run(sc.name+"/"+bn.name, func(t *testing.T) {
				app := newFullTestApp(t)
				app, _ = sendKey(app, tea.WindowSizeMsg{Width: width, Height: height})
				app = sc.open(t, app)
				if app.screen != sc.screen {
					t.Fatalf("screen = %v, want %v", app.screen, sc.screen)
				}
				// Clear whatever getting here left behind (openPostingList
				// syncs, which sets a status), then show this case's banner.
				app = clearBanner(app)
				if bn.msg != nil {
					app, _ = sendKey(app, bn.msg)
				}

				// One row per line: bubbletea truncates lines wider than
				// the window rather than wrapping them.
				view := ansi.Strip(app.View())
				if rows := strings.Count(view, "\n") + 1; rows > height {
					t.Errorf("view takes %d terminal rows, want at most %d", rows, height)
				}
				if bn.text != "" && !strings.Contains(view, bn.text) {
					t.Errorf("view doesn't show the banner %q", bn.text)
				}
			})
		}
	}
}

// clearBanner takes down the status/error banner App draws above every
// screen. It sets the fields directly rather than going through Update,
// so nothing is refitted: whatever size a screen has stays as it is.
func clearBanner(app *App) *App {
	app.status, app.err = "", nil
	return app
}
