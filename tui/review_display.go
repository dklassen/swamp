package tui

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"

	"github.com/dklassen/swamp/internal/domain/document"
	"github.com/dklassen/swamp/tui/model"
)

// outcomeDisplay is how each review outcome looks: its style, shared by
// reviewBadge and reviewGlyphSummary, and its glyph. An outcome missing here
// renders dim, as "[<outcome>]" and "?"; a test checks every
// document.Outcomes() has an entry.
var outcomeDisplay = map[document.Outcome]struct {
	style lipgloss.Style
	glyph string
}{
	document.Passed:  {model.PassStyle, "✓"},
	document.Flagged: {model.ErrStyle, "✗"},
}

// reviewBadge renders a document's latest review outcome as a short
// styled tag: dim "[not reviewed]" when hasReview is false (no review
// recorded yet), otherwise the outcome's name in capitals, styled per
// outcomeDisplay: green "[PASSED]", red "[FLAGGED]".
func reviewBadge(review document.Review, hasReview bool) string {
	if !hasReview {
		return model.DimStyle.Render("[not reviewed]")
	}
	display, ok := outcomeDisplay[review.Outcome]
	if !ok {
		return model.DimStyle.Render("[" + review.Outcome.String() + "]")
	}
	return display.style.Render("[" + strings.ToUpper(review.Outcome.String()) + "]")
}

// reviewGlyphSummary renders a compact, single-line summary of an
// application's latest cover-letter/resume review outcomes, for list
// rows (e.g. the active-applications table) that don't have room for
// reviewBadge's full "[FLAGGED]"/notes rendering (#83).
// Each document is abbreviated to its label's initials ("CL", "R"), then
// its outcome's glyph: a dim "-" with no review recorded yet.
func reviewGlyphSummary(reviews map[document.Type]document.Review) string {
	entries := make([]string, 0, len(document.Types()))
	for _, documentType := range document.Types() {
		var initials []rune
		for _, word := range strings.Fields(documentType.Label()) {
			initials = append(initials, unicode.ToUpper([]rune(word)[0]))
		}
		glyph := model.DimStyle.Render("-")
		if review, ok := reviews[documentType]; ok {
			glyph = model.DimStyle.Render("?")
			if display, ok := outcomeDisplay[review.Outcome]; ok {
				glyph = display.style.Render(display.glyph)
			}
		}
		entries = append(entries, string(initials)+":"+glyph)
	}
	return strings.Join(entries, " ")
}
