package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dklassen/swamp/internal/domain/posting"
	"github.com/dklassen/swamp/internal/testutil"
	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui/message"
)

// newSyncAllTestApp returns an App on the company list with companies
// Acme, Globex and Initech, whose boards each list one posting except
// those named in failing, whose fetch fails.
func newSyncAllTestApp(t *testing.T, failing ...string) (*App, testutil.TestApp) {
	t.Helper()
	s := testutil.NewTestApp(t)
	postings := map[string][]jobboard.Posting{}
	errBoards := map[string]error{}
	for _, slug := range []string{"acme", "globex", "initech"} {
		postings[slug] = []jobboard.Posting{{SourceID: slug + "-1", Title: "Engineer"}}
	}
	for _, slug := range failing {
		errBoards[slug] = errors.New("status 404")
	}
	mustCreateCompany(t, s, "Acme", jobboard.Ashby, "acme")
	mustCreateCompany(t, s, "Globex", jobboard.Ashby, "globex")
	mustCreateCompany(t, s, "Initech", jobboard.Ashby, "initech")
	syncer := sync.New(s.Companies, s.Postings, map[string]sync.PostingFetcher{jobboard.Ashby: &fakeFetcher{postings: postings, errBoards: errBoards}}, sync.DefaultConfig())
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})
	return app, s
}

// step runs one command and feeds its message to app (each one, for a
// batch), returning the commands Update gives back without running them
// -- unlike applyCmd, which drains the whole chain -- so tests can act
// between companies.
func step(t *testing.T, app *App, cmd tea.Cmd) (*App, tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("step: nil Cmd, want the next sync-all step")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		return sendKey(app, cmd())
	}
	var next []tea.Cmd
	for _, c := range batch {
		var n tea.Cmd
		app, n = sendKey(app, c())
		next = append(next, n)
	}
	return app, tea.Batch(next...)
}

// bannerLine is the last line app shows: the banner, when it has one.
func bannerLine(app *App) string {
	lines := strings.Split(ansi.Strip(viewText(app)), "\n")
	return lines[len(lines)-1]
}

// TestApp_SyncAll_R_StartsWithFirstCompany: 'R' on the company list
// starts syncing every company in the background, and the status names
// the one being fetched (#153).
func TestApp_SyncAll_R_StartsWithFirstCompany(t *testing.T) {
	app, _ := newSyncAllTestApp(t)

	app, cmd := sendIntentKey(t, app, runeKey('R'))
	if cmd == nil {
		t.Fatal("Update on 'R' returned nil Cmd, want the first sync step")
	}
	if want := "Syncing 1/3: Acme"; !strings.Contains(bannerLine(app), want) {
		t.Errorf("banner = %q, want it to contain %q", bannerLine(app), want)
	}
}

// TestApp_SyncAll_StepsThroughEveryCompanyThenSummarizes: each company's
// result advances the status to the next, the last one ends with the same
// summary `swamp fetch` prints, and everything was synced.
func TestApp_SyncAll_StepsThroughEveryCompanyThenSummarizes(t *testing.T) {
	app, s := newSyncAllTestApp(t)

	app, cmd := sendIntentKey(t, app, runeKey('R'))
	app, cmd = step(t, app, cmd)
	if want := "Syncing 2/3: Globex"; !strings.Contains(bannerLine(app), want) {
		t.Errorf("after the first company, banner = %q, want it to contain %q", bannerLine(app), want)
	}
	app, cmd = step(t, app, cmd)
	if want := "Syncing 3/3: Initech"; !strings.Contains(bannerLine(app), want) {
		t.Errorf("after the second company, banner = %q, want it to contain %q", bannerLine(app), want)
	}
	app, cmd = step(t, app, cmd)
	app = applyCmd(t, app, cmd) // the summary and the end-of-run reload
	if app.status != "3 companies, 0 failed" {
		t.Errorf("final status = %q, want %q", app.status, "3 companies, 0 failed")
	}
	if syncBarShowing(app) {
		t.Error("progress bar still showing after the last company")
	}
	for _, c := range app.companyList.companies {
		postings, err := s.Postings.ListByCompany(t.Context(), c.ID)
		if err != nil {
			t.Fatalf("ListPostingsByCompany: %v", err)
		}
		if len(postings) != 1 {
			t.Errorf("%s: %d postings, want 1 -- every company synced", c.Name, len(postings))
		}
	}
}

