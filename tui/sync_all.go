package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
	"github.com/dklassen/swamp/sync"
)

// syncAllState is a sync-all run ('R' on the company list, #153): every
// company synced one after another, in the background, while the TUI
// stays usable. App owns it, like the status line it reports to, so the
// run carries on if the user leaves the company list.
//
// The run is a chain of tea.Cmds, one per company: each returns a
// syncAllStepMsg, and handling that issues the next. Bubble Tea already
// runs every Cmd in its own goroutine, so this needs no goroutines,
// channels or cancel functions of its own (RFC 0001, option 2).
type syncAllState struct {
	// runID is bumped on every start, so a late message from an earlier
	// run is recognised and dropped.
	runID int
	// companies is a snapshot of the company list when the run started.
	companies []store.Company
	// next is the index of the company in flight.
	next    int
	results []sync.Result
	// running stays true until the in-flight company reports back, even
	// after a stop, so a quick stop-then-start can't overlap two runs.
	running bool
	// stopping means don't issue the next company once this one reports.
	stopping bool
}

// syncAllStepMsg is one company's result within run runID.
type syncAllStepMsg struct {
	runID  int
	result sync.Result
}

// syncAllStep syncs one company as a Cmd.
func syncAllStep(syncer *sync.Syncer, runID int, company store.Company) tea.Cmd {
	return func() tea.Msg {
		result, err := syncer.SyncCompany(context.Background(), company.ID)
		result.CompanyID = company.ID
		result.Name = company.Name
		result.Err = err
		return syncAllStepMsg{runID: runID, result: result}
	}
}

// startSyncAll starts a run over the current company list and returns the
// first company's Cmd.
func (a *App) startSyncAll() tea.Cmd {
	a.syncAll = syncAllState{
		runID:     a.syncAll.runID + 1,
		companies: append([]store.Company(nil), a.companies...),
		running:   true,
	}
	// An error from an earlier action takes priority over the status line
	// in the banner and would hide the run's progress.
	a.err = nil
	return a.issueSyncAllStep()
}

// issueSyncAllStep reports progress for the company about to be fetched
// and returns its Cmd, or finishes the run when none are left.
func (a *App) issueSyncAllStep() tea.Cmd {
	run := &a.syncAll
	if run.next >= len(run.companies) {
		return a.finishSyncAll()
	}
	company := run.companies[run.next]
	a.status = fmt.Sprintf("Syncing %d/%d: %s…", run.next+1, len(run.companies), company.Name)
	return syncAllStep(a.syncer, run.runID, company)
}

// handleSyncAllStep records one company's result, then issues the next
// company or finishes the run.
func (a *App) handleSyncAllStep(msg syncAllStepMsg) tea.Cmd {
	run := &a.syncAll
	if !run.running || msg.runID != run.runID {
		return nil
	}
	run.results = append(run.results, msg.result)
	run.next++
	if run.stopping {
		return a.finishSyncAll()
	}
	return a.issueSyncAllStep()
}

// finishSyncAll ends the run with its summary, and reloads what it may
// have changed: the company list once (open counts, last fetched), and
// the posting list if the selected company was synced.
func (a *App) finishSyncAll() tea.Cmd {
	run := &a.syncAll
	run.running = false
	summary := sync.Summarize(run.results).String()
	if run.next < len(run.companies) {
		summary = fmt.Sprintf("Stopped after %d/%d: %s", run.next, len(run.companies), summary)
	}
	a.status = summary

	cmds := []tea.Cmd{loadCompanies(a.store)}
	for _, r := range run.results {
		if r.Err == nil && r.CompanyID == a.selectedCompany.ID {
			cmds = append(cmds, loadPostings(a.store, a.selectedCompany.ID, a.hideArchived))
			break
		}
	}
	return tea.Batch(cmds...)
}

// toggleSyncAll is 'R': it starts a run, or stops the one under way. A
// stop lets the company in flight finish -- SyncCompany isn't cancelled
// mid-company -- and 'R' does nothing more until it has reported back,
// so two runs never overlap.
func (a *App) toggleSyncAll() tea.Cmd {
	run := &a.syncAll
	switch {
	case !run.running:
		return a.startSyncAll()
	case !run.stopping:
		run.stopping = true
		a.status = fmt.Sprintf("Stopping after %s…", run.companies[run.next].Name)
	}
	return nil
}
