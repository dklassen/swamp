package tui

import (
	"context"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/documents"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
)

// TestApp_AnotherProcessesChange_ReloadsTheHomeListQuietly: the agent
// starting an application shows up without a restart, and without a
// status line pushing the table down.
func TestApp_AnotherProcessesChange_ReloadsTheHomeListQuietly(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeFeed(nil, "tui:1")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Staff Engineer")
	application, err := s.CreateApplication(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if strings.Contains(app.View(), "Staff Engineer") {
		t.Fatal("the home list shows the application before any reload; the test can't tell a reload happened")
	}

	app = sendKeyAndApply(t, app, changesMsg{events: []store.ChangeEvent{{Table: "applications", RowID: application.ID, Op: "insert", Origin: "mcp:7"}}})

	if view := app.View(); !strings.Contains(view, "Staff Engineer") {
		t.Errorf("home list after the agent's change doesn't show it:\n%s", view)
	}
	if app.status != "" {
		t.Errorf("status line = %q after a background reload, want none", app.status)
	}
}

// TestApp_ItsOwnChange_IsSkipped: the TUI reloads after its own actions
// already; announcing them as updates would be false.
func TestApp_ItsOwnChange_IsSkipped(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeFeed(nil, "tui:1")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Staff Engineer")
	application, err := s.CreateApplication(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}

	app = sendKeyAndApply(t, app, changesMsg{events: []store.ChangeEvent{{Table: "applications", RowID: application.ID, Op: "insert", Origin: "tui:1"}}})

	if view := app.View(); strings.Contains(view, "Staff Engineer") || strings.Contains(view, "Updated by") {
		t.Errorf("the TUI's own change was treated as another process's:\n%s", view)
	}
}

// TestApp_ATickReadsTheFeed: each tick reads what's new in the change log;
// the reply schedules the next tick.
func TestApp_ATickReadsTheFeed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Staff Engineer")
	feed, err := s.NewChangeFeed(ctx)
	if err != nil {
		t.Fatalf("NewChangeFeed: %v", err)
	}
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeFeed(feed, "tui:1")
	if _, err := s.CreateApplication(ctx, posting.ID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}

	app, read := sendKey(app, changeTickMsg{})
	if read == nil {
		t.Fatal("a tick returned no command, want one reading the feed")
	}
	got, ok := read().(changesMsg)
	if !ok || got.err != nil || len(got.events) != 1 || got.events[0].Table != "applications" {
		t.Fatalf("reading the feed = %+v, want one applications event", got)
	}
	if _, next := sendKey(app, got); next == nil {
		t.Error("handling the events scheduled nothing, want the next tick")
	}
}

// TestApp_HomeListReload_KeepsTheCursorOnTheSameApplication: a row
// disappearing above the cursor mustn't move you to a different
// application.
func TestApp_HomeListReload_KeepsTheCursorOnTheSameApplication(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	for i, title := range []string{"First", "Second", "Third"} {
		posting := mustUpsertPosting(t, s, acme.ID, "job-"+string(rune('a'+i)), title)
		if _, err := s.CreateApplication(ctx, posting.ID); err != nil {
			t.Fatalf("CreateApplication: %v", err)
		}
	}
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeFeed(nil, "tui:1")
	rows := app.activeApplications
	if len(rows) != 3 {
		t.Fatalf("home list has %d rows, want 3", len(rows))
	}
	app, _ = sendKey(app, runeKey('j'))
	app, _ = sendKey(app, runeKey('j'))
	onLast := rows[2].ID

	if err := s.DeleteApplication(ctx, rows[0].ID); err != nil {
		t.Fatalf("DeleteApplication: %v", err)
	}
	app = sendKeyAndApply(t, app, changesMsg{events: []store.ChangeEvent{{Table: "applications", RowID: rows[0].ID, Op: "update", Origin: "tui:2"}}})

	if got := app.activeApplications[app.activeApplicationList.cursor].ID; got != onLast {
		t.Errorf("cursor is on application %d after the reload, want %d (the one it was on)", got, onLast)
	}
}

