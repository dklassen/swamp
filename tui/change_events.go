package tui

import (
	"context"
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
	if len(others) == 0 {
		return a.pollChanges()
	}
	a.status = "Updated by " + changedBy(others)
	return tea.Batch(a.reloadFor(others), a.pollChanges())
}

// reloadFor reruns what the current screen shows, if events touch it.
func (a *App) reloadFor(events []store.ChangeEvent) tea.Cmd {
	switch a.screen {
	case screenActiveApplications:
		if touches(events, "applications", "postings", "companies", "document_writes", "document_reviews", "document_exports") {
			return loadActiveApplications(a.store, a.documents)
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
