package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// overlay draws box centred over bg (lipgloss v1 can't layer), centred on
// width rather than bg's widest line, since the help line can run past it.
func overlay(bg, box string, width int) string {
	lines := strings.Split(bg, "\n")
	boxLines := strings.Split(box, "\n")
	top := max((len(lines)-len(boxLines))/2, 0)
	left := max((width-lipgloss.Width(box))/2, 0)
	right := left + lipgloss.Width(box)
	for i, b := range boxLines {
		if top+i >= len(lines) {
			break
		}
		line := lines[top+i]
		if w := lipgloss.Width(line); w < left {
			line += strings.Repeat(" ", left-w)
		}
		lines[top+i] = ansi.Truncate(line, left, "") + b + ansi.TruncateLeft(line, right, "")
	}
	return strings.Join(lines, "\n")
}

var confirmBoxStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("214")).
	Padding(0, 1)

// confirmBox frames a destructive action's confirmation, at most width wide.
func confirmBox(content string, width int) string {
	style := confirmBoxStyle
	if w := style.GetHorizontalFrameSize(); width > w && lipgloss.Width(content)+w > width {
		style = style.Width(width - w)
	}
	return style.Render(content)
}