// TestApp_CompanyListReload_KeepsTheCursorOnTheSameCompany: as for the home
// list, with a company deleted in another window.
func TestApp_CompanyListReload_KeepsTheCursorOnTheSameCompany(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	for _, name := range []string{"Acme", "Globex", "Initech"} {
		mustCreateCompany(t, s, name, "ashby", strings.ToLower(name))
	}
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeFeed(nil, "tui:1")
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyTab})
	if app.screen != screenCompanyList {
		t.Fatalf("screen after c = %v, want the company list", app.screen)
	}
	companies := app.companies
	app, _ = sendKey(app, runeKey('j'))
	app, _ = sendKey(app, runeKey('j'))
	onLast := companies[2].ID

	if err := s.SoftDeleteCompany(context.Background(), companies[0].ID); err != nil {
		t.Fatalf("SoftDeleteCompany: %v", err)
	}
	app = sendKeyAndApply(t, app, changesMsg{events: []store.ChangeEvent{{Table: "companies", RowID: companies[0].ID, Op: "update", Origin: "tui:2"}}})

	if len(app.companies) != 2 {
		t.Fatalf("company list has %d companies after the reload, want 2", len(app.companies))
	}
	if got := app.companies[app.companyList.cursor].ID; got != onLast {
		t.Errorf("cursor is on company %d after the reload, want %d", got, onLast)
	}
}

// TestApp_PostingListReload_KeepsTheCursorAndShowsTheChange: a sync elsewhere
// renames a posting above the cursor.
func TestApp_PostingListReload_KeepsTheCursorAndShowsTheChange(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "First"}, {SourceID: "job-2", Title: "Second"}, {SourceID: "job-3", Title: "Third"}},
	})
	app := newTestApp(t, s, syncer).WithChangeFeed(nil, "tui:1")
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 200, Height: 40})
	app = openPostingList(t, app)
	postings := app.postings
	if len(postings) != 3 {
		t.Fatalf("posting list has %d postings, want 3", len(postings))
	}
	app, _ = sendKey(app, runeKey('j'))
	app, _ = sendKey(app, runeKey('j'))
	onLast := postings[2].ID

	renamed := postings[0]
	if _, err := s.UpsertPosting(context.Background(), store.CreatePostingParams{CompanyID: renamed.CompanyID, Source: renamed.Source, SourceID: renamed.SourceID, IngestedFields: store.IngestedFields{Title: "Renamed by sync", RawPayload: renamed.RawPayload}}); err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	app = sendKeyAndApply(t, app, changesMsg{events: []store.ChangeEvent{{Table: "postings", RowID: renamed.ID, Op: "update", Origin: "fetch:9"}}})

	if !strings.Contains(app.View(), "Renamed by sync") {
		t.Errorf("posting list doesn't show the new title:\n%s", app.View())
	}
	if got := app.postings[app.postingList.cursor].ID; got != onLast {
		t.Errorf("cursor is on posting %d after the reload, want %d", got, onLast)
	}
}

// TestApp_TheAgentRevisesADraft_ApplicationDetailShowsIt: the case that
// started this. A passed review stops counting once the agent rewrites the
// document, and the screen must show that without a refresh key.
func TestApp_TheAgentRevisesADraft_ApplicationDetailShowsIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app, application := deleteTestApp(t) // resume drafted as "# Draft\n"
	app.WithChangeFeed(nil, "tui:1")
	if _, err := app.store.CreateDocumentReview(ctx, application.ID, documents.Resume, "# Draft\n", store.ReviewOutcomePassed, ""); err != nil {
		t.Fatalf("CreateDocumentReview: %v", err)
	}
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	app = sendKeyAndApply(t, app, runeKey('u')) // show the review the test just made
	if !strings.Contains(app.View(), "[PASSED]") {
		t.Fatalf("application detail doesn't show the passed review:\n%s", app.View())
	}

	if _, err := app.documents.Write(application.ID, documents.Resume, "# Revised by the agent\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	app = sendKeyAndApply(t, app, changesMsg{events: []store.ChangeEvent{{Table: "document_writes", RowID: 1, Op: "insert", New: `{"application_id":` + strconv.FormatInt(application.ID, 10) + `,"document_type":"resume","source":"write_document"}`, Origin: "mcp:7"}}})

	view := app.View()
	if strings.Contains(view, "[PASSED]") {
		t.Errorf("application detail still shows the review of the old version:\n%s", view)
	}
	if app.status != "" {
		t.Errorf("status line = %q after a background reload, want none", app.status)
	}
}

