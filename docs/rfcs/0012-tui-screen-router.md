# RFC 0012: The TUI's `App` becomes a router over screens that own their state

- **Status:** Draft
- **Date:** 2026-10-17 (revised 2026-10-19)
- **Related:** #55 (screen navigation and transition logic is scattered across two switches in `tui/app.go`); #89 (the return stack); #115 (screen instances); #41 (a stale error clears on a screen change); RFC 0008 (the change log, which each view follows); RFC 0013 (every routed message is an `Event[P]`, which replaces this RFC's message naming, `NavMsg`, `FocusChangedMsg`, `BannerMsg` and `ErrMsg`)
- **Bubble Tea:** v2 (`charm.land/bubbletea/v2`, `charm.land/bubbles/v2`). The first draft was written against v1. The text and the Template now use v2's interfaces.

## Summary

`tui.App` holds every screen's data, decides every transition, and handles every async result. `tui/app.go` is 1,968 lines. Its `App` struct has about 60 fields, and every screen's behaviour is spread over the screen's own file and two switches in `App` (#55).

This RFC proposes:

1. **`App` becomes a router.** It keeps what really belongs to every screen: the header and tabs, the banner, the terminal size, global keys, the navigation stack and the change-log cursors. It holds no screen's data.
2. **Each screen is a `SubComponent` that owns its own state**, handles its own async results and says where to go next. A `SubComponent` is a `tea.Model` that also registers routes.
3. **Screens register for the messages they handle.** A screen lists its routes in `Routes()`, one per message type, each made with `model.Always` or `model.WhenFocused` from a handler that takes that type. When the router opens a screen it registers them with `model.MsgRouter`, which keeps a lookup table from message type to routes. When a message arrives, every open screen with a route for its type gets it, whether or not that screen is on top. `WhenFocused` passes it only while the screen is in focus, which is how keys reach only the screen on top. Messages no handler took go to the top screen's `Update`.
4. **Navigation is a small, closed set of router operations** (push, pop, replace, switch tab). A screen asks for one by returning a `tea.Cmd`, as Elm's `Nav.pushUrl` does, and the router applies the message it produces. Each screen no longer has its own "intent" type that only `App` understands.
5. **Three packages.** The router stays in `tui`. Screens move to `tui/model`, and every message moves to `tui/message` (section 8).
6. **Work that isn't a screen is a `SubComponent` too.** Sync all and refresh are `model.Syncing`: it registers routes like a screen, but never goes on the stack, and the banner draws its view (section 6).

The router, `Router` and `SubComponent` come from the sketches this RFC started from. The routing is built (`tui/model/router.go`); the Template section has the stack, still to build. The rest of this document is where `tui/app.go` makes those sketches change shape.

## Decisions

