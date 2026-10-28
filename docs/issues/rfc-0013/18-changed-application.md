---
title: "[RFC 0013 step 18] Application writes are Event[Changed[Application]]"
labels: [rfc-0013, rfc0013-step-18]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Adopting: Still in tui (Write results)"]
depends_on: [16]
---

RFC 0013, step 5 (the application writes).

## Problem

`applicationCreatedMsg`, `applicationStatusUpdatedMsg`, `applicationNotesUpdatedMsg` and `applicationDeletedMsg` each carry an `application.Application` or an ID and an `error`. `App` applies each to up to five places (RFC 0012, section 1), guarded by `currentScreen` and the asker's instance.

## Change

- An `Application` snapshot in `tui/message`, `Subject` the application. The create sends `Created` from `ApplyTo`; status, notes and delete send `Updated`/`Deleted` from step 16; a failure sends `Error`.
- `App` switches on the event in place of the four structs until the screens that hear it migrate (steps 19 to 26). The guards on the asker become `Source.Instance` checks.
- Remove the four structs.

## Acceptance criteria

- [ ] Every place a status, notes, create or delete reaches today still updates (the existing `TestApp_Application*` tests pass).
- [ ] Every event has a nonzero `ID`, `At` in UTC and the asker's `Source.Instance`.
- [ ] Manual check against real data: create, change status, edit notes, delete.
- [ ] Verification checklist clean.
