package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/dklassen/swamp/store"
)

// TestApp_AnotherProcessesChange_ReloadsTheHomeListAndSaysWho: the agent
// starting an application (stage_prepare) shows up without a restart.
func TestApp_AnotherProcessesChange_ReloadsTheHomeListAndSaysWho(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeFeed(nil, "tui:1")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Staff Engineer")
	application, err := s.CreateApplication(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}
	if strings.Contains(app.View(), "Staff Engineer") {
		t.Fatal("the home list shows the application before any reload; the test can't tell a reload happened")
	}

	app = sendKeyAndApply(t, app, changesMsg{events: []store.ChangeEvent{{Table: "applications", RowID: application.ID, Op: "insert", Origin: "mcp:7"}}})

	view := app.View()
	for _, want := range []string{"Staff Engineer", "Updated by the agent"} {
		if !strings.Contains(view, want) {
			t.Errorf("home list after the agent's change doesn't contain %q:\n%s", want, view)
		}
	}
}

// TestApp_ItsOwnChange_IsSkipped: the TUI reloads after its own actions
// already; announcing them as updates would be false.
func TestApp_ItsOwnChange_IsSkipped(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeFeed(nil, "tui:1")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Staff Engineer")
	application, err := s.CreateApplication(context.Background(), posting.ID)
	if err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}

	app = sendKeyAndApply(t, app, changesMsg{events: []store.ChangeEvent{{Table: "applications", RowID: application.ID, Op: "insert", Origin: "tui:1"}}})

	if view := app.View(); strings.Contains(view, "Staff Engineer") || strings.Contains(view, "Updated by") {
		t.Errorf("the TUI's own change was treated as another process's:\n%s", view)
	}
}

// TestApp_ATickReadsTheFeed: each tick reads what's new in the change log;
// the reply schedules the next tick.
func TestApp_ATickReadsTheFeed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	acme := mustCreateCompany(t, s, "Acme", "ashby", "acme")
	posting := mustUpsertPosting(t, s, acme.ID, "job-1", "Staff Engineer")
	feed, err := s.NewChangeFeed(ctx)
	if err != nil {
		t.Fatalf("NewChangeFeed: %v", err)
	}
	app := newTestApp(t, s, newTestSyncer(s, nil)).WithChangeFeed(feed, "tui:1")
	if _, err := s.CreateApplication(ctx, posting.ID); err != nil {
		t.Fatalf("CreateApplication: %v", err)
	}

	app, read := sendKey(app, changeTickMsg{})
	if read == nil {
		t.Fatal("a tick returned no command, want one reading the feed")
	}
	got, ok := read().(changesMsg)
	if !ok || got.err != nil || len(got.events) != 1 || got.events[0].Table != "applications" {
		t.Fatalf("reading the feed = %+v, want one applications event", got)
	}
	if _, next := sendKey(app, got); next == nil {
		t.Error("handling the events scheduled nothing, want the next tick")
	}
}
