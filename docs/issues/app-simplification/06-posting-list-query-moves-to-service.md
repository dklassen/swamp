---
title: "[App simplification 6] The company's posting list is PostingService's query"
labels: [app-simplification]
depends_on: [01-helpers-leave-app-go]
---

## Problem

`loadPostings`, in `tui`, decides which of a company's postings are listed. It drops closed postings, applies the company's saved filters (failing on an unsupported field), reads each posting's markup one query at a time, and drops archived postings when asked. That's the only domain rule in the TUI. RFC 0010 says areas own their queries.

## Change

A `PostingService` method returns a company's listed postings with their markup and the company's filters, given whether to hide archived ones. `loadPostings` becomes a command that calls it and converts the result to snapshots. `openSnapshots`, `filterOutArchived`, `filterPostingsByCompanyFilters` and `splitCompanyFilters` move into the service, or into `internal/domain/filter` where they're about filters.

## Decide before starting

- **The method's name and return type**: one struct, or postings plus markup.
- **The N+1 markup reads**: keep them, or make one query now that the service owns it.
- **Where the department/location split belongs**: the service, or `filter`.

## Acceptance criteria

- [ ] The rules (closed, filters, archived) are tested through `PostingService`, arranging data through services (no SQL backdoors).
- [ ] `tui` has no posting-filtering logic.
- [ ] Posting list tests pass unchanged.
- [ ] Verification checklist clean.
