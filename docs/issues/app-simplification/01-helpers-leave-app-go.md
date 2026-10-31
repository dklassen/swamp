---
title: "[App simplification 1] Helpers leave app.go for the files that call them"
labels: [app-simplification]
depends_on: []
---

## Problem

About half of `tui/app.go`'s 1,143 lines aren't `App`. They're rendering, commands and filtering that other screens call, kept in `app.go` as if they were shared. Most have one caller file. Some are wrappers with a single call site that don't earn a name.

## Change

No behaviour change.

Fold each helper with one call site into it. A helper's own tests are rewritten to go through what it was folded into.

| Helper                                          | Its one call site is in                                      |
| ----------------------------------------------- | ------------------------------------------------------------ |
| `isTab`, `tabScreen`                            | `screenOf` (`app.go`)                                        |
| `documentAbbreviation`, `reviewGlyph`           | `reviewGlyphSummary`                                         |
| `detailDocumentField`                           | `postingDetailContent`                                       |
| `postingDetailContent`                          | `posting_detail.go`                                          |
| `renderCursorLine`                              | `document_review_select.go`                                  |
| `documentStatusLine`                            | `application_detail.go`                                      |
| `companyFiltersFrom`                            | `filter_select.go`                                           |
| `sortCompaniesByName`                           | `company_list.go`                                            |
| `createCompany`                                 | `company_form.go`                                            |
| `deleteCompany`                                 | `company_delete.go`                                          |
| `updateCompanyName`                             | `company_edit.go`                                            |
| `updateApplicationNotes`                        | `application_notes.go`                                       |
| `loadPostings`                                  | `posting_list.go`                                            |
| `openSnapshots`, `filterOutArchived`, `filterPostingsByCompanyFilters`, `splitCompanyFilters` | `loadPostings`. If step 6 is next, leave these for it. |

Move each helper with several call sites, all in one file, to that file, without folding it:

| Helper(s)                                                                         | File                |
| --------------------------------------------------------------------------------- | ------------------- |
| `renderFilterOption` (2), `toSet` (2)                                             | `filter_select.go`  |
| `sectionHeading`, `detailField`, `detailRow`, `detailValueWidth`, `wrapDetailValue` | `posting_detail.go` |
| `indexOfCompany` (5)                                                              | `company_list.go`   |
| `indexOfPosting` (3)                                                              | `posting_list.go`   |

Helpers called from two or more files stay shared, but move out of `app.go`: the review display (`reviewBadge`, `reviewGlyphSummary`, `outcomeDisplay`) and the write commands (`markupChanged`, `applicationWritten`, `toggleInterested`, `toggleArchived`, `createApplication`, `updateApplicationStatus`, `currentReviews`).

Leave alone:

- `onScreen`: one production caller, but 313 test uses. It's the tests' way to ask which screen is up.
- `newPostingDetail` and `newDocumentReviewForm`: step 4 removes them.
- `postingList`, `postingDetail` and `applicationDetail`: step 3 removes them.

Also:

- `screenStack.top()` has no production caller, only 9 test uses. Either delete it and rewrite those tests through `App`, or keep it as a test seam. Decide before deleting.
- The package comment says markup editing "is a later increment". It has shipped, so the comment is stale.

## Acceptance criteria

- [ ] `app.go` holds only `App`'s own state, routes, `Update`, `View` and banner.
- [ ] No helper left with a single call site, except those under "Leave alone".
- [ ] No test changes except those for folded helpers.
- [ ] Verification checklist clean.
