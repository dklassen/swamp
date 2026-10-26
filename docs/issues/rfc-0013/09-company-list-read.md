---
title: "[RFC 0013 step 9] CompaniesLoadedMsg becomes Event[Read[CompanyList]]"
labels: [rfc-0013, rfc0013-step-9]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: [Primitives, "Adopting: Already in tui/message"]
depends_on: [6, 8]
---

RFC 0013, step 4 (the company list's load). See `docs/rfcs/0013-tui-change-messages.md`.

## Problem

`message.CompaniesLoadedMsg` (`tui/message/structs.go:14`) carries `[]company.Company` and an `error`. `loadCompanies` (`tui/app.go:544`) sends it, and the company list's `companiesLoaded` takes it.

## Change

- A `CompanyList` body (`Companies []Company`, `OpenPostings map[int64]int`) with `TypeCompanyList`.
- `loadCompanies` sends `Event[Read[CompanyList]]`, or `Event[Error]` with no `Op` on failure.
- The company list's `companiesRead` takes it and keeps the cursor as it does today.
- Decide what the list does with a load's `Error`, given step 6's decision on open question 7.
- Remove `CompaniesLoadedMsg` and take it off the allow-list.

## Acceptance criteria

- [ ] The list shows the companies and open counts it loaded (existing tests).
- [ ] A failed load shows its error in the banner.
- [ ] The allow-list is down to 10.
- [ ] Verification checklist clean.
