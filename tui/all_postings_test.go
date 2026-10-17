package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/internal/domain/filter"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
)

// mustUpsertPostingAt is mustUpsertPosting with a department, location and
// workplace type.
func mustUpsertPostingAt(t *testing.T, s testutil.TestApp, companyID int64, sourceID, title, department, location, workplaceType string) posting.Posting {
	t.Helper()
	ingested, err := s.Postings.Ingest(context.Background(), companyID, jobboard.Ashby, sourceID, posting.IngestedFields{
		Title:         title,
		Department:    department,
		Location:      location,
		WorkplaceType: workplaceType,
	})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return ingested.Posting
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
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	globex := mustCreateCompany(t, s, "Globex", jobboard.Ashby, "globex")
	older := mustUpsertPostingAt(t, s, acme.ID, "1", "Backend Engineer", "Platform", "Toronto", "Hybrid")
	shelved := mustUpsertPostingAt(t, s, acme.ID, "2", "Shelved Role", "Platform", "Toronto", "")
	mustUpsertPostingAt(t, s, globex.ID, "3", "Account Executive", "Sales", "Remote", "")
	newer := mustUpsertPostingAt(t, s, globex.ID, "4", "Platform Engineer", "Infra", "New York", "")
	if _, err := s.Postings.SetArchived(ctx, shelved.ID); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}
	if _, err := s.Postings.SetInterested(ctx, older.ID); err != nil {
		t.Fatalf("SetInterested: %v", err)
	}
	addCompanyFilter(t, s, globex.ID, filter.FieldDepartment, "Infra")

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

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
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

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
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

func TestAllPostings_EnterOpensTheDetailAndEscComesBack(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	for i := 1; i <= 7; i++ {
		mustUpsertPosting(t, s, acme.ID, fmt.Sprint(i), fmt.Sprintf("Role %d", i))
	}
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 13})
	app = openAllPostings(t, app)
	for _, key := range []tea.KeyMsg{runeKey('l'), runeKey('j')} {
		app = sendKeyAndApply(t, app, key)
	}
	want := app.allPostings.page.Listings[1]

	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEnter})

	if app.screen != screenPostingDetail || app.postingDetail.posting.ID != want.ID || app.postingDetail.posting.Title != want.Title {
		t.Fatalf("after enter: screen %v showing posting %d %q, want detail for %d %q", app.screen, app.postingDetail.posting.ID, app.postingDetail.posting.Title, want.ID, want.Title)
	}
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEsc})
	if app.screen != screenAllPostings || len(app.allPostings.starts) != 2 || app.allPostings.cursor != 1 {
		t.Errorf("after esc: screen %v, page %d, row %d; want all postings, page 2, row 1", app.screen, len(app.allPostings.starts), app.allPostings.cursor)
	}
}

// A company's list browsed earlier stays in a.postings, but its order
// isn't the all-postings page's, so the detail mustn't step through it.
func TestAllPostings_DetailDoesNotStepThroughACompanyListBrowsedEarlier(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	for i := 1; i <= 3; i++ {
		mustUpsertPosting(t, s, acme.ID, fmt.Sprint(i), fmt.Sprintf("Role %d", i))
	}
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyTab})
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	if app.screen != screenPostingList || len(app.postings) != 3 {
		t.Fatalf("screen %v with %d postings, want Acme's posting list with 3", app.screen, len(app.postings))
	}
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEsc})
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyTab})

	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEnter})

	if app.screen != screenPostingDetail {
		t.Fatalf("screen = %v, want posting detail", app.screen)
	}
	if app.postingDetail.canNavigateSiblings {
		t.Error("posting detail opened from all postings offers stepping through Acme's posting list")
	}
}

func TestAllPostings_IMarksAndUnmarksInterested(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustUpsertPosting(t, s, acme.ID, "1", "Role 1")
	posting := mustUpsertPosting(t, s, acme.ID, "2", "Role 2")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openAllPostings(t, app)

	for _, want := range []bool{true, false} {
		app = sendKeyAndApply(t, app, runeKey('i'))
		markup, err := s.Postings.Markup(context.Background(), posting.ID)
		if err != nil {
			t.Fatalf("Markup: %v", err)
		}
		if got := markup.InterestedAt != nil; got != want {
			t.Errorf("after i: interested = %v, want %v", got, want)
		}
		if got := app.allPostings.page.Listings[0]; got.ID != posting.ID || got.Interested != want {
			t.Errorf("after i: top row = %d interested %v, want %d interested %v", got.ID, got.Interested, posting.ID, want)
		}
		if got := strings.Contains(app.View(), "★"); got != want {
			t.Errorf("after i: view shows ★ = %v, want %v", got, want)
		}
	}
}

