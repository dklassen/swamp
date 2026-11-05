---
title: "[Sort applications 2] Pick how the active applications list sorts"
labels: [enhancement]
depends_on: [01-active-applications-by-company-and-status]
---

## Problem

After 01, the home screen always lists applications by company, then status. Sometimes the question is "what's stuck longest" or "what's at Interviewing", and a fixed order can't answer it. The posting lists already switch order with `s`.

## Change

A key on the home screen cycles the order: company then status (the default), status then company, and longest at status first. A line names the current order, as the posting lists' page line does.

## Decided

- **The key is `o`.** It's free on the home screen, and status keeps `s`. On the posting lists `o` opens the browser, but a row here has no single URL.
- **Three orders, no newest first:** company then status (the default), status then company, and longest at status first.
- **`Active` takes an `application.Order`** (iota consts, one name table). The TUI maps its own message order to it, as `searchOrders` does for postings.
- **Ties:** each order falls back to the other keys, then application ID, so it's total and stable across reloads.
- **For the session:** the order survives leaving the home screen. A restart goes back to the default, as the posting lists' order does.

## Acceptance criteria

- [ ] Each order is tested through `ApplicationService`, total and stable.
- [ ] The key cycles orders and keeps the cursor on the same application.
- [ ] Manual check against real data.
- [ ] Verification checklist clean.
