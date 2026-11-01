package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/domain/application"
	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/tui/message"
)

// TestApp_AnotherProcessesChange_ReloadsTheHomeListQuietly: the agent
// starting an application shows up without a restart, and without a
// status line pushing the table down.
func TestApp_AnotherProcessesChange_ReloadsTheHomeListQuietly(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeLog(newTestChangeReader(s))
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Staff Engineer")
	if _, err := createApplicationIn(context.Background(), s, posting.ID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	if strings.Contains(viewText(app), "Staff Engineer") {
		t.Fatal("the home list shows the application before any reload; the test can't tell a reload happened")
	}

	app = readChangeLog(t, app)

	if view := viewText(app); !strings.Contains(view, "Staff Engineer") {
		t.Errorf("home list after the agent's change doesn't show it:\n%s", view)
	}
	if app.status != "" {
		t.Errorf("status line = %q after a background reload, want none", app.status)
	}
}

// TestApp_HomeListReload_KeepsTheCursorOnTheSameApplication: a row
// disappearing above the cursor mustn't move you to a different
// application.
func TestApp_HomeListReload_KeepsTheCursorOnTheSameApplication(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	for i, title := range []string{"First", "Second", "Third"} {
		posting := mustUpsertPosting(t, s, acme.ID, "job-"+string(rune('a'+i)), title)
		if _, err := createApplicationIn(ctx, s, posting.ID); err != nil {
			t.Fatalf("application.Create: %v", err)
		}
	}
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeLog(newTestChangeReader(s))
	rows := applicationsTabOf(app).apps
	if len(rows) != 3 {
		t.Fatalf("home list has %d rows, want 3", len(rows))
	}
	app, _ = sendKey(app, runeKey('j'))
	app, _ = sendKey(app, runeKey('j'))
	onLast := rows[2].ID

	if _, err := s.Applications.Delete(ctx, rows[0].ID); err != nil {
		t.Fatalf("application.Delete: %v", err)
	}
	app = readChangeLog(t, app)

	if got := applicationsTabOf(app).apps[applicationsTabOf(app).cursor].ID; got != onLast {
		t.Errorf("cursor is on application %d after the reload, want %d (the one it was on)", got, onLast)
	}
}

// TestApp_CompanyListReload_KeepsTheCursorOnTheSameCompany: as for the home
// list, with a company deleted in another window.
func TestApp_CompanyListReload_KeepsTheCursorOnTheSameCompany(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	for _, name := range []string{"Acme", "Globex", "Initech"} {
		mustCreateCompany(t, s, name, jobboard.Ashby, strings.ToLower(name))
	}
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeLog(newTestChangeReader(s))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})
	if app.onScreen() != message.ScreenCompanyList {
		t.Fatalf("screen after c = %v, want the company list", app.onScreen())
	}
	companies := companiesTabOf(app).companies
	app, _ = sendKey(app, runeKey('j'))
	app, _ = sendKey(app, runeKey('j'))
	onLast := companies[2].ID

	if _, err := s.Companies.Delete(context.Background(), companies[0].ID); err != nil {
		t.Fatalf("SoftDeleteCompany: %v", err)
	}
	app = readChangeLog(t, app)

	if len(companiesTabOf(app).companies) != 2 {
		t.Fatalf("company list has %d companies after the reload, want 2", len(companiesTabOf(app).companies))
	}
	if got := companiesTabOf(app).companies[companiesTabOf(app).cursor].ID; got != onLast {
		t.Errorf("cursor is on company %d after the reload, want %d", got, onLast)
	}
}

// TestApp_PostingListReload_KeepsTheCursorAndShowsTheChange: a sync elsewhere
// renames a posting above the cursor.
func TestApp_PostingListReload_KeepsTheCursorAndShowsTheChange(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "First"}, {SourceID: "job-2", Title: "Second"}, {SourceID: "job-3", Title: "Third"}},
	})
	app := newTestApp(t, s, syncer).WithChangeLog(newTestChangeReader(s))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 200, Height: 40})
	app = openPostingList(t, app)
	postings := openList(t, app).list.Postings
	if len(postings) != 3 {
		t.Fatalf("posting list has %d postings, want 3", len(postings))
	}
	app, _ = sendKey(app, runeKey('j'))
	app, _ = sendKey(app, runeKey('j'))
	onLast := postings[2].ID

	renamed := storedPosting(t, s, postings[0].ID)
	if _, err := s.Postings.Ingest(context.Background(), renamed.CompanyID, renamed.Source, renamed.SourceID, posting.IngestedFields{Title: "Renamed by sync", RawPayload: renamed.RawPayload}); err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	app = readChangeLog(t, app)

	if !strings.Contains(viewText(app), "Renamed by sync") {
		t.Errorf("posting list doesn't show the new title:\n%s", viewText(app))
	}
	if got := openList(t, app).list.Postings[openList(t, app).cursor].ID; got != onLast {
		t.Errorf("cursor is on posting %d after the reload, want %d", got, onLast)
	}
}

