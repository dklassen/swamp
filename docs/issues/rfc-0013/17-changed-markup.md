---
title: "[RFC 0013 step 17] Markup writes are Event[Changed[Markup]]"
labels: [rfc-0013, rfc0013-step-17]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Adopting: Still in tui (Write results)"]
depends_on: [14, 16]
---

RFC 0013, step 5 (the markup writes).

## Problem

`toggleInterested` and `toggleArchived` (`tui/app.go:703`) return `postingMarkupUpdatedMsg` with a `posting.Markup` and an `error`. `App` patches the posting list's markup, drops an archived row, and reloads all postings, each by checking `currentScreen`.

## Change

- A `Markup` snapshot in `tui/message`, `Subject` the posting. The commands send `Updated` from step 16's `change.Of`, or `Error`.
- The posting list routes `Changed[Markup]` and patches its own row, dropping it when archived while `hideArchived` is on. `App` keeps all postings' reload until step 21.
- Remove `postingMarkupUpdatedMsg`.

## Acceptance criteria

- [ ] Marking interested and archiving update the posting list's row whether or not it's on top (tests through its handler).
- [ ] A failed write shows its error.
- [ ] Manual check against real data.
- [ ] Verification checklist clean.
