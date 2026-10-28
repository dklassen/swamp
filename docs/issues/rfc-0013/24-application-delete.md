---
title: "[RFC 0013 step 24] The application delete confirmation deletes for itself"
labels: [rfc-0013, rfc0013-step-24]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Adopting: Still in tui", Template]
depends_on: [18, 22, 23]
---

RFC 0013, step 5 (application delete), as the company delete confirmation in RFC 0013's Template.

## Problem

`enterApplicationDeleteMsg` opens it from the home screen and detail; `y` sends `confirmApplicationDeleteMsg` to `App`, which reloads the application first (`applicationReloadedForDeleteMsg`), deletes it, and works out where to go back to.

## Change

- `Nav[Application]` pushes it with the application it names; no reload.
- `y` deletes; it closes on `Changed[Application]` `About` its application with `OnOp(OpDeleted)`, whoever deleted it, and on its own `Error`.
- Detail, if beneath it, pops on the same delete.
- Remove `enterApplicationDeleteMsg`, `cancelApplicationDeleteMsg`, `confirmApplicationDeleteMsg` and `applicationReloadedForDeleteMsg`.

## Acceptance criteria

- [ ] Deleting from detail returns to the home screen without the row; from the home screen, it stays there without the row.
- [ ] Manual check against real data.
- [ ] Verification checklist clean.
