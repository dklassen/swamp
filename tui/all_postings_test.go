package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
