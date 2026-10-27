package model

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

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

	// refreshes are those in flight, by company, since an outcome names
	// only its company's ID; the spinner turns while there are any, and
	// refreshingName is the latest one's company.
	refreshes      map[int64]refresh
	refreshingName string
	spinner        spinner.Model

	instance message.ScreenInstance
}

type refresh struct {
	name     string
	inFlight int
}

func NewSyncing(syncer *sync.Syncer) *Syncing {
	return &Syncing{
		syncer:    syncer,
		refreshes: map[int64]refresh{},
		spinner:   spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(CursorStyle)),
		instance:  message.NewScreenInstance(),
	}
}

func (m *Syncing) Instance() message.ScreenInstance { return m.instance }

func (m *Syncing) Routes() []*Route {
	return []*Route{
		Always(m.syncAllKey),
		Always(m.synced, From(m.instance)),
		Always(m.startRefresh),
		Always(m.refreshFailed, From(m.instance)),
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
	case len(m.refreshes) > 0:
		return tea.NewView(m.spinner.View() + DimStyle.Render("Refreshing "+m.refreshingName+"…"))
	}
	return tea.NewView("")
}

// syncAllState is a sync-all run: every company synced one after another
// while the TUI stays usable.
//
// The run is a chain of tea.Cmds, one per company: each reports a
// Ran[SyncReport], and handling that issues the next. Bubble Tea already
// runs every Cmd in its own goroutine, so this needs no goroutines,
// channels or cancel functions of its own (RFC 0001, option 2).
type syncAllState struct {
	// runID is bumped on every start, so a late message from an earlier
	// run is recognised and dropped.
	runID int
	// companies is a snapshot of the company list when the run started.
	companies []message.Company
	// next is the index of the company in flight.
	next int
	// failed and skipped name the companies that didn't sync, in order.
	failed, skipped []string
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

// syncAllStep syncs one company as a Cmd. A company that doesn't sync is
// reported with its code, not as an Error, which would take over the
// banner from the run's progress.
func syncAllStep(syncer *sync.Syncer, src message.Source, runID int, company message.Company) tea.Cmd {
	return func() tea.Msg {
		report := message.SyncReport{RunID: runID}
		result, err := syncer.SyncCompany(context.Background(), company.ID)
		if err != nil {
			report.Code = ErrorOf(err).Code
		} else {
			report = reportOf(runID, *result)
		}
		return message.New(src, message.Subject{Type: message.TypeCompany, ID: company.ID}, message.OpNone, time.Now().UTC(),
			message.Ran[message.SyncReport]{Value: report})
	}
}

func reportOf(runID int, r sync.Result) message.SyncReport {
	return message.SyncReport{RunID: runID, Fetched: r.Fetched, Created: r.Created, Updated: r.Updated, Closed: r.Closed, Reopened: r.Reopened}
}

// syncAllKey is 'R': it starts a run, or stops the one under way. A stop
// lets the company in flight finish -- SyncCompany isn't cancelled
// mid-company -- and 'R' does nothing more until it has reported back, so
// two runs never overlap.
func (m *Syncing) syncAllKey(e message.Event[message.Run[message.SyncAll]]) tea.Cmd {
	run := &m.run
	switch {
	case !run.running:
		return m.startSyncAll(e.Payload.Value.Companies)
	case !run.stopping:
		run.stopping = true
		m.line = fmt.Sprintf("Stopping after %s…", run.companies[run.next].Name)
	}
	return nil
}

// startSyncAll starts a run over companies and returns the first
// company's Cmd.
func (m *Syncing) startSyncAll(companies []message.Company) tea.Cmd {
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
	return syncAllStep(m.syncer, message.Source{Instance: m.instance}, run.runID, company)
}

// synced is a company's sync reporting back, a refresh's or a run's.
func (m *Syncing) synced(e message.Event[message.Ran[message.SyncReport]]) tea.Cmd {
	if e.Payload.Value.RunID == 0 {
		return m.refreshSynced(e)
	}
	return m.syncAllStep(e.Payload.Value)
}

// syncAllStep records one company's report, then issues the next company
// or finishes the run.
func (m *Syncing) syncAllStep(report message.SyncReport) tea.Cmd {
	run := &m.run
	if !run.running || report.RunID != run.runID {
		return nil
	}
	switch name := run.companies[run.next].Name; report.Code {
	case 0:
	case message.ErrSyncInProgress:
		run.skipped = append(run.skipped, name)
	default:
		run.failed = append(run.failed, name)
	}
	run.next++
	if run.stopping {
		return m.finishSyncAll()
	}
	return m.issueSyncAllStep()
}

// finishSyncAll ends the run, reporting its summary and its totals.
func (m *Syncing) finishSyncAll() tea.Cmd {
	run := &m.run
	run.running = false
	src, report := message.Source{Instance: m.instance}, message.SyncAllReport{Companies: run.next, Failed: run.failed, Skipped: run.skipped}
	summary := sync.Summary{Companies: report.Companies, Failed: report.Failed, Skipped: report.Skipped}.String()
	if run.next < len(run.companies) {
		summary = fmt.Sprintf("Stopped after %d/%d: %s", run.next, len(run.companies), summary)
	}
	return tea.Batch(message.ReportStatus(src, summary), func() tea.Msg {
		return message.New(src, message.Subject{}, message.OpNone, time.Now().UTC(), message.Ran[message.SyncAllReport]{Value: report})
	})
}

func refreshCompany(syncer *sync.Syncer, src message.Source, company message.Company) tea.Cmd {
	return func() tea.Msg {
		subject := message.Subject{Type: message.TypeCompany, ID: company.ID}
		result, err := syncer.SyncCompany(context.Background(), company.ID)
		if err != nil {
			return message.New(src, subject, message.OpNone, time.Now().UTC(), ErrorOf(err))
		}
		return message.New(src, subject, message.OpNone, time.Now().UTC(), message.Ran[message.SyncReport]{Value: reportOf(0, *result)})
	}
}

func (m *Syncing) startRefresh(e message.Event[message.Run[message.SyncCompany]]) tea.Cmd {
	return m.refresh(e.Payload.Value.Company)
}

// refresh re-syncs company with the spinner turning until it reports, or
// refuses while a run may be syncing it right now (#150). The refresh goes
// first in the batch so a fast one has already finished when the
// spinner's first tick arrives, which then stops it.
func (m *Syncing) refresh(company message.Company) tea.Cmd {
	if m.run.running {
		m.line = "Sync all in progress: refresh unavailable"
		return nil
	}
	r := m.refreshes[company.ID]
	r.name = company.Name
	r.inFlight++
	m.refreshes[company.ID] = r
	m.refreshingName = company.Name
	return tea.Batch(refreshCompany(m.syncer, message.Source{Instance: m.instance}, company), m.spinner.Tick)
}

// refreshed is a refresh of companyID coming back: its company's name, or
// false if no refresh of it is in flight.
func (m *Syncing) refreshed(companyID int64) (string, bool) {
	r, ok := m.refreshes[companyID]
	if !ok {
		return "", false
	}
	if r.inFlight--; r.inFlight == 0 {
		delete(m.refreshes, companyID)
	} else {
		m.refreshes[companyID] = r
	}
	return r.name, true
}

func (m *Syncing) refreshSynced(e message.Event[message.Ran[message.SyncReport]]) tea.Cmd {
	name, ok := m.refreshed(e.Meta.Subject.ID)
	if !ok {
		return nil
	}
	r := e.Payload.Value
	return message.ReportStatus(message.Source{Instance: m.instance}, fmt.Sprintf("%s: fetched %d, created %d, updated %d, closed %d, reopened %d",
		name, r.Fetched, r.Created, r.Updated, r.Closed, r.Reopened))
}

// refreshFailed leaves a failure to the banner, which shows every Error,
// except for a sync elsewhere, which isn't one.
func (m *Syncing) refreshFailed(e message.Event[message.Error]) tea.Cmd {
	name, ok := m.refreshed(e.Meta.Subject.ID)
	if !ok || e.Payload.Code != message.ErrSyncInProgress {
		return nil
	}
	// Another sync of this company (e.g. a scheduled `swamp fetch`) is
	// running; this one fetched and wrote nothing.
	return message.ReportStatus(message.Source{Instance: m.instance}, name+" is already syncing elsewhere; try again in a moment")
}

func (m *Syncing) tick(msg spinner.TickMsg) tea.Cmd {
	if len(m.refreshes) == 0 {
		return nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return cmd
}
