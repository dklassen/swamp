---
title: "[Sort applications 2] Pick how the active applications list sorts"
labels: [enhancement]
depends_on: [01-active-applications-by-company-and-status]
---

## Problem

After 01, the home screen always lists applications by company, then status. Sometimes the question is "what's stuck longest" or "what's at Interviewing", and a fixed order can't answer it. The posting lists already switch order with `s`.

## Change

A key on the home screen cycles the order: company then status (the default), status then company, and longest at status first. A line names the current order, as the posting lists' page line does.

## Open questions

- **The key:** `s` already sets status on the home screen. Use `o`, or move status?
- **The set of orders:** are these three right? Is newest first wanted?
- **Where the orders live:** an `application.Order` enum that `Active` takes (iota consts, names only on the wire), with the TUI mapping its own message order to it, as `searchOrders` does for postings.
- **Remembering it:** does the order survive leaving the screen, or a restart?

## Acceptance criteria

- [ ] Each order is tested through `ApplicationService`, total and stable.
- [ ] The key cycles orders and keeps the cursor on the same application.
- [ ] Manual check against real data.
- [ ] Verification checklist clean.
