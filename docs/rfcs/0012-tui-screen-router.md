# RFC 0012: The TUI's `App` becomes a router over screens that own their state

- **Status:** Draft
- **Date:** 2026-10-17 (revised 2026-10-19)
- **Related:** #55 (screen navigation and transition logic is scattered across two switches in `tui/app.go`); #89 (the return stack); #115 (screen instances); #41 (a stale error clears on a screen change); RFC 0008 (the change log, which each view follows)
- **Bubble Tea:** v2 (`charm.land/bubbletea/v2`, `charm.land/bubbles/v2`). The first draft was written against v1. The text and the Template now use v2's interfaces.

## Summary

`tui.App` holds every screen's data, decides every transition, and handles every async result. `tui/app.go` is 1,968 lines. Its `App` struct has about 60 fields, and every screen's behaviour is spread over the screen's own file and two switches in `App` (#55).

This RFC proposes:

1. **`App` becomes a router.** It keeps what really belongs to every screen: the header and tabs, the banner, the terminal size, global keys, the navigation stack and the change-log cursors. It holds no screen's data.
2. **Each screen is a `SubComponent` that owns its own state**, handles its own async results and says where to go next. A `SubComponent` is a `tea.Model` that also registers routes.
3. **Screens register for the messages they handle.** When the router opens a screen, the screen calls `model.Handle` once for each message type it wants, passing a handler that takes that type. The router keeps a lookup table from message type to handlers. When a message arrives, every open screen that registered for its type gets it, whether or not that screen is on top. Each registration has a scope: always, or only while the screen is in focus, which is how keys reach only the screen on top. Messages no handler took go to the top screen's `Update`.
4. **Navigation is a small, closed set of router operations** (push, pop, replace, switch tab). A screen asks for one by returning a `tea.Cmd`, as Elm's `Nav.pushUrl` does, and the router applies the message it produces. Each screen no longer has its own "intent" type that only `App` understands.
5. **Three packages.** The router stays in `tui`. Screens move to `tui/model`, and every message moves to `tui/message` (section 8).
6. **Work that isn't a screen is a `SubComponent` too.** Sync all and refresh are `model.Syncing`: it registers routes like a screen, but never goes on the stack, and the banner draws its view (section 6).

The router, `Router` and `SubComponent` come from the sketches this RFC started from. The Template section has the routing as the code to start from. The rest of this document is where `tui/app.go` makes those sketches change shape.

## Decisions

| Question                                | Decision                                                                                                                                                                                                         |
| --------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| How many screens can handle one message | Any number. A message goes to every open screen that registered for its type, whether or not it's on top (section 1).                                                                                            |
| How a message finds its screens         | Screens register for it. Each screen's `RegisterRoutes` lists the message types it handles, so who handles a message is written next to how they handle it. The lookup is keyed by a `model.RouteID`, an opaque value made from the message's concrete type that serialises to the type's name. |
| Who gets a registered message          | Whoever registered for its type, as the registration's scope allows: `model.AlwaysScope` (every message, owner in focus or not) or `model.FocusScope` (only while the owner is the top of the stack). The scope is a required argument to `Register`, with no default, and the router knows nothing about which message types are input. `Register` refuses nothing, so a component that's never in focus can still follow other views' messages in `AlwaysScope` (section 2). |
| How a handler is registered             | With `model.Handle(r, scope, handler)`, where the handler takes the concrete message type it handles, such as `func(message.ContentSizeMsg) tea.Cmd`. The route is that type, so a handler can't be registered for a message it doesn't take; that's checked by the compiler. `Handle` refuses a handler that takes an interface, which has no concrete type to route by. If the router ever hands a route the wrong type, the route sends `message.ErrMsg` rather than panicking. `ErrMsg` is for faults in the program, not for the user: the router logs the wrapped error to `swamp.log` and the banner shows only "internal error, see swamp.log". `BannerMsg` stays the way to tell the user something (section 2). |
| When a screen's routes go away          | When the screen closes. The router records the `SubComponent` that registered each handler, not a screen kind, and drops its handlers when it's popped or replaced (section 5).                                  |
| Where navigation lives                  | On the router, as a stack. Screens ask to push, pop or replace, and never hold the stack or name the screen underneath them (section 3).                                                                         |
| How a screen asks to navigate           | By returning a `tea.Cmd` whose message the router applies, in line with the Elm architecture. The message names the screen that asked, and the router ignores it unless that screen is still on top (section 4). |
| Screens or children                     | Every screen is its own entry on the router's stack. No screen holds others as children (section 3).                                                                                                             |
| What a screen is                        | A pointer type that implements `tea.Model` and `RegisterRoutes`. Handlers close over the pointer, so a screen is changed in place and is never copied (section 5).                                               |
| Where a screen's state changes          | Only on Bubble Tea's event loop: in `Init`, `Update`, `View` and handlers. A `tea.Cmd` captures copies of what it needs and reports back with a message, never through the receiver (section 5).                 |
| Where code lives                        | The router in `tui`, screens in `tui/model`, all messages in `tui/message` (section 8).                                                                                                                         |
| Which Bubble Tea                        | v2. A screen's `View` returns a `tea.View`; the router composes their `.Content` and sets the terminal options on its own. Keys are `tea.KeyPressMsg`, matched by `msg.String()`, and pastes are `tea.PasteMsg` (sections 2 and 6). |
| Where sync all and refresh live         | In `model.Syncing`, a `SubComponent` that isn't a screen. It owns the run, the refresh spinner, and refusing a refresh during a run; the banner draws its view. When a run or refresh finishes it broadcasts that, and the components that care reload themselves (section 6). |
| How messages are named                  | A request says what it asks for, in the imperative (`OpenCompanyEditMsg`, `ToggleSyncAllMsg`, `RefreshCompanyMsg`). A result says what happened, in the past tense (`CompanyCreatedMsg`, `CompaniesLoadedMsg`) (section 8). |
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

