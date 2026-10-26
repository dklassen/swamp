---
title: "[RFC 0013 step 1] The event envelope, and the eight primitives"
labels: [rfc-0013, rfc0013-step-1]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: [Summary, Primitives, "Decisions: The envelope", "Decisions: Payloads", Template, "Steps: 1"]
depends_on: []
---

RFC 0013, step 1. See `docs/rfcs/0013-tui-change-messages.md`.

## Problem

The 18 message types in `tui/message` each say who, what and when in their own way, or not at all. So nothing can route or filter them without knowing each struct. They also hold `error`s, `company.Company` (pointer fields), `sync.Result` and, in `FocusChangedMsg`, a `tea.Model`.

## Change

Add the types from the RFC's Template to `tui/message`, using only the standard library. Nothing uses them yet.

- `Kind`, `Type` and `Op` as strings, with the `Kind…` and `Op…` constants. `Type…` constants are added with the bodies that use them, in later steps.
- `Meta` (`ID`, `Kind`, `Type`, `Source`, `Subject`, `Op`, `At`), `Source`, `Subject`, with the RFC's JSON tags.
- `Body`, `Primitive`, `Snapshot`, `Event[P]` with `Metadata()`, and `New`, which sets `Kind` and `Type` from the payload.
- `NewID`: 128 bits from `crypto/rand`, as hex.
- The eight primitives: `Changed[T]` (with `Created`, `Updated`, `Deleted`), `Read[T]`, `Run[T]`, `Ran[T]`, `Error` (`Code`, `Text`, and the `ErrorCode` set), `Nav`, `Notify`, `Signal[T]`.
- A test that round-trips one event of each primitive through JSON and compares it with `cmp.Diff`. Today's 18 message structs aren't checked; each later step replaces the ones it touches.

## Decide before starting

- **`Nav`'s fields.** The RFC gives `Nav` a `NavOp` and a `Screen` kind, with `Subject` saying which entity. Step 11 raises whether that's enough for the company screens. Step 1 adds only what the RFC states.

## Acceptance criteria

- [ ] `New` sets `Kind` and `Type` from the payload. Table test, one case per primitive.
- [ ] `Created`/`Updated`/`Deleted` set `Op` and leave the missing side as the zero value, and the zero side is left out of JSON.
- [ ] `NewID` gives 32 hex characters, and two calls differ.
- [ ] One event of each primitive comes back from a JSON round trip unchanged.
- [ ] `tui/message`'s new file imports only the standard library.
- [ ] Verification checklist clean.
