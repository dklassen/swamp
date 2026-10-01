# RFC 0005: One way to say "this document has no review"

- **Status:** Accepted, revised 2026-10-01. Work in #203 (step 1) and #204 (step 2); see "Work breakdown".
- **Date:** 2026-09-28 (revised 2026-10-01: re-checked against the code after RFC 0004 shipped; `HasReview()` dropped; see "Corrections to the earlier draft")
- **Related:** RFC 0004 (`documents.Current`, shipped); `store/document_review.go`; `cmd/swamp/main.go`; `tui/app.go`

## Summary

"Does this document have a review?" is answered by whether the review map has an entry for the document. `store.LatestDocumentReviews` leaves out a type with no review, and `documents.Current` (RFC 0004) removes one whose review is stale. Every reader in the TUI and `stage` checks the map this way, except `swamp export`. It looks the review up without checking, gets a zero `DocumentReview` back, and recognises that by `CreatedAt.IsZero()`.

Separately, the TUI maps a review outcome to its style twice, in `reviewBadge` and `reviewGlyph`.

**Recommendation:** make the CLI check the map like everything else, and write down in the doc comments that a missing entry is the only "no review" signal. Optionally, give the TUI one outcome-to-style mapping. No new method on `store`.

## Problem

1. **The CLI infers absence from a timestamp.** `swamp export` calls `reviewSummary(reviews[documentType])` (`cmd/swamp/main.go:372`), and `reviewSummary` (`:399`) returns "not yet reviewed" when `review.CreatedAt.IsZero()`. That's correct only because the zero value happens to have a zero timestamp. It's the one place that decides presence from a review's fields instead of the map.

2. **The convention isn't written down.** A zero `DocumentReview` has `Outcome == ReviewOutcomePassed` (`iota` starts there). So a new reader that skips the map check would show an unreviewed document as passed. Every current reader gets this right, including the three added since this RFC was drafted (`tui/application_submit.go:102`, `tui/active_applications.go:184` and `:189`), but only `LatestDocumentReviews`' comment mentions that absent types are left out, and nothing says the map is the only signal.

3. **Outcome-to-style is in two places.** `reviewBadge` (`tui/app.go:214`) and `reviewGlyph` (`tui/app.go:253`) each `switch` on the outcome to choose `passStyle`, `errStyle` or `dimStyle`. Only the text differs (`[PASSED]` vs `✓`). A new outcome would need adding to both. This is small: two short switches.

## How it works today

```go
// cmd/swamp/main.go:372 and :399
fmt.Printf("%s: exported to %s (%s)\n", documentType, outPath, reviewSummary(reviews[documentType]))

func reviewSummary(review store.DocumentReview) string {
	if review.CreatedAt.IsZero() {
		return "not yet reviewed"
	}
	...
}
```

```go
// tui/application_detail.go:122 -- how every other reader does it
review, hasReview := m.application.LatestReviews[documentType]
b.WriteString(documentStatusLine(documentTitle(documentType), doc.Exists, doc.Path, review, hasReview))
```

`store.LatestDocumentReview` itself returns `(review, ok, err)`. The store already reports presence separately from the review, and the map carries that through.

## Options

### Option 1 (recommended): the map is the signal; the CLI follows it

1. **CLI:** `review, ok := reviews[documentType]`, then `reviewSummary(review, ok)`, the same shape as the TUI's `reviewBadge(review, hasReview)`. Drop `CreatedAt.IsZero()`.
2. **Doc comments:** on `store.DocumentReview`, `LatestDocumentReviews` and `documents.Current`, state that a missing entry is the only "no review" signal, and that a zero `DocumentReview` reads as passed, so presence is never inferred from its fields.
3. **TUI (optional):** one unexported helper mapping an outcome to its style, used by `reviewBadge` and `reviewGlyph`. Each keeps its own text and "no review" rendering (`[not reviewed]` and `-`).

