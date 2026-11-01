---
title: "[App simplification 6b] The company's posting list pages and sorts"
labels: [app-simplification]
depends_on: [06a-posting-list-is-company-service-query]
---

## Problem

After 6a a company's posting list shows one page of its postings, newest first, and says when there are more, but can't reach them. The all-postings tab already pages and sorts the same search.

## Change

`PostingListPage` gains the order and the cursor, and `ListedPostings` passes them to the search. The posting list pages and sorts as the all-postings tab does: ←/→ (h/l) step pages, `s` switches between newest first and best fit first, and a line shows "Page N of M · Total postings · order".

## Decided

- **`PostingListPage` adds `Order posting.Order`, `AfterID int64` and `AfterScore int`.** They're the caller's choices, as `IncludeArchived` and `Limit` are. The rule stays in `ListedPostings`.
- **Keyset paging, as the all-postings tab does:** `starts []pageStart` holds where each page began. The next page starts after the last row's `ID` and `Score`, the previous one drops the last start, and a page past the first that empties steps back.
- **The TUI's order is `message.PostingOrder`,** mapped through `searchOrders`: `OrderNewest` (`OrderIDDesc`, the default) and `OrderBestFit` (`OrderScoreDesc`).
- **A read carries the page and order it was read with** (`Order`, `AfterID`, `AfterScore`, `Limit`), and the list drops one that's no longer the page on screen, as `showSearch` does.
- **The paging code is copied, not shared.** The company list is its second user; extracting a pager waits for a third.
- **Changing order, `A` and saving filters** go back to page 1.
- **`Siblings` stays the page's IDs,** so stepping in detail stops at the page's edge.

## Acceptance criteria

- [ ] `ListedPostings` is tested through `CompanyService` for each order and for a cursor: the pages together list every posting once, in order.
- [ ] ←/→ reach every posting of a company with more than a page, and back.
- [ ] `s` switches the order and returns to page 1. The page line names the order.
- [ ] Archiving the last posting on a page past the first, while archived ones are hidden, steps back a page.
- [ ] Manual check against real data on a company with several pages.
- [ ] Verification checklist clean.