**So:** the lookup table maps a message type to a list of handlers, and the router calls every one. The company form and the company list both register for `message.CompanyCreatedMsg`:

```go
// company form
func (m *CompanyForm) RegisterRoutes(r Router, id message.ScreenInstance) error {
 m.id = id
 return model.Handle(r, model.AlwaysScope, m.companyCreated)
}

func (m *CompanyForm) companyCreated(created message.CompanyCreatedMsg) tea.Cmd {
 m.checking = false
 if created.Err != nil {
  return message.ReportErr(created.Err)
 }
 return message.Pop(m.id)
}

// company list
func (m *CompanyList) RegisterRoutes(r Router, id message.ScreenInstance) error {
 m.id = id
 return errors.Join(
  model.Handle(r, model.AlwaysScope, m.companiesLoaded),
  model.Handle(r, model.AlwaysScope, m.companyCreated),
 )
}

func (m *CompanyList) companyCreated(created message.CompanyCreatedMsg) tea.Cmd {
 if created.Err == nil {
  m.companies = append(m.companies, created.Company)
  sortCompaniesByName(m.companies)
 }
 return nil
}
```

The form leaves itself rather than naming the list as its destination. It doesn't need to know what's underneath it. Adding a screen that cares about a message means one more `Handle` call in that screen. Nothing changes where the message is created.

Handlers run in the order they were registered. `New` registers the persistent screens in a fixed order, not by ranging over a map, and a stacked screen's handlers come after them. No handler should depend on that order, though. Each one changes only its own screen, and any navigation it asks for arrives later as a message of its own (section 4).

### 2. Screens update whether or not they're showing

Most of the 26 `a.screen == …` checks exist because only `App` holds the data, so a handler has to work out which screen's copy to patch. Once a screen owns its data, it applies a result to itself whether it's on top or not. `documentReviewsLoadedMsg` stops being three guarded branches. Posting detail, application detail and the home screen each register for it, check that the result is for their own application, and take it.

So a registered message can go to its handlers even while another screen is up. Whether it does is up to the handler's **scope**, which the screen gives when it registers:

- **`model.AlwaysScope`**: every message of the type, whether or not the owner is in focus. A load's or a save's result, which a screen applies to itself while it's covered.
- **`model.FocusScope`**: only while the owner is in focus, meaning it's the top of the stack. Typed input, which only the screen someone is looking at should act on: in Bubble Tea v2, `tea.KeyPressMsg` (matched with `msg.String()`, as in `"ctrl+c"`, `"tab"`, `"j"`) and `tea.PasteMsg`, which v2 delivers apart from keys.

The scope is a required argument to `Register`, with no default, because either default has a silent failure. If "always" were the default, a screen that forgot to say "focus" on its key handler would act on keys typed into the screen on top of it. The company list's cursor would move while someone types into the company form. If "focus" were the default, a screen that forgot to say "always" on a result would miss it while covered, and the company list would be stale after a company was added. Making each registration state its scope means there's no forgotten case.

The scope belongs to the registration, not the message type, so the router needs no list of which messages are input. To decide whether a handler gets a message, it needs only the route table, each route's owner and scope, and which component is in focus (`Route.Accepts`). A new message type needs nothing from the router. An earlier draft had `Register` refuse typed input outright, but that was a ban standing in for a routing rule the Template didn't yet have.

`Register` refuses nothing: any component may register for any message type in either scope. That includes a component that's never in focus, such as `model.Syncing`. Its `FocusScope` registrations would never run, but its `AlwaysScope` ones let it follow what other views do, and that's a reason to listen to messages that aren't its own. A component that registers for what it can never get is a bug its own tests catch, not something the router guards against. `model.Handle` refuses only a handler it couldn't route at all (below).

A screen registers each handler with `model.Handle(r, scope, handler)`, not `Router.Register` directly. The handler takes the concrete type it handles, and `Handle` registers under that type, so which message a handler gets is its own signature: wiring a handler to another message doesn't compile. Go has no generic methods, so `Handle` is a function over `Router`, and `Register` keeps taking a `tea.Msg` and a `HandlerFunc`. `Handle` returns an error for a handler that takes an interface such as `tea.KeyMsg`: its zero value is nil, so the route would have no type and never be called. The compiler doesn't catch a handler that takes a pointer when the message is sent as a value, a message nobody sends, or the wrong scope; those are for the component's tests.

The route `Handle` registers still checks the message's type, since the router is what guarantees it. On a mismatch it returns `message.Fail(err)` instead of panicking. `Fail` sends a `message.ErrMsg`, the message for faults in the program: the router logs the wrapped error to `swamp.log`, where `cmd/swamp` sends `log` while the TUI owns the terminal, and the banner shows only "internal error, see swamp.log". It isn't a `BannerMsg`, which is for things the user can act on.

The top screen's `Update` gets a message when no handler was called for it, not when nothing registered for it. Otherwise a key whose only handler belongs to a screen in the background would go nowhere. `Update` still matters for Bubbles' own messages, such as a text input's cursor blink, and for a screen that takes keys there instead of through a handler.

### 3. Navigation is a stack

