---
title: "[App simplification 1] Helpers leave app.go for the files that call them"
labels: [app-simplification]
depends_on: []
---

## Problem

About half of `tui/app.go`'s 1,143 lines aren't `App`. They're rendering, commands and filtering that other screens call, kept in `app.go` as if they were shared. Most have one caller file. Some are wrappers with a single call site that don't earn a name.

## Change

No behaviour change. Move each helper next to its only caller:

| Helper(s) | Only caller |
|---|---|
| `renderFilterOption`, `toSet`, `companyFiltersFrom` | `filter_select.go` |
| `renderCursorLine` | `document_review_select.go` |
| `documentStatusLine` | `application_detail.go` |
| `postingDetailContent`, `sectionHeading`, `detailField`, `detailRow`, `detailValueWidth`, `wrapDetailValue`, `detailDocumentField` | `posting_detail.go` |
| `indexOfCompany`, `sortCompaniesByName` | `company_list.go` |
| `indexOfPosting`, `loadPostings` and its filters | `posting_list.go` (until step 6) |
| `createCompany` | `company_form.go` |
| `deleteCompany` | `company_delete.go` |
| `updateCompanyName` | `company_edit.go` |
| `updateApplicationNotes` | `application_notes.go` |

Helpers with two or more caller files stay shared, but move out of `app.go`: the review display (`reviewBadge`, `reviewGlyph`, `reviewGlyphSummary`, `documentAbbreviation`, `outcomeDisplay`) and the write commands (`markupChanged`, `applicationWritten`, `toggleInterested`, `toggleArchived`, `createApplication`, `updateApplicationStatus`, `currentReviews`).

Fold into their only call site:

- `isTab` and `tabScreen` into `screenOf`.
- `documentAbbreviation` and `reviewGlyph` into `reviewGlyphSummary`.
- `openSnapshots`, `filterOutArchived`, `filterPostingsByCompanyFilters` and `splitCompanyFilters` into `loadPostings`. The last two have their own tests, which are rewritten to go through `loadPostings`. If step 6 is next, leave these for it.

Leave alone:

- `onScreen`: one production caller, but 313 test uses. It's the tests' way to ask which screen is up.
- `newPostingDetail` and `newDocumentReviewForm`: step 4 removes them.
- `postingList`, `postingDetail` and `applicationDetail`: step 3 removes them.

Also:

- `renderCursorLine`'s comment says `applicationStatusModel` uses it. It doesn't, so correct the comment.
- `screenStack.top()` has no production caller, only 9 test uses. Either delete it and rewrite those tests through `App`, or keep it as a test seam. Decide before deleting.
- The package comment says markup editing "is a later increment". It has shipped, so the comment is stale.

## Acceptance criteria

- [ ] `app.go` holds only `App`'s own state, routes, `Update`, `View` and banner.
- [ ] No test changes except those for folded helpers.
- [ ] Verification checklist clean.
