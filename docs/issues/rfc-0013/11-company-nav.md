---
title: "[RFC 0013 step 11] The company screens' open and close requests become Event[Nav]"
labels: [rfc-0013, rfc0013-step-11, rfc-0012]
rfc: docs/rfcs/0013-tui-change-messages.md
related_rfc: docs/rfcs/0012-tui-screen-router.md
rfc_sections: [Primitives, "Adopting: Navigation without the model"]
depends_on: [4, 8]
---

RFC 0013, step 4 (navigation). See `docs/rfcs/0013-tui-change-messages.md`.

## Problem

`OpenCompanyFormMsg`, `OpenCompanyEditMsg`, `OpenCompanyDeleteMsg`, `OpenCompanyPostingsMsg` and `CloseCompanyDeleteMsg`, plus the edit's cancel from step 7, are each a type of their own. `App.update` switches on all of them (`tui/app.go:1096` to `:1114`). Three carry `company.Company`, and `OpenCompanyDeleteMsg` also carries `OpenPostings`.

## Change

- `Event[Nav]`: a `NavOp` (push, pop, replace) and the `Screen` kind to open, with the company as `Subject` and the asking model as `Source.Instance`.
- `App` acts on `Event[Nav]` in one place: it builds the screen's model for that kind, numbers it, registers it, and drops a request whose `Source.Instance` isn't the model on top. That last check replaces the `currentScreen == screenCompanyDelete` guard at `:1109`.
- Remove the five structs and the cancel request.

## Decide before starting

- **What `App` builds a screen from.** `Nav` names an entity by `Subject` (type and ID) only. But the edit opens with the company's name, the delete confirmation with its name and open count, and the posting list with the company. Options:
  - (a) `App` looks them up: a load, so the screen opens a message later, or opens and fills in on its `Read`.
  - (b) `Nav` carries a snapshot body, for example `Nav[T]`, which RFC 0013 doesn't have.
  - (c) The opened screen loads what it shows in its `Init`, as RFC 0012's Template has posting detail do.

  (c) matches RFC 0012 and keeps `Nav` plain. The delete confirmation's open count then needs a query of its own. Recommendation: (c), and record it in both RFCs.

## Acceptance criteria

- [ ] Every company screen opens and closes as before from the user's side (existing App tests).
- [ ] Two quick `n`s on the delete confirmation close it once (`TestApp_CompanyDeleteConfirmation_TwoQuickNs_CloseItOnce`).
- [ ] A `Nav` from a model no longer on top is dropped (test).
- [ ] The decision recorded in RFC 0013 and RFC 0012.
- [ ] Manual check against real data: open and leave each company screen.
- [ ] Verification checklist clean.