// TestApp_TheAgentRevisesADraft_ApplicationDetailShowsIt: the case that
// started this. A passed review stops counting once the agent rewrites the
// document, and the screen must show that without a refresh key.
func TestApp_TheAgentRevisesADraft_ApplicationDetailShowsIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app, s, screens, target := deleteTestAppWithScreens(t) // resume drafted as "# Draft\n"
	app.WithChangeLog(newTestChangeReader(s))
	if _, err := createReview(ctx, s, target.ID, document.Resume, "# Draft\n", document.Passed, ""); err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app = sendKeyAndApply(t, app, runeKey('u')) // show the review the test just made
	if !strings.Contains(viewText(app), "[PASSED]") {
		t.Fatalf("application detail doesn't show the passed review:\n%s", viewText(app))
	}

	if _, err := screens.documents.Write(target.ID, document.Resume, "# Revised by the agent\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := s.Documents.RecordWrite(ctx, target.ID, document.Resume, "# Revised by the agent\n", document.SourceWriteDocument); err != nil {
		t.Fatalf("RecordWrite: %v", err)
	}
	app = readChangeLog(t, app)

	view := viewText(app)
	if strings.Contains(view, "[PASSED]") {
		t.Errorf("application detail still shows the review of the old version:\n%s", view)
	}
	if app.status != "" {
		t.Errorf("status line = %q after a background reload, want none", app.status)
	}
}

func TestApp_ApplicationDeletedElsewhere_GoesBackAndSaysSo(t *testing.T) {
	t.Parallel()

	app, s, target := deleteTestApp(t)
	app.WithChangeLog(newTestChangeReader(s))
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, err := s.Applications.Delete(context.Background(), target.ID); err != nil {
		t.Fatalf("application.Delete: %v", err)
	}

	app = readChangeLog(t, app)

	if app.onScreen() != message.ScreenActiveApplications {
		t.Errorf("screen = %v, want the home list", app.onScreen())
	}
	if !strings.Contains(viewText(app), "was deleted") {
		t.Errorf("the home list doesn't say the application was deleted:\n%s", viewText(app))
	}
}

// TestApp_PostingDetailReload_ShowsTheChangeAndKeepsTheScroll: a sync
// renames the posting you're reading, halfway down its description.
func TestApp_PostingDetailReload_ShowsTheChangeAndKeepsTheScroll(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	long := strings.Repeat("A line of the job description.\n\n", 80)
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{
		"acme": {{SourceID: "job-1", Title: "Engineer", DescriptionText: long}},
	})
	app := newTestApp(t, s, syncer).WithChangeLog(newTestChangeReader(s))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)
	for range 20 {
		app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	scrolled := openDetail(t, app).viewport.YOffset()
	if scrolled == 0 {
		t.Fatal("posting detail didn't scroll; the test can't tell the scroll was kept")
	}

	p := storedPosting(t, s, openList(t, app).list.Postings[0].ID)
	if _, err := s.Postings.Ingest(context.Background(), p.CompanyID, p.Source, p.SourceID, posting.IngestedFields{Title: "Senior Engineer", DescriptionText: long, RawPayload: p.RawPayload}); err != nil {
		t.Fatalf("UpsertPosting: %v", err)
	}
	app = readChangeLog(t, app)

	if got := openDetail(t, app).posting.Title; got != "Senior Engineer" {
		t.Errorf("posting detail title = %q, want the renamed %q", got, "Senior Engineer")
	}
	if got := openDetail(t, app).viewport.YOffset(); got != scrolled {
		t.Errorf("scroll offset = %d after the reload, want %d", got, scrolled)
	}
}

