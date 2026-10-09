package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/dklassen/swamp/jobboard"
	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
)

// newSyncAllTestApp returns an App on the company list with companies
// Acme, Globex and Initech, whose boards each list one posting except
// those named in failing, whose fetch fails.
func newSyncAllTestApp(t *testing.T, failing ...string) (*App, *store.Store) {
	t.Helper()
	s := newTestStore(t)
	postings := map[string][]jobboard.Posting{}
	errBoards := map[string]error{}
	for _, slug := range []string{"acme", "globex", "initech"} {
		postings[slug] = []jobboard.Posting{{SourceID: slug + "-1", Title: "Engineer"}}
	}
	for _, slug := range failing {
		errBoards[slug] = errors.New("status 404")
	}
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	mustCreateCompany(t, s, "Globex", "ashby", "globex")
	mustCreateCompany(t, s, "Initech", "ashby", "initech")
	syncer := sync.New(s, map[string]sync.PostingFetcher{"ashby": &fakeFetcher{postings: postings, errBoards: errBoards}}, sync.DefaultConfig())
	app := newTestApp(t, s, syncer)
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyTab})
	return app, s
}

// step runs one command and feeds its message to app, returning the
// command Update gives back without running it -- unlike applyCmd, which
// drains the whole chain -- so tests can act between companies.
func step(t *testing.T, app *App, cmd tea.Cmd) (*App, tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("step: nil Cmd, want the next sync-all step")
	}
	return sendKey(app, cmd())
}

// TestApp_SyncAll_R_StartsWithFirstCompany: 'R' on the company list
// starts syncing every company in the background, and the status names
// the one being fetched (#153).
func TestApp_SyncAll_R_StartsWithFirstCompany(t *testing.T) {
	app, _ := newSyncAllTestApp(t)

	app, cmd := sendKey(app, runeKey('R'))
	if cmd == nil {
		t.Fatal("Update on 'R' returned nil Cmd, want the first sync step")
	}
	if want := "Syncing 1/3: Acme"; !strings.Contains(app.status, want) {
		t.Errorf("status = %q, want it to contain %q", app.status, want)
	}
}

// TestApp_SyncAll_StepsThroughEveryCompanyThenSummarizes: each company's
// result advances the status to the next, the last one ends with the same
// summary `swamp fetch` prints, and everything was synced.
func TestApp_SyncAll_StepsThroughEveryCompanyThenSummarizes(t *testing.T) {
	app, s := newSyncAllTestApp(t)

	app, cmd := sendKey(app, runeKey('R'))
	app, cmd = step(t, app, cmd)
	if want := "Syncing 2/3: Globex"; !strings.Contains(app.status, want) {
		t.Errorf("after the first company, status = %q, want it to contain %q", app.status, want)
	}
	app, cmd = step(t, app, cmd)
	if want := "Syncing 3/3: Initech"; !strings.Contains(app.status, want) {
		t.Errorf("after the second company, status = %q, want it to contain %q", app.status, want)
	}
	app, cmd = step(t, app, cmd)
	if app.status != "3 companies, 0 failed" {
		t.Errorf("final status = %q, want %q", app.status, "3 companies, 0 failed")
	}
	if app.syncAll.running {
		t.Error("syncAll.running after the last company = true, want false")
	}
	app = applyCmd(t, app, cmd) // the end-of-run reload
	for _, c := range app.companies {
		postings, err := s.ListPostingsByCompany(t.Context(), c.ID)
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

	app, cmd := sendKey(app, runeKey('R'))
	for range 3 {
		app, cmd = step(t, app, cmd)
	}
	if want := "3 companies, 1 failed (Globex)"; app.status != want {
		t.Errorf("final status = %q, want %q", app.status, want)
	}
	if app.err != nil {
		t.Errorf("app.err = %v, want nil -- a failed company is in the summary, not an error", app.err)
	}
}

// TestApp_SyncAll_NoCompanies_FinishesImmediately.
func TestApp_SyncAll_NoCompanies_FinishesImmediately(t *testing.T) {
	s := newTestStore(t)
	app := newTestApp(t, s, newTestSyncer(s, nil))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyTab})

	app, _ = sendKey(app, runeKey('R'))
	if app.status != "0 companies, 0 failed" || app.syncAll.running {
		t.Errorf("status = %q, running = %v, want %q and false", app.status, app.syncAll.running, "0 companies, 0 failed")
	}
}

