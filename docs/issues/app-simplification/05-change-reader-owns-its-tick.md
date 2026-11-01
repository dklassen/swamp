---
title: "[App simplification 5] The change reader schedules its own tick"
labels: [app-simplification]
depends_on: []
---

## Problem

`App` runs the change reader's timer: `WithChangeLog` builds and registers the reader, while `changes`, `changePoll`, `pollChanges` and `changeTick` keep it ticking. The reader then reads on that tick. Two types share one job, and `App` keeps every service field partly so `WithChangeLog` can build the reader.

## Change

`changeReader` asks for its own next tick and reads on it. `App` registers it and batches its `Init`. `changes`, `changePoll`, `pollChanges` and `changeTick` leave `App`.

## Decided

- **`cmd/swamp` builds the reader,** with `NewChangeReader(services…, origin, since, poll)`, as it builds `Screens`. `App` holds none of the six services, nor `origin`.
- **`WithChangeLog` takes the reader,** registers it and has `Init` batch its `Init`. Bubble Tea calls only `App.Init`, so the reader can't start its own tick by being registered elsewhere. Tests without a change log don't call it.
- **The tick's source is the reader's instance,** and its `tick` route is `From` it.
- **The focus and size signals carry no `Process`.** `App` set it from `origin`, but nothing reads it there, and a signal has no writer.
- **Tests take the services from their setup,** not from `App`: `deleteTestApp`, `submitTestApp` and `newFullTestApp` return their `testutil.TestApp`, and `storeOf` goes.

## Acceptance criteria

- [ ] `App` has no change-log fields or methods besides registering the reader.
- [ ] `logged_changes_test.go` and `change_events_test.go` pass. Any test that drove the tick through `App` drives the reader instead.
- [ ] Verification checklist clean.
