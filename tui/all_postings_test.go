package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"

	"github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/internal/domain/filter"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/domain/score"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
	"github.com/dklassen/swamp/tui/model"
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
		app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyTab})
	}
	if app.currentScreen != screenAllPostings {
		t.Fatalf("screen after two tabs = %v, want all postings", app.currentScreen)
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
	taken := mustUpsertPostingAt(t, s, acme.ID, "5", "Filled Role", "Platform", "Toronto", "")
	if _, err := s.Postings.SetArchived(ctx, shelved.ID); err != nil {
		t.Fatalf("SetArchived: %v", err)
	}
	if err := s.Postings.Close(ctx, taken.ID, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := s.Postings.SetInterested(ctx, older.ID); err != nil {
		t.Fatalf("SetInterested: %v", err)
	}
	addCompanyFilter(t, s, globex.ID, filter.FieldDepartment, "Infra")

	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openAllPostings(t, app)

	view := strings.Join(viewLines(app), "\n")
	for _, hidden := range []string{"Shelved Role", "Account Executive", "Filled Role"} {
		if strings.Contains(view, hidden) {
			t.Errorf("view shows %q, which is archived, closed or hidden by its company's filters:\n%s", hidden, view)
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
	size := app.allPostings.page.Limit
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
		if line := fmt.Sprintf("Page %d of %d", n, pages); !strings.Contains(viewText(app), line) {
			t.Errorf("view doesn't say %q:\n%s", line, viewText(app))
		}
	}

	check(1)
	app = sendKeyAndApply(t, app, runeKey('h'))
	check(1) // no page before the first
	for n := 2; n <= pages; n++ {
		key := runeKey('l')
		if n%2 == 0 {
			key = tea.KeyPressMsg{Code: tea.KeyRight}
		}
		app = sendKeyAndApply(t, app, key)
		check(n)
	}
	app = sendKeyAndApply(t, app, runeKey('l'))
	check(pages) // no page after the last
	for n := pages - 1; n >= 1; n-- {
		key := runeKey('h')
		if n%2 == 0 {
			key = tea.KeyPressMsg{Code: tea.KeyLeft}
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
	tall := app.allPostings.page.Limit

	app = sendKeyAndApply(t, app, tea.WindowSizeMsg{Width: 120, Height: 13})

	if short := app.allPostings.pageSize(app.screenRows()); short >= tall || app.allPostings.page.Limit != short || len(app.allPostings.page.Listings) != short {
		t.Errorf("after shrinking: page read at %d rows with %d listings, want %d (it was %d)", app.allPostings.page.Limit, len(app.allPostings.page.Listings), short, tall)
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
	for _, key := range []tea.KeyPressMsg{runeKey('l'), runeKey('j')} {
		app = sendKeyAndApply(t, app, key)
	}
	want := app.allPostings.page.Listings[1]

	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})

	if app.currentScreen != screenPostingDetail || openDetail(t, app).posting.ID != want.ID || openDetail(t, app).posting.Title != want.Title {
		t.Fatalf("after enter: screen %v showing posting %d %q, want detail for %d %q", app.currentScreen, openDetail(t, app).posting.ID, openDetail(t, app).posting.Title, want.ID, want.Title)
	}
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEsc})
	if app.currentScreen != screenAllPostings || len(app.allPostings.starts) != 2 || app.allPostings.cursor != 1 {
		t.Errorf("after esc: screen %v, page %d, row %d; want all postings, page 2, row 1", app.currentScreen, len(app.allPostings.starts), app.allPostings.cursor)
	}
}

// A company's list browsed earlier isn't in the all-postings page's order,
// so the detail mustn't step through it.
func TestAllPostings_DetailDoesNotStepThroughACompanyListBrowsedEarlier(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	for i := 1; i <= 3; i++ {
		mustUpsertPosting(t, s, acme.ID, fmt.Sprint(i), fmt.Sprintf("Role %d", i))
	}
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyTab})
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.currentScreen != screenPostingList || len(openList(t, app).list.Postings) != 3 {
		t.Fatalf("screen %v with %d postings, want Acme's posting list with 3", app.currentScreen, len(openList(t, app).list.Postings))
	}
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEsc})
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyTab})

	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen = %v, want posting detail", app.currentScreen)
	}
	if openDetail(t, app).canStep() {
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
		if got := strings.Contains(viewText(app), "★"); got != want {
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
	app.allPostings.openURL = func(_ message.Source, _ message.Subject, url string) tea.Cmd {
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
	for _, key := range []tea.KeyPressMsg{runeKey('l'), runeKey('j')} {
		app = sendKeyAndApply(t, app, key)
	}
	page2 := pageTitles(app)

	added := mustUpsertPosting(t, s, acme.ID, "8", "Role 8")
	app = sendKeyAndApply(t, app, changesMsg{view: app.currentScreen, events: []change.Event{{Table: "postings", RowID: added.ID, Op: "insert", Origin: "fetch:9"}}})

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
	app = sendKeyAndApply(t, app, changesMsg{view: app.currentScreen, events: []change.Event{{Table: "posting_markup", RowID: posting.ID, Op: "update", Origin: "tui:2"}}})

	if !strings.Contains(viewText(app), "★") {
		t.Errorf("view doesn't star the posting another window marked interested:\n%s", viewText(app))
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
	if view := viewText(app); !strings.Contains(view, "/ to search") {
		t.Errorf("view doesn't offer / to search:\n%s", view)
	}

	app = sendKeyAndApply(t, app, runeKey('/'))

	if !app.allPostings.searching || !strings.Contains(viewText(app), "🔍 ▏") {
		t.Errorf("after /: searching %v, view:\n%s", app.allPostings.searching, viewText(app))
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
	if !strings.Contains(viewText(app), "2 of 3") {
		t.Errorf("search line doesn't say 2 of 3:\n%s", viewText(app))
	}
}

// The total a search counts against is the postings the view shows, so a
// closed posting counts toward neither side.
func TestAllPostings_SearchCountsOnlyOpenPostings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustUpsertPosting(t, s, acme.ID, "1", "Platform Engineer")
	mustUpsertPosting(t, s, acme.ID, "2", "Designer")
	filled := mustUpsertPosting(t, s, acme.ID, "3", "Platform Lead")
	if err := s.Postings.Close(ctx, filled.ID, nil); err != nil {
		t.Fatalf("Close: %v", err)
	}
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openAllPostings(t, app)

	app = searchAllPostings(t, app, "plat")

	if diff := cmp.Diff([]string{"Platform Engineer"}, pageTitles(app)); diff != "" {
		t.Errorf("search results (-want +got):\n%s", diff)
	}
	if !strings.Contains(viewText(app), "1 of 2") {
		t.Errorf("search line doesn't say 1 of 2:\n%s", viewText(app))
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
	return sendKeyAndApply(t, app, message.New(message.Source{Instance: app.allPostings.Instance()}, message.Subject{}, message.OpNone, time.Now().UTC(),
		message.Signal[message.SearchDebounce]{Value: message.SearchDebounce{Query: app.allPostings.query}}))
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

	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEsc})

	if app.allPostings.searching || app.allPostings.query != "" {
		t.Errorf("after esc: searching %v, query %q; want closed and empty", app.allPostings.searching, app.allPostings.query)
	}
	if diff := cmp.Diff([]string{"Designer", "Platform Engineer"}, pageTitles(app)); diff != "" {
		t.Errorf("after esc (-want +got):\n%s", diff)
	}
	if app.currentScreen != screenAllPostings {
		t.Errorf("esc left the view: screen %v", app.currentScreen)
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

	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app = sendKeyAndApply(t, app, runeKey('i'))

	if app.currentScreen != screenAllPostings || app.allPostings.searching || app.allPostings.query != "design" {
		t.Errorf("after enter, i: screen %v, searching %v, query %q; want the view, not typing, query \"design\"", app.currentScreen, app.allPostings.searching, app.allPostings.query)
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
	if view := viewText(app); !strings.Contains(view, "enter: keep results") {
		t.Errorf("typing help doesn't say enter keeps the results:\n%s", view)
	}

	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})

	view := viewText(app)
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
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})

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
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})

	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEsc})

	if app.currentScreen != screenAllPostings || app.allPostings.query != "" {
		t.Errorf("after esc: screen %v, query %q; want the view, query cleared", app.currentScreen, app.allPostings.query)
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
	size := app.allPostings.page.Limit
	if size >= 7 {
		t.Fatalf("page size %d, want fewer than the 7 matches", size)
	}
	pages := (7 + size - 1) / size

	var seen []string
	for n := 1; n <= pages; n++ {
		if line := fmt.Sprintf("Page %d of %d · 7 postings", n, pages); !strings.Contains(viewText(app), line) {
			t.Errorf("view doesn't say %q:\n%s", line, viewText(app))
		}
		seen = append(seen, pageTitles(app)...)
		app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyRight})
	}
	want := []string{"Platform 7", "Platform 6", "Platform 5", "Platform 4", "Platform 3", "Platform 2", "Platform 1"}
	if diff := cmp.Diff(want, seen); diff != "" {
		t.Errorf("paging through the matches (-want +got):\n%s", diff)
	}
	if !app.allPostings.searching || app.allPostings.query != "platform" {
		t.Errorf("paging closed the search: searching %v, query %q", app.allPostings.searching, app.allPostings.query)
	}
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyLeft})
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

	if view := viewText(app); !strings.Contains(view, `No postings match "kafka".`) {
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
	if !strings.Contains(app.View().Content, model.DimStyle.Render(padCol("2", fitColWidth))) {
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

	if len(app.allPostings.starts) != 1 || !strings.Contains(viewText(app), "best fit first") {
		t.Fatalf("after s: page %d, view:\n%s", len(app.allPostings.starts), viewText(app))
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
	app = sendKeyAndApply(t, app, changesMsg{view: app.currentScreen, events: []change.Event{{Table: "posting_scores", RowID: 1, Op: "insert", Origin: "mcp:7"}}})

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
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})

	view := ansi.Strip(viewText(app))
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
	app = sendKeyAndApply(t, app, changesMsg{view: app.currentScreen, events: []change.Event{{Table: "posting_scores", RowID: 3, Op: "insert", New: fmt.Sprintf(`{"posting_id":%d,"score":5}`, p.ID), Origin: "mcp:7"}}})

	if view := ansi.Strip(viewText(app)); !strings.Contains(view, "5 · after the rewrite") {
		t.Errorf("detail doesn't show the score the agent just recorded:\n%s", view)
	}
}