// TestApp_SyncAll_RAgainDuringRun_StopsAfterInFlightCompany: 'R' during
// a run stops it -- the company in flight finishes, no further one starts
// -- and the status says how far it got. Esc keeps meaning "back".
func TestApp_SyncAll_RAgainDuringRun_StopsAfterInFlightCompany(t *testing.T) {
	app, _ := newSyncAllTestApp(t)

	app, cmd := sendKey(app, runeKey('R'))
	app, _ = sendKey(app, runeKey('R')) // stop while Acme is in flight
	app, next := step(t, app, cmd)      // Acme reports back

	if want := "Stopped after 1/3: 1 company, 0 failed"; app.status != want {
		t.Errorf("status = %q, want %q", app.status, want)
	}
	if app.syncAll.running {
		t.Error("syncAll.running after stopping = true, want false")
	}
	if next == nil {
		t.Fatal("stopping returned nil Cmd, want the end-of-run reload")
	}
	if msg, ok := next().(tea.BatchMsg); ok {
		for _, c := range msg {
			if _, isStep := c().(syncAllStepMsg); isStep {
				t.Error("stopping issued another company's sync")
			}
		}
	}
}

// TestApp_SyncAll_StopThenRBeforeInFlightReports_NoSecondRun: after a
// stop, 'R' is ignored until the in-flight company has reported back, so
// two runs can never overlap.
func TestApp_SyncAll_StopThenRBeforeInFlightReports_NoSecondRun(t *testing.T) {
	app, _ := newSyncAllTestApp(t)

	app, cmd := sendKey(app, runeKey('R'))
	app, _ = sendKey(app, runeKey('R')) // stop
	app, again := sendKey(app, runeKey('R'))
	if again != nil {
		t.Error("'R' while the stopped run's company is still in flight returned a Cmd, want nil")
	}
	if app.syncAll.runID != 1 {
		t.Errorf("runID = %d, want 1 -- no second run started", app.syncAll.runID)
	}
	app, _ = step(t, app, cmd)
	if app.syncAll.running {
		t.Error("syncAll.running after the in-flight company reported = true, want false")
	}
}

// TestApp_SyncAll_MessageFromEarlierRun_Ignored: a result arriving late
// from a previous run must not count towards, or advance, the current one.
func TestApp_SyncAll_MessageFromEarlierRun_Ignored(t *testing.T) {
	app, _ := newSyncAllTestApp(t)

	app, cmd := sendKey(app, runeKey('R'))
	app, _ = sendKey(app, runeKey('R'))
	app, _ = step(t, app, cmd) // run 1 stops after Acme
	app, _ = sendKey(app, runeKey('R'))
	status := app.status

	app, stray := sendKey(app, syncAllStepMsg{runID: 1, result: sync.Result{Name: "Acme"}})
	if stray != nil {
		t.Error("a message from run 1 issued a Cmd during run 2, want nil")
	}
	if len(app.syncAll.results) != 0 || app.syncAll.next != 0 || app.status != status {
		t.Errorf("after a stray run-1 message: results %d, next %d, status %q; want 0, 0, %q",
			len(app.syncAll.results), app.syncAll.next, app.status, status)
	}
}

// TestApp_SyncAll_RDuringRun_Refused: while a run is under way, 'r' is
// refused with a status message rather than starting a second sync of a
// company the run may be syncing -- the in-process half of #150.
func TestApp_SyncAll_RDuringRun_Refused(t *testing.T) {
	app, _ := newSyncAllTestApp(t)

	app, _ = sendKey(app, runeKey('R'))
	app, cmd := sendKey(app, runeKey('r'))
	if cmd != nil {
		t.Error("'r' during a sync-all run returned a Cmd, want nil")
	}
	if want := "Sync all in progress: refresh unavailable"; app.status != want {
		t.Errorf("status = %q, want %q", app.status, want)
	}
}

// TestApp_SyncAll_FilterSaveDuringRun_Refused: saving a filter selection
// re-syncs the company, so it's refused during a run too -- nothing is
// saved or narrowed, and the user stays on the filter screen to save once
// the run finishes.
func TestApp_SyncAll_FilterSaveDuringRun_Refused(t *testing.T) {
	app, s := newSyncAllTestApp(t)
	app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyEnter}) // Acme's postings
	app = applyCmd(t, app, cmd)
	app, cmd = sendKey(app, runeKey('f'))
	app = applyCmd(t, app, cmd)
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeySpace})

	app.syncAll = syncAllState{runID: 1, running: true, companies: app.companies}
	app, cmd = sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("saving filters during a sync-all run returned a Cmd, want nil")
	}
	if app.screen != screenFilterSelect {
		t.Errorf("screen = %v, want still screenFilterSelect", app.screen)
	}
	if want := "Sync all in progress: save filters when it finishes"; app.status != want {
		t.Errorf("status = %q, want %q", app.status, want)
	}
	saved, err := s.ListCompanyFilters(t.Context(), app.selectedCompany.ID)
	if err != nil {
		t.Fatalf("ListCompanyFilters: %v", err)
	}
	if len(saved) != 0 {
		t.Errorf("saved filters = %+v, want none", saved)
	}
}