- **Pro:** the CLI change is one call site. It matches what every other reader already does and what the store returns.
- **Con:** the convention stays a convention, enforced by comments and review rather than the type system.

### Option 2: `DocumentReview.HasReview()`, true when `ID != 0` (the earlier draft's recommendation)

- **Con:** it moves the truth from the map to the review's contents. A review in the map with no ID would read as "no review". 20 test fixtures build reviews that way (e.g. `{Outcome: store.ReviewOutcomeFlagged}` in `tui/active_applications_test.go:57` and `:223`), so the TUI's tests would fail or need IDs invented for them.
- **Con:** it gives two answers to one question (the map, and the method) that can disagree. The earlier draft's own open question 2 noted the map check *is* the existence test.
- Rejected.

### Option 3: a presence-carrying type (e.g. `map[Type]*DocumentReview`, or an `Optional` wrapper)

- **Pro:** the type system would stop a reader skipping the check.
- **Con:** changes every reader and `stage`'s JSON output, for a risk no current reader has. AGENTS.md also prefers a plain value over a pointer unless the distinction is needed.
- Rejected for now. Revisit if a reader gets it wrong.

### Option 4: status quo

The CLI keeps working while `store` fills the map the way it does. Rejected, because step 1 is a few lines and removes the one exception.

## Recommendation

Option 1. Step 1 is the substance. Step 2 is a small cleanup, worth doing only if the helper reads better than the two switches.

## Work breakdown

Every issue carries the `rfc-0005` label and an `rfc0005-step-N` label.

| Step | Issue | Work | Size |
|---|---|---|---|
| 1 | #203 | CLI checks the map (`reviewSummary(review, ok)`), table test including "no review with a non-zero `CreatedAt`"; doc comments on `DocumentReview`, `LatestDocumentReviews`, `documents.Current` | S |
| 2 | #204 | TUI: one outcome-to-style helper for `reviewBadge` and `reviewGlyph`; output unchanged | S |

The steps are independent.

## Corrections to the earlier draft

| Earlier draft said | Actually (2026-10-01) |
|---|---|
| Add `DocumentReview.HasReview()` (`ID != 0`) and derive the TUI's `hasReview` from it | Dropped (Option 2). The map check is correct and is what the store returns. |
| Absence is answered "three different ways in three different places" | One way (the map) everywhere except the CLI. `stage` and the TUI agree; RFC 0004's `documents.Current` also signals stale reviews by removing them. |
| The absent renders differ as "–" vs "-" | `[not reviewed]` (`reviewBadge`) and `-` (`reviewGlyph`). |
| Line numbers: `main.go:390`, `app.go:200`/`:216`/`:242`/`:1511`, `application_detail.go:115-118`, `app.go:447`/`:613`, `stage.go:114` | `main.go:399` (call at `:372`), `app.go:198`/`:214`/`:253`/`:1569`, `application_detail.go:122`; `app.go:464`/`:637` are now `documents.Current` calls; `stage.go:150`. |
| Don't combine with RFC 0004's change | RFC 0004 has shipped. |

## Open questions

All resolved on 2026-10-01:

1. **`ID != 0` or `CreatedAt.IsZero()` as the predicate?** Neither; there's no predicate on the review (Option 2).
2. **Should presence live on the map rather than the review?** Yes: the map is the signal.
3. **Is a flagged review's rendering tested?** Yes: ✗ in `TestActiveApplicationListModel_View_ShowsReviewGlyphsPerApplication` (`tui/active_applications_test.go:52`), and `[FLAGGED]` in `tui/app_test.go:1735`.

## Out of scope

- `stage.needsRework` reads outcomes from the filtered map. It already treats absence correctly; leave it.
- `ApplicationView`'s review fields (`store/application_view.go`): the map is already the signal there.
- New `ReviewOutcome` values. Step 2 makes one cheaper to add in the TUI, but adding one is a feature.