func TestAllPostings_XArchivesAndThePostingLeavesThePage(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	for i := 1; i <= 3; i++ {
		mustUpsertPosting(t, s, acme.ID, fmt.Sprint(i), fmt.Sprintf("Role %d", i))
	}
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openAllPostings(t, app)
	app = sendKeyAndApply(t, app, runeKey('j'))
	archived := app.allPostings.page.Listings[1]

	app = sendKeyAndApply(t, app, runeKey('x'))

	markup, err := s.Postings.Markup(context.Background(), archived.ID)
	if err != nil {
		t.Fatalf("Markup: %v", err)
	}
	if markup.ArchivedAt == nil {
		t.Fatalf("%q isn't archived after x", archived.Title)
	}
	if diff := cmp.Diff([]string{"Role 3", "Role 1"}, pageTitles(app)); diff != "" {
		t.Errorf("page after archiving %q (-want +got):\n%s", archived.Title, diff)
	}
	if app.allPostings.page.Total != 2 || app.allPostings.cursor != 1 {
		t.Errorf("Total %d, cursor on row %d; want 2, on row 1", app.allPostings.page.Total, app.allPostings.cursor)
	}
}

func TestAllPostings_ArchivingTheLastPagesOnlyPostingStepsBack(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 13})
	size := app.allPostings.pageSize(app.screenRows())
	// One more than a page: the second page has one posting.
	for i := 1; i <= size+1; i++ {
		mustUpsertPosting(t, s, acme.ID, fmt.Sprint(i), fmt.Sprintf("Role %d", i))
	}
	app = openAllPostings(t, app)
	app = sendKeyAndApply(t, app, runeKey('l'))
	if len(app.allPostings.starts) != 2 || len(app.allPostings.page.Listings) != 1 {
		t.Fatalf("on page %d with %d postings, want page 2 with 1", len(app.allPostings.starts), len(app.allPostings.page.Listings))
	}

	app = sendKeyAndApply(t, app, runeKey('x'))

	if len(app.allPostings.starts) != 1 || len(app.allPostings.page.Listings) != size {
		t.Errorf("after archiving page 2's only posting: page %d with %d postings, want page 1 with %d", len(app.allPostings.starts), len(app.allPostings.page.Listings), size)
	}
}

func TestAllPostings_OOpensTheJobURL(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	if _, err := s.Postings.Ingest(context.Background(), acme.ID, jobboard.Ashby, "1", posting.IngestedFields{Title: "Engineer", JobURL: "https://jobs.example.com/acme/1"}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	app := newTestApp(t, s, newTestSyncer(s, nil))
	var opened []string
	app.openURL = func(url string) tea.Cmd {
		opened = append(opened, url)
		return nil
	}
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openAllPostings(t, app)

	sendKeyAndApply(t, app, runeKey('o'))

	if diff := cmp.Diff([]string{"https://jobs.example.com/acme/1"}, opened); diff != "" {
		t.Errorf("URLs opened (-want +got):\n%s", diff)
	}
}

// Another process's new posting updates the count and lands on page 1,
// not under the page the user is reading.
func TestAllPostings_ANewPostingElsewhere_KeepsThePageAndCountsIt(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	for i := 1; i <= 7; i++ {
		mustUpsertPosting(t, s, acme.ID, fmt.Sprint(i), fmt.Sprintf("Role %d", i))
	}
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeLog(0, "tui:1")
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 13})
	app = openAllPostings(t, app)
	for _, key := range []tea.KeyMsg{runeKey('l'), runeKey('j')} {
		app = sendKeyAndApply(t, app, key)
	}
	page2 := pageTitles(app)

	added := mustUpsertPosting(t, s, acme.ID, "8", "Role 8")
	app = sendKeyAndApply(t, app, changesMsg{view: app.screen, events: []change.Event{{Table: "postings", RowID: added.ID, Op: "insert", Origin: "fetch:9"}}})

	if diff := cmp.Diff(page2, pageTitles(app)); diff != "" {
		t.Errorf("page 2 changed under the user (-before +after):\n%s", diff)
	}
	if len(app.allPostings.starts) != 2 || app.allPostings.cursor != 1 || app.allPostings.page.Total != 8 {
		t.Errorf("page %d, row %d, Total %d; want page 2, row 1, Total 8", len(app.allPostings.starts), app.allPostings.cursor, app.allPostings.page.Total)
	}
	app = sendKeyAndApply(t, app, runeKey('h'))
	if titles := pageTitles(app); len(titles) == 0 || titles[0] != "Role 8" {
		t.Errorf("page 1 = %q, want Role 8 first", titles)
	}
}