// A posting archived while its detail is up over all postings is gone
// from the page by the time the user comes back to it.
func TestAllPostings_ArchivedWhileDetailIsUp_LeavesThePageUnderIt(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	for i := 1; i <= 3; i++ {
		mustUpsertPosting(t, s, acme.ID, fmt.Sprint(i), fmt.Sprintf("Role %d", i))
	}
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openAllPostings(t, app)
	shown := app.allPostings.page.Listings[0]
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})

	app = applyCmd(t, app, toggleArchived(s.Postings, message.Source{}, shown.ID, false))

	if app.currentScreen != screenPostingDetail {
		t.Fatalf("screen = %v, want posting detail still up", app.currentScreen)
	}
	if diff := cmp.Diff([]string{"Role 2", "Role 1"}, pageTitles(app)); diff != "" {
		t.Errorf("all postings under the detail (-want +got):\n%s", diff)
	}
}

func testListings() []message.Listing {
	return []message.Listing{
		{ID: 2, CompanyName: "Globex", Title: "Designer", ListingStatus: message.ListingOpen},
		{ID: 1, CompanyName: "Acme", Title: "Engineer", Department: "Platform", Location: "Berlin", WorkplaceType: "Hybrid",
			JobURL: "https://jobs.example.com/acme/1", ApplicationURL: "https://jobs.example.com/acme/1/apply", ListingStatus: message.ListingOpen, Interested: true},
	}
}

