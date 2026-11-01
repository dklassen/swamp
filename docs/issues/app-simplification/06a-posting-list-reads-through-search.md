---
title: "[App simplification 6a] The company's posting list is a search"
labels: [app-simplification]
depends_on: [01-helpers-leave-app-go]
---

## Problem

`postingListModel.load` (`tui/posting_list.go`) decides which of a company's postings are listed. It reads every posting with `ListByCompany`, then in Go drops closed ones, applies the company's saved filters with `filter.Match`, reads each posting's markup one query at a time, and drops archived ones while they're hidden. It's the only domain rule in the TUI. RFC 0010 says areas own their queries.

The rule is written twice more, in SQL. `SearchPostingsByID` and its sibling orders match company filters "as filter.Match's do", and `CountOpenPostingsByCompany` counts "what a company's posting list shows by default" for the company list's Open column. The Go copy already differs: it fails on an unsupported filter field, and the SQL ignores one. (`company_filters` has a CHECK constraint on the field, so stored filters can't reach that branch.)

`markupChanged` repeats the archived rule a fourth time: it patches the list in place and drops a posting archived while archived ones are hidden.

## Change

`load` reads one page with `PostingService.Search` and the company's filters with `CompanyService.Filters`, converts both to snapshots and sends them. The Go loop, the per-posting `Markup` reads and the `filter.Match` call leave `tui`. Nothing changes in `Search`, its SQL, or posting detail.

## Decided

- **The search:** `posting.Search{CompanyID: company.ID, ListingStatus: &posting.ListingOpen, IncludeArchived: !hideArchived, Order: posting.OrderIDDesc, Limit: pageSize}`. Company filters apply by default.
- **The page is what fits.** `pageSize` is the rows the table can show, as in the all-postings tab, so nothing is read unbounded and there's no fixed limit. A resize that changes it reads again. The list no longer scrolls: `visibleWindow` goes from it.
- **More than a page is said, not reached.** When `HasMore`, the list shows "Showing N of Total postings". Reaching the rest is 6b.
- **`message.PostingList` carries listings:** `Listings []Listing`, `HasMore`, `Total`, `Departments`, `Locations`. `Postings` and the `Markup` map go. The marker reads `Listing.Archived` / `Listing.Interested`.
- **Detail opens on the row,** with `listingPosting(current)` as the all-postings tab does. Detail already loads the full posting in `Init`. `Siblings` is the page's IDs.
- **A markup change reloads** when its posting is on the page, as the all-postings tab does, instead of patching the list. No posting-filtering logic is left in `tui`.
- **The cursor** stays on its posting when it's still on the page after a read, and is clamped otherwise.
- **`A`, `f`, `r`, `i`, `x`, `o`** keep their behaviour. The filter summary line still comes from `Departments` / `Locations`.

## Decide before starting

- **Order:** today's list is `first_seen_at DESC, id DESC`; `OrderIDDesc` is `id DESC`. Both are set at insert and IDs are `AUTOINCREMENT`, so they should agree. Check on a copy of the database for rows where they don't (e.g. a backfilled `first_seen_at`) before calling it no change.
- **`filter.Match` and `filter.Posting`** have no production caller once `load` stops using them, and the search queries' comment cites `filter.Match` as the reference behaviour. Delete them and reword that comment, or keep them as the spec the SQL is tested against.

## Acceptance criteria

- [ ] `tui` has no posting-filtering logic: no closed, filter or archived checks on postings.
- [ ] Posting list tests are updated for `message.PostingList`'s new shape and still cover closed, filtered, archived-hidden and archived-shown postings, arranging data through services.
- [ ] A company with more open postings than fit shows the "Showing N of Total" line.
- [ ] Manual check against real data: the list shows the same postings, in the same order, as before for a company that fits on one page.
- [ ] Verification checklist clean.