// #284: posting detail opened from application detail keeps showing its
// posting, with the new content, when a write elsewhere reloads it.
func TestApp_PostingDetailFromApplicationDetail_ReloadKeepsThePosting(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	p := mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer")
	mustCreateApplication(t, s, p.ID)
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeLog(newTestChangeReader(s))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 40})
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter}) // home -> application detail
	app = sendKeyAndApply(t, app, runeKey('p'))                        // -> posting detail
	if app.onScreen() != message.ScreenPostingDetail {
		t.Fatalf("screen after p = %v, want posting detail", app.onScreen())
	}

	stored := storedPosting(t, s, p.ID)
	if _, err := s.Postings.Ingest(context.Background(), stored.CompanyID, stored.Source, stored.SourceID, posting.IngestedFields{Title: "Engineer", Location: "Remote", RawPayload: stored.RawPayload}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	app = readChangeLog(t, app)

	if view := viewText(app); !strings.Contains(view, "Remote") || strings.Contains(view, "No application started") {
		t.Errorf("posting detail after the reload doesn't show the posting's new location with its application:\n%s", view)
	}
}

// TestApp_ApplicationStartedElsewhere_PostingDetailShowsIt: the agent
// starts an application on the posting you're reading. The event comes
// from the real change log, so it carries only what the triggers record.
func TestApp_ApplicationStartedElsewhere_PostingDetailShowsIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := testutil.NewTestApp(t)
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	syncer := newTestSyncer(s, map[string][]jobboard.Posting{"acme": {{SourceID: "job-1", Title: "Engineer"}}})
	app := newTestApp(t, s, syncer).WithChangeLog(newTestChangeReader(s))
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 40})
	app = openPostingList(t, app)
	app = openPostingDetail(t, app)
	if !strings.Contains(viewText(app), "No application started") {
		t.Fatalf("posting detail already shows an application; the test can't tell a reload happened:\n%s", viewText(app))
	}
	app = readChangeLog(t, app)

	if _, err := createApplicationIn(ctx, s, openDetail(t, app).posting.ID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}
	app = readChangeLog(t, app)

	if view := viewText(app); strings.Contains(view, "No application started") || !strings.Contains(view, applicationStatusLabel(application.Started)) {
		t.Errorf("posting detail doesn't show the application started elsewhere:\n%s", view)
	}
}

// TestApp_ChangesWhileAFormIsOpen_LeaveItAlone: a form you're filling in
// is never rebuilt; the screen beneath shows the change once you're back.
func TestApp_ChangesWhileAFormIsOpen_LeaveItAlone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app, s, target := deleteTestApp(t)
	app.WithChangeLog(newTestChangeReader(s))
	app = sendKeyAndApply(t, app, runeKey('s'))
	if app.onScreen() != message.ScreenApplicationStatus {
		t.Fatalf("screen after s = %v, want the status form", app.onScreen())
	}
	app, _ = sendKey(app, runeKey('j'))
	cursor := statusPicker(t, app).cursor
	acme, err := s.Companies.Get(ctx, mustPosting(t, s, target.PostingID).CompanyID)
	if err != nil {
		t.Fatalf("GetCompany: %v", err)
	}
	other := mustUpsertPosting(t, s, acme.ID, "job-2", "Started By The Agent")
	if _, err := createApplicationIn(ctx, s, other.ID); err != nil {
		t.Fatalf("application.Create: %v", err)
	}

	app = readChangeLog(t, app)
	if app.onScreen() != message.ScreenApplicationStatus || statusPicker(t, app).cursor != cursor {
		t.Fatalf("the status form changed under you: screen %v, cursor %d (was %d)", app.onScreen(), statusPicker(t, app).cursor, cursor)
	}

	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEsc})
	if !strings.Contains(viewText(app), "Started By The Agent") {
		t.Errorf("the home list didn't catch up after the form closed:\n%s", viewText(app))
	}
}

func mustPosting(t *testing.T, s testutil.TestApp, id int64) posting.Posting {
	t.Helper()
	p, err := s.Postings.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("GetPosting: %v", err)
	}
	return *p
}

// TestApp_EnteringApplicationDetail_ShowsAChangeMadeBeforeIt: the home
// list's copy of an application can be out of date, e.g. after an event
// that arrived on another screen and was cleared there.
func TestApp_EnteringApplicationDetail_ShowsAChangeMadeBeforeIt(t *testing.T) {
	t.Parallel()

	app, s, target := deleteTestApp(t)
	app.WithChangeLog(newTestChangeReader(s))
	if err := updateApplicationStatusIn(context.Background(), s, target.PostingID, application.Interviewing); err != nil {
		t.Fatalf("application.UpdateStatus: %v", err)
	}

	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})

	if app.onScreen() != message.ScreenApplicationDetail {
		t.Fatalf("screen = %v, want application detail", app.onScreen())
	}
	if view := viewText(app); !strings.Contains(view, "Interviewing") {
		t.Errorf("application detail doesn't show the status changed before entering it:\n%s", view)
	}
}

