package tui

import "github.com/charmbracelet/x/cellbuf"

// wrapToWidth word-wraps content so no line exceeds width columns,
// breaking a word if it's longer than width on its own (e.g. a long job
// URL). width <= 0 (e.g. before the first tea.WindowSizeMsg arrives)
// returns content unchanged rather than wrapping to a nonsensical width.
//
// Uses cellbuf.Wrap directly rather than lipgloss's Width() styling,
// which also pads short lines to fill the width -- fine for lipgloss's
// box-layout use case, not what a scrollable viewport wants.
func wrapToWidth(content string, width int) string {
	if width <= 0 {
		return content
	}
	return cellbuf.Wrap(content, width, "")
}
