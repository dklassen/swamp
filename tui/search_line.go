package tui

import (
	"fmt"

	"github.com/dklassen/swamp/tui/model"
)

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