// TestApp_ReturningToTheHomeList_ShowsAChangeClearedElsewhere: an event
// for another application arrives on application detail, which it doesn't
// touch, and is cleared there; the home list must still show it.
func TestApp_ReturningToTheHomeList_ShowsAChangeClearedElsewhere(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	first := mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer").ID)
	second := mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-2", "Designer").ID)
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeLog(newTestChangeReader(s))
	if strings.Contains(viewText(app), "Interviewing") {
		t.Fatal("the home list shows Interviewing before any change; the test can't tell a reload happened")
	}

	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.onScreen() != message.ScreenApplicationDetail {
		t.Fatalf("screen = %v, want application detail", app.onScreen())
	}
	other := first
	if shownDetail(app).Application.ID == first.ID {
		other = second
	}
	if err := updateApplicationStatusIn(ctx, s, other.PostingID, application.Interviewing); err != nil {
		t.Fatalf("application.UpdateStatus: %v", err)
	}
	app = readChangeLog(t, app)

	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEsc})

	if app.onScreen() != message.ScreenActiveApplications {
		t.Fatalf("screen = %v, want the home list", app.onScreen())
	}
	if view := viewText(app); !strings.Contains(view, "Interviewing") {
		t.Errorf("the home list doesn't show the change made while on application detail:\n%s", view)
	}
}

// TestApp_TabBackToTheHomeList_ShowsAChangeClearedOnTheCompanyList: the
// company list reloads only for companies and postings, so an application
// event that arrives there is cleared without reaching the home list.
func TestApp_TabBackToTheHomeList_ShowsAChangeClearedOnTheCompanyList(t *testing.T) {
	t.Parallel()

	app, s, target := deleteTestApp(t)
	app.WithChangeLog(newTestChangeReader(s))
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyTab})
	if app.onScreen() != message.ScreenCompanyList {
		t.Fatalf("screen = %v, want the company list", app.onScreen())
	}
	if err := updateApplicationStatusIn(context.Background(), s, target.PostingID, application.Interviewing); err != nil {
		t.Fatalf("application.UpdateStatus: %v", err)
	}
	app = readChangeLog(t, app)

	for range 2 {
		app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyTab})
	}

	if app.onScreen() != message.ScreenActiveApplications {
		t.Fatalf("screen = %v, want the home list", app.onScreen())
	}
	if view := viewText(app); !strings.Contains(view, "Interviewing") {
		t.Errorf("the home list doesn't show the change made while on the company list:\n%s", view)
	}
}

// A write by another process shows on the screen that shows it.
func TestApp_AnotherProcessesWrite_ShowsOnTheScreenThatShowsIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// open goes to the screen, from the home list.
		open  func(t *testing.T, app *App) *App
		write func(t *testing.T, s testutil.TestApp, acme company.Company)
		want  string
	}{
		{name: "a company renamed, on the company list", open: func(t *testing.T, app *App) *App {
			app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})
			return app
		}, write: func(t *testing.T, s testutil.TestApp, acme company.Company) {
			if _, err := s.Companies.SetName(context.Background(), acme.ID, "Acme Corp"); err != nil {
				t.Fatalf("SetName: %v", err)
			}
		}, want: "Acme Corp"},
		{name: "an application started, on the home list", open: func(t *testing.T, app *App) *App { return app }, write: func(t *testing.T, s testutil.TestApp, acme company.Company) {
			mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-2", "Staff Engineer").ID)
		}, want: "Staff Engineer"},
		{name: "a posting synced, on its company's posting list", open: openPostingList, write: func(t *testing.T, s testutil.TestApp, acme company.Company) {
			mustUpsertPosting(t, s, acme.ID, "job-1", "Principal Engineer")
		}, want: "Principal Engineer"},
		{name: "a posting synced, on its detail", open: func(t *testing.T, app *App) *App {
			return openPostingDetail(t, openPostingList(t, app))
		}, write: func(t *testing.T, s testutil.TestApp, acme company.Company) {
			mustUpsertPosting(t, s, acme.ID, "job-1", "Principal Engineer")
		}, want: "Principal Engineer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testutil.NewTestApp(t)
			acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
			syncer := newTestSyncer(s, map[string][]jobboard.Posting{"acme": {{SourceID: "job-1", Title: "Engineer"}}})
			app := tt.open(t, newTestApp(t, s, syncer).WithChangeLog(newTestChangeReader(s)))
			// Past what opening it wrote, such as a sync.
			app = readChangeLog(t, app)
			if strings.Contains(viewText(app), tt.want) {
				t.Fatalf("the screen shows %q before the write; the test can't tell it was heard:\n%s", tt.want, viewText(app))
			}

			tt.write(t, s, acme)
			app = readChangeLog(t, app)

			if view := viewText(app); !strings.Contains(view, tt.want) {
				t.Errorf("after another process's write, the screen doesn't show %q:\n%s", tt.want, view)
			}
		})
	}
}

