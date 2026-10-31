---
title: "[App simplification 4] Navigation is a table injected into New"
labels: [app-simplification]
depends_on: []
---

## Problem

`App` has eight `nav*` handlers, one per `Nav[T]`, each the same shape: an `onTop` guard, a switch on `(Op, Screen)` calling a constructor, then `unknownNav`. What opens which screen is spread over eight switches. `App` holds six services, `documents`, `syncer` and `openURL` only to pass them to those constructors.

## Decided

- **`Navigation` is generic: it knows screen kinds, not screens.** It owns the tabs, the stack and the routes that open screens, and is configured with tabs and tables. It holds no services, session state, payload or screen types, and never refers to `App`. `App` uses only its exported methods.
- **The tables are preconfigured in `Screens`,** which holds the services and the session state screens open with (`sized`, `exportDir`, `hideArchived`, kept from the messages that carry them), and gives the tabs and one table per payload.
- **`cmd/swamp` and the tests put them together:** a router, `NewScreens` (registered with the router), `NewNavigation(router, screens.Tabs(), screens.Tables()...)`, then `New(router, …, nav)`. RFC 0012's open question 3 ("Does `New` build the tabs, or take them?") is updated to match.
- **One typed map per `T`** (`Opens[T]`), not one untyped map, so a screen given the wrong payload doesn't compile (RFC 0012).
- **The header still reads screens by kind** (`Navigation.Screen` and type assertions), as before. Crumbs taken from the `Nav` payload when a screen opens are a follow-up (the step 3 redo).
- **Any screen in the table can be replaced,** as any can be pushed. The table is keyed by screen alone, with no list of allowed ops per entry. Today only `ScreenPostingDetail` and `ScreenDocumentReviewForm` accept `NavReplace`, and no test relies on any other being refused. What this gives up:
  - Replacing with a kind already open lower on the stack panics (`screenStack.push`) instead of being refused by `unknownNav`. Push already does this.
  - The table no longer records which replaces are meant. A comment on the table names the two that are.

## Change

One generic handler replaces the eight:

```go
func navTo[T message.Body](n *Navigation, table map[message.Screen]func(T) stackScreen) func(message.Event[message.Nav[T]]) tea.Cmd
```

It keeps the `onTop` guard and `unknownNav`, and `Navigation.Routes` has one per table. `newPostingDetail` and `newDocumentReviewForm` go.

The tabs, the stack, `tab`, `syncer`, `documents`, `openURL`, `exportDir` and `hideArchived` leave `App`. `App` keeps the services `WithChangeLog` needs (see step 5), the banner, and its own `sized`, which decides when to resend the size.

## Acceptance criteria

- [ ] One generic nav handler; no `switch` on `Screen` in `app.go`.
- [ ] `New` takes the table; `cmd/swamp` and the tests pass it in.
- [ ] A `Nav` for a screen missing from the table is reported, as `unknownNav` does today. Tested.
- [ ] A replace onto any screen in the table opens it. Tested.
- [ ] RFC 0012 open question 3 updated.
- [ ] Verification checklist clean.
