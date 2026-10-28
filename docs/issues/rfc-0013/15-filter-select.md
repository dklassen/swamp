---
title: "[RFC 0013 step 15] The company filter picker saves its own filters and reports a Ran"
labels: [rfc-0013, rfc0013-step-15]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Adopting: Still in tui", "Open questions: 1"]
depends_on: [13, 14]
---

RFC 0013, step 5 (the filter picker).

## Problem

The picker asks `App` to save (`saveFilterSelectionMsg`), `App` calls `CompanyService.SetFilters` and answers with `companyFiltersSavedMsg`, and loads the options for it (`filterOptionsLoadedMsg`). Cancel is `cancelFilterSelectMsg`.

## Change

- The posting list pushes the picker with `Nav[FilterSelection]` (the company and its current filters). It loads its options in `Init` as `Read[FilterOptions]`.
- On save the picker calls `SetFilters` itself and gets `Ran[FilterSelection]` back, heard only by itself (RFC 0013 open question 1's default). It pops; the posting list routes the same `Ran` `About` its company and reloads.
- Remove `saveFilterSelectionMsg`, `companyFiltersSavedMsg`, `filterOptionsLoadedMsg`, `cancelFilterSelectMsg` and `enterFilterSelectMsg`.

## Acceptance criteria

- [ ] Saving filters closes the picker and the posting list shows the filtered postings.
- [ ] A failed save shows its error and leaves the picker open.
- [ ] Manual check against real data.
- [ ] Verification checklist clean.
