---
title: "[RFC 0013 step 14] The posting list is a stacked screen that loads with Read and reloads on its company's Ran"
labels: [rfc-0013, rfc0013-step-14, rfc-0012]
rfc: docs/rfcs/0013-tui-change-messages.md
related_rfc: docs/rfcs/0012-tui-screen-router.md
rfc_sections: ["Adopting: Still in tui", "Decisions: Payloads (What a Ran[T] reports)"]
depends_on: [10, 13]
---

RFC 0013, step 5 (the posting list). RFC 0012 open question 1 decided it's stacked.

## Problem

The posting list's postings, markup, applications and `hideArchived` are `App`'s fields. `App` loads them (`postingsLoadedMsg`), reloads them on a `Ran[SyncReport]` about `selectedCompany` (`tui/app.go:1182`), flips `hideArchived` on `toggleHideArchivedMsg`, refreshes on `refreshSelectedCompanyMsg` and goes back on `backToCompanyListMsg`.

## Change

- `Nav[Company]` to `ScreenCompanyPostings` pushes a posting list built for that company, with `App`'s `hideArchived`. It loads in `Init`; the result is `Event[Read[PostingList]]`, or `Error`.
- `hideArchived` is `App`'s for the session (RFC 0012, section 7). The list flips its own copy, reloads, and sends `Signal[HideArchived]`, which `App` keeps for the next opening.
- `r` sends `Run[SyncCompany]`; the list routes `Ran[SyncReport]` `About` its company and reloads. `App`'s reload goes.
- esc sends `Pop`. Remove `postingsLoadedMsg`, `refreshSelectedCompanyMsg`, `toggleHideArchivedMsg`, `backToCompanyListMsg` and `selectedCompany`.
- `enterPostingDetailMsg` and `enterFilterSelectMsg` stay `App`'s until steps 15 and 19; the list sends them as before.
- RFC 0012: the "App still reloads the posting list" bullet goes.

## Acceptance criteria

- [ ] Opening a company's postings shows them; esc goes back to the company list.
- [ ] A sync of that company reloads the list with the cursor on its posting; a sync of another company doesn't (test with `About`).
- [ ] Hiding archived postings lasts across openings in one session.
- [ ] Manual check against real data: open, refresh, hide archived, back, reopen.
- [ ] Verification checklist clean.
