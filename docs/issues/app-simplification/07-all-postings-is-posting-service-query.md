---
title: "[App simplification 7] The all-postings tab is PostingService's query"
labels: [app-simplification]
depends_on: [06b-posting-list-pages-and-sorts]
---

## Problem

`allPostingsModel.loadAt` (`tui/all_postings.go`) decides which postings the all-postings tab lists. It builds `posting.Search{ListingStatus: &open, ...}` itself, then builds a second search, `{ListingStatus: &open, Limit: 1}`, for the count without the query that the search line and the header's tab show. After 6a, it's the last place `tui` builds a `posting.Search`. RFC 0010 says areas own their queries.

The two searches run outside a transaction, so a sync between them can leave the page and the count disagreeing.

## Change

A `PostingService` method returns one page of what the all-postings tab shows, and how many it shows with no query. It builds both searches, in one transaction: open postings only, company filters applied, archived ones left out. The TUI passes only what the user chose or the screen decides (the query, the order, the cursor and the page size) and converts the result to snapshots. `posting.Search` and `posting.ListingOpen` leave `all_postings.go`.

## Decided

- **On `PostingService`,** since the postings it lists aren't keyed by a company.
- **The method:** `ListedPostings(ctx, page PostingsPage) (*ListedPostings, error)`.
  - `PostingsPage` is the caller's choices only: `Query string`, `Order posting.Order`, `AfterID int64`, `AfterScore int`, `Limit int`. There's no field for listing status, archived postings or company filters, so a caller can't change the rule.
  - `ListedPostings` is `Page posting.SearchPage` and `WithoutQuery int`. With no query, `WithoutQuery` is `Page.Total`, and only one search runs.
  - The search inside is `posting.Search{ListingStatus: &posting.ListingOpen, Query: page.Query, Order: page.Order, AfterID: page.AfterID, AfterScore: page.AfterScore, Limit: page.Limit}`.
- **`PostingService.Search` stays.** `stage.Search`, behind the MCP `search_postings` tool, still needs a search the caller builds.
- **`message.PostingSearch` and the tab's paging don't change.** `searchOrders` stays in `tui`, since `message.PostingOrder` is the TUI's.

## Out of scope

- `stage.Search` building its own `posting.Search` from `SearchOptions`. It's the agent's general search, not a list with a rule.
- Sharing a pager between the tab and the company's posting list (6b's "waits for a third").

## Acceptance criteria

- [ ] `PostingService.ListedPostings` is tested through services, arranging data through services: closed and archived postings left out, company filters applied, a deleted company's postings never listed, the query narrowing the page but not `WithoutQuery`, and paging in each order listing every posting once.
- [ ] With no query, `WithoutQuery` equals the sum of `CountOpenByCompany`'s counts, so the tab and the company list's Open column can't disagree.
- [ ] `tui` builds no `posting.Search` and has no listing-status checks on postings.
- [ ] Manual check against real data: the tab lists the same postings, in the same order, with the same counts, as before, with and without a query.
- [ ] Verification checklist clean.