// readChangeLog has app read the change log once, as a tick does, and
// applies what that sends.
func readChangeLog(t *testing.T, app *App) *App {
	t.Helper()
	return applyCmd(t, app, app.changes.read())
}

// Each tick reads the change log and asks for the next, but a tick while
// a read is in flight only asks for the next, so a page is never read
// twice.
func TestApp_ATick_ReadsTheLogOnceAtATime(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	reader := NewChangeReader(s.Companies, s.Postings, s.Applications, s.Documents, s.Scores, s.Changes, "tui:1", 0, time.Millisecond)
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeLog(reader)
	tick := func() tea.Msg {
		return message.New(message.Source{Instance: reader.Instance()}, message.Subject{}, message.OpNone, time.Now().UTC(), message.Signal[message.ChangeTick]{})
	}
	// sent is how many reads and ticks msgs hold, and the read.
	sent := func(msgs []tea.Msg) (reads, ticks int, read tea.Msg) {
		for _, msg := range msgs {
			switch msg.(type) {
			case message.Event[message.Read[message.ChangePage]]:
				reads, read = reads+1, msg
			case message.Event[message.Signal[message.ChangeTick]]:
				ticks++
			}
		}
		return reads, ticks, read
	}

	app, cmd := sendKey(app, tick())
	reads, ticks, read := sent(runCmd(t, cmd))
	if reads != 1 || ticks != 1 {
		t.Fatalf("a tick sent %d reads and %d ticks, want 1 of each", reads, ticks)
	}
	app, cmd = sendKey(app, tick())
	if reads, ticks, _ := sent(runCmd(t, cmd)); reads != 0 || ticks != 1 {
		t.Errorf("a tick during a read sent %d reads and %d ticks, want only the tick", reads, ticks)
	}
	app, _ = sendKey(app, read)
	_, cmd = sendKey(app, tick())
	if reads, _, _ := sent(runCmd(t, cmd)); reads != 1 {
		t.Errorf("a tick once the read is in sent %d reads, want 1", reads)
	}
}

// The TUI's own write comes back through the change log too, but the
// command that made it already sent its event: reading the log after it
// sends nothing more.
func TestApp_ItsOwnWrite_IsAppliedOnce(t *testing.T) {
	t.Parallel()

	s := testutil.NewTestApp(t)
	acme := mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustCreateApplication(t, s, mustUpsertPosting(t, s, acme.ID, "job-1", "Engineer").ID)
	tui := servicesAs(t, s.Path, "tui:1")
	app := newTestApp(t, tui, newTestSyncer(tui, nil)).WithChangeLog(newTestChangeReader(tui))
	app = readChangeLog(t, app)

	app = sendKeyAndApply(t, app, runeKey('s'))
	app, _ = sendKey(app, runeKey('j'))
	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})

	msgs := runCmd(t, app.changes.read())
	page, ok := msgs[len(msgs)-1].(message.Event[message.Read[message.ChangePage]])
	if !ok {
		t.Fatalf("reading the log after the TUI's own write failed: %#v", msgs)
	}
	if n := len(msgs) - 1; n != 0 {
		t.Errorf("the log read after the TUI's own write sent %d events, want none: %#v", n, msgs[:n])
	}
	if page.Payload.Value.After == app.changes.after {
		t.Error("the TUI's own write wasn't in the log; the test can't tell it was skipped")
	}
}

// servicesAs is every service on the database at path, as a process
// whose writes are stamped with origin.
func servicesAs(t *testing.T, path, origin string) testutil.TestApp {
	t.Helper()
	db := testutil.OpenStore(t, path, origin)
	return testutil.TestApp{
		Path:         path,
		Store:        db,
		Companies:    service.NewCompanyService(db),
		Postings:     service.NewPostingService(db),
		Applications: service.NewApplicationService(db),
		Documents:    service.NewDocumentService(db),
		Scores:       service.NewScoringService(db),
		Changes:      service.NewChangeLog(db),
	}
}
