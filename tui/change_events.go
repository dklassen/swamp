package tui

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/internal/domain/change"
	"github.com/dklassen/swamp/internal/service"
	"github.com/dklassen/swamp/tui/message"
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
	events []change.Event
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

func readChanges(changes *service.ChangeLog, view screen, from int64, tick bool) tea.Cmd {
	return func() tea.Msg {
		events, err := changes.After(context.Background(), from)
		return changesMsg{view: view, from: from, events: events, tick: tick, err: err}
	}
}

// handleTick reads for the view on screen. A form has none: the view
// under it catches up when it's back.
func (a *App) handleTick() tea.Cmd {
	if !reloads(a.currentScreen) {
		return a.pollChanges()
	}
	return readChanges(a.changeLogService, a.currentScreen, a.cursor(a.currentScreen), true)
}

// activate catches the view now on screen up on what changed while
// another screen was up.
func (a *App) activate() tea.Cmd {
	if !a.changeLog || !reloads(a.currentScreen) {
		return nil
	}
	return readChanges(a.changeLogService, a.currentScreen, a.cursor(a.currentScreen), false)
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
	if msg.view != a.currentScreen || msg.from > a.cursor(a.currentScreen) {
		return next
	}
	a.seen[a.currentScreen] = max(a.cursor(a.currentScreen), upTo)
	others := slices.DeleteFunc(slices.Clone(msg.events), func(e change.Event) bool { return e.Origin == a.origin })
	return tea.Batch(a.reloadFor(others), next)
}

// reloads is whether screen is a view that follows the change log.
func reloads(s screen) bool {
	switch s {
	case screenActiveApplications, screenCompanyList, screenAllPostings, screenPostingList, screenPostingDetail, screenApplicationDetail:
		return true
	}
	return false
}

// reloadFor reruns what the current screen shows, if events touch it.
func (a *App) reloadFor(events []change.Event) tea.Cmd {
	switch a.currentScreen {
	case screenActiveApplications:
		if touches(events, "applications", "postings", "companies", "document_writes", "document_reviews", "document_exports") {
			return requestLoad[message.ActiveApplications](message.Source{Instance: a.instance})
		}
	case screenApplicationDetail:
		d := a.applicationDetail()
		if d == nil {
			return nil
		}
		if slices.ContainsFunc(events, func(e change.Event) bool {
			return eventApplicationID(e) == d.detail.Application.ID || (e.Table == "postings" && e.RowID == d.detail.Posting.ID)
		}) {
			return requestLoad[message.ApplicationDetail](message.Source{Instance: a.instance})
		}
	case screenPostingDetail:
		d := a.postingDetail()
		if d == nil {
			return nil
		}
		var cmds []tea.Cmd
		if slices.ContainsFunc(events, func(e change.Event) bool {
			return (e.Table == "postings" && e.RowID == d.posting.ID) || (e.Table == "applications" && eventPostingID(e) == d.posting.ID) || (d.hasApplication() && eventApplicationID(e) == d.application.ID)
		}) {
			cmds = append(cmds, requestLoad[message.PostingApplication](message.Source{Instance: a.instance}))
		}
		if slices.ContainsFunc(events, func(e change.Event) bool {
			return e.Table == "posting_scores" && eventPostingID(e) == d.posting.ID
		}) {
			cmds = append(cmds, requestLoad[message.PostingScores](message.Source{Instance: a.instance}))
		}
		return tea.Batch(cmds...)
	case screenPostingList:
		if touches(events, "postings") {
			return a.reloadPostings()
		}
	case screenAllPostings:
		// companies too: a deleted company's postings leave the list.
		if touches(events, "postings", "posting_markup", "posting_scores", "companies") {
			return requestLoad[message.PostingSearch](message.Source{Instance: a.instance})
		}
	case screenCompanyList:
		// postings too: the list shows each company's open postings.
		if touches(events, "companies", "postings") {
			return a.reloadCompanies()
		}
	}
	return nil
}

func touches(events []change.Event, tables ...string) bool {
	return slices.ContainsFunc(events, func(e change.Event) bool { return slices.Contains(tables, e.Table) })
}

// reloadPostings asks the posting list to read its postings again.
func (a *App) reloadPostings() tea.Cmd {
	return requestLoad[message.PostingList](message.Source{Instance: a.instance})
}

// eventApplicationID is the application an event is about, or 0.
func eventApplicationID(e change.Event) int64 {
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

// eventPostingID is the posting an application or score event is about, or
// 0.
func eventPostingID(e change.Event) int64 {
	if e.Table != "applications" && e.Table != "posting_scores" {
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