func TestApp_ApplicationDeletedElsewhere_GoesBackAndSaysSo(t *testing.T) {
	t.Parallel()

	app, application := deleteTestApp(t)
	app.WithChangeFeed(nil, "tui:1")
	app = sendKeyAndApply(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	if err := app.store.DeleteApplication(context.Background(), application.ID); err != nil {
		t.Fatalf("DeleteApplication: %v", err)
	}

	app = sendKeyAndApply(t, app, changesMsg{events: []store.ChangeEvent{{Table: "applications", RowID: application.ID, Op: "update", Origin: "tui:2"}}})

	if app.screen != screenActiveApplications {
		t.Errorf("screen = %v, want the home list", app.screen)
	}
	if !strings.Contains(app.View(), "was deleted") {
		t.Errorf("the home list doesn't say the application was deleted:\n%s", app.View())
	}
}

// TestApp_PostingDetailReload_ShowsTheChangeAndKeepsTheScroll: a sync
// renames the posting you're reading, halfway down its description.
func TestApp_PostingDetailReload_ShowsTheChangeAndKeepsTheScroll(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	long := strings.Repeat("A line of the job description.\n\n", 80)
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", DescriptionText: long}},
	})
	app := newTestApp(t, s, syncer).WithChangeFeed(nil, "tui:1")
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)
	for range 20 {
		app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyDown})
	}
	scrolled := app.postingDetail.viewport.YOffset
	if scrolled == 0 {
		t.Fatal("posting detail didn't scroll; the test can't tell the scroll was kept")
	}

	p := app.postings[0]
	if _, err := s.UpsertPosting(context.Background(), store.CreatePostingParams{CompanyID: p.CompanyID, Source: p.Source, SourceID: p.SourceID, IngestedFields: store.IngestedFields{Title: "Senior Engineer", DescriptionText: long, RawPayload: p.RawPayload}}); err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	app = sendKeyAndApply(t, app, changesMsg{events: []store.ChangeEvent{{Table: "postings", RowID: p.ID, Op: "update", Origin: "fetch:9"}}})

	if got := app.postingDetail.posting.Title; got != "Senior Engineer" {
		t.Errorf("posting detail title = %q, want the renamed %q", got, "Senior Engineer")
	}
	if got := app.postingDetail.viewport.YOffset; got != scrolled {
		t.Errorf("scroll offset = %d after the reload, want %d", got, scrolled)
	}
}

// TestApp_ApplicationStartedElsewhere_PostingDetailShowsIt: the agent
// starts an application on the posting you're reading. The event comes
// from the real change log, so it carries only what the triggers record.
func TestApp_ApplicationStartedElsewhere_PostingDetailShowsIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{"acme": {{SourceID: "job-1", Title: "Engineer"}}})
	app := newTestApp(t, s, syncer).WithChangeFeed(nil, "tui:1")
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 40})
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)
	if !strings.Contains(app.View(), "No application started") {
		t.Fatalf("posting detail already shows an application; the test can't tell a reload happened:\n%s", app.View())
	}
	feed, err := s.NewChangeFeed(ctx)
	if err != nil {
		t.Fatalf("NewChangeFeed: %v", err)
	}

	if _, err := s.CreateApplication(ctx, app.postingDetail.posting.ID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	events, err := feed.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	app = sendKeyAndApply(t, app, changesMsg{events: events})

	if view := app.View(); strings.Contains(view, "No application started") || !strings.Contains(view, applicationStatusLabel(store.ApplicationStatusStarted)) {
		t.Errorf("posting detail doesn't show the application started elsewhere:\n%s", view)
	}
}

// TestApp_ChangesWhileAFormIsOpen_WaitUntilItCloses: a form you're filling
// in is never rebuilt; the screen beneath catches up once you're back.
func TestApp_ChangesWhileAFormIsOpen_WaitUntilItCloses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app, application := deleteTestApp(t)
	app.WithChangeFeed(nil, "tui:1")
	app = sendKeyAndApply(t, app, runeKey('s'))
	if app.screen != screenApplicationStatusSelect {
		t.Fatalf("screen after s = %v, want the status form", app.screen)
	}
	app, _ = sendKey(app, runeKey('j'))
	form := app.applicationStatus
	acme, err := app.store.GetCompany(ctx, mustPosting(t, app.store, application.PostingID).CompanyID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	other := mustUpsertPosting(t, app.store, acme.ID, "job-2", "Started By The Agent")
	if _, err := app.store.CreateApplication(ctx, other.ID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}

	app = sendKeyAndApply(t, app, changesMsg{events: []store.ChangeEvent{{Table: "applications", RowID: 99, Op: "insert", Origin: "mcp:7"}}})
	if app.screen != screenApplicationStatusSelect || app.applicationStatus.cursor != form.cursor {
		t.Fatalf("the status form changed under you: screen %v, cursor %d (was %d)", app.screen, app.applicationStatus.cursor, form.cursor)
	}

	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyEsc})
	app = sendKeyAndApply(t, app, changesMsg{}) // the next tick, with nothing new
	if !strings.Contains(app.View(), "Started By The Agent") {
		t.Errorf("the home list didn't catch up after the form closed:\n%s", app.View())
	}
}

func mustPosting(t *testing.T, s *store.Store, id int64) store.Posting {
	t.Helper()
	p, err := s.GetPosting(context.Background(), id)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}
	return p
}