// allPostingsWith is an all-postings view, 40 rows tall, showing page 1 of
// a search with no query that found listings.
func allPostingsWith(t *testing.T, listings []message.Listing) *allPostingsModel {
	t.Helper()
	m := newAllPostingsModel(nil)
	m.resize(message.New(message.Source{}, message.Subject{}, message.OpNone, time.Now().UTC(), message.Signal[message.ContentSize]{Value: message.ContentSize{Width: 120, Rows: 40}}))
	m.searchRead(searchReadOf(m, message.PostingSearch{Order: message.OrderNewest, Limit: m.pageSize(40), Listings: listings, Total: len(listings), WithoutQuery: len(listings)}))
	return m
}

func searchReadOf(m *allPostingsModel, page message.PostingSearch) message.Event[message.Read[message.PostingSearch]] {
	return message.New(message.Source{Instance: m.Instance()}, message.Subject{}, message.OpNone, time.Now().UTC(), message.Read[message.PostingSearch]{Value: page})
}

// routedAllPostings is allPostingsWith(testListings()) registered with a
// router, so events reach it only through its routes' filters.
func routedAllPostings(t *testing.T) (*allPostingsModel, *model.MsgRouter) {
	t.Helper()
	m := allPostingsWith(t, testListings())
	r := model.NewMsgRouter()
	if err := r.Register(m); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return m, r
}