// TestApp_SyncAll_FailingCompany_RunContinuesAndSummaryNamesIt.
func TestApp_SyncAll_FailingCompany_RunContinuesAndSummaryNamesIt(t *testing.T) {
	app, _ := newSyncAllTestApp(t, "globex")

	app, cmd := sendIntentKey(t, app, runeKey('R'))
	for range 3 {
		app, cmd = step(t, app, cmd)
	}
	app = applyCmd(t, app, cmd)
	if want := "3 companies, 1 failed (Globex)"; app.status != want {
		t.Errorf("final status = %q, want %q", app.status, want)
	}
	if app.err != nil {
		t.Errorf("app.err = %v, want nil -- a failed company is in the summary, not an error", app.err)
	}
}

// TestApp_SyncAll_NoCompanies_FinishesImmediately.
func TestApp_SyncAll_NoCompanies_FinishesImmediately(t *testing.T) {
	s := testutil.NewTestApp(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})

	app, cmd := sendIntentKey(t, app, runeKey('R'))
	app = applyCmd(t, app, cmd)
	if app.status != "0 companies, 0 failed" || syncBarShowing(app) {
		t.Errorf("status = %q, bar showing = %v, want %q and no bar", app.status, syncBarShowing(app), "0 companies, 0 failed")
	}
}

// TestApp_SyncAll_RAgainDuringRun_StopsAfterInFlightCompany: 'R' during
// a run stops it -- the company in flight finishes, no further one starts
// -- and the status says how far it got. Esc keeps meaning "back".
func TestApp_SyncAll_RAgainDuringRun_StopsAfterInFlightCompany(t *testing.T) {
	app, _ := newSyncAllTestApp(t)

	app, cmd := sendIntentKey(t, app, runeKey('R'))
	app, _ = sendIntentKey(t, app, runeKey('R')) // stop while Acme is in flight
	app, next := step(t, app, cmd)               // Acme reports back

	if syncBarShowing(app) {
		t.Error("progress bar still showing after stopping")
	}
	if next == nil {
		t.Fatal("stopping returned nil Cmd, want the summary and the end-of-run reload")
	}
	if msg, ok := next().(tea.BatchMsg); ok {
		for _, c := range msg {
			if _, isStep := c().(message.Event[message.Ran[message.SyncReport]]); isStep {
				t.Error("stopping issued another company's sync")
			}
		}
	}
	app = applyCmd(t, app, next)
	if want := "Stopped after 1/3: 1 company, 0 failed"; app.status != want {
		t.Errorf("status = %q, want %q", app.status, want)
	}
}

// TestApp_SyncAll_StopThenRBeforeInFlightReports_NoSecondRun: after a
// stop, 'R' is ignored until the in-flight company has reported back, so
// two runs can never overlap.
func TestApp_SyncAll_StopThenRBeforeInFlightReports_NoSecondRun(t *testing.T) {
	app, _ := newSyncAllTestApp(t)

	app, cmd := sendIntentKey(t, app, runeKey('R'))
	app, _ = sendIntentKey(t, app, runeKey('R')) // stop
	app, again := sendIntentKey(t, app, runeKey('R'))
	if again != nil {
		t.Error("'R' while the stopped run's company is still in flight returned a Cmd, want nil")
	}
	if want := "Stopping after Acme…"; !strings.HasSuffix(bannerLine(app), want) {
		t.Errorf("banner = %q, want it to end with %q -- no second run started", bannerLine(app), want)
	}
	app, _ = step(t, app, cmd)
	if syncBarShowing(app) {
		t.Error("progress bar still showing after the in-flight company reported")
	}
}

