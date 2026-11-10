---
title: "[Stage into services 11] Application forms have their own FormService"
labels: [stage-into-services]
depends_on: [6]
---

## Problem

A posting's application form is split across two services. `PostingService.SaveForm` and `Form` store and read one by hand (the TUI's `f` on application detail), while `DraftingService` holds the board fetchers and fetches a missing form when drafting starts. Forms are their own concern: downloaded from a board, or made by hand and linked to a posting. Drafting only reads them.

## Change

- A new `FormService` takes `PostingService.SaveForm` and `Form`, with their tests rewritten as `FormService` tests. `SaveForm` still replaces any stored form: a form made by hand wins.
- The TUI's application form screen and every other caller of the two methods move to `FormService`.
- The fetchers stay in `DraftingService` for now; step 13 gives them to `FormService` and step 14 takes them from `DraftingService`.

## Acceptance criteria

- [ ] `FormService` tests, each watched fail first: a saved form reads back; a second save replaces the first; a posting with no form reads as `store.ErrNotFound`.
- [ ] `PostingService` has no form methods left.
- [ ] Manual check: on a copy of the real database, `f` on application detail saves a form and reopens it.
- [ ] Verification checklist clean.
