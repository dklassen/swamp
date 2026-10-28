---
title: "[RFC 0013 step 20] The status picker and notes editor write for themselves"
labels: [rfc-0013, rfc0013-step-20]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Adopting: Still in tui"]
depends_on: [13, 18]
---

RFC 0013, step 5 (status and notes).

## Problem

The status picker and the notes editor are opened by `enterApplicationStatusMsg` (from posting detail and the home screen) and `enterApplicationNotesMsg`. `App` loads the picker's application (`applicationStatusLoadedMsg`), runs both writes, and decides where each goes back to.

## Change

- `Nav[Application]` to `ScreenApplicationStatus` or `ScreenApplicationNotes` pushes them with the application they show; no load.
- Each runs its own write (step 18's events) and pops on its own `Changed[Application]` (`From` itself); on its own `Error` it stays open.
- Remove `enterApplicationStatusMsg`, `enterApplicationNotesMsg`, `cancelApplicationStatusMsg`, `cancelApplicationNotesMsg` and `applicationStatusLoadedMsg`.

## Acceptance criteria

- [ ] From posting detail and from the home screen, a status change closes the picker and shows on the screen beneath.
- [ ] A failed write shows its error and keeps the screen open.
- [ ] Manual check against real data.
- [ ] Verification checklist clean.