// TestApp_SyncAll_MessageFromEarlierRun_Ignored: a result arriving late
// from a previous run must not count towards, or advance, the current one.
func TestApp_SyncAll_MessageFromEarlierRun_Ignored(t *testing.T) {
	app, _ := newSyncAllTestApp(t)

	app, cmd := sendIntentKey(t, app, runeKey('R'))
	app, _ = sendIntentKey(t, app, runeKey('R'))
	app, _ = step(t, app, cmd) // run 1 stops after Acme
	app, _ = sendIntentKey(t, app, runeKey('R'))
	banner := bannerLine(app)

	app, stray := sendKey(app, synced(message.Source{Instance: app.syncing.Instance()}, app.companyList.companies[0].ID, 1, 0))
	for _, msg := range runCmd(t, stray) {
		if _, isStep := msg.(message.Event[message.Ran[message.SyncReport]]); isStep {
			t.Error("a message from run 1 issued a company's sync during run 2")
		}
	}
	if bannerLine(app) != banner {
		t.Errorf("banner after a stray run-1 message = %q, want %q (unchanged)", bannerLine(app), banner)
	}
}

// TestApp_SyncAll_RDuringRun_Refused: while a run is under way, 'r' is
// refused with a status message rather than starting a second sync of a
// company the run may be syncing -- the in-process half of #150.
func TestApp_SyncAll_RDuringRun_Refused(t *testing.T) {
	app, _ := newSyncAllTestApp(t)

	app, _ = sendIntentKey(t, app, runeKey('R'))
	app, cmd := sendIntentKey(t, app, runeKey('r'))
	if cmd != nil {
		t.Error("'r' during a sync-all run returned a Cmd, want nil")
	}
	if want := "Sync all in progress: refresh unavailable"; !strings.HasSuffix(bannerLine(app), want) {
		t.Errorf("banner = %q, want it to end with %q", bannerLine(app), want)
	}
}

// TestApp_SyncAll_FilterSaveDuringRun_Saves: saving filters doesn't sync,
// so a run in progress is no reason to refuse it.
func TestApp_SyncAll_FilterSaveDuringRun_Saves(t *testing.T) {
	app, s := newSyncAllTestApp(t)
	if _, err := s.Postings.Ingest(t.Context(), app.companyList.companies[0].ID, jobboard.Ashby, "acme-eng", posting.IngestedFields{Title: "Engineer", Department: "Engineering"}); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	app, _ = sendIntentKey(t, app, runeKey('R'))                  // a run under way, its first step not run
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter}) // Acme's postings
	app = applyCmd(t, app, cmd)
	app, _ = openFilters(t, app)
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})

	app = sendKeyAndApply(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.currentScreen != screenPostingList {
		t.Errorf("screen = %v, want screenPostingList", app.currentScreen)
	}
	saved, err := s.Companies.Filters(t.Context(), openList(t, app).company.ID)
	if err != nil {
		t.Fatalf("Filters: %v", err)
	}
	if len(saved) != 1 {
		t.Errorf("saved filters = %+v, want the one selected", saved)
	}
}

// TestApp_SyncAll_LeavingCompanyList_RunContinues: the run doesn't belong
// to the company list screen, so it carries on and still reports its
// summary after the user moves elsewhere.
func TestApp_SyncAll_LeavingCompanyList_RunContinues(t *testing.T) {
	app, _ := newSyncAllTestApp(t)

	app, cmd := sendIntentKey(t, app, runeKey('R'))
	app, _ = sendKey(app, tea.KeyPressMsg{Code: tea.KeyTab})
	if app.currentScreen != screenAllPostings {
		t.Fatalf("screen after tab = %v, want screenAllPostings", app.currentScreen)
	}
	for range 3 {
		app, cmd = step(t, app, cmd)
	}
	app = applyCmd(t, app, cmd)
	if app.status != "3 companies, 0 failed" {
		t.Errorf("status after leaving the company list = %q, want %q", app.status, "3 companies, 0 failed")
	}
}

// TestApp_SyncAll_CompanySyncingElsewhere_SummarizedAsSkipped: a company
// another process is syncing (its lease is held, #150) is skipped by the
// run and reported as skipped, not failed.
func TestApp_SyncAll_CompanySyncingElsewhere_SummarizedAsSkipped(t *testing.T) {
	app, s := newSyncAllTestApp(t)
	globex := app.companyList.companies[1]
	if taken, err := s.Companies.AcquireSyncLease(t.Context(), globex.ID, "swamp-fetch", time.Minute); err != nil || !taken {
		t.Fatalf("AcquireSyncLease = %v, %v", taken, err)
	}

	app, cmd := sendIntentKey(t, app, runeKey('R'))
	for range 3 {
		app, cmd = step(t, app, cmd)
	}
	app = applyCmd(t, app, cmd)
	if want := "3 companies, 0 failed, 1 skipped (Globex)"; app.status != want {
		t.Errorf("final status = %q, want %q", app.status, want)
	}
}