func TestAllPostings_MarkedInterestedElsewhere_ShowsTheStar(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "1", "Role 1")
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeLog(0, "tui:1")
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openAllPostings(t, app)

	if _, err := s.Postings.SetInterested(context.Background(), posting.ID); err != nil {
		t.Fatalf("SetInterested: %v", err)
	}
	app = sendKeyAndApply(t, app, changesMsg{view: app.screen, events: []change.Event{{Table: "posting_markup", RowID: posting.ID, Op: "update", Origin: "tui:2"}}})

	if !strings.Contains(app.View(), "★") {
		t.Errorf("view doesn't star the posting another window marked interested:\n%s", app.View())
	}
}

func TestAllPostings_SlashOpensTheSearchLine(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustUpsertPosting(t, s, acme.ID, "1", "Role 1")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openAllPostings(t, app)
	if view := app.View(); !strings.Contains(view, "/ to search") {
		t.Errorf("view doesn't offer / to search:\n%s", view)
	}

	app = sendKeyAndApply(t, app, runeKey('/'))

	if !app.allPostings.searching || !strings.Contains(app.View(), "🔍 ▏") {
		t.Errorf("after /: searching %v, view:\n%s", app.allPostings.searching, app.View())
	}
}

// Typing reloads page 1 once, after a pause: the timer a key starts does
// nothing if another key came after it.
func TestAllPostings_TypingSearchesAfterAPause(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	globex := mustCreateCompany(t, s, "Globex", jobboard.Ashby, "globex")
	mustUpsertPosting(t, s, acme.ID, "1", "Platform Engineer")
	mustUpsertPosting(t, s, acme.ID, "2", "Designer")
	mustUpsertPosting(t, s, globex.ID, "3", "Platform Lead")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openAllPostings(t, app)
	app = sendKeyAndApply(t, app, runeKey('/'))

	var timers []tea.Cmd
	for _, r := range "plat" {
		var cmd tea.Cmd
		app, cmd = sendKey(app, runeKey(r))
		timers = append(timers, cmd)
	}
	if app.allPostings.query != "plat" || len(pageTitles(app)) != 3 {
		t.Fatalf("while typing: query %q, %d postings; want \"plat\" and the page not yet reloaded", app.allPostings.query, len(pageTitles(app)))
	}

	app = applyCmd(t, app, timers[0])
	if len(pageTitles(app)) != 3 {
		t.Errorf("the first key's timer reloaded the page: %q", pageTitles(app))
	}
	app = applyCmd(t, app, timers[len(timers)-1])

	if diff := cmp.Diff([]string{"Platform Lead", "Platform Engineer"}, pageTitles(app)); diff != "" {
		t.Errorf("after the pause (-want +got):\n%s", diff)
	}
	if !strings.Contains(app.View(), "2 of 3") {
		t.Errorf("search line doesn't say 2 of 3:\n%s", app.View())
	}
}

// searchAllPostings opens the search line and types query, then runs the
// search its pause would.
func searchAllPostings(t *testing.T, app *App, query string) *App {
	t.Helper()
	app = sendKeyAndApply(t, app, runeKey('/'))
	for _, r := range query {
		app, _ = sendKey(app, runeKey(r))
	}
	return sendKeyAndApply(t, app, allPostingsSearchMsg{typed: app.allPostings.typed})
}

