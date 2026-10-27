package model

import (
	"context"
	"errors"
	"fmt"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/domain/company"
	"github.com/dklassen/swamp/sync"
	"github.com/dklassen/swamp/tui/message"
)

// Syncing runs the syncs that carry on in the background whichever
// screen is up: a sync-all run ('R', #153) and single-company refreshes
// ('r'). Its view is their progress in the banner. It owns both so it can
// refuse a refresh while a run may be syncing that company (#150).
type Syncing struct {
	syncer *sync.Syncer

	run syncAllState
	// line is the run's progress, shown beside its bar.
	line string

	// refreshing counts refreshes in flight, the spinner turns while it's
	// above zero, and refreshingName is the latest one's company.
	refreshing     int
	refreshingName string
	spinner        spinner.Model

	instance message.ScreenInstance
}

func NewSyncing(syncer *sync.Syncer) *Syncing {
	return &Syncing{
		syncer:   syncer,
		spinner:  spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(CursorStyle)),
		instance: message.NewScreenInstance(),
	}
}

func (m *Syncing) Instance() message.ScreenInstance { return m.instance }

func (m *Syncing) Routes() []*Route {
	return []*Route{
		Always(m.syncAllKey),
		Always(m.syncAllStep),
		Always(m.startRefresh),
		Always(m.companyRefreshed),
		Always(m.tick),
	}
}

func (m *Syncing) Init() tea.Cmd { return nil }

// Update has nothing to do: everything the model handles is registered.
func (m *Syncing) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

// View is the run's bar and progress, or the refresh spinner, or nothing.
func (m *Syncing) View() tea.View {
	switch {
	case m.run.running:
		return tea.NewView(m.run.bar() + "  " + DimStyle.Render(m.line))
	case m.refreshing > 0:
		return tea.NewView(m.spinner.View() + DimStyle.Render("Refreshing "+m.refreshingName+"…"))
	}
	return tea.NewView("")
}

// syncAllState is a sync-all run: every company synced one after another
// while the TUI stays usable.
//
// The run is a chain of tea.Cmds, one per company: each returns a
// message.SyncAllStepMsg, and handling that issues the next. Bubble Tea
// already runs every Cmd in its own goroutine, so this needs no
// goroutines, channels or cancel functions of its own (RFC 0001, option 2).
type syncAllState struct {
	// runID is bumped on every start, so a late message from an earlier
	// run is recognised and dropped.
	runID int
	// companies is a snapshot of the company list when the run started.
	companies []company.Company
	// next is the index of the company in flight.
	next    int
	results []sync.Result
	// running stays true until the in-flight company reports back, even
	// after a stop, so a quick stop-then-start can't overlap two runs.
	running bool
	// stopping means don't issue the next company once this one reports.
	stopping bool
}

// syncBar draws a run's progress. Drawn from next on every render rather
// than animated: its frame ticks would have to ride along the one-Cmd-
// per-company chain.
var syncBar = progress.New(progress.WithDefaultBlend(), progress.WithFillCharacters(progress.DefaultFullCharFullBlock, progress.DefaultEmptyCharBlock), progress.WithoutPercentage(), progress.WithWidth(24))

// bar is the share of companies already synced.
func (r *syncAllState) bar() string {
	if len(r.companies) == 0 {
		return syncBar.ViewAs(0)
	}
	return syncBar.ViewAs(float64(r.next) / float64(len(r.companies)))
}

// syncAllStep syncs one company as a Cmd.
func syncAllStep(syncer *sync.Syncer, runID int, company company.Company) tea.Cmd {
	return func() tea.Msg {
		result, err := syncer.SyncCompany(context.Background(), company.ID)
		if err != nil {
			return message.SyncAllStepMsg{RunID: runID, Result: sync.Result{CompanyID: company.ID, Name: company.Name, Err: err}}
		}
		return message.SyncAllStepMsg{RunID: runID, Result: *result}
	}
}

// syncAllKey is 'R': it starts a run, or stops the one under way. A stop
// lets the company in flight finish -- SyncCompany isn't cancelled
// mid-company -- and 'R' does nothing more until it has reported back, so
// two runs never overlap.
func (m *Syncing) syncAllKey(toggle message.ToggleSyncAllMsg) tea.Cmd {
	run := &m.run
	switch {
	case !run.running:
		return m.startSyncAll(toggle.Companies)
	case !run.stopping:
		run.stopping = true
		m.line = fmt.Sprintf("Stopping after %s…", run.companies[run.next].Name)
	}
	return nil
}

