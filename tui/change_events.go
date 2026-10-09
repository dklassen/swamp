package tui

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
)

// changePollInterval is how often the TUI reads the change log. Nothing is
// lost between reads, so this only decides how soon a change appears.
const changePollInterval = 500 * time.Millisecond

type changeTickMsg struct{}

// changesMsg is the events after from, read for view. tick is whether a
// tick asked, and so whether to schedule the next.
type changesMsg struct {
	view   screen
	from   int64
	events []store.ChangeEvent
	tick   bool
	err    error
}

// WithChangeLog makes the App show changes other processes make, read
// from the change log after since. origin is this process's own, whose
// events it skips.
func (a *App) WithChangeLog(since int64, origin string) *App {
	a.changeLog = true
	a.origin = origin
	a.since = since
	a.latestSeen = since
	return a
}

// pollChanges schedules the next tick; nil without a change log.
func (a *App) pollChanges() tea.Cmd {
	if !a.changeLog {
		return nil
	}
	return tea.Tick(changePollInterval, func(time.Time) tea.Msg { return changeTickMsg{} })
}

// cursor is view's place in the change log: the newest event it has
// accounted for.
func (a *App) cursor(view screen) int64 {
	if c, ok := a.seen[view]; ok {
		return c
	}
	return a.since
}

func readChanges(s *store.Store, view screen, from int64, tick bool) tea.Cmd {
	return func() tea.Msg {
		events, err := s.ChangeEventsAfter(context.Background(), from)
		return changesMsg{view: view, from: from, events: events, tick: tick, err: err}
	}
}

// handleTick reads for the view on screen. A form has none: the view
// under it catches up when it's back.
func (a *App) handleTick() tea.Cmd {
	if !reloads(a.screen) {
		return a.pollChanges()
	}
	return readChanges(a.store, a.screen, a.cursor(a.screen), true)
}

// activate catches the view now on screen up on what changed while
// another screen was up.
func (a *App) activate() tea.Cmd {
	if !a.changeLog || !reloads(a.screen) {
		return nil
	}
	if a.screen == screenApplicationDetail {
		// Its documents are files, which can change with no event.
		a.seen[a.screen] = a.latestSeen
		id := a.applicationDetail.application.ID
		return tea.Batch(reloadApplicationDetail(a.store, id), loadDocumentReviews(a.store, a.documents, id))
	}
	return readChanges(a.store, a.screen, a.cursor(a.screen), false)
}

// handleChanges reloads the view on screen if other processes' events
// touch it, and moves its place in the log past them.
func (a *App) handleChanges(msg changesMsg) tea.Cmd {
	var next tea.Cmd
	if msg.tick {
		next = a.pollChanges()
	}
	if msg.err != nil {
		a.err = msg.err
		return next
	}
	upTo := msg.from
	if n := len(msg.events); n > 0 {
		upTo = msg.events[n-1].ID
	}
	a.latestSeen = max(a.latestSeen, upTo)
	// A read for another view, or one starting past this view's place,
	// could lack events it hasn't seen: it keeps its place and reads again.
	if msg.view != a.screen || msg.from > a.cursor(a.screen) {
		return next
	}
	a.seen[a.screen] = max(a.cursor(a.screen), upTo)
	others := slices.DeleteFunc(slices.Clone(msg.events), func(e store.ChangeEvent) bool { return e.Origin == a.origin })
	return tea.Batch(a.reloadFor(others), next)
}

// reloads is whether screen is a view that follows the change log.
func reloads(s screen) bool {
	switch s {
	case screenActiveApplications, screenCompanyList, screenPostingList, screenPostingDetail, screenApplicationDetail:
		return true
	}
	return false
}

// reloadFor reruns what the current screen shows, if events touch it.
func (a *App) reloadFor(events []store.ChangeEvent) tea.Cmd {
	switch a.screen {
	case screenActiveApplications:
		if touches(events, "applications", "postings", "companies", "document_writes", "document_reviews", "document_exports") {
			return loadActiveApplications(a.store, a.documents)
		}
	case screenApplicationDetail:
		view := a.applicationDetail.application
		if slices.ContainsFunc(events, func(e store.ChangeEvent) bool {
			return eventApplicationID(e) == view.ID || (e.Table == "postings" && e.RowID == view.Posting.ID)
		}) {
			return tea.Batch(reloadApplicationDetail(a.store, view.ID), loadDocumentReviews(a.store, a.documents, view.ID))
		}
	case screenPostingDetail:
		d := a.postingDetail
		if slices.ContainsFunc(events, func(e store.ChangeEvent) bool {
			return (e.Table == "postings" && e.RowID == d.posting.ID) || eventPostingID(e) == d.posting.ID || (d.hasApplication && eventApplicationID(e) == d.application.ID)
		}) {
			return reloadPosting(a.store, d.posting.ID)
		}
	case screenPostingList:
		if touches(events, "postings") {
			return reloadPostings(a.store, a.selectedCompany.ID, a.hideArchived)
		}
	case screenCompanyList:
		// postings too: the list shows each company's open postings.
		if touches(events, "companies", "postings") {
			return loadCompanies(a.store)
		}
	}
	return nil
}

