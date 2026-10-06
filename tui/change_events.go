package tui

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dklassen/swamp/store"
)

// changePollInterval is how often the TUI reads the change log. Nothing is
// lost between reads, so this only decides how soon a change appears.
const changePollInterval = 500 * time.Millisecond

type changeTickMsg struct{}

type changesMsg struct {
	events []store.ChangeEvent
	err    error
}

// WithChangeFeed makes the App show changes other processes make, read
// from feed. origin is this process's own, whose events it skips.
func (a *App) WithChangeFeed(feed *store.ChangeFeed, origin string) *App {
	a.changeFeed = feed
	a.origin = origin
	return a
}

// pollChanges schedules the next read of the change log; nil without a
// feed (as in tests, which send changesMsg themselves).
func (a *App) pollChanges() tea.Cmd {
	if a.changeFeed == nil {
		return nil
	}
	return tea.Tick(changePollInterval, func(time.Time) tea.Msg { return changeTickMsg{} })
}

func readChanges(feed *store.ChangeFeed) tea.Cmd {
	return func() tea.Msg {
		events, err := feed.Next(context.Background())
		return changesMsg{events: events, err: err}
	}
}

// handleChanges reloads what other processes' events touch and says who
// made them. It always schedules the next read.
func (a *App) handleChanges(msg changesMsg) tea.Cmd {
	if msg.err != nil {
		a.err = msg.err
		return a.pollChanges()
	}
	others := slices.DeleteFunc(slices.Clone(msg.events), func(e store.ChangeEvent) bool { return e.Origin == a.origin })
	if len(others) > 0 {
		a.status = "Updated by " + changedBy(others)
	}
	// A form in progress is never rebuilt: hold the events until the
	// screen is one that reloads, on a later tick.
	a.heldChanges = append(a.heldChanges, others...)
	if len(a.heldChanges) == 0 || !reloads(a.screen) {
		return a.pollChanges()
	}
	events := a.heldChanges
	a.heldChanges = nil
	return tea.Batch(a.reloadFor(events), a.pollChanges())
}

// reloads is whether reloadFor handles screen; on any other, events wait.
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
			return (e.Table == "postings" && e.RowID == d.posting.ID) || (d.hasApplication && eventApplicationID(e) == d.application.ID)
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

// changedBy names the processes behind events, for the status line.
func changedBy(events []store.ChangeEvent) string {
	var names []string
	for _, e := range events {
		if name := writerName(e.Origin); !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return strings.Join(names, " and ")
}

func writerName(origin string) string {
	kind, _, _ := strings.Cut(origin, ":")
	switch kind {
	case "":
		return "a change outside Swamp"
	case "mcp":
		return "the agent"
	case "tui":
		return "another window"
	default:
		return "swamp " + kind
	}
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