func TestAllPostingsModel_SearchRead_ShowsOnlyThePageOnScreen(t *testing.T) {
	t.Parallel()

	page := func(edit func(*message.PostingSearch)) message.PostingSearch {
		p := message.PostingSearch{Order: message.OrderNewest, Limit: 10, Listings: []message.Listing{{ID: 9, Title: "Late"}}, Total: 1}
		edit(&p)
		return p
	}
	tests := []struct {
		name  string
		from  func(m *allPostingsModel) message.ScreenInstance
		read  message.PostingSearch
		shown bool
	}{
		{name: "its own read of the page on screen", read: page(func(*message.PostingSearch) {}), shown: true},
		{name: "a late one for an earlier query", read: page(func(p *message.PostingSearch) { p.Query = "plat" })},
		{name: "a late one in the other order", read: page(func(p *message.PostingSearch) { p.Order = message.OrderBestFit })},
		{name: "a late one for another page", read: page(func(p *message.PostingSearch) { p.AfterID = 40 })},
		{name: "another screen's", read: page(func(*message.PostingSearch) {}), from: func(*allPostingsModel) message.ScreenInstance { return 9 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, r := routedAllPostings(t)
			e := searchReadOf(m, tt.read)
			if tt.from != nil {
				e.Meta.Source.Instance = tt.from(m)
			}

			r.Dispatch(e)

			want := testListings()
			if tt.shown {
				want = tt.read.Listings
			}
			if diff := cmp.Diff(want, m.page.Listings); diff != "" {
				t.Errorf("page (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAllPostingsModel_Pause_SearchesOnlyForTheQueryTyped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		from     message.ScreenInstance
		query    string
		searched bool
	}{
		{name: "its own pause after the last key", query: "plat", searched: true},
		{name: "its own pause after an earlier key", query: "pl"},
		{name: "another screen's", from: 9, query: "plat"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, r := routedAllPostings(t)
			m.query = "plat"
			m.starts = append(m.starts, pageStart{id: 1})
			from := tt.from
			if from == 0 {
				from = m.Instance()
			}

			cmd := r.Dispatch(message.New(message.Source{Instance: from}, message.Subject{}, message.OpNone, time.Now().UTC(),
				message.Signal[message.SearchDebounce]{Value: message.SearchDebounce{Query: tt.query}}))

			if searched := cmd != nil && len(m.starts) == 1; searched != tt.searched {
				t.Errorf("searched = %t (on page %d), want %t", searched, len(m.starts), tt.searched)
			}
		})
	}
}

func TestAllPostingsModel_O_OpensTheRowsJobURLItself(t *testing.T) {
	t.Parallel()

	m := allPostingsWith(t, testListings())
	type open struct {
		src   message.Source
		about message.Subject
		url   string
	}
	var opened []open
	m.openURL = func(src message.Source, about message.Subject, url string) tea.Cmd {
		opened = append(opened, open{src, about, url})
		return nil
	}

	runCmd(t, m.keyPressed(runeKey('o'))) // the first row has no URL
	runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyDown}))
	runCmd(t, m.keyPressed(runeKey('o')))

	row := testListings()[1]
	want := []open{{message.Source{Instance: m.Instance()}, message.Subject{Type: message.TypePosting, ID: row.ID}, row.JobURL}}
	if diff := cmp.Diff(want, opened, cmp.AllowUnexported(open{})); diff != "" {
		t.Errorf("opened (-want +got):\n%s", diff)
	}
}

