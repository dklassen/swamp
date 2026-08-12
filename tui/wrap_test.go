package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestWrapToWidth_LongLine_NoLineExceedsWidth(t *testing.T) {
	long := strings.Repeat("word ", 40) // much wider than 20 cols
	wrapped := wrapToWidth(long, 20)

	for _, line := range strings.Split(wrapped, "\n") {
		if w := lipgloss.Width(line); w > 20 {
			t.Fatalf("line %q has width %d, want <= 20", line, w)
		}
	}
}

func TestWrapToWidth_ZeroWidth_ReturnsContentUnchanged(t *testing.T) {
	content := "some content that would normally wrap"
	if got := wrapToWidth(content, 0); got != content {
		t.Fatalf("wrapToWidth(_, 0) = %q, want unchanged %q", got, content)
	}
}

func TestWrapToWidth_ShortLine_Unchanged(t *testing.T) {
	content := "short"
	if got := wrapToWidth(content, 80); got != content {
		t.Fatalf("wrapToWidth(%q, 80) = %q, want unchanged", content, got)
	}
}