func TestAllPostings_EscClearsTheSearch(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustUpsertPosting(t, s, acme.ID, "1", "Platform Engineer")
	mustUpsertPosting(t, s, acme.ID, "2", "Designer")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openAllPostings(t, app)
	app = searchAllPostings(t, app, "design")
	if diff := cmp.Diff([]string{"Designer"}, pageTitles(app)); diff != "" {
		t.Fatalf("search results (-want +got):\n%s", diff)
	}

	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEsc})

	if app.allPostings.searching || app.allPostings.query != "" {
		t.Errorf("after esc: searching %v, query %q; want closed and empty", app.allPostings.searching, app.allPostings.query)
	}
	if diff := cmp.Diff([]string{"Designer", "Platform Engineer"}, pageTitles(app)); diff != "" {
		t.Errorf("after esc (-want +got):\n%s", diff)
	}
	if app.screen != screenAllPostings {
		t.Errorf("esc left the view: screen %v", app.screen)
	}
}

func TestAllPostings_EnterKeepsTheResultsAndKeysActOnThem(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustUpsertPosting(t, s, acme.ID, "1", "Platform Engineer")
	designer := mustUpsertPosting(t, s, acme.ID, "2", "Designer")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openAllPostings(t, app)
	app = searchAllPostings(t, app, "design")

	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	app = sendKeyAndApply(t, app, runeKey('i'))

	if app.screen != screenAllPostings || app.allPostings.searching || app.allPostings.query != "design" {
		t.Errorf("after enter, i: screen %v, searching %v, query %q; want the view, not typing, query \"design\"", app.screen, app.allPostings.searching, app.allPostings.query)
	}
	markup, err := s.Postings.Markup(context.Background(), designer.ID)
	if err != nil {
		t.Fatalf("Markup: %v", err)
	}
	if markup.InterestedAt == nil {
		t.Error("i didn't mark the highlighted match interested")
	}
	if diff := cmp.Diff([]string{"Designer"}, pageTitles(app)); diff != "" {
		t.Errorf("results after enter, i (-want +got):\n%s", diff)
	}
}

func TestAllPostings_View_KeptResults(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustUpsertPosting(t, s, acme.ID, "1", "Platform Engineer")
	mustUpsertPosting(t, s, acme.ID, "2", "Designer")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 200, Height: 30})
	app = openAllPostings(t, app)
	app = searchAllPostings(t, app, "design")
	if view := app.View(); !strings.Contains(view, "enter: keep results") {
		t.Errorf("typing help doesn't say enter keeps the results:\n%s", view)
	}

	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEnter})

	view := app.View()
	for _, want := range []string{"🔍 design  1 of 2", "/: edit search", "esc: clear search", "i: interested"} {
		if !strings.Contains(view, want) {
			t.Errorf("view doesn't show %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "▏") {
		t.Errorf("view shows a typing caret after enter:\n%s", view)
	}
}

func TestAllPostings_SlashEditsKeptResultsQuery(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustUpsertPosting(t, s, acme.ID, "1", "Designer")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openAllPostings(t, app)
	app = searchAllPostings(t, app, "design")
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEnter})

	app = sendKeyAndApply(t, app, runeKey('/'))
	app, _ = sendKey(app, runeKey('i'))

	if !app.allPostings.searching || app.allPostings.query != "designi" {
		t.Errorf("after /, i: searching %v, query %q; want typing, \"designi\"", app.allPostings.searching, app.allPostings.query)
	}
}

func TestAllPostings_EscClearsKeptResults(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustUpsertPosting(t, s, acme.ID, "1", "Platform Engineer")
	mustUpsertPosting(t, s, acme.ID, "2", "Designer")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openAllPostings(t, app)
	app = searchAllPostings(t, app, "design")
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEnter})

	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEsc})

	if app.screen != screenAllPostings || app.allPostings.query != "" {
		t.Errorf("after esc: screen %v, query %q; want the view, query cleared", app.screen, app.allPostings.query)
	}
	if diff := cmp.Diff([]string{"Designer", "Platform Engineer"}, pageTitles(app)); diff != "" {
		t.Errorf("after esc (-want +got):\n%s", diff)
	}
}