// storedAllPostings is an all-postings view over a store with two open
// postings, registered with a router, in focus or not.
func storedAllPostings(t *testing.T, focused bool) (*allPostingsModel, *model.MsgRouter) {
	t.Helper()
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustUpsertPosting(t, s, acme.ID, "job-2", "Designer")
	m := newAllPostingsModel(s.Postings)
	r := model.NewMsgRouter()
	if err := r.Register(m); err != nil {
		t.Fatalf("Register: %v", err)
	}
	r.Dispatch(sizedTo(message.ContentSize{Width: 120, Rows: 40}))
	if focused {
		r.Dispatch(focusOn(m.Instance()))
	}
	return m, r
}

func sizedTo(size message.ContentSize) tea.Msg {
	return message.New(message.Source{}, message.Subject{}, message.OpNone, time.Now().UTC(), message.Signal[message.ContentSize]{Value: size})
}

// searchReadsFrom is whether msgs hold m's read of its page.
func searchReadsFrom(t *testing.T, m *allPostingsModel, msgs []tea.Msg) bool {
	t.Helper()
	read := false
	for _, msg := range msgs {
		if e, ok := msg.(message.Event[message.Read[message.PostingSearch]]); ok {
			assertEventFrom(t, e.Meta, message.Source{Instance: m.Instance()})
			read = true
		}
	}
	return read
}

