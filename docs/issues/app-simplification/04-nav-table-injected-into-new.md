---
title: "[App simplification 4] Navigation is a table injected into New"
labels: [app-simplification]
depends_on: []
---

## Problem

`App` has eight `nav*` handlers, one per `Nav[T]`, each the same shape: an `onTop` guard, a switch on `(Op, Screen)` calling a constructor, then `unknownNav`. What opens which screen is spread over eight switches. `App` holds six services, `documents`, `syncer` and `openURL` only to pass them to those constructors.

## Decided

- **The table is a package-level value passed into `New`,** not built inside `New`. Go has no constant maps, so it's a `var` (or a func returning one). `cmd/swamp` and the tests pass the same value. RFC 0012's open question 3 ("Does `New` build the tabs, or take them?") is updated to match.
- **Any screen in the table can be replaced,** as any can be pushed. The table is keyed by screen alone, with no list of allowed ops per entry. Today only `ScreenPostingDetail` and `ScreenDocumentReviewForm` accept `NavReplace`, and no test relies on any other being refused. What this gives up:
  - Replacing with a kind already open lower on the stack panics (`screenStack.push`) instead of being refused by `unknownNav`. Push already does this.
  - The table no longer records which replaces are meant. A comment on the table names the two that are.

## Change

One generic handler replaces the eight:

```go
func navTo[T any](a *App, opens map[message.Screen]func(screenDeps, T) stackScreen) func(message.Event[message.Nav[T]]) tea.Cmd
```

It keeps the `onTop` guard and `unknownNav`, and `Routes` registers one per `T`. `newPostingDetail` and `newDocumentReviewForm` go.

A table can't close over services or `App`'s live state, because it's built before either exists. So each entry takes what it needs as an argument (`screenDeps` above): the services and stores, plus the state the constructors read when the screen opens (`sized`, `exportDir`, `hideArchived`, `openURL`). The service fields leave `App` for that struct, apart from those `WithChangeLog` needs (see step 5).

## Decide before starting

- **One value holding a typed map per `T`, or one untyped map.** A struct of eight typed maps keeps handlers typed (RFC 0012: a handler wired to the wrong message doesn't compile). One `map[Screen]func(screenDeps, any)` is shorter, but brings back the type assertion the router removed.

## Acceptance criteria

- [ ] One generic nav handler; no `switch` on `Screen` in `app.go`.
- [ ] `New` takes the table; `cmd/swamp` and the tests pass it in.
- [ ] A `Nav` for a screen missing from the table is reported, as `unknownNav` does today. Tested.
- [ ] A replace onto any screen in the table opens it. Tested.
- [ ] RFC 0012 open question 3 updated.
- [ ] Verification checklist clean.
