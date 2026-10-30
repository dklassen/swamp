---
title: "[RFC 0013 step 27] The application form loads and saves for itself"
labels: [rfc-0013, rfc0013-step-27]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Adopting: Still in tui", "Open questions: 1"]
depends_on: [13, 23]
---

RFC 0013, step 5 (the application form).

## Problem

Application detail opens the form with `enterApplicationFormMsg`; `App` loads it (`applicationFormLoadedMsg`) and hears its save (`applicationFormSavedMsg`).

## Change

- `Nav[Application]` pushes it; it loads in `Init` as `Read[ApplicationForm]`, its Subject the posting, since application detail doesn't hold the form. Keys but esc wait for the read; a failed read closes it, leaving the error.
- It saves with `PostingService.SaveForm` and gets `Ran[ApplicationForm]`, heard only by itself (open question 1's default), and pops. A failed save keeps it open to try again.
- Remove `enterApplicationFormMsg`, `cancelApplicationFormMsg`, `applicationFormLoadedMsg` and `applicationFormSavedMsg`.

## Acceptance criteria

- [ ] Saving closes the form and the saved answers show when it's reopened.
- [ ] A failed save shows its error and keeps the form open.
- [ ] Manual check against real data.
- [ ] Verification checklist clean.
