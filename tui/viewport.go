package tui

// visibleWindow returns the [start, end) slice bounds into a list of n
// items so that at most rows are shown at once, keeping cursor inside the
// window (centered when there's room to scroll). If everything already
// fits (n <= rows) or rows is unknown (0, e.g. before the first
// tea.WindowSizeMsg arrives), the full range is returned rather than
// hiding items.
func visibleWindow(cursor, n, rows int) (start, end int) {
	if rows <= 0 || n <= rows {
		return 0, n
	}
	start = cursor - rows/2
	if start < 0 {
		start = 0
	}
	maxStart := n - rows
	if start > maxStart {
		start = maxStart
	}
	return start, start + rows
}
