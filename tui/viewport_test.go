package tui

import "testing"

func TestVisibleWindow(t *testing.T) {
	tests := []struct {
		name      string
		cursor    int
		total     int
		rows      int
		wantStart int
		wantEnd   int
	}{
		{
			name:      "fits everything returns full range",
			cursor:    0,
			total:     5,
			rows:      10,
			wantStart: 0,
			wantEnd:   5,
		},
		{
			name:      "cursor near top starts at zero",
			cursor:    1,
			total:     100,
			rows:      10,
			wantStart: 0,
			wantEnd:   10,
		},
		{
			name:      "cursor in middle centers cursor",
			cursor:    50,
			total:     100,
			rows:      10,
			wantStart: 45,
			wantEnd:   55,
		},
		{
			name:      "cursor near bottom clamps to end",
			cursor:    99,
			total:     100,
			rows:      10,
			wantStart: 90,
			wantEnd:   100,
		},
		{
			// Before the first tea.WindowSizeMsg arrives, height is 0 --
			// don't hide everything, just show it all rather than an
			// empty window.
			name:      "zero rows returns full range",
			cursor:    5,
			total:     20,
			rows:      0,
			wantStart: 0,
			wantEnd:   20,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := visibleWindow(tt.cursor, tt.total, tt.rows)
			if start != tt.wantStart || end != tt.wantEnd {
				t.Fatalf("visibleWindow(%d, %d, %d) = (%d, %d), want (%d, %d)", tt.cursor, tt.total, tt.rows, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}