func TestAllPostings_PagesThroughSearchResults(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	for i := 1; i <= 7; i++ {
		mustUpsertPosting(t, s, acme.ID, fmt.Sprintf("p%d", i), fmt.Sprintf("Platform %d", i))
		mustUpsertPosting(t, s, acme.ID, fmt.Sprintf("d%d", i), fmt.Sprintf("Designer %d", i))
	}
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 13})
	app = openAllPostings(t, app)
	app = searchAllPostings(t, app, "platform")
	size := app.allPostings.size
	if size >= 7 {
		t.Fatalf("page size %d, want fewer than the 7 matches", size)
	}
	pages := (7 + size - 1) / size

	var seen []string
	for n := 1; n <= pages; n++ {
		if line := fmt.Sprintf("Page %d of %d · 7 postings", n, pages); !strings.Contains(app.View(), line) {
			t.Errorf("view doesn't say %q:\n%s", line, app.View())
		}
		seen = append(seen, pageTitles(app)...)
		app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyRight})
	}
	want := []string{"Platform 7", "Platform 6", "Platform 5", "Platform 4", "Platform 3", "Platform 2", "Platform 1"}
	if diff := cmp.Diff(want, seen); diff != "" {
		t.Errorf("paging through the matches (-want +got):\n%s", diff)
	}
	if !app.allPostings.searching || app.allPostings.query != "platform" {
		t.Errorf("paging closed the search: searching %v, query %q", app.allPostings.searching, app.allPostings.query)
	}
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyLeft})
	if len(app.allPostings.starts) != pages-1 {
		t.Errorf("← went to page %d, want %d", len(app.allPostings.starts), pages-1)
	}
}

func TestAllPostings_NoMatches_SaysWhatWasSearched(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustUpsertPosting(t, s, acme.ID, "1", "Platform Engineer")
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openAllPostings(t, app)

	app = searchAllPostings(t, app, "kafka")

	if view := app.View(); !strings.Contains(view, `No postings match "kafka".`) {
		t.Errorf("view doesn't say what matched nothing:\n%s", view)
	}
}

// mustScore records fit for p against a fixed profile.
func mustScore(t *testing.T, s testutil.TestApp, p posting.Posting, fit int, profile string) {
	t.Helper()
	if _, err := recordScores(context.Background(), s, score.Provenance{Model: "claude-opus-5-5", ProfileSHA256: profile}, []score.Entry{
		{PostingID: p.ID, Score: fit, Reason: "reason", ContentSHA256: p.ContentSHA256},
	}); err != nil {
		t.Fatalf("recordScores: %v", err)
	}
}

func TestAllPostings_ShowsEachPostingsFit(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	scored := mustUpsertPosting(t, s, acme.ID, "1", "Scored")
	stale := mustUpsertPosting(t, s, acme.ID, "2", "Rewritten since")
	mustUpsertPosting(t, s, acme.ID, "3", "Unscored")
	mustScore(t, s, scored, 4, "p")
	mustScore(t, s, stale, 2, "p")
	if _, err := s.Postings.Ingest(context.Background(), acme.ID, jobboard.Ashby, "2", posting.IngestedFields{Title: "Rewritten since", DescriptionText: "New text."}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 140, Height: 30})
	app = openAllPostings(t, app)

	fit := map[string]string{}
	for _, line := range viewLines(app) {
		for _, title := range []string{"Scored", "Rewritten since", "Unscored"} {
			if strings.Contains(line, title) {
				fields := strings.Split(line, "│")
				fit[title] = strings.TrimSpace(fields[len(fields)-2])
			}
		}
	}
	if diff := cmp.Diff(map[string]string{"Scored": "4", "Rewritten since": "2", "Unscored": ""}, fit); diff != "" {
		t.Errorf("Fit column (-want +got):\n%s", diff)
	}
	if !strings.Contains(app.View(), dimStyle.Render(padCol("2", fitColWidth))) {
		t.Error("the stale score isn't drawn dimmed")
	}
}

