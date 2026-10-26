---
title: "[RFC 0013 step 4] App numbers every model it registers; models know their ScreenInstance"
labels: [rfc-0013, rfc0013-step-4]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Decisions: The envelope (Source)", Template, "Steps: 4"]
depends_on: [1]
---

RFC 0013, step 4 (the "instance numbering" part). See `docs/rfcs/0013-tui-change-messages.md`.

## Problem

An event names who asked by `Source.Instance`, a `message.ScreenInstance`, and focus will be a `ScreenInstance` too (step 5). `message.ScreenInstance` exists, but nothing assigns one. `App` registers the company list, `Syncing` and each company delete confirmation (`tui/app.go:236`, `:1104`) without numbering them. The delete confirmation tells a closed opening from a new one only by being a different pointer.

## Change

- `App` hands out a new `ScreenInstance` for every model it registers, starting at 1. 0 stays "none".
- A model that sends events learns its instance when it's built (`newCompanyDeleteModel(…, instance)`), so its commands can put it in `Source` without capturing the model.
- A reopened delete confirmation gets a new number.
- `App` remembers the focused model's instance, ready for step 5.

No message changes yet.

## Decide before starting

- **Built with it, or told it at registration?** RFC 0012 had `RegisterRoutes(r, id)`. The code has `Routes()` with no arguments. Passing the instance to the constructor needs no interface change. Recommendation: the constructor.

## Acceptance criteria

- [ ] Two openings of the company delete confirmation get different instances (App test, through what the user does).
- [ ] The company list, `Syncing` and the confirmation all have nonzero, distinct instances.
- [ ] Verification checklist clean.