`returnStack` (#89) exists because posting detail, status select and document review have more than one entry point. `ChangeScreenMsg{Target}` names a destination, so "back to where I came from" can't be expressed. The router keeps the stack and offers:

| Operation       | Today's equivalent                                                        |
| --------------- | ------------------------------------------------------------------------- |
| `Push(next)`    | `enterFrom(next)`                                                         |
| `Pop`           | `returnBack()`, and every hard-coded "esc goes to X"                      |
| `Replace(next)` | the document review picker replacing itself with the form (`app.go:1716`) |
| `Tab(id)`       | tab cycling between the three home tabs (`app.go:1479`)                   |

A screen that opens another has to give it its data: today `App` calls `newPostingDetailModel(…, p, app, hasApp, …)` with arguments from its own fields. `Push` takes the new screen's model, already built, so the caller passes what the new screen needs directly. `App` doesn't have to know any screen's arguments.

### 4. Navigation is a command

A screen asks to navigate the way it asks for anything else: it returns a `tea.Cmd`. The command produces a `message.NavMsg`, and the router applies it to the stack when it arrives. This is Elm's pattern: `Nav.pushUrl` returns a `Cmd`, and the page changes when the resulting message comes back through `update`. A screen's `Update` keeps Bubble Tea's shape, `(tea.Model, tea.Cmd)`, with no third return value.

```go
type NavMsg struct {
 From ScreenInstance // the screen that asked
 Op   NavOp          // push, pop, replace, tab
 Kind Screen         // the screen to open, for push and replace
 Next SubComponent
}

func Pop(from ScreenInstance) tea.Cmd {
 return func() tea.Msg { return NavMsg{From: from, Op: NavPop} }
}
```

Two things follow from the message arriving later, rather than in the `Update` call that asked for it:

- **The request names who asked.** By the time a `NavMsg` arrives, the user may already have left that screen, so popping blindly would pop the wrong one. `From` is the asking screen's instance, and the router ignores the message unless that screen is on top. That replaces the "only navigate if they're still here" checks (`app.go:1237`, `1341`, `1380`).
- **Keys typed in between go to the screen that's leaving.** Today every transition happens inside the `Update` call that caused it. Now a key already queued when the `NavMsg` is created is handled first, by the old screen. That's the accepted cost of the Elm shape.

### 5. Short-lived screens and stale results

Lists and tabs live for the whole session. Forms, pickers and confirmations are built each time they open and are gone once they close. #115 exists because a save from a form that was closed and reopened would otherwise act on the new form. The sketches build every screen once, so they have no way to tell two openings of the same form apart. And a handler left registered by a closed screen would still run on that screen's late results.

**So:** there are two kinds of screen:

- **Persistent:** built in `New` and always open. These are the three tabs, plus any screen that holds data for the session.
- **Stacked:** opened with `Push` and gone after `Pop`. A message for a screen that has closed reaches nothing, so a late save result for a closed form goes nowhere.

**The router opens a screen in a fixed order:**

1. It gives the screen a new `ScreenInstance`.
2. It calls `RegisterRoutes(a, id)`, recording the screen itself as the owner of every handler registered during that call.
3. It calls `Init()`, so the results of the loads `Init` starts can't arrive before the screen's handlers are there to take them.

Closing a screen removes the handlers it owns. A screen never unregisters anything itself, so it can't forget to.

A route records its owner as the `SubComponent` itself rather than its `message.Screen` kind, because not everything that registers is a screen: `model.Syncing` has no kind and is never on the stack (section 6). Removing a closed screen's handlers compares owners, which works the same for both.

`RegisterRoutes` is also how a screen learns its instance. `Init` is `tea.Model`'s, so it takes no arguments, and the opener can't pass the instance because the router hasn't assigned it yet. A stacked screen keeps the instance to compare with a result's `From`, as it does today, and puts it in every `NavMsg` it sends (section 4). Handlers are found by message type, so a second opening of the same form registers for the same messages and would still get the first opening's late result without that check.

Every open screen lives in one map, `screens`, keyed by kind (Template). That works because **a kind of screen is open at most once at a time.** That's true of today's code: following every transition in `updateKeyMsg`, no screen can open a chain of screens that leads back to its own kind. `open` panics on a kind that's already open. A new transition that breaks the rule then fails the first time it's tried, instead of quietly replacing the open screen and its handlers.

Each handler closes over its screen's pointer. The model the router keeps in `screens` and the one the handlers change have to be the same value, so **every screen is a pointer type**. When `Update` returns, the router stores whatever model came back, and for a screen that's the same pointer it was handed.

**A screen's state is only touched on Bubble Tea's event loop.** Bubble Tea calls `App.Init`, `Update` and `View` from one goroutine, and the router calls each screen's `Init`, `Update`, `View` and handlers from inside them, synchronously. Those are the only places a screen reads or writes its own fields. A `tea.Cmd` is different: Bubble Tea runs it on a goroutine of its own, while the loop goes on handling messages. Value receivers used to hide this, because a command closed over a copy. With pointer receivers, a command that touches the screen is a data race:

```go
// Races: m.cursor is read on the command's goroutine while Update moves it,
// and may index past the end of visible.
return m, func() tea.Msg { return message.OpenCompanyDeleteMsg{Company: visible[m.cursor]} }

// Safe: the company is read now, on the loop, and the closure holds a copy.
c := visible[m.cursor]
return m, func() tea.Msg { return message.OpenCompanyDeleteMsg{Company: c} }
```

The company list was the first screen converted, and its first draft had exactly the racing form for every key.

So, for every screen:

- **A command captures values, never the receiver.** Compute what it needs before returning it. `message.Pop(m.id)` is safe, because `m.id` is read when `Pop` is called, not when the command runs. A method value such as `return m, m.load` is not: `load` runs on another goroutine with `m` in hand. A method value on a value receiver is safe, because Go copies the value when the method value is made: `m.spinner.Tick` copies the Bubbles `spinner.Model`, on the loop. Loads stay free functions that take the services and IDs they need, such as `loadPostings(svc, companyID, hideArchived)`, as they are today.
- **A command's result changes the screen only by coming back as a message.** The command returns a message, and the handler for it, running on the loop, applies it. A command never sets a field, even one nothing else reads yet.
- **A slice or map handed on is copied.** Anything passed to a command or to a screen being pushed shares its backing array with the screen that passed it. `companyCreated` appends to and sorts `m.companies` in place, so a slice handed out without a copy changes under whoever holds it. The same goes for a pointer to anything a screen goes on changing. `R` on the company list is the worked example: it sends `ToggleSyncAllMsg{Companies: slices.Clone(m.companies)}`, so a reload landing mid-run can't change which companies the run syncs.

`go test -race` catches a command that breaks these rules once a test runs it alongside the screen's `Update`. Nothing in the type system or the linters does.

### 6. What stays with the router

These belong to every screen, so they stay on `App`:

- **The header, tabs and banner.** Screens report errors and status by returning a command that produces a `message.BannerMsg` (`message.ReportErr`, `ReportLoad`, `ReportStatus`). A load's result is flagged, so a successful load leaves another load's error in place, keeping the difference `setLoadErr` makes today (`app.go:433`). Any other report replaces both the error and the status line, so `ReportStatus("")` clears the banner. Clearing a stale error on a screen change (#41) is already the router's job. A fault in the program is a `message.ErrMsg` (`message.Fail`), not a `BannerMsg`: the router logs it and the banner shows only that there was one (section 2).

  The banner draws the error, else `Syncing`'s view if it has one, else the status line. Because reports now arrive as messages, three things differ from when `App` set the fields directly. All three are accepted:
  - A sync-all run's final summary clears an error left on screen, so the summary shows. Before, the error hid it.
  - While a run is going, the banner shows its bar and progress. A status line from anything else, such as opening the browser, waits until the run ends. Before, it replaced the run's progress text until the next company started.
  - A banner update lands one message after the action that caused it.

  The tabs show a count for applications and all postings but not companies. The router doesn't read the company list's data, and the company count wasn't worth a message.
- **The terminal size.** The router owns `tea.WindowSizeMsg`. It works out the area left for a screen and routes it as a `message.ContentSizeMsg`, which every screen registers for in `AlwaysScope`, so screens in the background, persistent and stacked, are already the right size when they come back on top. Two things differ from subtracting a fixed amount from `tea.WindowSizeMsg`:
  - **The rows taken aren't constant.** `screenRows()` (`app.go:1788`) counts the header's and the banner's lines. The banner grows and shrinks with errors, status lines, the sync-all bar and the refresh spinner, and none of those changes arrives as a `tea.WindowSizeMsg`. So after every message, the router compares the size it would send with the last one sent, and resends it if they differ. A window resize and the banner growing go through the same check. That replaces the `setHeight` switch at the end of `Update` (`app.go:1041`).
  - **A separate `message.ContentSizeMsg`, not a rewritten `tea.WindowSizeMsg`.** A screen that gets a `tea.WindowSizeMsg` can't tell the terminal's size from the area it's been given. With its own type, the terminal's size only ever means the terminal.

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
- **A `SubComponent` that isn't a screen.** `New` builds and registers it like a tab, but it has no `message.Screen` kind, is never on the stack, and is never in focus. It registers in `AlwaysScope`, and could follow other views' messages the same way (section 2). It registers for `ToggleSyncAllMsg`, `RefreshCompanyMsg`, `SyncAllStepMsg`, `CompanyRefreshedMsg` and `spinner.TickMsg`. Its `View` is the run's bar and progress line, or the refresh spinner, or empty, and `banner()` draws it.
- **It reports like a screen.** Summaries and refresh outcomes go to the banner as `ReportStatus`/`ReportErr`. The end of a run is announced as `SyncAllFinishedMsg{Results}`.
- **Screens start it with a message.** The company list's `R` sends `ToggleSyncAllMsg` with its own copy of the companies (section 7). Its `r` and the posting list's `r` send `RefreshCompanyMsg`.
- **The end of a sync is broadcast, and listeners act on it.** When a run or a refresh finishes, `SyncAllFinishedMsg` and `CompanyRefreshedMsg` go to every component that registered for them. The company list and the posting list register and reload themselves; the posting list checks whether its company was synced. `App` reloads nothing and doesn't read `selectedCompany` to decide.
- **Spinner ticks.** `Syncing` registers for `spinner.TickMsg`, so every tick goes to it, and the spinner drops ticks with another spinner's ID. A screen with a spinner of its own would register for the tick too, and each would ignore the other's.

### 7. Shared data has one owner

Some data on `App` is read by more than one screen today. Screens then depend on `App` to coordinate them, where a message could let each one act on its own.

| Data                               | Read by                                                             | Owner after                                                                                                                                                                                           |
| ---------------------------------- | ------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `postings`                         | posting list, posting detail (previous/next, `canNavigateSiblings`) | posting list. Opening posting detail passes the posting IDs in list order. Opening it from all postings or application detail passes none, which is what `canNavigateSiblings` works out today (#93). |
| `applicationsByPosting`            | posting detail, seeded by application detail (`app.go:1541`)        | posting detail. Whoever opens it passes the application, if any. The map becomes the one posting's application.                                                                                       |
| `companies`, `companyOpenPostings` | company list, company delete (open count), sync all, the Companies tab's count | company list. Opening delete passes the count. Sync all gets its own copy in `ToggleSyncAllMsg`. The Companies tab no longer shows a count (section 6).                                                |
| `exportDir`                        | export, submit                                                      | router, passed to both when they open. A remembered value for the session isn't any one screen's.                                                                                                     |
| `hideArchived`                     | posting list                                                        | posting list. It lives for the session, so the posting list has to be persistent (open question 1).                                                                                                   |

### 8. Three packages

The router, the screens and the messages each get a package:

| Package       | Holds                                                                                                                                                                        | Imports                     |
| ------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------- |
| `tui`         | `App`: the stack, the lookup table, the header, tabs and banner, the change log                                                                                              | `tui/model`, `tui/message` |
| `tui/model`  | one exported type per screen, and `Syncing`; `Router`, `HandlerFunc`, `SubComponent`, `Dialog` and `ChangeFollower`, the interfaces the router and screens meet at, and `Handle`, which registers a typed handler; the lookup table's entry types (`Route`, `RouteID`, `RouteIDOf`); the styles and view helpers the screens draw with | `tui/message`               |
| `tui/message` | every message type, with exported fields; `Screen` and `ScreenInstance`; the commands screens return to the router (`Push`, `Pop`, `Replace`, `ReportErr`, `Fail`, …)                | neither                     |

`tui/message` imports neither of the other two, so a screen and the router both depend on it and never on each other's internals. `NavMsg` names a screen by `message.Screen`, which is why the screen kinds live there and are exported.

A message moves to `tui/message` when it's created, or when it's first touched by this refactor: when a screen sends it, registers for it, or handles it. Nothing moves just to move. Until a message moves it stays unexported in `tui`, and only the router can use it. `tui/message` may import domain packages for the types its fields carry (`company`, `sync`), never `tui` or `tui/model`.

When a message moves, it's named for what it communicates:

- **A request** is imperative and says what it asks for, not which key asked: `ToggleSyncAllMsg` (start a run, or stop the one under way), `RefreshCompanyMsg`, `OpenCompanyFormMsg`, `OpenCompanyEditMsg`, `OpenCompanyDeleteMsg` (the confirmation, not the delete), `OpenCompanyPostingsMsg`. The `Open…` requests are what `App` acts on until `NavMsg` exists, and `Push` replaces them then.
- **A result** is past tense and says what happened: `CompaniesLoadedMsg`, `CompanyCreatedMsg`, `CompanyDeletedMsg`, `CompanyNameUpdatedMsg`, `CompanyRefreshedMsg`, `SyncAllFinishedMsg`. `SyncAllStepMsg` is one company's result within a run.

Splitting the package also fixes the last point under Problem: a screen in `tui/model` is tested by calling its own `Update`, `View` and handlers, with no `App`. A test registers the screen with a fake `Router` that records handlers by message type, then delivers messages the way the router would (`tui/model/syncing_test.go`). `App`'s own tests assert what the user sees, such as the banner line, the screen on top, and the commands that come back, never a screen's unexported fields.

What splitting the package costs:

- **Every interface method the router calls is exported.** A type in `tui/model` can't satisfy an interface whose method is unexported in `tui`, so `dialog()` and `reloadFor` become `Dialog()` and `ReloadFor`.
- **The helpers screens draw with move to `tui/model`:** `visibleWindow`, `tableRows`, `searchLine`, the column-width helpers and the styles. Anything the header or banner also uses is exported from `tui/model` for the router to call. The styles have already moved, as one block (`model.CursorStyle`, `model.DimStyle`, …), when `Syncing` became the first thing in `tui/model` to draw with them.

## Template

This is the routing from the sketches, adjusted for the sections above. It shows each mechanism, not every case: the bodies left as `…` are today's code moved. Each screen and message moves onto it as the work breakdown reaches it.

### Messages and screens (`tui/message`)

```go
package message

// Screen is a kind of screen. A kind is open at most once at a time.
type Screen int

const (
 ScreenCompanyList Screen = iota
 ScreenApplicationList
 // …
)

// ScreenInstance tells two openings of the same kind of screen apart.
type ScreenInstance int

type CompanyCreatedMsg struct {
 Company company.Company
 Err     error
}

type ActiveApplicationsLoadedMsg struct {
 Applications []posting.ActiveApplication
 Progress     map[int64]map[document.Type]DocumentProgress
 Err          error
}
```

### Screens (`tui/model`)

```go
package model

// HandlerFunc handles a message of the type it was registered for.
type HandlerFunc func(tea.Msg) tea.Cmd

// Router is what a screen registers its routes with.
type Router interface {
 // Register calls handler for every msg of the same concrete type that
 // scope lets through (section 2).
 Register(msg tea.Msg, scope Scope, handler HandlerFunc) error
}

// Handle registers handle for every T that scope lets through. The route
// is T itself, so a handler can't be registered for a message it doesn't
// take.
func Handle[T tea.Msg](r Router, scope Scope, handle func(T) tea.Cmd) error {
 var zero T
 if any(zero) == nil {
  return fmt.Errorf("model.Handle: the handler takes %v, an interface, which has no type to route by; take a concrete message type", reflect.TypeFor[T]())
 }
 return r.Register(zero, scope, func(msg tea.Msg) tea.Cmd {
  m, ok := msg.(T)
  if !ok {
   return message.Fail(fmt.Errorf("handler for %T got %T", zero, msg))
  }
  return handle(m)
 })
}

// Scope is when a registered handler gets the messages it registered for.
type Scope int

const (
 // AlwaysScope: every message of the type, whether or not the owner is
 // in focus, such as a load's result.
 AlwaysScope Scope = iota
 // FocusScope: only while the owner is in focus, such as typed input.
 FocusScope
)

// SubComponent is a screen, or anything else the router sends messages
// to. Implementations are pointer types, since the handlers they register
// change them in place.
type SubComponent interface {
 tea.Model
 // RegisterRoutes runs when the router opens the component, before Init.
 RegisterRoutes(r Router, id message.ScreenInstance) error
}

// Dialog marks a screen drawn over the one beneath it, like a delete
// confirmation.
type Dialog interface {
 Dialog()
}

// Route is one registered handler and the component that registered it.
type Route struct {
 Owner  SubComponent
 Scope  Scope
 Handle HandlerFunc
}

func NewRoute(owner SubComponent, scope Scope, handle HandlerFunc) *Route {
 return &Route{owner, scope, handle}
}

// Accepts reports whether r's handler gets a message while focused is the
// component in focus; focused is nil when none is.
func (r *Route) Accepts(focused SubComponent) bool {
 return r.Scope == AlwaysScope || r.Owner == focused
}

// RouteID names the messages one set of handlers is for. It's opaque, so
// only RouteIDOf knows how it is made, and the table can be keyed some
// other way without touching Register, route or unregister.
type RouteID struct {
 // Keyed by type identity, not name: %T prints the package's name, not
 // its path, so two packages' same-named types would share a string.
 typ reflect.Type
}

// RouteIDOf is msg's concrete type. A value and a pointer to it are
// different IDs, so a message is always sent and registered as a value.
func RouteIDOf(msg tea.Msg) RouteID {
 return RouteID{typ: reflect.TypeOf(msg)}
}

// String is the type's name as %T prints it, such as
// "message.CompanyCreatedMsg".
func (id RouteID) String() string {
 if id.typ == nil {
  return "<nil>"
 }
 return id.typ.String()
}
```

A screen opens another by returning `Push`, with the new screen built but not started. Its loads go in its `Init`, not in the opener's command:

```go
// posting list
case enterPostingDetail:
 return m, message.Push(m.id, message.ScreenPostingDetail, NewPostingDetail(p, app, m.postingIDs()))

// posting detail
func (m *PostingDetail) Init() tea.Cmd {
 return tea.Batch(loadApplication(…), loadDocumentReviews(…), loadPostingScores(…))
}
```

### Navigation (`tui/message`, applied in `tui`)

```go
// tui/message
type NavMsg struct {
 From ScreenInstance // the screen that asked
 Op   NavOp          // NavPush, NavPop, NavReplace, NavTab
 Kind Screen         // the screen to open, for push and replace
 Next tea.Model      // a model.SubComponent; tea.Model so message imports no screen code
}

func Push(from ScreenInstance, kind Screen, next tea.Model) tea.Cmd {
 return func() tea.Msg { return NavMsg{From: from, Op: NavPush, Kind: kind, Next: next} }
}

func Pop(from ScreenInstance) tea.Cmd {
 return func() tea.Msg { return NavMsg{From: from, Op: NavPop} }
}

func Replace(from ScreenInstance, kind Screen, next tea.Model) tea.Cmd {
 return func() tea.Msg { return NavMsg{From: from, Op: NavReplace, Kind: kind, Next: next} }
}
```

```go
// tui

// stacked is one entry on the stack. The bottom entry is always the current
// tab, so the top is always a.stack[len(a.stack)-1].
type stacked struct {
 kind     message.Screen
 instance message.ScreenInstance
}

// navigate applies msg to the stack and does what every change of the top
// screen needs.
func (a *App) navigate(msg message.NavMsg) tea.Cmd {
 var cmd tea.Cmd
 switch msg.Op {
 case message.NavPush:
  cmd = a.open(msg.Kind, msg.Next.(model.SubComponent))
 case message.NavPop:
  a.close()
 case message.NavReplace:
  a.close()
  cmd = a.open(msg.Kind, msg.Next.(model.SubComponent))
 case message.NavTab:
  a.stack[0] = a.nextTab()
 }
 a.err = nil // a stale error from the screen just left (#41)
 return tea.Batch(cmd, a.activate())
}

// open makes next the top screen: instance, then routes, then Init (section 5).
func (a *App) open(kind message.Screen, next model.SubComponent) tea.Cmd {
 if _, open := a.screens[kind]; open {
  panic(fmt.Sprintf("tui: %v is already open", kind))
 }
 id := a.newScreenInstance()
 a.register(next, id)
 a.screens[kind] = next
 a.stack = append(a.stack, stacked{kind: kind, instance: id})
 return next.Init()
}

func (a *App) close() {
 if len(a.stack) == 1 {
  return // a tab never closes
 }
 kind := a.top().kind
 a.unregister(a.screens[kind])
 delete(a.screens, kind)
 a.stack = a.stack[:len(a.stack)-1]
}
```

### The banner

```go
// tui/message

// BannerMsg is how a screen reports to the banner.
type BannerMsg struct {
 Err    error
 Status string
 // Load: a load's result. Loads finish in any order, so a successful
 // one leaves another load's error in place.
 Load bool
}

func ReportErr(err error) tea.Cmd   { return func() tea.Msg { return BannerMsg{Err: err} } }
func ReportLoad(err error) tea.Cmd  { return func() tea.Msg { return BannerMsg{Err: err, Load: true} } }
func ReportStatus(s string) tea.Cmd { return func() tea.Msg { return BannerMsg{Status: s} } }

// ErrMsg is a fault in the program, such as a handler given a message it
// wasn't registered for, which nothing the user does can fix. It isn't
// meant to be shown; the router logs it.
type ErrMsg struct{ Err error }

// Fail sends err as an ErrMsg.
func Fail(err error) tea.Cmd { return func() tea.Msg { return ErrMsg{Err: err} } }
```

```go
// tui

// errInternal is the banner for an ErrMsg: the error itself means nothing
// to the user, so it says where the details went.
var errInternal = errors.New("internal error, see swamp.log")

func (a *App) setBanner(msg message.BannerMsg) {
 if msg.Load {
  a.setLoadErr(msg.Err) // only a failure changes the banner
  return
 }
 a.err, a.status = msg.Err, msg.Status
}

// banner draws the error, else what's syncing, else the status line.
func (a *App) banner() string {
 if a.err != nil {
  return "\n\n" + model.ErrStyle.Render("error: "+a.err.Error())
 }
 if syncing := a.syncing.View().Content; syncing != "" {
  return "\n\n" + syncing
 }
 if a.status != "" {
  return "\n\n" + model.DimStyle.Render(a.status)
 }
 return ""
}
```

### The router (`tui`)

```go
type App struct {
 // Every open screen: the three tabs, and whatever is stacked over the
 // current one. A kind of screen is open at most once (section 5).
 screens map[message.Screen]model.SubComponent
 // routes is every registered component's handlers, by message type,
 // and registering is the one whose RegisterRoutes is running.
 routes      map[model.RouteID][]model.Route
 registering model.SubComponent
 // syncing runs sync all and refreshes; the banner draws its view.
 syncing *model.Syncing

 stack []stacked
 // sized is the last size sent to the screens.
 sized message.ContentSizeMsg

 width, height int
 err           error
 status        string
 seen          map[message.Screen]int64
 // change-log fields, services the router itself uses
}

func New(/* services */) *App {
 a := &App{
  screens: make(map[message.Screen]model.SubComponent),
  routes:  make(map[model.RouteID][]model.Route),
  seen:    make(map[message.Screen]int64),
  syncing: model.NewSyncing(syncer),
 }
 // Not a screen: it has no kind and is never on the stack (section 6).
 a.register(a.syncing, a.newScreenInstance())
 // A slice, not a map, so handlers run in the same order every time
 // (section 1).
 for _, tab := range []struct {
  kind   message.Screen
  screen model.SubComponent
 }{
  {message.ScreenActiveApplications, model.NewActiveApplications(…)},
  {message.ScreenCompanyList, model.NewCompanyList(…)},
  {message.ScreenAllPostings, model.NewAllPostings(…)},
 } {
  id := a.newScreenInstance()
  a.register(tab.screen, id)
  a.screens[tab.kind] = tab.screen
  if tab.kind == message.ScreenActiveApplications {
   a.stack = []stacked{{kind: tab.kind, instance: id}}
  }
 }
 return a
}

// register runs s's RegisterRoutes, recording s as its handlers' owner.
func (a *App) register(s model.SubComponent, id message.ScreenInstance) {
 a.registering = s
 if err := s.RegisterRoutes(a, id); err != nil {
  panic(fmt.Sprintf("tui: registering %T: %v", s, err))
 }
}

// Register is Router's, for the component whose RegisterRoutes is running.
func (a *App) Register(msg tea.Msg, scope model.Scope, handler model.HandlerFunc) error {
 id := model.RouteIDOf(msg)
 a.routes[id] = append(a.routes[id], model.NewRoute(a.registering, scope, handler))
 return nil
}

// unregister drops every handler owner registered.
func (a *App) unregister(owner model.SubComponent) {
 for id, rs := range a.routes {
  a.routes[id] = slices.DeleteFunc(rs, func(r model.Route) bool { return r.Owner == owner })
 }
}

func (a *App) Init() tea.Cmd {
 cmds := []tea.Cmd{a.pollChanges()}
 for _, screen := range a.screens {
  cmds = append(cmds, screen.Init())
 }
 return tea.Batch(cmds...)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
 cmd, handled := a.own(msg)
 if !handled {
  cmd = tea.Batch(cmd, a.route(msg))
 }
 // The window or the banner may have changed size, on any message.
 if size := (message.ContentSizeMsg{Width: a.width, Rows: a.screenRows()}); size != a.sized {
  a.sized = size
  cmd = tea.Batch(cmd, a.route(size))
 }
 return a, cmd
}

// own does the router's part of msg. handled means no screen needs it.
func (a *App) own(msg tea.Msg) (tea.Cmd, bool) {
 switch msg := msg.(type) {
 case message.NavMsg:
  // The user may have left the screen that asked before this arrived.
  if msg.From != a.top().instance {
   return nil, true
  }
  return a.navigate(msg), true
 case message.BannerMsg:
  a.setBanner(msg)
  return nil, true
 case message.ErrMsg:
  // The TUI owns the terminal, so cmd/swamp points log at swamp.log.
  log.Printf("tui: %v", msg.Err)
  a.err = errInternal
  return nil, true
 case tea.WindowSizeMsg:
  a.width, a.height = msg.Width, msg.Height
  return tea.ClearScreen, true
 case changeTickMsg:
  return a.handleTick(), true
 case changesMsg:
  return a.handleChanges(msg), true
 case tea.KeyPressMsg:
  // The terminal is in raw mode, so ctrl+c arrives as a key.
  if msg.String() == "ctrl+c" {
   return tea.Quit, true
  }
  if msg.String() == "tab" && len(a.stack) == 1 {
   return a.navigate(message.NavMsg{Op: message.NavTab}), true
  }
 }
 return nil, false
}

// route delivers msg to the screens it's for.
func (a *App) route(msg tea.Msg) tea.Cmd {
 // Every handler for msg's type whose scope lets it through: always, or
 // only while its owner is in focus (section 2). Handlers run here, on the
 // event loop; only the commands they return run elsewhere (section 5).
 top := a.top().kind
 focused := a.screens[top]
 var cmds []tea.Cmd
 for _, r := range a.routes[model.RouteIDOf(msg)] {
  if r.Accepts(focused) {
   cmds = append(cmds, r.Handle(msg))
  }
 }
 if len(cmds) > 0 {
  return tea.Batch(cmds...)
 }

 // No handler was called: keys the top screen takes in Update, and Bubbles
 // components' own messages such as a text input's cursor blink.
 next, cmd := focused.Update(msg)
 a.screens[top] = next.(model.SubComponent)
 return cmd
}

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
- **The router records who registered each handler and drops it on close,** so a late result for a closed screen reaches nothing, and no `Update` is called on a missing map entry. It records the registering `SubComponent` itself, so a component with no screen kind can register too (sections 5 and 6).
- **Routes are registered when a screen opens, not once at start-up.** Stacked screens come and go, so `open` registers and `close` unregisters. `New` registers the tabs once, and `Init` doesn't register again.
- **`RegisterRoutes` takes the screen's instance,** and runs before `Init`. `Init` is `tea.Model`'s and takes nothing, and a screen whose loads started before its handlers existed would lose their results (section 5).
- **Delivery depends on a scope each registration gives.** `model.AlwaysScope` fans a message out to every handler; `model.FocusScope` delivers only to the handler whose owner is in focus. That's how keys and pastes reach only the screen on top, with no list of input types in the router and nothing refused at registration (section 2).
- **`ChangeScreenMsg{Target}` becomes `NavMsg`,** which operates on the stack instead of naming a destination, and names the screen that asked (sections 3 and 4).
- **The router does its part of a message, then routes it,** unless no screen needs it. `BrowserOpenedMsg` updates the banner and still reaches its screen.
- **Work that isn't a screen registers like one.** `model.Syncing` registers for its messages, including its spinner's tick, and the router draws its view in the banner. The sketches had the router own that work and its Bubbles messages (section 6).
- **Size is resent whenever it changes,** compared after every message, so a banner growing or shrinking resizes the screens as a window resize does (section 6).
- **Bubble Tea v2, not v1.** A screen's `View` returns a `tea.View`, not a string, and the router composes `.Content` and sets the terminal options on its own view. Keys are `tea.KeyPressMsg`, matched by `msg.String()`, not `msg.Type`, and pastes arrive as `tea.PasteMsg` (sections 2 and 6).
- **Only ctrl+c quits globally,** not `q` (section 6).
- **Handlers take their message's concrete type and register with `model.Handle`,** which routes by that type, so a handler wired to the wrong message doesn't compile. The sketches' handlers took `tea.Msg` and asserted, which panics on a mismatch. A mismatch the router causes sends a `message.ErrMsg`, which the router logs (section 2).
- **Routes are keyed by an opaque `model.RouteID`, not a bare string.** `model.RouteIDOf` is the only way to make one, so its insides can change without touching `Register`, `route` or `unregister`. It holds the message's `reflect.Type`: a key by type identity, where `%T`'s string uses the package's name rather than its path and so would collide for same-named types in two packages of the same name. It also skips building a string for every message. `String()` serialises it to the name `%T` prints, such as `message.CompanyCreatedMsg`, for logs and errors.

## Open questions

1. **Which screens are persistent?** The three tabs for certain. The posting list holds `hideArchived` for the session. Should it be persistent and re-pointed at each company, or stacked, with `hideArchived` moving to the router?
2. **Where do overlays live?** This RFC makes them stacked screens that report they're overlays. The alternative is a separate overlay slot on the router, which is simpler while there's never more than one at a time.
3. **Does `New` build the tabs, or take them?** The Template builds them in `New` from the services it's given, as today. Passing them in instead would let tests choose which screens exist. But `cmd/swamp` would then have to build every screen, and each test needs fresh screens, never a shared package-level map.

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

While screens are partly converted, the code works in ways the Template doesn't:

- **`App` still does its own part of a routed message.** `Update` routes a message to its registered handlers and also runs `App`'s old `update` switch on it, which still sets `a.err` and `a.currentScreen` for the screens not yet converted.
- **Tab reaches the company list before `App` switches tabs.** `route` runs before the old `updateKeyMsg`, which still handles tab, so the list's key handler sees tab first and ignores it. A tab press has no `Text` in v2, so an open search doesn't type it. In the Template, `handle` takes tab before `route`.
- **The `Open…` requests are `App`'s to act on.** The company list sends `OpenCompanyFormMsg`, `OpenCompanyEditMsg`, `OpenCompanyDeleteMsg` and `OpenCompanyPostingsMsg`, and `App` switches `a.currentScreen`, until `NavMsg` and the stack replace them (section 4).
- **`App` still reloads after a sync.** On `SyncAllFinishedMsg` and `CompanyRefreshedMsg` it reloads the company list, and the posting list if `selectedCompany` was synced, until those screens register for the messages themselves (section 6).
- **Screens not yet converted are still `App`'s fields,** called directly, and the company list is still a typed field, not an entry in `screens`.
- **App tests wait a message for a converted screen's intent.** A converted screen's key comes back as a command, so tests that press one use `sendIntentKey`, which delivers the intent, or `applyCmd`. This is section 4's "keys typed in between" cost, as seen from a test.

`go test -race ./tui/...` is clean through "Company list keys are routed". Last run against real data after "Company list registers its routes". Resizing the window on the Companies tab checked by hand after "Company list size is routed".


## Out of scope

- **Changing any screen's behaviour, keys or layout,** except the banner differences that come from reporting through messages (section 6).
- **#283 (a live-updates indicator) and #284/#285 (posting changes reaching detail screens).** They may get easier once screens own their data, but they're separate changes.
- **Splitting `tui/model` further,** for example a package per screen. Three packages are enough to separate the router, the screens and the messages (section 8).
