---
title: "[App simplification 3] Screens name their own crumb and tab label"
labels: [app-simplification]
depends_on: []
---

## Problem

`App.crumbs` (`tui/header.go`) switches over every screen kind and reads screens' unexported fields: `postingListModel.company.Name`, `postingDetailModel.posting.Title`, `applicationDetailModel.detail`, `documentReviewFormModel.review`. `App.tabBar` reads `activeApplicationList.apps` and `allPostings.page.WithoutQuery`. RFC 0012 section 1 says `App` holds no screen's data. Here it reads it anyway.

`postingList()`, `postingDetail()` and `applicationDetail()` exist only for this, with one caller each.

## Change

Each screen names itself: a method on `stackScreen` for its crumb or crumbs, and one on the tabs for their label. `crumbs` becomes a loop over the path's screens. The type assertions and the three lookups go.

Not a separate model. The header has no state of its own and handles no messages; it's a pure function of the screens on the path and the width. `fitCrumbs` and the rendering stay plain functions in `header.go`.

## Decide before starting

- **Posting detail under application detail.** It shows "Posting" there, because application detail's crumbs already name the posting. Its crumb depends on the screen below. Options: the method takes the screen beneath, or the header drops a crumb that repeats the one before.
- **The method's name and return type**: one crumb, or several (application detail gives two today).

## Acceptance criteria

- [ ] `header.go` has no switch over screen kinds and no type assertions on screens.
- [ ] `TestApp_Screens_Breadcrumb*` and the tab tests pass unchanged.
- [ ] Verification checklist clean.
