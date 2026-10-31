---
title: "[App simplification 2] Which screens are overlays is said once"
labels: [app-simplification]
depends_on: []
---

## Problem

`App.View` and `App.beneath` each list `ScreenCompanyDelete` and `ScreenApplicationDelete` as the screens drawn over the one below. A new confirmation added to one list but not the other would get either the overlay or the header of the screen beneath it, but not both.

RFC 0012 section 8 names a `Dialog` interface for this. It was never added.

## Change

Say once which screens are overlays: either a method the screen reports (`Dialog()`, as the RFC names it), or a single list in `tui`. `View` and `beneath` both read it.

## Decide before starting

- **A method on the screen or a list in `App`.** A method follows RFC 0012 (open question 2: overlays are stacked screens that report they are one). A list is less code while there are two.

## Acceptance criteria

- [ ] The overlay screens are named in one place.
- [ ] Existing overlay and header tests pass unchanged.
- [ ] RFC 0012 updated if the `Dialog` decision changes it.
- [ ] Verification checklist clean.
