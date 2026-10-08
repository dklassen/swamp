package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"
)

func TestOverlay_CentresTheBoxAndKeepsTheRest(t *testing.T) {
	t.Parallel()

	bg := strings.Join([]string{
		"0123456789",
		"abcdefghij",
		"ABCDEFGHIJ",
		"klmnopqrst",
		"KLMNOPQRST",
	}, "\n")
	box := "####\n#  #"

	want := strings.Join([]string{
		"0123456789",
		"abc####hij",
		"ABC#  #HIJ",
		"klmnopqrst",
		"KLMNOPQRST",
	}, "\n")
	if diff := cmp.Diff(want, ansi.Strip(overlay(bg, box, 10))); diff != "" {
		t.Errorf("overlay (-want +got):\n%s", diff)
	}
}

// TestOverlay_PadsShortLines: the box mustn't slide left on a row that ends
// before it, such as the blank rows under a short list.
func TestOverlay_PadsShortLines(t *testing.T) {
	t.Parallel()

	bg := strings.Join([]string{"0123456789", "ab", "", "klmnopqrst"}, "\n")

	want := strings.Join([]string{"0123456789", "ab ####", "   #  #", "klmnopqrst"}, "\n")
	if diff := cmp.Diff(want, ansi.Strip(overlay(bg, "####\n#  #", 10))); diff != "" {
		t.Errorf("overlay (-want +got):\n%s", diff)
	}
}

// TestOverlay_StylesDontBleed: the box is cut into styled rows, such as
// the highlighted cursor row; its text mustn't take the row's style, and
// the row's style must pick up again after it.
func TestOverlay_StylesDontBleed(t *testing.T) {
	t.Parallel()

	const red = "\x1b[31m"
	bg := strings.Join([]string{"0123456789", red + "abcdefghij\x1b[0m", "klmnopqrst"}, "\n")

	row := strings.Split(overlay(bg, "####", 10), "\n")[1]

	if got := ansi.Strip(row); got != "abc####hij" {
		t.Fatalf("row = %q, want %q", got, "abc####hij")
	}
	at := strings.Index(row, "####")
	sgr := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	before := sgr.FindAllString(row[:at], -1)
	if len(before) == 0 || (before[len(before)-1] != "\x1b[m" && before[len(before)-1] != "\x1b[0m") {
		t.Errorf("the box's text follows styles %q, want a reset last: the row's red bleeds into it\nrow %q", before, row)
	}
	after := row[at+len("####"):]
	if i := strings.Index(after, red); i == -1 || i > strings.Index(after, "hij") {
		t.Errorf("the row after the box is %q, want it red again before hij", after)
	}
}

// TestOverlay_CentresOnTheScreenNotTheWidestLine: a help line can run past
// the terminal's width (it wraps), and mustn't push the box off-centre.
func TestOverlay_CentresOnTheScreenNotTheWidestLine(t *testing.T) {
	t.Parallel()

	bg := strings.Join([]string{"0123456789", "abcdefghij", "a help line far wider than the screen"}, "\n")

	got := strings.Split(ansi.Strip(overlay(bg, "####", 10)), "\n")[1]
	if got != "abc####hij" {
		t.Errorf("row = %q, want the box centred on the 10-column screen: %q", got, "abc####hij")
	}
}