func TestAllPostingsModel_ReadsItsPageAgain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		focused bool
		msg     func(m *allPostingsModel) tea.Msg
		read    bool
	}{
		{name: "on coming into focus", msg: func(m *allPostingsModel) tea.Msg { return focusOn(m.Instance()) }, read: true},
		{name: "not on staying in focus", focused: true, msg: func(m *allPostingsModel) tea.Msg { return focusOn(m.Instance()) }},
		{name: "not on another screen's focus", msg: func(*allPostingsModel) tea.Msg { return focusOn(9) }},
		{name: "on a load request", msg: func(*allPostingsModel) tea.Msg {
			return message.New(message.Source{Instance: 9}, message.Subject{}, message.OpNone, time.Now().UTC(), message.Load[message.PostingSearch]{})
		}, read: true},
		{name: "on a markup change", msg: func(*allPostingsModel) tea.Msg {
			return message.Updated(message.Source{Instance: 9}, 1, time.Now().UTC(), message.Markup{PostingID: 1}, message.Markup{PostingID: 1, ArchivedAt: time.Now().UTC()})
		}, read: true},
		{name: "on a posting changed by another process", msg: func(*allPostingsModel) tea.Msg {
			return message.Logged(1, mcp, message.Subject{Type: message.TypePosting, ID: 1}, message.OpUpdated, time.Now().UTC(), message.Posting{ID: 1, ListingStatus: message.ListingClosed})
		}, read: true},
		{name: "on a posting scored by another process", msg: func(*allPostingsModel) tea.Msg {
			return message.Logged(1, mcp, message.Subject{Type: message.TypePosting, ID: 1}, message.OpCreated, time.Now().UTC(), message.Score{ID: 1, PostingID: 1, Score: 4})
		}, read: true},
		{name: "on a company changed by another process", msg: func(*allPostingsModel) tea.Msg {
			return message.Logged(1, mcp, message.Subject{Type: message.TypeCompany, ID: 1}, message.OpDeleted, time.Now().UTC(), message.Company{})
		}, read: true},
		{name: "on fewer rows, in focus", focused: true, msg: func(*allPostingsModel) tea.Msg { return sizedTo(message.ContentSize{Width: 120, Rows: 20}) }, read: true},
		{name: "not on the same rows, narrower", focused: true, msg: func(*allPostingsModel) tea.Msg { return sizedTo(message.ContentSize{Width: 80, Rows: 40}) }},
		{name: "not on fewer rows, out of focus", msg: func(*allPostingsModel) tea.Msg { return sizedTo(message.ContentSize{Width: 120, Rows: 20}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, r := storedAllPostings(t, tt.focused)
			for _, msg := range runCmd(t, m.Init()) {
				r.Dispatch(msg)
			}

			if got := searchReadsFrom(t, m, runCmd(t, r.Dispatch(tt.msg(m)))); got != tt.read {
				t.Errorf("read = %t, want %t", got, tt.read)
			}
		})
	}
}

func TestAllPostingsModel_Handlers_FailOnTheWrongMessage(t *testing.T) {
	t.Parallel()
	assertHandlersFailOnWrongMessage(t, newAllPostingsModel(nil))
}

func TestAllPostingsModel_Enter_PushesDetailOnTheRowsPosting(t *testing.T) {
	t.Parallel()

	m := allPostingsWith(t, testListings())
	runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyDown}))

	got := runCmd(t, m.keyPressed(tea.KeyPressMsg{Code: tea.KeyEnter}))
	if len(got) != 1 {
		t.Fatalf("enter sent %#v, want one Nav", got)
	}
	nav, ok := got[0].(message.Event[message.Nav[message.PostingDetail]])
	if !ok {
		t.Fatalf("enter sent %T, want a Nav to posting detail", got[0])
	}
	assertEventFrom(t, nav.Meta, message.Source{Instance: m.Instance()})
	row := testListings()[1]
	if want := (message.Subject{Type: message.TypePosting, ID: row.ID}); nav.Meta.Subject != want {
		t.Errorf("Subject = %+v, want %+v", nav.Meta.Subject, want)
	}
	want := message.Nav[message.PostingDetail]{Op: message.NavPush, Screen: message.ScreenPostingDetail, Value: message.PostingDetail{Posting: message.Posting{
		ID: row.ID, Title: row.Title, Department: row.Department, Location: row.Location, WorkplaceType: row.WorkplaceType,
		JobURL: row.JobURL, ApplicationURL: row.ApplicationURL, ListingStatus: row.ListingStatus,
	}}}
	if diff := cmp.Diff(want, nav.Payload); diff != "" {
		t.Errorf("Nav (-want +got):\n%s", diff)
	}
}

func TestAllPostingsModel_ChangesElsewhere_ReadOnceAtATime(t *testing.T) {
	t.Parallel()
	m, r := storedAllPostings(t, false)
	for _, msg := range runCmd(t, m.Init()) {
		r.Dispatch(msg)
	}
	assertReadsOnceAtATime[message.PostingSearch](t, r,
		message.Logged(1, mcp, message.Subject{Type: message.TypePosting, ID: 1}, message.OpUpdated, time.Now().UTC(), message.Posting{ID: 1}))
}