// startSyncAll starts a run over companies and returns the first
// company's Cmd.
func (m *Syncing) startSyncAll(companies []company.Company) tea.Cmd {
	m.run = syncAllState{
		runID:     m.run.runID + 1,
		companies: companies,
		running:   true,
	}
	// An error from an earlier action takes priority over the run's
	// progress in the banner and would hide it.
	return tea.Batch(message.ReportStatus(message.Source{Instance: m.instance}, ""), m.issueSyncAllStep())
}

// issueSyncAllStep shows progress for the company about to be fetched
// and returns its Cmd, or finishes the run when none are left.
func (m *Syncing) issueSyncAllStep() tea.Cmd {
	run := &m.run
	if run.next >= len(run.companies) {
		return m.finishSyncAll()
	}
	company := run.companies[run.next]
	m.line = fmt.Sprintf("Syncing %d/%d: %s…", run.next+1, len(run.companies), company.Name)
	return syncAllStep(m.syncer, run.runID, company)
}

// syncAllStep records one company's result, then issues the next company
// or finishes the run.
func (m *Syncing) syncAllStep(step message.SyncAllStepMsg) tea.Cmd {
	run := &m.run
	if !run.running || step.RunID != run.runID {
		return nil
	}
	run.results = append(run.results, step.Result)
	run.next++
	if run.stopping {
		return m.finishSyncAll()
	}
	return m.issueSyncAllStep()
}

// finishSyncAll ends the run, reporting its summary and announcing its
// results.
func (m *Syncing) finishSyncAll() tea.Cmd {
	run := &m.run
	run.running = false
	summary := sync.Summarize(run.results).String()
	if run.next < len(run.companies) {
		summary = fmt.Sprintf("Stopped after %d/%d: %s", run.next, len(run.companies), summary)
	}
	results := append([]sync.Result(nil), run.results...)
	return tea.Batch(message.ReportStatus(message.Source{Instance: m.instance}, summary), func() tea.Msg { return message.SyncAllFinishedMsg{Results: results} })
}

func refreshCompany(syncer *sync.Syncer, company company.Company) tea.Cmd {
	return func() tea.Msg {
		result, err := syncer.SyncCompany(context.Background(), company.ID)
		if err != nil {
			return message.CompanyRefreshedMsg{Name: company.Name, Err: err}
		}
		return message.CompanyRefreshedMsg{Name: company.Name, Result: *result}
	}
}

func (m *Syncing) startRefresh(req message.RefreshCompanyMsg) tea.Cmd {
	return m.refresh(req.Company)
}

// refresh re-syncs company with the spinner turning until it reports, or
// refuses while a run may be syncing it right now (#150). The refresh goes
// first in the batch so a fast one has already finished when the
// spinner's first tick arrives, which then stops it.
func (m *Syncing) refresh(company company.Company) tea.Cmd {
	if m.run.running {
		m.line = "Sync all in progress: refresh unavailable"
		return nil
	}
	m.refreshing++
	m.refreshingName = company.Name
	return tea.Batch(refreshCompany(m.syncer, company), m.spinner.Tick)
}

func (m *Syncing) companyRefreshed(refreshed message.CompanyRefreshedMsg) tea.Cmd {
	m.refreshing = max(m.refreshing-1, 0)
	switch {
	case errors.Is(refreshed.Err, sync.ErrSyncInProgress):
		// Another sync of this company (e.g. a scheduled `swamp fetch`) is
		// running; this one fetched and wrote nothing.
		return message.ReportStatus(message.Source{Instance: m.instance}, refreshed.Name+" is already syncing elsewhere; try again in a moment")
	case refreshed.Err != nil:
		return ReportErr(message.Source{Instance: m.instance}, refreshed.Err)
	}
	r := refreshed.Result
	return message.ReportStatus(message.Source{Instance: m.instance}, fmt.Sprintf("%s: fetched %d, created %d, updated %d, closed %d, reopened %d",
		r.Name, r.Fetched, r.Created, r.Updated, r.Closed, r.Reopened))
}

func (m *Syncing) tick(msg spinner.TickMsg) tea.Cmd {
	if m.refreshing == 0 {
		return nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return cmd
}
