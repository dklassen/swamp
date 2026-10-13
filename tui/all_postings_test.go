package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/filter"
	"github.com/dklassen/swamp/store"
)

// mustUpsertPostingAt is mustUpsertPosting with a department, location and
// workplace type.
func mustUpsertPostingAt(t *testing.T, s *store.Store, companyID int64, sourceID, title, department, location, workplaceType string) store.Posting {
	t.Helper()
	p, err := s.UpsertPosting(context.Background(), store.CreatePostingParams{
		CompanyID: companyID,
		Source:    "ashby",
		SourceID:  sourceID,
		IngestedFields: store.IngestedFields{
			Title:         title,
			Department:    department,
			Location:      location,
			WorkplaceType: workplaceType,
		},
	})
	if err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	return p
}

// openAllPostings tabs from the home list to the all-postings view,
// applying the loads each tab starts.
func openAllPostings(t *testing.T, app *App) *App {
	t.Helper()
	for range 2 {
		app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyTab})
	}
	if app.screen != screenAllPostings {
		t.Fatalf("screen after two tabs = %v, want all postings", app.screen)
	}
	return app
}

func TestAllPostings_ShowsOpenPostingsAcrossCompaniesNewestFirst(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	globex := mustCreateCompany(t, s, "Globex", "ashby", "globex")
	older := mustUpsertPostingAt(t, s, acme.ID, "1", "Backend Engineer", "Platform", "Toronto", "Hybrid")
	shelved := mustUpsertPostingAt(t, s, acme.ID, "2", "Shelved Role", "Platform", "Toronto", "")
	mustUpsertPostingAt(t, s, globex.ID, "3", "Account Executive", "Sales", "Remote", "")
	newer := mustUpsertPostingAt(t, s, globex.ID, "4", "Platform Engineer", "Infra", "New York", "")
	if _, err := s.SetPostingArchived(ctx, shelved.ID); err != nil {
		t.Fatalf("SetPostingArchived: %v", err)
	}
	if _, err := s.SetPostingInterested(ctx, older.ID); err != nil {
		t.Fatalf("SetPostingInterested: %v", err)
	}
	if _, err := s.CreateCompanyFilter(ctx, globex.ID, filter.FieldDepartment, "Infra"); err != nil {
		t.Fatalf("CreateCompanyFilter: %v", err)
	}

	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openAllPostings(t, app)

	view := strings.Join(viewLines(app), "\n")
	for _, hidden := range []string{"Shelved Role", "Account Executive"} {
		if strings.Contains(view, hidden) {
			t.Errorf("view shows %q, which is archived or hidden by its company's filters:\n%s", hidden, view)
		}
	}
	newerAt, olderAt := strings.Index(view, "Platform Engineer"), strings.Index(view, "Backend Engineer")
	if newerAt < 0 || olderAt < 0 || newerAt > olderAt {
		t.Fatalf("want Platform Engineer above Backend Engineer, newest first:\n%s", view)
	}
	for _, want := range []string{
		older.FirstSeenAt.Local().Format("Jan 2"), "Acme", "Platform", "Toronto (Hybrid)", "★",
		newer.FirstSeenAt.Local().Format("Jan 2"), "Globex", "Infra", "New York",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
}

// pageTitles is the titles on the page on screen, top to bottom.
func pageTitles(app *App) []string {
	var titles []string
	for _, l := range app.allPostings.page.Listings {
		titles = append(titles, l.Title)
	}
	return titles
}

func TestAllPostings_PagesForwardAndBack(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	for i := 1; i <= 7; i++ {
		mustUpsertPosting(t, s, acme.ID, fmt.Sprint(i), fmt.Sprintf("Role %d", i))
	}
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 13})
	app = openAllPostings(t, app)
	size := app.allPostings.size
	if size < 1 || size >= 7 {
		t.Fatalf("page size = %d at height 13, want fewer than seven so they take several pages", size)
	}
	pages := (7 + size - 1) / size
	// want is page n's titles, newest first.
	want := func(n int) []string {
		var titles []string
		for i := 7 - (n-1)*size; i >= 1 && len(titles) < size; i-- {
			titles = append(titles, fmt.Sprintf("Role %d", i))
		}
		return titles
	}
	check := func(n int) {
		t.Helper()
		if diff := cmp.Diff(want(n), pageTitles(app)); diff != "" {
			t.Errorf("page %d titles (-want +got):\n%s", n, diff)
		}
		if line := fmt.Sprintf("Page %d of %d", n, pages); !strings.Contains(app.View(), line) {
			t.Errorf("view doesn't say %q:\n%s", line, app.View())
		}
	}

	check(1)
	app = sendKeyAndApply(t, app, runeKey('h'))
	check(1) // no page before the first
	for n := 2; n <= pages; n++ {
		key := runeKey('l')
		if n%2 == 0 {
			key = tea.KeyMsg{Type: tea.KeyRight}
		}
		app = sendKeyAndApply(t, app, key)
		check(n)
	}
	app = sendKeyAndApply(t, app, runeKey('l'))
	check(pages) // no page after the last
	for n := pages - 1; n >= 1; n-- {
		key := runeKey('h')
		if n%2 == 0 {
			key = tea.KeyMsg{Type: tea.KeyLeft}
		}
		app = sendKeyAndApply(t, app, key)
		check(n)
	}
}

func TestAllPostings_ResizingReadsThePageAtTheNewSize(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	for i := 1; i <= 30; i++ {
		mustUpsertPosting(t, s, acme.ID, fmt.Sprint(i), fmt.Sprintf("Role %d", i))
	}
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 40})
	app = openAllPostings(t, app)
	tall := app.allPostings.size

	app = sendKeyAndApply(t, app, tea.WindowSizeMsg{Width: 120, Height: 13})

	if short := app.allPostings.pageSize(app.screenRows()); short >= tall || app.allPostings.size != short || len(app.allPostings.page.Listings) != short {
		t.Errorf("after shrinking: page read at %d rows with %d listings, want %d (it was %d)", app.allPostings.size, len(app.allPostings.page.Listings), short, tall)
	}
}