| Question                                | Decision                                                                                                                                                                                                         |
| --------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| How many screens can handle one message | Any number. A message goes to every open screen that registered for its type, whether or not it's on top (section 1).                                                                                            |
| How a message finds its screens         | Screens register for it. Each screen's `Routes()` lists the message types it handles, so who handles a message is written next to how they handle it. The lookup is keyed by a `model.RouteID`, an opaque value made from the message's concrete type that serialises to the type's name. |
| Who gets a registered message          | Whoever has a route for its type, as the route allows: `model.Always` (every message, owner in focus or not) or `model.WhenFocused` (only while the owner's `Focused()` is true). Each route is made by one or the other, so there's no default, and the router knows nothing about which message types are input. A route may also take RFC 0013's filters on the event's `Meta` (section 2). |
| How a handler is registered             | As a route in `Routes()`, made with `model.Always(handler)` or `model.WhenFocused(owner, handler)`, where the handler takes the concrete message type it handles, such as `func(message.Event[message.Signal[message.ContentSize]]) tea.Cmd`. The route is that type, so a handler can't be registered for a message it doesn't take; that's checked by the compiler. `Register` refuses a handler that takes an interface, which has no concrete type to route by. If the router ever hands a route the wrong type, the route sends an `Error` with code `fault` (`message.Fail`) rather than panicking: `App` logs it to `swamp.log` and the banner shows only "internal error, see swamp.log" (section 2). |
| When a screen's routes go away          | When the screen closes. The router records the `Routable` that registered each route, not a screen kind, and `Deregister` drops them when it's popped or replaced (section 5).                                  |
| How a screen gets its instance          | It draws its own when it's built (`message.NewScreenInstance`) and exposes it as `Instance()`. `Register` refuses 0, or an instance it has seen, even after a `Deregister` (section 5). |
| Where navigation lives                  | On the router, as a stack. Screens ask to push, pop or replace, and never hold the stack or name the screen underneath them (section 3).                                                                         |
| How a screen asks to navigate           | By returning a `tea.Cmd` whose message the router applies, in line with the Elm architecture. The message names the screen that asked, and the router ignores it unless that screen is still on top (section 4). |
| Screens or children                     | Every screen is its own entry on the router's stack. No screen holds others as children (section 3).                                                                                                             |
| What a screen is                        | A pointer type that implements `tea.Model` and `model.Routable` (`Routes()` and `Instance()`). Handlers close over the pointer, so a screen is changed in place and is never copied (section 5).                |
| Where a screen's state changes          | Only on Bubble Tea's event loop: in `Init`, `Update`, `View` and handlers. A `tea.Cmd` captures copies of what it needs and reports back with a message, never through the receiver (section 5).                 |
| Where code lives                        | The router in `tui`, screens in `tui/model`, all messages in `tui/message` (section 8).                                                                                                                         |
| Which Bubble Tea                        | v2. A screen's `View` returns a `tea.View`; the router composes their `.Content` and sets the terminal options on its own. Keys are `tea.KeyPressMsg`, matched by `msg.String()`, and pastes are `tea.PasteMsg` (sections 2 and 6). |
| Where sync all and refresh live         | In `model.Syncing`, a `SubComponent` that isn't a screen. It owns the run, the refresh spinner, and refusing a refresh during a run; the banner draws its view. When a run or refresh finishes it broadcasts that, and the components that care reload themselves (section 6). |
| How messages are named                  | Every message in `tui/message` is an `Event[P]` of one of RFC 0013's primitives, such as `Event[Changed[Company]]` or `Event[Run[SyncAll]]`. The primitive says whether it's a request or a result; its content type names what it's about and drops `Msg` (section 8). |
| Where the styles live                   | In `tui/model`, exported and moved as one block, so the palette is in one place (section 8).                                                                                                                    |

## Problem

All of this is from reading `tui/app.go` and `tui/change_events.go` before this conversion began.

- **`App` owns every screen's data.** `companies`, `companyOpenPostings`, `postings`, `postingMarkup`, `applicationsByPosting`, `activeApplications`, `activeApplicationProgress`, `activeFilterDepartments`/`Locations`, `selectedCompany` and `hideArchived` all sit on `App`. Screens are handed a slice of it on every call, for example `companyList.Update(msg, a.companies)` and `postingList.View(postingListSnapshot{…})`.
- **One feature lives in four places** (#55): the screen emits an intent, `updateKeyMsg` interprets it and builds the next screen, the next screen's file handles its keys, and `update` applies the save's result. Setting an application's status touches `application_status.go`, `updateKeyMsg` (two entry points), `update`'s `applicationStatusUpdatedMsg` case, and `rebuildPostingDetailApplication`.
- **26 `a.screen == …` checks** in `app.go`, almost all asking "is the screen this result is for still up?" A result handler has to know which screens care about it and which one is showing.
- **26 writes to `a.err`.** Every handler decides for itself whether a success clears the banner (`a.err = msg.err`) or leaves another load's error in place (`setLoadErr`).
- **Transitions are spelled out per screen.** `returnStack` (#89) covers the screens with more than one entry point. The rest hard-code where esc goes (`a.screen = screenCompanyList`, `a.screen = screenApplicationDetail`, …).
- **Cross-cutting concerns switch on the screen.** Refitting heights after the banner changes (`Update`, `app.go:1041`), overlays (`View`/`beneath`), whether a screen follows the change log (`reloads`) and what it reloads (`reloadFor`) are each a switch over `screen`.
- **Screens can only be tested through `App`.** `app_test.go` is 3,981 lines and 126 tests, and asserts `app.screen` 292 times across the package's tests.

A new screen means a new `screen` constant, new fields on `App`, a case in `updateKeyMsg`, cases in `update` for its results, and cases in `body`, `Update`'s height refit, `reloads` and `reloadFor` where they apply. Nothing makes those agree.

## What the code says the router must handle

The sketches had a router that sends each message type to one screen, `ChangeScreenMsg{Target}` sent as a `tea.Cmd`, and a fallback that sends everything else to the current screen. Each point below is a case in today's code that shape doesn't cover.

### 1. One message, several screens

| Message                       | Screens it changes today (`app.go`)                                                                                                                |
| ----------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `companyCreatedMsg`           | company form (clears "checking board…", leaves), company list (adds the row) — 1068                                                                |
| `companyNameUpdatedMsg`       | company edit (clears saving, leaves), company list (replaces the row, re-sorts) — 1089                                                             |
| `applicationStatusUpdatedMsg` | status select (pops), submit (leaves with a status line), posting detail (rebuilds), active applications (reloads), `applicationsByPosting` — 1230 |
| `documentReviewsLoadedMsg`    | posting detail, application detail, the home screen's row for that application — 1399                                                              |
| `postingMarkupUpdatedMsg`     | posting list (drops an archived row), all postings (reloads) — 1145                                                                                |
| `applicationCreatedMsg`       | posting detail (rebuilds), active applications (reloads) — 1187                                                                                    |

A single `TargetScreen()` can only send each of these to one of the screens.

**So:** the lookup table maps a message type to a list of routes, and the router calls every one. The company form and the company list both route `Event[Changed[Company]]`: the form, filtered `From` its own instance, pops itself; the list adds the row (RFC 0013's Template has the list's handler).

The form leaves itself rather than naming the list as its destination. It doesn't need to know what's underneath it. Adding a screen that cares about a message means one more route in that screen's `Routes()`. Nothing changes where the message is created.

Handlers run in the order they were registered. `New` registers the persistent screens in a fixed order, not by ranging over a map, and a stacked screen's routes come after them. No handler should depend on that order, though. Each one changes only its own screen, and any navigation it asks for arrives later as a message of its own (section 4).

### 2. Screens update whether or not they're showing

Most of the 26 `a.screen == …` checks exist because only `App` holds the data, so a handler has to work out which screen's copy to patch. Once a screen owns its data, it applies a result to itself whether it's on top or not. `documentReviewsLoadedMsg` stops being three guarded branches. Posting detail, application detail and the home screen each register for it, check that the result is for their own application, and take it.

So a registered message can go to its handlers even while another screen is up. Whether it does is up to how the screen made the route:

- **`model.Always`**: every message of the type, whether or not the owner is in focus. A load's or a save's result, which a screen applies to itself while it's covered.
- **`model.WhenFocused`**: only while the owner is in focus, meaning it's the top of the stack. Typed input, which only the screen someone is looking at should act on: in Bubble Tea v2, `tea.KeyPressMsg` (matched with `msg.String()`, as in `"ctrl+c"`, `"tab"`, `"j"`) and `tea.PasteMsg`, which v2 delivers apart from keys. It takes the owner, a `model.Focusable`, and asks its `Focused()` when a message arrives.

There's no default, because either default has a silent failure. If "always" were the default, a screen that forgot to say "focus" on its key handler would act on keys typed into the screen on top of it. The company list's cursor would move while someone types into the company form. If "focus" were the default, a screen that forgot to say "always" on a result would miss it while covered, and the company list would be stale after a company was added. Making each route say which it is means there's no forgotten case.

Focus belongs to the route, not the message type, so the router needs no list of which messages are input. A new message type needs nothing from the router. A screen knows whether it's in focus from the `Event[Signal[FocusChanged]]` `App` sends whenever the top screen changes, which names the `ScreenInstance` now in focus, 0 for none (RFC 0013).

A route can also filter on an event's `Meta` with RFC 0013's `About`, `From` and `OnOp`, so a dialog hears only its own company's delete, or only its own request's failure, without looking at the payload.

`Register` refuses only what it couldn't route or tell apart: a nil owner, an instance of 0 or one it has seen, a nil or zero route, a handler that takes an interface such as `tea.KeyMsg` (its zero value is nil, so the route would have no type and never be called), and a filter on a type with no `Meta`. Any component may otherwise route any message type, either way. That includes a component that's never in focus, such as `model.Syncing`: its `Always` routes let it follow what other views do. A component that registers for what it can never get is a bug its own tests catch, not something the router guards against.

The handler takes the concrete type it handles, and `Always` and `WhenFocused` route by that type, so which message a handler gets is its own signature: wiring a handler to another message doesn't compile. The compiler doesn't catch a handler that takes a pointer when the message is sent as a value, a message nobody sends, or the wrong choice of `Always` or `WhenFocused`; those are for the component's tests.

The route still checks the message's type, since the router is what guarantees it. On a mismatch it returns `message.Fail` instead of panicking, an `Event[Error]` with code `fault`, for faults in the program: `App` logs it to `swamp.log`, where `cmd/swamp` sends `log` while the TUI owns the terminal, and the banner shows only "internal error, see swamp.log".

The top screen's `Update` gets a message when no handler was called for it, not when nothing registered for it. Otherwise a key whose only handler belongs to a screen in the background would go nowhere. `Update` still matters for Bubbles' own messages, such as a text input's cursor blink, and for a screen that takes keys there instead of through a handler.

### 3. Navigation is a stack

`returnStack` (#89) exists because posting detail, status select and document review have more than one entry point. `ChangeScreenMsg{Target}` names a destination, so "back to where I came from" can't be expressed. The router keeps the stack and offers:

| Operation       | Today's equivalent                                                        |
| --------------- | ------------------------------------------------------------------------- |
| `Push(screen, subject, value)`    | `enterFrom(next)`                                                         |
| `Pop`                             | `returnBack()`, and every hard-coded "esc goes to X"                      |
| `Replace(screen, subject, value)` | the document review picker replacing itself with the form (`app.go:1716`) |
| `Tab`                             | tab cycling between the three home tabs (`app.go:1479`)                   |

A screen that opens another has to give it its data: today `App` calls `newPostingDetailModel(…, p, app, hasApp, …)` with arguments from its own fields. `Push` carries the snapshot the new screen shows, from the screen that has it, and `App` builds the screen from that, with no read (RFC 0013, "Navigation without the model").

### 4. Navigation is a command

A screen asks to navigate the way it asks for anything else: it returns a `tea.Cmd`. The command produces an `Event[Nav[T]]` (`message.Push`, `message.Pop`), and the router applies it to the stack when it arrives. This is Elm's pattern: `Nav.pushUrl` returns a `Cmd`, and the page changes when the resulting message comes back through `update`. A screen's `Update` keeps Bubble Tea's shape, `(tea.Model, tea.Cmd)`, with no third return value.

A `Nav[T]` holds its `NavOp`, the `Screen` kind to open and `Value`, the snapshot of type `T` the screen shows; a pop, or a screen that shows nothing yet, carries `None`. The event's `Subject` is the entity the screen opens on, and its `Source.Instance` the screen that asked. It holds no model, so it stays plain data (RFC 0013).

Two things follow from the message arriving later, rather than in the `Update` call that asked for it:

- **The request names who asked.** By the time a `Nav` arrives, the user may already have left that screen, so popping blindly would pop the wrong one. The router acts on it only if `Source.Instance` is the screen on top, and drops it otherwise. That replaces the "only navigate if they're still here" checks (`app.go:1237`, `1341`, `1380`). A `Nav` for a screen the router doesn't open that way is a `fault`.
- **Keys typed in between go to the screen that's leaving.** Today every transition happens inside the `Update` call that caused it. Now a key already queued when the `Nav` is created is handled first, by the old screen. That's the accepted cost of the Elm shape.

### 5. Short-lived screens and stale results

Lists and tabs live for the whole session. Forms, pickers and confirmations are built each time they open and are gone once they close. #115 exists because a save from a form that was closed and reopened would otherwise act on the new form. The sketches build every screen once, so they have no way to tell two openings of the same form apart. And a handler left registered by a closed screen would still run on that screen's late results.

**So:** there are two kinds of screen:

- **Persistent:** built in `New` and always open. These are the three tabs, plus any screen that holds data for the session.
- **Stacked:** opened with `Push` and gone after `Pop`. A message for a screen that has closed reaches nothing, so a late save result for a closed form goes nowhere.

**The router opens a screen in a fixed order:**

1. It builds the screen from the `Nav`'s value; the screen draws its own `ScreenInstance` as it's built.
2. It calls `Register(screen)`, which records the screen itself as the owner of every route in its `Routes()`.
3. It calls `Init()`, so the results of the loads `Init` starts can't arrive before the screen's handlers are there to take them.

Closing a screen `Deregister`s every route it owns. A screen never deregisters anything itself, so it can't forget to.

A route records its owner as the `Routable` itself rather than its `message.Screen` kind, because not everything that registers is a screen: `model.Syncing` has no kind and is never on the stack (section 6). Removing a closed screen's routes compares owners, which works the same for both.

A screen draws its instance (`message.NewScreenInstance`: random, never 0, no shared counter) rather than being given one, so it has it from the start and a command can carry it without capturing the model. It puts it in the `Source` of every event it sends, and filters its results `From` it (section 4). Routes are found by message type, so a second opening of the same form registers for the same messages and would still get the first opening's late result without that filter. The router refuses an instance it has seen, even after a `Deregister`, so a reopened form can't hear the closed one's results.

Every open screen lives in one map, `screens`, keyed by kind (Template). That works because **a kind of screen is open at most once at a time.** That's true of today's code: following every transition in `updateKeyMsg`, no screen can open a chain of screens that leads back to its own kind. `open` panics on a kind that's already open. A new transition that breaks the rule then fails the first time it's tried, instead of quietly replacing the open screen and its handlers.

Each handler closes over its screen's pointer. The model the router keeps in `screens` and the one the handlers change have to be the same value, so **every screen is a pointer type**. When `Update` returns, the router stores whatever model came back, and for a screen that's the same pointer it was handed.

**A screen's state is only touched on Bubble Tea's event loop.** Bubble Tea calls `App.Init`, `Update` and `View` from one goroutine, and the router calls each screen's `Init`, `Update`, `View` and handlers from inside them, synchronously. Those are the only places a screen reads or writes its own fields. A `tea.Cmd` is different: Bubble Tea runs it on a goroutine of its own, while the loop goes on handling messages. Value receivers used to hide this, because a command closed over a copy. With pointer receivers, a command that touches the screen is a data race:

```go
// Races: m.cursor is read on the command's goroutine while Update moves it,
// and may index past the end of visible.
return m, func() tea.Msg {
 return message.Push(src, message.ScreenCompanyDelete, about, message.CompanyRow{Company: visible[m.cursor]})()
}

// Safe: Push's arguments are read now, on the loop, and its command holds copies.
return m, message.Push(src, message.ScreenCompanyDelete, about, message.CompanyRow{Company: visible[m.cursor]})
```

The company list was the first screen converted, and its first draft had exactly the racing form for every key.

So, for every screen:

- **A command captures values, never the receiver.** Compute what it needs before returning it. `message.Pop(message.Source{Instance: m.instance})` is safe, because `m.instance` is read when `Pop` is called, not when the command runs. A method value such as `return m, m.load` is not: `load` runs on another goroutine with `m` in hand. A method value on a value receiver is safe, because Go copies the value when the method value is made: `m.spinner.Tick` copies the Bubbles `spinner.Model`, on the loop. Loads stay free functions that take the services and IDs they need, such as `loadPostings(svc, companyID, hideArchived)`, as they are today.
- **A command's result changes the screen only by coming back as a message.** The command returns a message, and the handler for it, running on the loop, applies it. A command never sets a field, even one nothing else reads yet.
- **A slice or map handed on is copied.** Anything passed to a command or to a screen being pushed shares its backing array with the screen that passed it. `companyCreated` appends to and sorts `m.companies` in place, so a slice handed out without a copy changes under whoever holds it. The same goes for a pointer to anything a screen goes on changing. `R` on the company list is the worked example: it sends `Run[SyncAll]` with its own copy of the companies, so a reload landing mid-run can't change which companies the run syncs.

`go test -race` catches a command that breaks these rules once a test runs it alongside the screen's `Update`. Nothing in the type system or the linters does.

### 6. What stays with the router

These belong to every screen, so they stay on `App`:

- **The header, tabs and banner.** The banner hears every `Event[Error]` and `Event[Notify]` (RFC 0013). An `Error` shows its text, or, for code `fault`, is logged and shows only that there was one (section 2). A `Notify` (`message.ReportStatus`) replaces both the error and the status line, so `ReportStatus(src, "")` clears the banner. An error stays up until the next `Error` or `Notify`; a load that succeeds sends neither, so another load's failure stays. Clearing a stale error on a screen change (#41) is already the router's job.

  The banner draws the error, else `Syncing`'s view if it has one, else the status line. Because reports now arrive as messages, three things differ from when `App` set the fields directly. All three are accepted:
  - A sync-all run's final summary clears an error left on screen, so the summary shows. Before, the error hid it.
  - While a run is going, the banner shows its bar and progress. A status line from anything else, such as opening the browser, waits until the run ends. Before, it replaced the run's progress text until the next company started.
  - A banner update lands one message after the action that caused it.

  The tabs show a count for applications and all postings but not companies. The router doesn't read the company list's data, and the company count wasn't worth a message.
- **The terminal size.** The router owns `tea.WindowSizeMsg`. It works out the area left for a screen and routes it as an `Event[Signal[ContentSize]]`, which every screen routes with `Always`, so screens in the background, persistent and stacked, are already the right size when they come back on top. Two things differ from subtracting a fixed amount from `tea.WindowSizeMsg`:
  - **The rows taken aren't constant.** `screenRows()` (`app.go:1788`) counts the header's and the banner's lines. The banner grows and shrinks with errors, status lines, the sync-all bar and the refresh spinner, and none of those changes arrives as a `tea.WindowSizeMsg`. So after every message, the router compares the size it would send with the last one sent, and resends it if they differ. A window resize and the banner growing go through the same check. That replaces the `setHeight` switch at the end of `Update` (`app.go:1041`).
  - **A separate `ContentSize`, not a rewritten `tea.WindowSizeMsg`.** A screen that gets a `tea.WindowSizeMsg` can't tell the terminal's size from the area it's been given. With its own type, the terminal's size only ever means the terminal.

  `tea.ClearScreen` stays with the router (`app.go:1456`): it's about the terminal rewrapping the old frame, not about any screen.

- **Terminal options.** In Bubble Tea v2 a `View` is a `tea.View`, which carries the terminal's options (the alternate screen, cursor and mouse mode) as well as content. Only the router's `View` reaches the terminal, so the options are set there (`v.AltScreen = true`). A screen's `View` sets only `Content`, and the router ignores the rest.

- **Overlays.** A confirmation screen implements `Dialog`. The router draws the screen beneath it and then the box, replacing `beneath`.
- **Global keys.** ctrl+c, and tab on the three home tabs, both matched on `tea.KeyPressMsg` by `msg.String()`. Not `q`: the company form, notes, the application form and `/` search all take typed text, so a global `q` would quit while someone types. The home screen and the company list quit on `q` themselves today (`active_applications.go:107`, `company_list.go:79`), and they keep doing so by returning `tea.Quit`.
- **The change log.** The router keeps each view's place in the log (`seen`) and when to read. A screen that follows the log implements:

  ```go
  type ChangeFollower interface {
   ReloadFor(events []change.Event) tea.Cmd
  }
  ```

  `reloads(s)` becomes "implements `ChangeFollower`", and `reloadFor`'s switch moves into the six screens that follow the log. The interface has six implementations from the start.

Sync all and single-company refresh don't stay with the router, even though they draw in the banner. They're `model.Syncing`:

- **One model for both.** A refresh is refused while sync all runs, since the run may be syncing that company right now (#150), and both draw in the same banner slot. Keeping them together means the refusal is a check on the model's own state, not a question the router has to ask.
- **A `SubComponent` that isn't a screen.** `New` builds and registers it like a tab, but it has no `message.Screen` kind, is never on the stack, and is never in focus. Its routes are all `Always`, and it could follow other views' messages the same way (section 2). It routes `Run[SyncAll]` and `Run[SyncCompany]`, the `Ran` and `Error` outcomes of its own syncs (filtered `From` its instance), and `spinner.TickMsg`. Its `View` is the run's bar and progress line, or the refresh spinner, or empty, and `banner()` draws it.
- **It reports like a screen.** Summaries go to the banner as a `Notify`, and a refresh that didn't sync as an `Error`. Each company synced is a `Ran[SyncReport]` about that company, and the end of a run a `Ran[SyncAllReport]` (RFC 0013).
- **Screens start it with a message.** The company list's `R` sends `Run[SyncAll]` with its own copy of the companies (section 7). Its `r` and the posting list's `r` send `Run[SyncCompany]`.
- **The end of a sync is broadcast, and listeners act on it.** Each `Ran` goes to every component with a route for it. The company list and the posting list route it and reload themselves; the posting list filters it `About` its company. `App` reloads nothing and doesn't read `selectedCompany` to decide.
- **Spinner ticks.** `Syncing` registers for `spinner.TickMsg`, so every tick goes to it, and the spinner drops ticks with another spinner's ID. A screen with a spinner of its own would register for the tick too, and each would ignore the other's.

### 7. Shared data has one owner

Some data on `App` is read by more than one screen today. Screens then depend on `App` to coordinate them, where a message could let each one act on its own.

| Data                               | Read by                                                             | Owner after                                                                                                                                                                                           |
| ---------------------------------- | ------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `postings`                         | posting list, posting detail (previous/next, `canNavigateSiblings`) | posting list. Opening posting detail passes the posting IDs in list order. Opening it from all postings or application detail passes none, which is what `canNavigateSiblings` works out today (#93). |
| `applicationsByPosting`            | posting detail, seeded by application detail (`app.go:1541`)        | posting detail. Whoever opens it passes the application, if any. The map becomes the one posting's application.                                                                                       |
| `companies`, `companyOpenPostings` | company list, company delete (open count), sync all, the Companies tab's count | company list. Opening delete passes the count. Sync all gets its own copy in `Run[SyncAll]`. The Companies tab no longer shows a count (section 6).                                                |
| `exportDir`                        | export, submit                                                      | router, passed to both when they open. A remembered value for the session isn't any one screen's.                                                                                                     |
| `hideArchived`                     | posting list                                                        | router. It lives for the session and the posting list is stacked, so the router passes it in and keeps what the list sets (open question 1).                                                           |

### 8. Three packages

The router, the screens and the messages each get a package:

| Package       | Holds                                                                                                                                                                        | Imports                     |
| ------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------- |
| `tui`         | `App`: the stack, the lookup table, the header, tabs and banner, the change log                                                                                              | `tui/model`, `tui/message` |
| `tui/model`  | one exported type per screen, and `Syncing`; `Router`, `MsgRouter`, `Routable`, `SubComponent`, `Focusable`, `Dialog` and `ChangeFollower`, the types the router and screens meet at; `Route`, made only by `Always` and `WhenFocused`, its filters (`About`, `From`, `OnOp`), and `RouteID`/`RouteIDOf`; the styles and view helpers the screens draw with | `tui/message`               |
| `tui/message` | every message type, as RFC 0013's `Event[P]` and its primitives and bodies; `Screen` and `ScreenInstance`; the commands screens return to the router (`Push`, `Pop`, `ReportStatus`, `Fail`, …)                | the standard library, and Bubble Tea for the commands |

`tui/message` imports neither of the other two, so a screen and the router both depend on it and never on each other's internals. A `Nav` names a screen by `message.Screen`, which is why the screen kinds live there and are exported.

A message moves to `tui/message` when it's created, or when it's first touched by this refactor: when a screen sends it, registers for it, or handles it. Nothing moves just to move. Until a message moves it stays unexported in `tui`, and only the router can use it. `tui/message` imports no domain package, `tui` or `tui/model`; a test fails on any import but the standard library and Bubble Tea (RFC 0013).

When a message moves, it becomes an `Event[P]`: the primitive (`Changed`, `Read`, `Run`, `Ran`, `Error`, `Nav`, `Notify`, `Signal`, …) says whether it's a request or a result, and its content type says what it's about, without `Msg`. RFC 0013 lists what each of today's structs becomes.

Splitting the package also fixes the last point under Problem: a screen in `tui/model` is tested by calling its own `Update`, `View` and handlers, with no `App`. A test registers the screen with its own `model.NewMsgRouter()` and dispatches messages to it the way `App` would (`tui/model/syncing_test.go`). `App`'s own tests assert what the user sees, such as the banner line, the screen on top, and the commands that come back, never a screen's unexported fields.

What splitting the package costs:

- **Every interface method the router calls is exported.** A type in `tui/model` can't satisfy an interface whose method is unexported in `tui`, so `dialog()` and `reloadFor` become `Dialog()` and `ReloadFor`.
- **The helpers screens draw with move to `tui/model`:** `visibleWindow`, `tableRows`, `searchLine`, the column-width helpers and the styles. Anything the header or banner also uses is exported from `tui/model` for the router to call. The styles have already moved, as one block (`model.CursorStyle`, `model.DimStyle`, …), when `Syncing` became the first thing in `tui/model` to draw with them.

## Template

The routing is built: `tui/model/router.go` has `MsgRouter`, `Routable`, `Always`, `WhenFocused` and the filters, and RFC 0013's Template has the events, the banner's `Error` and `Notify`, and a screen's `Routes()`. What follows is the part still to build, the stack, in today's names. Bodies left as `…` are today's code moved.

A screen opens another by returning `Push` with what the new screen shows. Its loads go in its own `Init`, not in the opener's command:

```go
// posting list
case enterPostingDetail:
 return m, message.Push(m.source(), message.ScreenPostingDetail, about, message.PostingDetail{…})

// posting detail
func (m *PostingDetail) Init() tea.Cmd {
 return tea.Batch(loadApplication(…), loadDocumentReviews(…), loadPostingScores(…))
}
```

### The stack (`tui`)

```go
// stacked is one entry on the stack. The bottom entry is always the current
// tab, so the top is always a.stack[len(a.stack)-1].
type stacked struct {
 kind   message.Screen
 screen model.Routable
}

type App struct {
 router *model.MsgRouter
 // Every open screen: the three tabs, and whatever is stacked over the
 // current one. A kind of screen is open at most once (section 5).
 screens map[message.Screen]model.SubComponent
 // syncing runs sync all and refreshes; the banner draws its view.
 syncing *model.Syncing
 stack   []stacked
 // sized is the last size sent to the screens.
 sized message.ContentSize

 width, height int
 err           error
 status        string
 seen          map[message.Screen]int64
 // change-log fields, services the router itself uses
}

// One route per Nav[T], since each T is its own type, beside the banner's.
func (a *App) Routes() []*model.Route {
 return []*model.Route{
  model.Always(a.errorReported),
  model.Always(a.notified),
  model.Always(a.navNone),
  model.Always(a.navCompany),
  // …
 }
}

func (a *App) navCompany(e message.Event[message.Nav[message.Company]]) tea.Cmd {
 if !a.onTop(e.Meta) {
  return nil // the user left the screen that asked (section 4)
 }
 switch e.Payload.Screen {
 case message.ScreenCompanyEdit:
  return a.push(e.Payload.Screen, newCompanyEdit(…, e.Payload.Value))
 }
 return a.unknownNav(e)
}

// push makes next the top screen: routes, then Init (section 5).
func (a *App) push(kind message.Screen, next model.Routable) tea.Cmd {
 if _, open := a.screens[kind]; open {
  panic(fmt.Sprintf("tui: %v is already open", kind))
 }
 if err := a.router.Register(next); err != nil {
  return message.Fail(a.source(), err)
 }
 a.screens[kind] = next
 a.stack = append(a.stack, stacked{kind: kind, screen: next})
 return tea.Batch(next.Init(), a.focusChanged())
}

func (a *App) pop() tea.Cmd {
 if len(a.stack) == 1 {
  return nil // a tab never closes
 }
 top := a.top()
 if err := a.router.Deregister(top.screen); err != nil {
  return message.Fail(a.source(), err)
 }
 delete(a.screens, top.kind)
 a.stack = a.stack[:len(a.stack)-1]
 a.err = nil // a stale error from the screen just left (#41)
 return a.focusChanged()
}

// focusChanged tells every screen which is on top now; WhenFocused routes
// read it through Focused().
func (a *App) focusChanged() tea.Cmd {
 return a.router.Dispatch(message.New(a.source(), message.Subject{}, message.OpNone, time.Now().UTC(),
  message.Signal[message.FocusChanged]{Value: message.FocusChanged{Focused: a.top().screen.Instance()}}))
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
 cmd, handled := a.handle(msg) // ctrl+c, tab, the window's size, the change log
 if !handled {
  cmd = tea.Batch(cmd, a.route(msg))
 }
 // The window or the banner may have changed size, on any message.
 if size := (message.ContentSize{Width: a.width, Rows: a.screenRows()}); size != a.sized {
  a.sized = size
  cmd = tea.Batch(cmd, a.router.Dispatch(message.New(a.source(), message.Subject{}, message.OpNone, time.Now().UTC(),
   message.Signal[message.ContentSize]{Value: size})))
 }
 return a, cmd
}

// route delivers msg to every route for its type that lets it through, and
// to the top screen's Update if none was called (section 2).
func (a *App) route(msg tea.Msg) tea.Cmd { … }

// View composes the screens' content strings: in Bubble Tea v2 a View is a
// tea.View, and only the router's goes to the terminal.
func (a *App) View() tea.View {
 top := a.top().kind
 body := a.screens[top].View().Content
 if _, ok := a.screens[top].(model.Dialog); ok {
  beneath := a.stack[len(a.stack)-2].kind
  body = overlay(a.screens[beneath].View().Content, body, a.width)
 }
 v := tea.NewView(a.header() + body + a.banner())
 v.AltScreen = true // terminal options are the router's (section 6)
 return v
}
```

### The change log

```go
// tui/model

// ChangeFollower is a screen that reloads when other processes' changes
// touch what it shows.
type ChangeFollower interface {
 ReloadFor(events []change.Event) tea.Cmd
}
```

```go
// tui
func (a *App) handleChanges(msg changesMsg) tea.Cmd {
 … // move the top screen's place in the log, as today
 if f, ok := a.screens[a.top().kind].(model.ChangeFollower); ok {
  return tea.Batch(f.ReloadFor(others), next)
 }
 return next
}

// activate catches the screen now on top up on what changed while it wasn't.
func (a *App) activate() tea.Cmd {
 if _, ok := a.screens[a.top().kind].(model.ChangeFollower); !ok {
  return nil
 }
 return readChanges(a.changeLogService, a.top().kind, a.cursor(a.top().kind), false)
}
```

Application detail also reloads every time it comes back on top, because its documents are files that change with no event. That stays a special case in `activate`, as it is today.

How this differs from the sketches:

- **Several screens can register for one message type,** because messages already go to several screens (section 1). The lookup holds a list of handlers, not one.
- **The router records who registered each handler and drops it on close,** so a late result for a closed screen reaches nothing, and no `Update` is called on a missing map entry. It records the registering `Routable` itself, so a component with no screen kind can register too (sections 5 and 6).
- **Routes are registered when a screen opens, not once at start-up.** Stacked screens come and go, so `push` registers and `pop` deregisters. `New` registers the tabs once, and `Init` doesn't register again.
- **A screen draws its own instance** when it's built, and is registered before `Init`. `Init` is `tea.Model`'s and takes nothing, and a screen whose loads started before its handlers existed would lose their results (section 5).
- **Delivery depends on how each route was made.** `model.Always` fans a message out to every handler; `model.WhenFocused` delivers only while its owner is in focus. That's how keys and pastes reach only the screen on top, with no list of input types in the router (section 2).
- **`ChangeScreenMsg{Target}` becomes `Event[Nav[T]]`,** which operates on the stack instead of naming a destination, names the screen that asked, and carries what the opened screen shows (sections 3 and 4).
- **The router does its part of a message, then routes it,** unless no screen needs it. The banner hears every `Error` and `Notify`, which still reach the screens with routes for them.
- **Work that isn't a screen registers like one.** `model.Syncing` registers for its messages, including its spinner's tick, and the router draws its view in the banner. The sketches had the router own that work and its Bubbles messages (section 6).
- **Size is resent whenever it changes,** compared after every message, so a banner growing or shrinking resizes the screens as a window resize does (section 6).
- **Bubble Tea v2, not v1.** A screen's `View` returns a `tea.View`, not a string, and the router composes `.Content` and sets the terminal options on its own view. Keys are `tea.KeyPressMsg`, matched by `msg.String()`, not `msg.Type`, and pastes arrive as `tea.PasteMsg` (sections 2 and 6).
- **Only ctrl+c quits globally,** not `q` (section 6).
- **Handlers take their message's concrete type, made into a route with `model.Always` or `model.WhenFocused`,** which routes by that type, so a handler wired to the wrong message doesn't compile. The sketches' handlers took `tea.Msg` and asserted, which panics on a mismatch. A mismatch the router causes sends an `Error` with code `fault`, which `App` logs (section 2).
- **Routes are keyed by an opaque `model.RouteID`, not a bare string.** `model.RouteIDOf` is the only way to make one, so its insides can change without touching `Register`, `Dispatch` or `Deregister`. It holds the message's `reflect.Type`: a key by type identity, where `%T`'s string uses the package's name rather than its path and so would collide for same-named types in two packages of the same name. It also skips building a string for every message. `String()` serialises it to the name `%T` prints, such as `tea.KeyPressMsg`, for logs and errors.

## Open questions

1. **Which screens are persistent?** Decided: the three tabs. The posting list is stacked, built for each company it opens on, and `hideArchived` moves to the router, which passes it in and keeps what the list last set.
2. **Where do overlays live?** This RFC makes them stacked screens that report they're overlays. The alternative is a separate overlay slot on the router, which is simpler while there's never more than one at a time.
3. **Does `New` build the tabs, or take them?** `New` builds them from the services it's given, as today. Passing them in instead would let tests choose which screens exist. But `cmd/swamp` would then have to build every screen, and each test needs fresh screens, never a shared package-level map.

## Progress

Where the conversion stands. Each step keeps `go build`, `go test`, the linter and `gofmt` clean.

| Step | What changed |
| ---- | ------------ |
| Company list registers its routes | The company list owns its companies and open counts, and registers for `CompaniesLoadedMsg` and the create, delete and rename results. `App` gets the minimal router: `routes`, `Register`, `routeIDOf`, `route`, and resending `ContentSizeMsg` whenever the size changes. The list's keys return commands that carry copies. |
| No company count on the tab | The router stops reading the list's companies for the header (section 6). |
| Sync all and refresh leave `App` | The sync model owns the run, the refresh and the spinner, and the banner draws its view (section 6). The company messages move to `tui/message` and are renamed (section 8). |
| `tui/model` begins | `Syncing`, the styles, the router interfaces (`Router`, `HandlerFunc`, `SubComponent`) and the lookup table's entry types (`Route{Owner, Handle}`, `RouteID`, `RouteIDOf`, all exported) move to `tui/model`. The posting list's `r` sends `RefreshCompanyMsg` instead of calling the model. `App`'s tests stop reading the model's state, and `Syncing` gets tests of its own. |
| `RouteID` becomes opaque | `model.RouteID` is a struct holding the message's `reflect.Type`, made only by `RouteIDOf`, with `String()` giving the `%T` name. Callers are unchanged: it's still comparable, so still a map key. Tested in `tui/model/router_test.go`. |
| Handlers are scoped | `Register(msg, scope, handler)` takes a required `model.Scope`: `AlwaysScope` or `FocusScope`. `route` calls a handler only when `Route.Accepts` the component in focus, which until the stack exists is the converted screen `a.currentScreen` names, so only the company list. `Register` refuses nothing. The company list's and `Syncing`'s registrations are all `AlwaysScope`. |
| `a.screen` becomes `a.currentScreen` | The field names the screen showing, the one `focused()` and `FocusScope` go by. |
| Company list keys are routed | The company list registers `tea.KeyPressMsg` in `FocusScope` with its `keyPressed` handler, and `updateKeyMsg` loses its company-list case. Its `Update` keeps only pastes and the size. Tested by `TestApp_CompanyList_GetsEachKeyOnce`: with both paths in place, enter came back as a batch of two commands. |
| Company list size is routed | `App` routes the `ContentSizeMsg` it resends instead of calling the list's `Update`, and the list registers for it in `AlwaysScope`, so it's sized while another tab is up. The Template's `resize` goes; its `Update` routes the size too. Tested by the company-list cases of `TestApp_Screens_FitTheTerminalUnderTheBanner`, which failed with the size routed and nothing registered. |
| Handlers are typed | `model.Handle(r, scope, handler)` registers a handler that takes its concrete message type, under that type, and refuses one that takes an interface. Every handler on the company list and `Syncing` is registered with it and loses its `tea.Msg` parameter and type assertion. A route handed the wrong type sends the new `message.ErrMsg` (`message.Fail`), which `App.handle` logs to `swamp.log`, showing "internal error, see swamp.log". `Syncing`'s `RefreshCompanyMsg` closure becomes the method `startRefresh`, and `tick` no longer passes any message on to the spinner. The list's `sized` is renamed `resize`. Tested by the `TestHandle_*` tests, `TestApp_ErrMsg_ShowsTheInternalErrorBanner`, and `TestCompanyListModel_Handlers_FailOnTheWrongMessage` and `TestSyncing_Handlers_FailOnTheWrongMessage`, which panicked before the checks. |
| Company list pastes are routed | The list registers `tea.PasteMsg` in `FocusScope` with `model.Handle`, so `paste` returns a `tea.Cmd`, and `App.paste` loses its company-list case. The list's `Update` now does nothing: everything it handles is registered. Tested by `TestApp_CompanyList_PasteIntoSearch_FiltersByIt`, which failed with both paths in place, the paste typed twice into the search. |
| Company delete is routed | The confirmation is a `SubComponent` that App registers on `OpenCompanyDeleteMsg` and deregisters on `CloseCompanyDeleteMsg`, built with the current width since it misses the last `ContentSizeMsg`. Its keys are `WhenFocused`; `y` sends the delete itself. It closes on its own company's `CompanyDeletedMsg` only, and the company list reports that result to the banner, so App no longer handles it. Focus moves both ways through the `FocusChangedMsg` App sends on any screen change. Tested by `TestCompanyDeleteModel_*`, `TestApp_CompanyDeleteConfirmation_TwoQuickNs_CloseItOnce`, `…_HoldsTheListsKeysUntilItCloses` and `TestApp_CompanyDeleteFails_ShowsTheErrorOnTheList`. |
| Company form and edit are routed | As the delete confirmation: App registers each on its `Open…` request and deregisters it on `CloseCompanyFormMsg` or `CloseCompanyEditMsg`, which esc and a successful save send. Keys and pastes are `WhenFocused`. A create or rename result carries `From`, the instance that asked, and the form or edit acts only on its own. The company list reports the result to the banner, so App no longer handles it; esc sends an empty notice, so a failed save's error goes with the form. Tested by `TestCompanyFormModel_*`, `TestCompanyEditModel_*` and `TestApp_CompanyRenameFails_ShowsTheErrorAndKeepsTheEditOpen`. |
| Company navigation is `Event[Nav[T]]` | The seven `Open…` and `Close…` structs become RFC 0013's `Nav[T]` (`message.Push`, `message.Pop`), which carries the snapshot the screen opens on, so `App` builds a screen from the message without a read. `App` acts on a `Nav` only from the screen on top, section 4's `From` check, which replaces the second-esc guards. Tested by `TestCompanyListModel_KeysPushScreensWithWhatTheyShow`, `TestApp_NavFromAModelNoLongerOnTop_IsDropped` and `TestApp_CompanyDeleteConfirmation_TwoQuickNs_CloseItOnce`. |
| Names follow RFC 0013 | The rows above use the names of their time. `Register(msg, scope, handler)`, `model.Handle` and the scopes are now `Routes()` with `model.Always`/`model.WhenFocused` on `model.MsgRouter`; screens draw their own instance; every `…Msg` in `tui/message` is an `Event[P]`. |

While screens are partly converted, the code works in ways the Template doesn't:

- **`App` still does its own part of a routed message.** `Update` routes a message to its registered handlers and also runs `App`'s old `update` switch on it, which still sets `a.err` and `a.currentScreen` for the screens not yet converted.
- **Tab reaches the company list before `App` switches tabs.** `route` runs before the old `updateKeyMsg`, which still handles tab, so the list's key handler sees tab first and ignores it. A tab press has no `Text` in v2, so an open search doesn't type it. In the Template, `handle` takes tab before `route`.
- **The stack holds only converted screens.** Each records the `currentScreen` it was pushed over and `pop` puts it back; screens on `App`'s old paths still go by `currentScreen`, and focus is sent when `currentScreen` or the screen in focus changes (a replace keeps `currentScreen`) rather than by `push`, `pop` and `replace`. `New` sends it once for the home list, which is up first.
- **Old screens and pushed ones interleave.** The review screens open over posting or application detail, and submit, the application form and delete over application detail, by `currentScreen`; while one is up the detail is out of focus, and `App` reads the detail on the stack for the change log and the breadcrumb. A submit or delete that finishes pops application detail on its way home. The home list and application detail ask `App` for its screens with a command, which `App` drops if the asker is no longer up.
- **Screens not yet converted are still `App`'s fields,** called directly, and the company list is still a typed field, not an entry in `screens`.
- **App tests wait a message for a converted screen's intent.** A converted screen's key comes back as a command, so tests that press one use `sendIntentKey`, which delivers the intent, or `applyCmd`. This is section 4's "keys typed in between" cost, as seen from a test.

`go test -race ./tui/...` is clean through "Company list keys are routed". Last run against real data after "Company list registers its routes". Resizing the window on the Companies tab checked by hand after "Company list size is routed".


## Out of scope

- **Changing any screen's behaviour, keys or layout,** except the banner differences that come from reporting through messages (section 6).
- **#283 (a live-updates indicator) and #284/#285 (posting changes reaching detail screens).** They may get easier once screens own their data, but they're separate changes.
- **Splitting `tui/model` further,** for example a package per screen. Three packages are enough to separate the router, the screens and the messages (section 8).
