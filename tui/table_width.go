package tui

const (
	// fallbackTableWidth is how wide a list table is drawn before the
	// terminal reports its size.
	fallbackTableWidth = 100
	// minFlexColWidth keeps a table's flexible column readable on a
	// narrow terminal; past that, the table runs off the right edge.
	minFlexColWidth = 10
)

// tableWidth is how wide every list table is drawn in a terminal width
// columns wide. All of them share it, so they match from screen to
// screen; a cap or a width that depends on the content goes here.
func tableWidth(width int) int {
	if width <= 0 {
		return fallbackTableWidth
	}
	return width
}

// flexColWidth is what's left for a table's one flexible column at
// tableWidth(width), after its fixed columns. lipgloss/table's own
// Width wraps cells rather than cutting them off, and gives extra room
// to every column, so the list tables size their columns themselves.
func flexColWidth(width int, fixed ...int) int {
	cols := len(fixed) + 1
	// A space either side of every cell, and a border left of every
	// column plus the one closing the row.
	rest := tableWidth(width) - 2*cols - (cols + 1)
	for _, w := range fixed {
		rest -= w
	}
	return max(rest, minFlexColWidth)
}
