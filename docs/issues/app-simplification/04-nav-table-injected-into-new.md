---
title: "[App simplification 4] Navigation is a table injected into New"
labels: [app-simplification]
depends_on: []
---

## Problem

`App` has eight `nav*` handlers, one per `Nav[T]`, each the same shape: an `onTop` guard, a switch on `(Op, Screen)` calling a constructor, then `unknownNav`. What opens which screen is spread over eight switches. `App` holds six services, `documents`, `syncer` and `openURL` only to pass them to those constructors.

## Decided

- **The table is a `Navigation` passed into `New`,** not built inside `New`. `cmd/swamp` and the tests build it with `NewNavigation`. RFC 0012's open question 3 ("Does `New` build the tabs, or take them?") is updated to match.
- **`Navigation` has one owner for everything it reads.** It's built with the services and stores its screens use, and keeps its own copy of the session state they open with (`sized`, `exportDir`, `hideArchived`) from the messages that carry it. Nothing is shared with `App`: a struct both used would change for two reasons.
- **One typed map per `T`,** not one untyped map, so a screen given the wrong payload doesn't compile (RFC 0012).
- **Any screen in the table can be replaced,** as any can be pushed. The table is keyed by screen alone, with no list of allowed ops per entry. Today only `ScreenPostingDetail` and `ScreenDocumentReviewForm` accept `NavReplace`, and no test relies on any other being refused. What this gives up:
  - Replacing with a kind already open lower on the stack panics (`screenStack.push`) instead of being refused by `unknownNav`. Push already does this.
  - The table no longer records which replaces are meant. A comment on the table names the two that are.

## Change

One generic handler replaces the eight:

```go
func navTo[T message.Body](a *App, table opens[T]) func(message.Event[message.Nav[T]]) tea.Cmd
```

It keeps the `onTop` guard and `unknownNav`, and `Routes` registers one per `T`. `newPostingDetail` and `newDocumentReviewForm` go.

`syncer`, `documents`, `openURL`, `exportDir` and `hideArchived` leave `App` for `Navigation`. `App` keeps the services `WithChangeLog` needs (see step 5) and its own `sized`, which decides when to resend the size.

## Acceptance criteria

- [ ] One generic nav handler; no `switch` on `Screen` in `app.go`.
- [ ] `New` takes the table; `cmd/swamp` and the tests pass it in.
- [ ] A `Nav` for a screen missing from the table is reported, as `unknownNav` does today. Tested.
- [ ] A replace onto any screen in the table opens it. Tested.
- [ ] RFC 0012 open question 3 updated.
- [ ] Verification checklist clean.
