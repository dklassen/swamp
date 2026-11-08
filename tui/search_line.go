package tui

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/dklassen/swamp/tui/model"
)

// listSearch is a list's '/' prompt and the cursor it moves. Ephemeral:
// closed and empty each time the app starts.
type listSearch struct {
	// searching is whether the prompt is open and taking keys; query is
	// what's been typed into it.
	searching bool
	query     string
	cursor    int
}

// searchKey handles a key while the prompt is open, matches being how many
// rows the query matches. Every edit to the query re-filters straight
// away and puts the cursor back on the first match. It reports whether
// esc closed the search, whose query the list then clears.
func (s *listSearch) searchKey(msg tea.KeyPressMsg, matches int) (cleared bool) {
	switch {
	default:
		if msg.Text != "" {
			s.query += msg.Text
			s.cursor = 0
		}
	case key.Matches(msg, keys.Backspace):
		if r := []rune(s.query); len(r) > 0 {
			s.query = string(r[:len(r)-1])
			s.cursor = 0
		} else {
			s.searching = false
		}
	case key.Matches(msg, keys.Esc):
		s.searching = false
		return true
	case key.Matches(msg, keys.SearchDown):
		if s.cursor < matches-1 {
			s.cursor++
		}
	case key.Matches(msg, keys.SearchUp):
		if s.cursor > 0 {
			s.cursor--
		}
	case key.Matches(msg, keys.Enter):
		// Keeps the matches, so letters act on them instead of being typed.
		s.searching = false
	}
	return false
}

// searchPaste types pasted text into the prompt, if it's open.
func (s *listSearch) searchPaste(content string) {
	if s.searching {
		s.query += content
		s.cursor = 0
	}
}

// searchLine is the line above a searchable list's table, showing the
// query and how many of total rows match it while the search is open, or
// once its results are kept. It's there when the search is closed too, so
// opening it doesn't move the table.
func searchLine(searching bool, query string, matches, total int) string {
	switch {
	case !searching && query == "":
		return model.DimStyle.Render("🔍 / to search")
	case !searching:
		return "🔍 " + query + "  " + model.DimStyle.Render(fmt.Sprintf("%d of %d", matches, total))
	}
	return "🔍 " + query + "▏  " + model.DimStyle.Render(fmt.Sprintf("%d of %d", matches, total))
}
