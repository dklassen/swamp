---
title: "[RFC 0013 step 2] Routes filter on an event's Meta: About, From, OnOp"
labels: [rfc-0013, rfc0013-step-2]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Decisions: Routing and filtering", Template, "Steps: 2"]
depends_on: [1]
---

RFC 0013, step 2. See `docs/rfcs/0013-tui-change-messages.md`.

## Problem

`model.MsgRouter.Dispatch` (`tui/model/router.go`) calls every route registered for a message's Go type, and each handler decides whether the message is for it. For example, `companyDeleteModel.companyDeleted` checks the company ID itself. A route can't say "only this company" or "only what I asked for".

## Change

In `tui/model/router.go`:

- `type Filter func(message.Meta) bool`.
- `Always[T](handle, filters ...Filter)` and `WhenFocused[T](owner, handle, filters ...Filter)`. With no filters, a route behaves as it does today, so every existing `Routes()` compiles unchanged.
- The route calls the handler only if every filter passes on the message's `Metadata()`, after the focus check.
- `About(typ, id)` checks `Subject`, `From(instance)` checks `Source.Instance`, `OnOp(ops...)` checks `Op`. `OnOp` never passes a zero `Op`, so a request never passes it.
- A route with filters on a `T` that has no `Metadata() message.Meta` method, such as `tea.KeyPressMsg`, sets `route.err`. `Register` then fails, as it already does for a handler that takes an interface.

## Acceptance criteria

- [ ] An event a filter rejects never reaches the handler, one test per filter. The tests use a test-only `Event` type, since no routed message is an event yet.
- [ ] A route with two filters needs both to pass.
- [ ] `Register` fails, and registers none of the owner's routes, when one route has a filter on a type without `Metadata()`. The error names the owner and the type.
- [ ] `OnOp` rejects an event with no `Op`.
- [ ] Existing router tests unchanged and passing.
- [ ] Verification checklist clean.