func touches(events []store.ChangeEvent, tables ...string) bool {
	return slices.ContainsFunc(events, func(e store.ChangeEvent) bool { return slices.Contains(tables, e.Table) })
}

// reloadPostings is loadPostings for a list already on screen.
func reloadPostings(s *store.Store, companyID int64, hideArchived bool) tea.Cmd {
	load := loadPostings(s, companyID, hideArchived)
	return func() tea.Msg {
		msg := load()
		if loaded, ok := msg.(postingsLoadedMsg); ok {
			loaded.keepCursor = true
			return loaded
		}
		return msg
	}
}

// eventApplicationID is the application an event is about, or 0.
func eventApplicationID(e store.ChangeEvent) int64 {
	switch e.Table {
	case "applications":
		return e.RowID
	case "document_writes", "document_reviews", "document_exports":
		var row struct {
			ApplicationID int64 `json:"application_id"`
		}
		values := e.New
		if values == "" {
			values = e.Old
		}
		if json.Unmarshal([]byte(values), &row) == nil {
			return row.ApplicationID
		}
	}
	return 0
}

// eventPostingID is the posting an application event is about, or 0.
func eventPostingID(e store.ChangeEvent) int64 {
	if e.Table != "applications" {
		return 0
	}
	var row struct {
		PostingID int64 `json:"posting_id"`
	}
	values := e.New
	if values == "" {
		values = e.Old
	}
	if json.Unmarshal([]byte(values), &row) == nil {
		return row.PostingID
	}
	return 0
}

type applicationDetailReloadedMsg struct {
	id          int64
	application store.Application
	deleted     bool
	err         error
}

func reloadApplicationDetail(s *store.Store, id int64) tea.Cmd {
	return func() tea.Msg {
		application, err := s.GetApplicationByID(context.Background(), id)
		if errors.Is(err, store.ErrNotFound) {
			return applicationDetailReloadedMsg{id: id, deleted: true}
		}
		return applicationDetailReloadedMsg{id: id, application: application, err: err}
	}
}

// handleApplicationDetailReloaded refreshes the application on screen, or
// leaves it if it was deleted underneath.
func (a *App) handleApplicationDetailReloaded(msg applicationDetailReloadedMsg) tea.Cmd {
	if a.screen != screenApplicationDetail || a.applicationDetail.application.ID != msg.id {
		return nil
	}
	a.err = msg.err
	if msg.deleted {
		a.screen = screenActiveApplications
		a.status = "The application you were viewing was deleted elsewhere"
		return loadActiveApplications(a.store, a.documents)
	}
	if msg.err == nil {
		a.applicationDetail.application.Application = msg.application
	}
	return nil
}

// replacePostingDetail swaps in m, keeping the scroll position when it's
// the same posting: a reload mustn't jump you back to the top.
func (a *App) replacePostingDetail(m postingDetailModel) {
	if m.posting.ID == a.postingDetail.posting.ID {
		m.viewport.SetYOffset(a.postingDetail.viewport.YOffset)
	}
	a.postingDetail = m
}

type postingReloadedMsg struct {
	posting store.Posting
	err     error
}

func reloadPosting(s *store.Store, id int64) tea.Cmd {
	return func() tea.Msg {
		p, err := s.GetPosting(context.Background(), id)
		return postingReloadedMsg{posting: p, err: err}
	}
}

// handlePostingReloaded shows the posting's new content, then reloads its
// application, which rebuilds the screen again with the same scroll.
func (a *App) handlePostingReloaded(msg postingReloadedMsg) tea.Cmd {
	a.err = msg.err
	if msg.err != nil || a.screen != screenPostingDetail || a.postingDetail.posting.ID != msg.posting.ID {
		return nil
	}
	if i := indexOfPosting(a.postings, msg.posting.ID); i >= 0 {
		a.postings[i] = msg.posting
	}
	d := a.postingDetail
	a.replacePostingDetail(newPostingDetailModel(a.store, a.documents, a.width, a.screenRows(), msg.posting, d.application, d.hasApplication, d.latestReviews, a.canNavigateSiblings(msg.posting.ID)))
	return loadApplication(a.store, msg.posting.ID)
}
