---
title: "[App simplification 6b] The company's posting list pages and sorts"
labels: [app-simplification]
depends_on: [06a-posting-list-reads-through-search]
---

## Problem

After 6a a company's posting list shows one page of its postings, newest first, and says when there are more, but can't reach them. The all-postings tab already pages and sorts the same search.

## Change

The posting list pages and sorts as the all-postings tab does: ←/→ (h/l) step pages, `s` switches between newest first and best fit first, and a line shows "Page N of M · Total postings · order".

## Decided

- **Keyset paging, as the all-postings tab does:** `starts []pageStart` holds where each page began. The next page starts after the last row's `ID` and `Score`, the previous one drops the last start, and a page past the first that empties steps back.
- **The order is `message.PostingOrder`,** read through `searchOrders`: `OrderNewest` (`OrderIDDesc`, the default) and `OrderBestFit` (`OrderScoreDesc`).
- **A read carries the page and order it was read with** (`Order`, `AfterID`, `AfterScore`, `Limit`), and the list drops one that's no longer the page on screen, as `showSearch` does.
- **The paging code is copied, not shared.** The company list is its second user; extracting a pager waits for a third.
- **Changing order, `A` and saving filters** go back to page 1.
- **`Siblings` stays the page's IDs,** so stepping in detail stops at the page's edge.

## Acceptance criteria

- [ ] ←/→ reach every posting of a company with more than a page, and back.
- [ ] `s` switches the order and returns to page 1. The page line names the order.
- [ ] Archiving the last posting on a page past the first, while archived ones are hidden, steps back a page.
- [ ] Manual check against real data on a company with several pages.
- [ ] Verification checklist clean.
