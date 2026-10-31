---
title: "[RFC 0013 step 29] Every message is an event; App's old paths are gone"
labels: [rfc-0013, rfc0013-step-29, rfc-0012]
rfc: docs/rfcs/0013-tui-change-messages.md
related_rfc: docs/rfcs/0012-tui-screen-router.md
rfc_sections: ["Steps: 5", "Which messages are events"]
depends_on: [13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28]
---

RFC 0013, the end of step 5.

## Problem

After steps 13 to 28 no `…Msg` struct should be left in `tui`, and `App.update`'s switch and `currentScreen` should have nothing left to do.

## Change

- No message type in `tui` or `tui/model` that isn't an `Event[P]` or Bubble Tea's own. No test guards it.
- `App.update` and `currentScreen` go; the stack is the only navigation (`returnStack` went with step 25).
- RFC 0012: its "While screens are partly converted" bullets go, and its status is decided. RFC 0013: status from Draft, and the open questions settled by these steps marked.

## Acceptance criteria

- [ ] No `…Msg` struct in `tui`.
- [ ] `go test -race ./tui/...` clean (host-side).
- [ ] Manual check against real data across every screen.
- [ ] Verification checklist clean.
