package tui

import "testing"

func TestVisibleWindow_FitsEverything_ReturnsFullRange(t *testing.T) {
	start, end := visibleWindow(0, 5, 10)
	if start != 0 || end != 5 {
		t.Fatalf("visibleWindow(0, 5, 10) = (%d, %d), want (0, 5)", start, end)
	}
}

func TestVisibleWindow_CursorNearTop_StartsAtZero(t *testing.T) {
	start, end := visibleWindow(1, 100, 10)
	if start != 0 {
		t.Fatalf("start = %d, want 0", start)
	}
	if end-start != 10 {
		t.Fatalf("window size = %d, want 10", end-start)
	}
}

func TestVisibleWindow_CursorInMiddle_CentersCursor(t *testing.T) {
	start, end := visibleWindow(50, 100, 10)
	if start != 45 {
		t.Fatalf("start = %d, want 45 (cursor - rows/2)", start)
	}
	if end != 55 {
		t.Fatalf("end = %d, want 55", end)
	}
}

func TestVisibleWindow_CursorNearBottom_ClampsToEnd(t *testing.T) {
	start, end := visibleWindow(99, 100, 10)
	if end != 100 {
		t.Fatalf("end = %d, want 100 (last item visible)", end)
	}
	if start != 90 {
		t.Fatalf("start = %d, want 90", start)
	}
}

func TestVisibleWindow_ZeroRows_ReturnsFullRange(t *testing.T) {
	// Before the first tea.WindowSizeMsg arrives, height is 0 -- don't
	// hide everything, just show it all rather than an empty window.
	start, end := visibleWindow(5, 20, 0)
	if start != 0 || end != 20 {
		t.Fatalf("visibleWindow(5, 20, 0) = (%d, %d), want (0, 20)", start, end)
	}
}
