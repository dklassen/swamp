---
title: "[Sort applications 1] Active applications list by company, then status"
labels: [enhancement]
---

## Problem

The home screen lists active applications in an order that's hard to scan. `orderForHome` (`tui/active_applications.go`, from #164) puts Started applications first, the one longest at its status first, and leaves the rest in the store's most-recently-changed order. A company's applications end up spread down the list, and the order changes every time one is touched.

`ApplicationService.Active` has two callers that get different orders. The TUI re-sorts with `orderForHome`. `stage.startedNotInterested` (behind the MCP posting lists) keeps the store's most-recently-changed order. The order is the TUI's rule, but it belongs to the applications area (RFC 0010).

## Change

`ApplicationService.Active` returns applications by company name, then status in workflow order. Every caller gets the same order. `orderForHome` is removed, and the store's order no longer matters to anyone.

## Decided

- **Company name, case-insensitive,** so "acme labs" doesn't sort after "Zeta Corp".
- **Status in workflow order:** the order of `application.Status`'s constants (Started, Submitted, Interviewing, Offer received, …). It isn't alphabetical by label.
- **Ties:** oldest at its status first (`StatusSince`), which keeps what #164 wanted inside each group, then application ID, so the order is total and stable across reloads.
- **#164's "Started first across all companies" is dropped.** Next step and Age still show which one is stale.
- **In the service, not SQL:** `StatusSince` is computed in `Active`, so the sort runs after it's filled.
- **The home screen's `/` search keeps the list's order;** it filters and doesn't re-sort.

## Out of scope

- A sort the user picks (see 02).
- The company list's own sort (`tui/company_list.go`, by name, case-sensitive).

## Acceptance criteria

- [ ] `ApplicationService.Active` is tested through services, arranging data through services: companies whose names differ only in case, several statuses per company, and a tie broken by status age, then ID.
- [ ] `orderForHome` and its tests are gone, and `tui` doesn't re-sort `Active`'s result.
- [ ] `ListActiveApplications`'s comment no longer says the order is something anyone depends on, or its `ORDER BY` is removed.
- [ ] Manual check against real data: the home screen groups each company's applications, in workflow order.
- [ ] Verification checklist clean.
