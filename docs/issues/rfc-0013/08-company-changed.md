---
title: "[RFC 0013 step 8] Company writes are Event[Changed[Company]]; the three company result structs go"
labels: [rfc-0013, rfc0013-step-8]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Decisions: Payloads", "Adopting: Already in tui/message", Template, "Steps: 4"]
depends_on: [2, 3, 6, 7]
---

RFC 0013, step 4 (the company writes). See `docs/rfcs/0013-tui-change-messages.md`.

## Problem

The company list has a route for each of `CompanyCreatedMsg`, `CompanyDeletedMsg` and `CompanyNameUpdatedMsg` (`tui/company_list.go:54`), one per action on one entity. All three carry `company.Company`, which has pointer fields, and an `error`. The delete confirmation checks the company ID in its handler.

## Change

- The `Company` snapshot and `TypeCompany` in `tui/message`, and `companySnapshot(company.Company) message.Company` outside it. `DeletedAt` is zero while the company isn't deleted.
- The create, rename and delete commands send `Created`/`Updated`/`Deleted` with `Source{Process, Instance}`, where `Updated` and `Deleted` get `Before`, `After` and `At` from step 3's `change.Of`. A failure sends `Event[Error]` with the company as `Subject` and the op that didn't happen.
- `Source.Process` is the TUI's origin, `"tui:<pid>"`, as `store.Config.Origin` stamps it. `App` gets it from `cmd/swamp`.
- The company list: one `companyChanged` route, switching on `Op`, as in the Template.
- The delete confirmation: `companyDeleted` with `About(TypeCompany, id), OnOp(OpDeleted)`, and `deleteFailed` with `From(instance)`. Neither handler reads the payload.
- The form and edit (step 7): their result route filters `From(instance)`, and so does their `Error` route.
- Remove the three structs.

## Acceptance criteria

- [ ] The list adds, renames and drops a row on the matching `Op` (tests through its handlers).
- [ ] The delete confirmation closes when its company is deleted, whoever deleted it, and when its own delete fails. A delete of another company doesn't close it (test).
- [ ] A failed rename shows its error and leaves the edit open.
- [ ] Every event the commands send has a nonzero `ID`, `At` in UTC, and `Source.Instance` of the model that asked.
- [ ] Manual check against real data: add, rename, delete, and a failed rename.
- [ ] Verification checklist clean.