func TestAllPostings_SSortsByFitAndBack(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	var postings []posting.Posting
	for i := 1; i <= 6; i++ {
		postings = append(postings, mustUpsertPosting(t, s, acme.ID, fmt.Sprint(i), fmt.Sprintf("Role %d", i)))
	}
	// Scores by first seen: 1:3, 2:5, 3:none, 4:1, 5:5, 6:none.
	for i, score := range map[int]int{0: 3, 1: 5, 3: 1, 4: 5} {
		mustScore(t, s, postings[i], score, "p")
	}
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 140, Height: 13})
	app = openAllPostings(t, app)
	app = sendKeyAndApply(t, app, runeKey('l')) // leave page 1, which s returns to

	app = sendKeyAndApply(t, app, runeKey('s'))

	if len(app.allPostings.starts) != 1 || !strings.Contains(app.View(), "best fit first") {
		t.Fatalf("after s: page %d, view:\n%s", len(app.allPostings.starts), app.View())
	}
	var seen []string
	for {
		seen = append(seen, pageTitles(app)...)
		if !app.allPostings.page.HasMore {
			break
		}
		app = sendKeyAndApply(t, app, runeKey('l'))
	}
	want := []string{"Role 5", "Role 2", "Role 1", "Role 4", "Role 6", "Role 3"}
	if diff := cmp.Diff(want, seen); diff != "" {
		t.Errorf("by fit, page by page (-want +got):\n%s", diff)
	}

	app = sendKeyAndApply(t, app, runeKey('s'))

	if titles := pageTitles(app); len(app.allPostings.starts) != 1 || len(titles) == 0 || titles[0] != "Role 6" {
		t.Errorf("after s again: page %d starting %q, want page 1 newest first", len(app.allPostings.starts), titles)
	}
}

func TestAllPostings_AScoreRecordedElsewhere_Shows(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	p := mustUpsertPosting(t, s, acme.ID, "1", "Role 1")
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeLog(0, "tui:1")
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 140, Height: 30})
	app = openAllPostings(t, app)

	mustScore(t, s, p, 4, "p")
	app = sendKeyAndApply(t, app, changesMsg{view: app.screen, events: []change.Event{{Table: "posting_scores", RowID: 1, Op: "insert", Origin: "mcp:7"}}})

	if got := app.allPostings.page.Listings[0].Score; got != 4 {
		t.Errorf("score after the agent recorded one = %d, want 4", got)
	}
}

func TestPostingDetail_ShowsTheScoresAndFollowsNewOnes(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	p := mustUpsertPosting(t, s, acme.ID, "1", "Platform Engineer")
	if _, err := recordScores(context.Background(), s, score.Provenance{Model: "claude-sonnet-5", ClientName: "claude-code", ClientVersion: "2.4.0", ProfileSHA256: "p"}, []score.Entry{
		{PostingID: p.ID, Score: 2, Reason: "first look", ContentSHA256: p.ContentSHA256},
	}); err != nil {
		t.Fatalf("recordScores: %v", err)
	}
	if _, err := recordScores(context.Background(), s, score.Provenance{Model: "claude-opus-5-5", ClientName: "claude-code", ClientVersion: "2.4.1", ProfileSHA256: "edited p"}, []score.Entry{
		{PostingID: p.ID, Score: 4, Reason: "Go and Kafka, remote", ContentSHA256: p.ContentSHA256},
	}); err != nil {
		t.Fatalf("recordScores: %v", err)
	}
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeLog(0, "tui:1")
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 140, Height: 60})
	app = openAllPostings(t, app)
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEnter})

	view := ansi.Strip(app.View())
	for _, want := range []string{"Fit", "4 · Go and Kafka, remote", "claude-opus-5-5", "claude-code 2.4.1", "2 · first look", "claude-sonnet-5"} {
		if !strings.Contains(view, want) {
			t.Errorf("detail is missing %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "4 · Go and Kafka") > strings.Index(view, "2 · first look") {
		t.Errorf("the current score isn't above the earlier one:\n%s", view)
	}

	if _, err := recordScores(context.Background(), s, score.Provenance{Model: "claude-opus-5-5", ProfileSHA256: "rewritten p"}, []score.Entry{
		{PostingID: p.ID, Score: 5, Reason: "after the rewrite", ContentSHA256: p.ContentSHA256},
	}); err != nil {
		t.Fatalf("recordScores: %v", err)
	}
	app = sendKeyAndApply(t, app, changesMsg{view: app.screen, events: []change.Event{{Table: "posting_scores", RowID: 3, Op: "insert", New: fmt.Sprintf(`{"posting_id":%d,"score":5}`, p.ID), Origin: "mcp:7"}}})

	if view := ansi.Strip(app.View()); !strings.Contains(view, "5 · after the rewrite") {
		t.Errorf("detail doesn't show the score the agent just recorded:\n%s", view)
	}
}
