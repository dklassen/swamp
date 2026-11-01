---
title: "[App simplification 6a] A company's posting list is CompanyService's query"
labels: [app-simplification]
depends_on: [01-helpers-leave-app-go]
---

## Problem

`postingListModel.load` (`tui/posting_list.go`) decides which of a company's postings are listed. It reads every posting with `ListByCompany`, then in Go drops closed ones, applies the company's saved filters with `filter.Match`, reads each posting's markup one query at a time, and drops archived ones while they're hidden. It's the only domain rule in the TUI. RFC 0010 says areas own their queries.

The rule is written twice more, in SQL. `SearchPostingsByID` and its sibling orders match company filters "as filter.Match's do", and `CountOpenPostingsByCompany` counts "what a company's posting list shows by default" for the company list's Open column. The Go copy already differs: it fails on an unsupported filter field, and the SQL ignores one. (`company_filters` has a CHECK constraint on the field, so stored filters can't reach that branch.)

`markupChanged` repeats the archived rule a fourth time: it patches the list in place and drops a posting archived while archived ones are hidden.

## Change

A `CompanyService` method returns one page of what a company's posting list shows, with the company's filters. It builds the `posting.Search` itself: the company, open postings only, its filters applied. The TUI passes only what the user chose or the screen decides (whether archived postings are hidden, and the page size) and converts the result to snapshots. The Go loop, the per-posting `Markup` reads and the `filter.Match` call leave `tui`. Nothing changes in `Search` or its SQL.

## Decided

- **On `CompanyService`,** since it answers a question about a company. It reads through a `PostingSearchRepository`, as it already reads postings through `PostingRepository`.
- **The method:** `ListedPostings(ctx, companyID int64, page PostingListPage) (*CompanyPostings, error)`.
  - `PostingListPage` is the caller's choices only: `IncludeArchived bool`, `Limit int`. 6b adds the order and the cursor. There's no field for listing status or company filters, so a caller can't change the rule.
  - `CompanyPostings` is `Page posting.SearchPage` and `Filters []filter.Filter`.
  - The search inside is `posting.Search{CompanyID: companyID, ListingStatus: &posting.ListingOpen, IncludeArchived: page.IncludeArchived, Order: posting.OrderIDDesc, Limit: page.Limit}`.
- **The page is what fits.** `pageSize` is the rows the table can show, as in the all-postings tab, so nothing is read unbounded and there's no fixed limit. A resize that changes it reads again. The list no longer scrolls: `visibleWindow` goes from it.
- **More than a page is said, not reached.** When `HasMore`, the list shows "Showing N of Total postings". Reaching the rest is 6b.
- **`message.PostingList` carries listings:** `Listings []Listing`, `HasMore`, `Total`, `Departments`, `Locations`. `Postings` and the `Markup` map go. The marker reads `Listing.Archived` / `Listing.Interested`.
- **Splitting filters into departments and locations stays in `tui`.** It's how the summary line and the filter select screen show them, not a rule about postings.
- **Detail opens on the row,** with `listingPosting(current)` as the all-postings tab does. Detail already loads the full posting in `Init`. `Siblings` is the page's IDs.
- **A markup change reloads** when its posting is on the page, as the all-postings tab does, instead of patching the list. No posting-filtering logic is left in `tui`.
- **The cursor** stays on its posting when it's still on the page after a read, and is clamped otherwise.
- **`A`, `f`, `r`, `i`, `x`, `o`** keep their behaviour.

## Decide before starting

- **Order:** today's list is `first_seen_at DESC, id DESC`; `OrderIDDesc` is `id DESC`. Both are set at insert and IDs are `AUTOINCREMENT`, so they should agree. Check on a copy of the database for rows where they don't (e.g. a backfilled `first_seen_at`) before calling it no change.
- **`filter.Match` and `filter.Posting`** have no production caller once `load` stops using them, and the search queries' comment cites `filter.Match` as the reference behaviour. Delete them and reword that comment, or keep them as the spec the SQL is tested against.

## Out of scope

The all-postings tab builds its own `posting.Search` in `tui` too (`ListingStatus: &open`, plus a second search for the count without the query). The same move applies there. It gets its own issue.

## Acceptance criteria

- [ ] `ListedPostings` is tested through `CompanyService`, arranging data through services: closed postings left out, company filters applied, archived postings left out or included, a deleted company's postings never listed, `HasMore` and `Total` when there's more than a page.
- [ ] With archived postings hidden, `ListedPostings`'s `Total` for a company equals `CountOpenByCompany`'s count for it, so the list and the company list's Open column can't disagree.
- [ ] `tui` builds no `posting.Search` for the posting list and has no closed, filter or archived checks on postings.
- [ ] Posting list tests are updated for `message.PostingList`'s new shape.
- [ ] Manual check against real data: the list shows the same postings, in the same order, as before for a company that fits on one page.
- [ ] Verification checklist clean.
