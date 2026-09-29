# RFC 0005: Making "does this document have a review" a property of `store.DocumentReview`

- **Status:** Draft, for discussion
- **Date:** 2026-09-28
- **Related:** RFC 0004 (documents type dispatch), `tui/app.go`, `cmd/swamp/main.go`, `store/document_review.go`

## Summary

"Does this document have a (current) review at all?" is answered three different ways in three different places, and one of them works only by accident. The TUI plumbs a separate `hasReview bool` through four functions; the CLI (`swamp export`) decides absence with `review.CreatedAt.IsZero()` (`cmd/swamp/main.go:390`) — which is only correct because `store.LatestDocumentReviews` happens to omit absent types from its map, an implementation detail the CLI is not allowed to know. And in the TUI, the outcome→visual mapping (`passed`→✓ green, `flagged`→✗ red) is written twice, 26 lines apart (`tui/app.go:216` `reviewBadge`, `tui/app.go:242` `reviewGlyph`).

**Main finding:** the "absence" representation is the zero-value `DocumentReview` (empty `ID`, empty `CreatedAt`, `Outcome` at its zero constant). Nothing names that. Every reader of the code has to know it.

**Recommendation:** add `store.DocumentReview.HasReview() bool` (true when the row actually exists — `ID != 0`, which for a persisted SQLite row is always true, so this is a pure "am I the zero value?" predicate), use it as the single absence check, and extract one `outcomeStyle()` helper in the TUI so the outcome→visual mapping exists in exactly one place. No new packages, no boundary crossings, no `store`-side changes beyond one method.

## Problem

1. **Absence has no name.** A missing-from-the-map document yields Go's zero-value `store.DocumentReview`:

   ```go
   // store/document_review.go
   type DocumentReview struct {
       ID              int64
       ...
       Outcome         ReviewOutcome   // zero value == ReviewOutcomePassed (iota start)
       ...
       CreatedAt       time.Time
   }
   ```

   Callers distinguish "real review" from "no review" with ad-hoc signals:
   - TUI: `review, hasReview := latestReviews[documentType]` (comma-ok) — threaded as a `hasReview bool` parameter through `documentStatusLine` (`tui/app.go:200`), `reviewBadge` (:216), `reviewGlyph` (:242), and `detailDocumentField` (:1511), with the bool re-derived at `application_detail.go:115-118` and `app.go:447`/:613.
   - CLI: `if review.CreatedAt.IsZero() { return "not yet reviewed" }` (`cmd/swamp/main.go:390`). Works, but only because `LatestDocumentReviews` omits absent keys — the CLI has no other way of knowing that, and if `store` ever started returning zero-valued entries in the map (a perfectly reasonable future choice), `reviewSummary` would silently report "not yet reviewed" while the rest of the codebase reports the review's real outcome.
   - `stage`: `needsRework` (`stage/stage.go:114`) reads `r.Outcome == store.ReviewOutcomeFlagged` over the *filtered* map only, so it gets absence "for free" from the filtering — a third, implicit coupling to "map key present ⇔ review exists".

2. **Zero-value ambiguity is load-bearing and invisible.** Because `ReviewOutcomePassed` is `iota` start, `DocumentReview{}.Outcome == ReviewOutcomePassed`. Any future renderer or exporter that reads `Outcome` without first checking existence will display an unreviewed document as *passed*. The existing three sites each defend against this independently; nothing in the type system helps them.

3. **Outcome→visual is duplicated in the TUI.** `reviewBadge` (`tui/app.go:216`) and `reviewGlyph` (:242) each `switch review.Outcome` to pick a style/glyph for the same two outcomes. One is a styled word, one a styled glyph — the *presentation details* differ; the *outcome classification* (what does this outcome *mean* for display: pass/flag/unknown) is identical.

## How it works today

```go
// tui/app.go:242 -- one of two copies
func reviewGlyph(review store.DocumentReview, hasReview bool) string {
    if !hasReview {
        return dimStyle.Render("-")
    }
    switch review.Outcome {
    case store.ReviewOutcomePassed:
        return passStyle.Render("✓")
    case store.ReviewOutcomeFlagged:
        return errStyle.Render("✗")
    default:
        return dimStyle.Render("?")
    }
}
```

```go
// cmd/swamp/main.go:389 -- absence check via a timestamp that the type only
// *happens* to have zero when absent
func reviewSummary(review store.DocumentReview) string {
    if review.CreatedAt.IsZero() {
        return "not yet reviewed"
    }
    if review.Outcome == store.ReviewOutcomeFlagged {
        return "flagged: " + review.Notes
    }
    return "passed"
}
```

## Options

### Option 1 (recommended): name the predicate, shrink the duplication

