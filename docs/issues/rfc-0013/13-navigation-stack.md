---
title: "[RFC 0013 step 13] App keeps RFC 0012's navigation stack; the company screens open and close on it"
labels: [rfc-0013, rfc0013-step-13, rfc-0012]
rfc: docs/rfcs/0013-tui-change-messages.md
related_rfc: docs/rfcs/0012-tui-screen-router.md
rfc_sections: ["Steps: 5", "Adopting: Navigation without the model"]
depends_on: [11]
---

RFC 0013, step 5, a prerequisite: RFC 0012 sections 3 to 5 and its Template's stack. See `docs/rfcs/0012-tui-screen-router.md`.

## Problem

`App` opens each converted screen with a function of its own (`openCompanyForm`, `openCompanyEdit`, `openCompanyDelete`) and closes it with a switch over `currentScreen` (`closeTop`, `tui/app.go:277`). `focused()` is another switch over the same screens. Every later step would add a case to all three.

## Change

- `App.stack []stacked` (kind and screen) over the current tab, as in RFC 0012's Template. `push(kind, screen)` panics on a kind already open, `Register`s, appends and returns `Init`; `pop` `Deregister`s and drops the top. Both send `Signal[FocusChanged]` with the new top's instance.
- One route per `Nav[T]` body, each a switch over `Screen` that builds the screen from `Value` and calls `push`; a pop calls `pop`. `onTop` reads the stack's top instead of `focused()`'s switch.
- The company form, edit and delete move onto it. `openCompanyForm`, `openCompanyEdit`, `openCompanyDelete`, `closeTop` and their `currentScreen` cases go.
- While screens are still on `App`'s old paths, `currentScreen` names them; the stack holds only converted screens. Record that as a transitional bullet in RFC 0012.

## Acceptance criteria

- [ ] Opening and closing the form, edit and delete behave as before (the existing `TestApp_Company*` tests pass unchanged).
- [ ] A `Nav` from a screen not on top is dropped (test); pushing a kind that's already open panics, as RFC 0012 section 5 has it (test).
- [ ] Manual check against real data: open and close each company screen, and two quick esc presses.
- [ ] Verification checklist clean.
