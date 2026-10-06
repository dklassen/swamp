package tui

import (
	"strings"
	"testing"

	"github.com/dklassen/swamp/jobboard"
)

// newPostingRefreshTestApp is on Acme's posting list, with board as Acme's
// job board: add to board["acme"] for the next refresh to find.
func newPostingRefreshTestApp(t *testing.T) (*App, map[string][]jobboard.Posting) {
	t.Helper()
	s := newTestStore(t)
	mustCreateCompany(t, s, "Acme", "ashby", "acme")
	board := map[string][]jobboard.Posting{"acme": {
		{SourceID: "job-1", Title: "Engineer"},
		{SourceID: "job-2", Title: "Designer"},
	}}
	app := newTestApp(t, s, newTestSyncer(s, board))
	return clearBanner(openPostingList(t, app)), board
}

func TestApp_PostingList_R_RefreshesTheCompany(t *testing.T) {
	t.Parallel()

	app, board := newPostingRefreshTestApp(t)
	board["acme"] = append(board["acme"], jobboard.Posting{SourceID: "job-3", Title: "Analyst"})

	app = sendKeyAndApply(t, app, runeKey('r'))
	if app.screen != screenPostingList {
		t.Fatalf("screen after r = %v, want the posting list", app.screen)
	}
	var titles []string
	for _, p := range app.postings {
		titles = append(titles, p.Title)
	}
	if len(titles) != 3 || !strings.Contains(strings.Join(titles, ","), "Analyst") {
		t.Errorf("postings after r = %v, want the new Analyst posting too", titles)
	}
	if !strings.HasPrefix(app.status, "Acme: fetched 3") {
		t.Errorf("status = %q, want the sync's result", app.status)
	}
}

// The cursor stays on the posting it was on, though the refresh adds one
// that sorts ahead of it.
func TestApp_PostingList_R_KeepsTheCursor(t *testing.T) {
	t.Parallel()

	app, board := newPostingRefreshTestApp(t)
	app = sendKeyAndApply(t, app, runeKey('j'))
	before := app.postingList.selected(app.postings)
	board["acme"] = append(board["acme"], jobboard.Posting{SourceID: "job-3", Title: "Analyst"})

	app = sendKeyAndApply(t, app, runeKey('r'))
	if got := app.postingList.selected(app.postings); got != before {
		t.Errorf("posting under the cursor after r = %d, want %d (the one it was on)", got, before)
	}
}
