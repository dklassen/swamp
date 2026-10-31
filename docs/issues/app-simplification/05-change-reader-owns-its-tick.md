---
title: "[App simplification 5] The change reader schedules its own tick"
labels: [app-simplification]
depends_on: []
---

## Problem

`App` runs the change reader's timer: `WithChangeLog` builds and registers the reader, while `changes`, `changePoll`, `pollChanges` and `changeTick` keep it ticking. The reader then reads on that tick. Two types share one job, and `App` keeps every service field partly so `WithChangeLog` can build the reader.

## Change

`changeReader` asks for its own next tick and reads on it. `App` registers it and batches its `Init`. `changes`, `changePoll`, `pollChanges` and `changeTick` leave `App`.

## Decide before starting

- **Who builds the reader.** `App` today, from its services. If `cmd/swamp` builds it and passes it in (as step 4 does with the nav table), `App` needs none of the services that remain after step 4.
- **The tick's source.** Today it's `App`'s instance, filtered by `model.From(a.instance)`. It becomes the reader's instance.

## Acceptance criteria

- [ ] `App` has no change-log fields or methods besides registering the reader.
- [ ] `logged_changes_test.go` and `change_events_test.go` pass. Any test that drove the tick through `App` drives the reader instead.
- [ ] Verification checklist clean.
