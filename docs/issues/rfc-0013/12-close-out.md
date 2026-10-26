---
title: "[RFC 0013 step 12] tui/message holds only events of plain data; RFC 0012 amended"
labels: [rfc-0013, rfc0013-step-12, rfc-0012]
rfc: docs/rfcs/0013-tui-change-messages.md
related_rfc: docs/rfcs/0012-tui-screen-router.md
rfc_sections: ["Steps: 4 (When step 4 lands)", "Decisions: Payloads (What tui/message may import)"]
depends_on: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11]
---

RFC 0013, the end of step 4. See `docs/rfcs/0013-tui-change-messages.md`.

## Problem

After steps 5 to 11, every routed message is an event, but the rule's test still has its allow-list and `tui/message` may still import `company`. RFC 0012 still describes `NavMsg{Next tea.Model}`, `FocusChangedMsg{Focused tea.Model}` and its old naming rule.

## Change

- Delete the allow-list. The rule's test now holds every type in `tui/message`.
- `tui/message` imports the standard library, and Bubble Tea only for its command constructors. It doesn't import `company` or `sync`. A test or `go list` check enforces this.
- RFC 0012, in the three places RFC 0013 names: its naming rule (a message is an `Event[P]` of a primitive), `NavMsg` (`Event[Nav]`, a screen kind and a subject, not `Next`) and `FocusChangedMsg` (a `Signal` naming a `ScreenInstance`). Also bring its names up to date with the code (`Router`/`Register`/`AlwaysScope`/`FocusScope` are now `MsgRouter`, `Routes()`, `Always`, `WhenFocused`), kept compact.
- RFC 0013: record the decisions taken in steps 1, 4, 6 and 11 as one-line rules, and mark the open questions they settled.

## Acceptance criteria

- [ ] The rule's test has no allow-list and passes.
- [ ] `go list -deps ./tui/message` shows neither `internal/domain/company` nor `sync`.
- [ ] Both RFCs read consistently with the code.
- [ ] `go test -race ./tui/...` clean (host-side, since the VM has no cgo).
- [ ] Verification checklist clean.