1. `store`: add `func (r DocumentReview) HasReview() bool { return r.ID != 0 }` — "is this a persisted row, or the zero-value placeholder for an absent type?" (SQLite `INTEGER PRIMARY KEY` is always ≥ 1 for a real row; zero ⇔ not from the DB. This is an invariant of persistence, documented on the method.)
2. TUI: keep the `hasReview bool` parameters (they're fine — that's idiomatic "we know at the call site"), but derive them *as* `review.HasReview()` so the derivation is one named thing instead of three comma-ok sites.
3. TUI: one unexported helper, e.g. `func outcomeStyle(review store.DocumentReview) (lipgloss.Style, string)` or a small `reviewDisplay(review) (word, glyph string, style lipgloss.Style)` consulted by both `reviewBadge` and `reviewGlyph`. The `!HasReview()` early-return stays in each (their absent-renders differ: "–" vs "-"), but the pass/flag/unknown classification no longer exists twice.
4. CLI: `reviewSummary` switches `CreatedAt.IsZero()` → `!review.HasReview()`. Same output, no longer correct-by-accident.

- **Pro:** smallest possible diff; no boundary crossings; names the implicit invariant; removes the `CreatedAt`-as-sentinel footgun; both TUI outcome switches collapse to one.
- **Con:** `HasReview()` on a *store* type is a domain-level word for a persistence-level fact. (It's fine — `store` already owns "is this review current" via `IsCurrent`, so "is this review *present*" is adjacent territory owned for the same reason.)

### Option 2: push "outcome meaning" into `store` as well (`ReviewOutcome.Present()`, `.Label()`, …)

Also add `ReviewOutcome` accessors for display (label, severity).

- **Pro:** TUI and CLI both derive display from `store`.
- **Con:** `store` becomes the home of presentation policy (colors, glyphs, "✓"/"✗" are *visual* choices, not domain facts). That inverts the dependency — display rules leaking into the data layer. Rejected for this repo's split, where `store` is deliberately presentation-free.

### Option 3: status quo

Nothing changes; the three absence checks and two outcome switches stay. Defensible today; the `CreatedAt.IsZero()` site is a live footgun the moment `store` changes how it fills its map.

## Recommendation

Option 1. It's the only option that names the absence signal without moving presentation into `store`, and its diff is a few methods and two call-site rewrites. Do *not* combine with RFC 0004's `documents` work in one change — they touch the same files (`tui/app.go`, `stage/stage.go`, `cmd/swamp/main.go`) but are independently reviewable, and separating them keeps each reviewable against the other's absence.

## Work breakdown (proposed issue sequence)

1. **Issue: `store`: add `DocumentReview.HasReview()` (zero-value predicate).** Self-contained: new method + a test asserting zero-value ⇒ false, and a round-tripped row ⇒ true. No call-site changes yet. *(Independent; unblocks 2 and 3.)*
2. **Issue: TUI: single outcome→display helper.** Extract `outcomeStyle`/consolidate `reviewBadge` + `reviewGlyph`; switch `hasReview` derivations to `review.HasReview()`; update existing badge/glyph tests (they already pin the exact strings, so this is behavior-safe).
3. **Issue: CLI: `reviewSummary` uses `HasReview()` instead of `CreatedAt.IsZero()`.** One hunk; add a test with a zero-value `DocumentReview` asserting "not yet reviewed", and one with a flagged review asserting the notes surface.
4. *(Optional, after 1–3)* Document on `store.DocumentReview` — in the struct doc comment — that **the zero value is the canonical "no review" placeholder** and that `HasReview()` is the only supported test for it. This turns today's implicit convention into a stated one, so issue 1's method has a contract to document.

## Open questions

1. **`ID != 0` vs `CreatedAt.IsZero()` as the predicate body.** `ID` is "persisted", `CreatedAt` is "timestamped". For SQLite autoincrement both are equivalent today. Recommend `ID` (it's the primary key; "has an ID" is the most direct "does this row exist" statement). *Flagging rather than silent.*
2. **Should `HasReview()` exist on the *map* instead** (i.e., a `store` helper `HasReview(reviews map[DocumentType]DocumentReview, t DocumentType) bool`)? Arguably no — comma-ok on the map *is* the existence test, and the zero value is what leaks into per-review methods. The per-review method exists because `reviewBadge`/`reviewGlyph`/`reviewSummary` all take a `DocumentReview` that *may be* the zero value. (If the TUI ever changes to pass "known types" rather than map lookups, this method retires with it.)
3. **Test coverage:** do we have a test that a *flagged* review renders "✗"? (I verified `reviewGlyph`/`reviewBadge` have test-pinned strings in `tui/app_test.go` — the summary confirms badge tests exist; a flagged-review test should be added or confirmed in issue 2.)

## Out of scope

- `stage.needsRework` — it consumes *filtered* maps and is a functional (not presentational) use of outcome; leave it.
- `ApplicationView`'s pre-computed review fields (`store/application_view.go`) — a separate projection concern; the absence signal there is already the map's.
- Adding new `ReviewOutcome` values (e.g. "needs_changes") — that's a feature, not a refactor; the `HasReview()`/`outcomeStyle` split makes it cheaper to do safely when it happens, but it's not part of this.
