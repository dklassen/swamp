---
title: "[RFC 0013 step 22] The home screen owns its applications and follows their writes"
labels: [rfc-0013, rfc0013-step-22]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Adopting: Still in tui"]
depends_on: [18, 20]
---

RFC 0013, step 5 (the home tab, active applications).

## Problem

`activeApplications` and `activeApplicationProgress` are `App`'s fields, loaded by `activeApplicationsLoadedMsg` and reloaded after every application write and review. The screen's `enter…` requests go through `App`.

## Change

- A pointer `Routable` registered in `New`, owning its rows. It loads in `Init` as `Read[ActiveApplications]` and reloads on a `Changed[Application]` (and `Changed[Review]` from step 25).
- Its push to the status picker is step 20's `Nav`. Detail, delete and export stay `App`'s `enter…` requests until steps 23, 24 and 26.
- Remove `activeApplicationsLoadedMsg` and `backToActiveApplicationsMsg` once detail pops.

## Acceptance criteria

- [ ] A status change, delete or review anywhere shows on the home screen when it's back on top, with the cursor kept on its row.
- [ ] Manual check against real data.
- [ ] Verification checklist clean.