// TestApp_SyncAll_LeavingCompanyList_RunContinues: the run belongs to
// App, not the company list screen, so it carries on and still reports
// its summary after the user moves elsewhere.
func TestApp_SyncAll_LeavingCompanyList_RunContinues(t *testing.T) {
	app, _ := newSyncAllTestApp(t)

	app, cmd := sendKey(app, runeKey('R'))
	app, _ = sendKey(app, tea.KeyMsg{Type: tea.KeyTab})
	if app.screen != screenActiveApplications {
		t.Fatalf("screen after tab = %v, want screenActiveApplications", app.screen)
	}
	for range 3 {
		app, cmd = step(t, app, cmd)
	}
	if app.status != "3 companies, 0 failed" {
		t.Errorf("status after leaving the company list = %q, want %q", app.status, "3 companies, 0 failed")
	}
}

// TestApp_SyncAll_CompanySyncingElsewhere_SummarizedAsSkipped: a company
// another process is syncing (its lease is held, #150) is skipped by the
// run and reported as skipped, not failed.
func TestApp_SyncAll_CompanySyncingElsewhere_SummarizedAsSkipped(t *testing.T) {
	app, s := newSyncAllTestApp(t)
	globex := app.companies[1]
	if taken, err := s.AcquireSyncLease(t.Context(), globex.ID, "swamp-fetch", time.Minute); err != nil || !taken {
		t.Fatalf("AcquireSyncLease = %v, %v", taken, err)
	}

	app, cmd := sendKey(app, runeKey('R'))
	for range 3 {
		app, cmd = step(t, app, cmd)
	}
	if want := "3 companies, 0 failed, 1 skipped (Globex)"; app.status != want {
		t.Errorf("final status = %q, want %q", app.status, want)
	}
}

// Refreshing a company's postings waits for a sync-all run, which may be
// syncing that company, as refreshing it from the company list does.
func TestApp_SyncAll_PostingListRefreshUnavailable(t *testing.T) {
	app, _ := newSyncAllTestApp(t)

	app, _ = sendKey(app, runeKey('R')) // the run's first step isn't run
	app, cmd := sendKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	app = applyCmd(t, app, cmd)
	if app.screen != screenPostingList {
		t.Fatalf("screen = %v, want the posting list", app.screen)
	}
	app, cmd = sendKey(app, runeKey('r'))
	if cmd != nil {
		t.Errorf("r during sync all returned a command, want none")
	}
	if want := "Sync all in progress: refresh unavailable"; app.status != want {
		t.Errorf("status = %q, want %q", app.status, want)
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

// TestApp_SyncAll_ProgressBarFillsAsCompaniesFinish: the bar shows the share
// of companies already synced, beside the company in flight.
func TestApp_SyncAll_ProgressBarFillsAsCompaniesFinish(t *testing.T) {
	app, _ := newSyncAllTestApp(t)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})

	app, cmd := sendKey(app, runeKey('R'))
	for done := range 3 {
		filled, total := barCells(app.View())
		if total == 0 {
			t.Fatalf("after %d of 3 companies, no progress bar on the last line:\n%s", done, app.View())
		}
		if want := total * done / 3; filled < want-1 || filled > want+1 {
			t.Errorf("after %d of 3 companies, bar filled %d of %d cells, want about %d", done, filled, total, want)
		}
		app, cmd = step(t, app, cmd)
	}
	if _, total := barCells(app.View()); total != 0 {
		t.Errorf("progress bar still showing after the run finished:\n%s", app.View())
	}
}

// TestApp_Refresh_ShowsASpinnerUntilTheCompanyReports: one company's
// fetch takes as long as its board does, with nothing to measure a bar by.
func TestApp_Refresh_ShowsASpinnerUntilTheCompanyReports(t *testing.T) {
	app, _ := newSyncAllTestApp(t)
	app, _ = sendKey(app, tea.WindowSizeMsg{Width: 120, Height: 30})

	app, cmd := sendKey(app, runeKey('r'))

	lines := strings.Split(ansi.Strip(app.View()), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "Refreshing Acme…") || !strings.ContainsAny(last, spinnerGlyphs()) {
		t.Errorf("last line while refreshing = %q, want a spinner and Refreshing Acme…", last)
	}

	app = applyCmd(t, app, cmd)

	lines = strings.Split(ansi.Strip(app.View()), "\n")
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