// Refreshing a company's postings waits for a sync-all run, which may be
// syncing that company, as refreshing it from the company list does.
func TestApp_SyncAll_PostingListRefreshUnavailable(t *testing.T) {
	app, _ := newSyncAllTestApp(t)

	app, _ = sendIntentKey(t, app, runeKey('R')) // the run's first step isn't run
	app, cmd := sendKey(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	app = applyCmd(t, app, cmd)
	if app.currentScreen != screenPostingList {
		t.Fatalf("screen = %v, want the posting list", app.currentScreen)
	}
	app, cmd = sendIntentKey(t, app, runeKey('r'))
	if cmd != nil {
		t.Errorf("r during sync all returned a command, want none")
	}
	if want := "Sync all in progress: refresh unavailable"; !strings.HasSuffix(bannerLine(app), want) {
		t.Errorf("banner = %q, want it to end with %q", bannerLine(app), want)
	}
}

// barCells is how much of the sync-all bar on the view's last line is
// filled, and its length in cells; 0, 0 when there's no bar.
func barCells(view string) (filled, total int) {
	lines := strings.Split(ansi.Strip(view), "\n")
	last := lines[len(lines)-1]
	filled = strings.Count(last, "█")
	return filled, filled + strings.Count(last, "░")
}

// syncBarShowing is whether app's banner has the sync-all bar, which it
// does for exactly as long as a run is under way.
func syncBarShowing(app *App) bool {
	_, total := barCells(viewText(app))
	return total > 0
}

// TestApp_SyncAll_ProgressBarFillsAsCompaniesFinish: the bar shows the share
// of companies already synced, beside the company in flight.
func TestApp_SyncAll_ProgressBarFillsAsCompaniesFinish(t *testing.T) {
	app, _ := newSyncAllTestApp(t)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})

	app, cmd := sendIntentKey(t, app, runeKey('R'))
	for done := range 3 {
		filled, total := barCells(viewText(app))
		if total == 0 {
			t.Fatalf("after %d of 3 companies, no progress bar on the last line:\n%s", done, viewText(app))
		}
		if want := total * done / 3; filled < want-1 || filled > want+1 {
			t.Errorf("after %d of 3 companies, bar filled %d of %d cells, want about %d", done, filled, total, want)
		}
		app, cmd = step(t, app, cmd)
	}
	if _, total := barCells(viewText(app)); total != 0 {
		t.Errorf("progress bar still showing after the run finished:\n%s", viewText(app))
	}
}

// TestApp_Refresh_ShowsASpinnerUntilTheCompanyReports: one company's
// fetch takes as long as its board does, with nothing to measure a bar by.
func TestApp_Refresh_ShowsASpinnerUntilTheCompanyReports(t *testing.T) {
	app, _ := newSyncAllTestApp(t)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})

	app, cmd := sendIntentKey(t, app, runeKey('r'))

	lines := strings.Split(ansi.Strip(viewText(app)), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "Refreshing Acme…") || !strings.ContainsAny(last, spinnerGlyphs()) {
		t.Errorf("last line while refreshing = %q, want a spinner and Refreshing Acme…", last)
	}

	app = applyCmd(t, app, cmd)

	lines = strings.Split(ansi.Strip(viewText(app)), "\n")
	last = lines[len(lines)-1]
	if strings.Contains(last, "Refreshing") || strings.ContainsAny(last, spinnerGlyphs()) {
		t.Errorf("last line after the refresh = %q, want the spinner gone", last)
	}
	if !strings.Contains(last, "Acme: fetched 1") {
		t.Errorf("last line after the refresh = %q, want the refresh's summary", last)
	}
}

// spinnerGlyphs is every frame of spinner.Dot without the space each ends in.
func spinnerGlyphs() string {
	return strings.ReplaceAll(strings.Join(spinner.Dot.Frames, ""), " ", "")
}
