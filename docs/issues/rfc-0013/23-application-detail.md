---
title: "[RFC 0013 step 23] Application detail loads for itself; the editor reports a Ran"
labels: [rfc-0013, rfc0013-step-23]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Adopting: Still in tui (Jobs outside the database)"]
depends_on: [19, 22]
---

RFC 0013, step 5 (application detail and the editor). Related: #285.

## Problem

`App` loads application detail (`applicationLoadedMsg`, `documentReviewsLoadedMsg`), reloads it on `refreshApplicationDetailMsg`, on `applicationDetailReloadedMsg` from the change log and each time it comes back on top, and hears `editorClosedMsg` after a document is edited.

## Change

- `Nav[ApplicationDetail]` pushes it, carrying the home row's application, posting, company name and reviews, so it opens on what it shows; it loads in `Init` as `Read[ApplicationDetail]` and reloads when it comes back on top (a `Signal[FocusChanged]` naming itself), since its documents change with no event.
- The editor's command records the write before it reports, and sends `Ran[EditDocument]` or `Error`; detail reloads on its own `Ran`.
- It routes `Changed[Application]` `About` its application; `Changed[Review]` comes with step 25.
- Remove `applicationLoadedMsg`, `refreshApplicationDetailMsg`, `editorClosedMsg`, `enterApplicationDetailMsg`, `backToActiveApplicationsMsg` (detail pops), and `activate`'s special case for it.

## Acceptance criteria

- [ ] Editing a document and coming back shows the new write.
- [ ] A status change made from posting detail shows when detail is back on top.
- [ ] Manual check against real data.
- [ ] Verification checklist clean.
