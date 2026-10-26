# RFC 0013: Router messages are events: shared metadata and a few primitive payloads

- **Status:** Draft
- **Date:** 2026-10-23
- **Related:** RFC 0012 (the screen router: `model.MsgRouter`, `Routes()`, `Always` and `WhenFocused`, which this builds on, and whose message naming, `FocusChangedMsg` and `NavMsg` this amends); RFC 0008 (the change log)

## Summary

`model.MsgRouter.Dispatch` sends a message to every route registered for its concrete type, and each handler decides for itself whether the message is meant for it. The TUI has 80 message types, most made for one action (`CompanyCreatedMsg`, `CompanyNameUpdatedMsg`, `CompanyDeletedMsg`), so a model that shows companies needs one route per action. Each says who, what and when differently or not at all, so nothing can route or filter on them without knowing each struct. They also carry domain structs with pointer fields, Go `error`s and, in `FocusChangedMsg`, a model itself.

This RFC gives every message the shape of an event in an event-driven architecture: an envelope of metadata that every event shares, and a payload particular to it. The metadata is what routing and filtering read; only the handler that gets the event reads the payload.

1. **Every message in `tui/message` is an `Event[P]`**: a `Meta` (`ID`, `Kind`, `Type`, `Source`, `Subject`, `Op`, `At`) and a `Payload P`.
2. **A payload is one of eight primitives**: `Changed`, `Read`, `Run`, `Ran`, `Error`, `Nav`, `Notify` and `Signal`. The 80 types fold into them.
3. **Routes filter on `Meta`.** `Always` and `WhenFocused` take filters (`About`, `From`, `OnOp`) that the router applies to `Meta` before the handler runs.
4. **Events are plain data.** No pointers, interfaces, functions or channels; the standard library only; they round-trip through JSON/protobufs/etc unchanged.
5. **Payloads carry snapshots, not domain objects.** `message.Company` is a value type that `tui/message` owns.
6. **A model issues its own writes and loads as commands.** What comes back is routed: `Changed`, `Read` or `Error`. Only work another model owns the state of is a routed request (`Run`).
7. **The service gives the before and after.** An update or delete returns `change.Of[T]`, read in the write's transaction.

## Primitives

Each primitive is a `Kind`. Requests have no `Op`; only `Changed` says something was written, and `Error` says which op didn't happen.

| Kind | Payload | What it is | Folds in | Count |
| --- | --- | --- | --- | --- |
| `changed` | `Changed[T]` | A write that happened: `Before` and `After` of one entity | The company, application, markup and document review write results; the change log's `changesMsg`, as writes by another process | 10 |
| `read` | `Read[T]` | A read's result, for the model that asked | The 14 `…LoadedMsg` and `…ReloadedMsg` | 14 |
| `run` | `Run[T]` | A request for a job whose state another model owns | `RefreshCompanyMsg`, `refreshSelectedCompanyMsg`, `ToggleSyncAllMsg` | 3 |
| `ran` | `Ran[T]` | That a job ran, with its report `T`: how many succeeded and failed, and what else a responding model needs to update, never the rows the job wrote | `CompanyRefreshedMsg`, `SyncAllStepMsg`, `SyncAllFinishedMsg`, `applicationExportedMsg`, `browserOpenedMsg`, `editorClosedMsg`, and the saves only the asker hears, `companyFiltersSavedMsg` and `applicationFormSavedMsg` | 8 |
| `error` | `Error` | An error: a write, load or run that didn't happen, or a fault in the program, and why. The only error event | Every `Err` field, `ErrMsg`, and `documentChangedDuringReviewMsg` (a review refused because the document changed) | 2 |
| `nav` | `Nav` | A request to the stack: push, pop, replace | `Open…`, `Close…`, `cancel…`, `backTo…`, `enter…`, `navigatePostingMsg` | 31 |
| `notify` | `Notify` | A status line for the banner | `BannerMsg` | 1 |
| `signal` | `Signal[T]` | A UI fact with no entity: size, focus, a timer | `ContentSizeMsg`, `FocusChangedMsg`, `changeTickMsg`, `allPostingsSearchMsg` | 4 |

The rest go, since a model does the work itself: the write requests (`confirmApplicationDeleteMsg`, `confirmApplicationSubmitMsg`, `saveFilterSelectionMsg`), the reload requests (`refreshApplicationDetailMsg`, `refreshPostingDetailMsg`, `toggleHideArchivedMsg`) and `openJobURLMsg`. That's 7, for 80 in all. If one model ever needs to ask another to write or load, an `update` or `load` kind is added then.

"Refresh" meant two things, and they land in different kinds. Reading again what's in the database is a load the model issues. Fetching from a job board is a `Run[SyncCompany]` to `Syncing`, which reports a `Ran[SyncReport]` for each company it syncs, with the company as `Subject`, its counts (fetched, created, updated, closed, reopened) and `FetchedAt`. The sync sends no event per posting: a model showing that company's postings reloads on the `Ran`, and the change log carries the rows to other processes.

## Decisions

### The envelope

| Question | Decision |
| --- | --- |
| What every event shares | `Meta`: `ID`, `Kind`, `Type`, `Source`, `Subject`, `Op` and `At`. It's the same struct for every event, so the router, a log line or a Kafka producer can read it without knowing the payload. |
| `ID` | Unique per event, so a consumer that sees one twice can drop the second. 128 random bits from `crypto/rand`, as hex, set by the constructor. An event made from a `change_events` row takes its ID from that row instead, so the same row read twice gives the same ID. |
| `Kind` | Which primitive the payload is. A consumer that wants every write, or every failure, reads `Kind` alone. |
| `Type` | What the payload is about within its kind: `company`, `company_list`, `sync_company`, `content_size`, …, from constants in `tui/message`. `Kind` and `Type` together name the payload's schema off the wire. For `error`, `Type` is its `Code`'s name; for `nav`, the name of the `Screen` it opens; for `notify`, `status`. |
| How `Kind` and `Type` stay right | Each primitive has `Kind()`, and its content has `Type()`. `New` sets both from the payload, so neither can disagree with it, and every constructor goes through `New`. |
| `Source` | Who: `Process`, the writer's `"kind:pid"` as `change.Event.Origin` records it, and `Instance`, the `ScreenInstance` that asked, 0 for none. A late result for a dialog that was closed and opened again carries the old number, so it doesn't match. A number is used rather than the model because it serialises, and a command can carry it without capturing the model. |
| `Subject` | Which entity the event is about: its `Type` and `ID`. Zero for an event about no one entity. On Kafka it's the record key, so one entity's events stay in order in one partition. An outcome carries its request's subject. |
| `Op` | `"created"`, `"updated"` or `"deleted"`: what was written, on `changed`, or what wasn't, on `error`. `OpNone`, the zero, on every other kind, so a request can never pass `OnOp`. |
| `At` | When it happened. For a write, the database's time for it: `UpdatedAt`, `CreatedAt` or `DeletedAt` (`SoftDeleteApplication` sets no `updated_at`), UTC with one-second resolution, so it says when, not in what order. For anything else, `time.Now().UTC()` when the event was made. |
| Constants in Go, names on the wire | What code switches on (`Kind`, `Op`, `NavOp`, `ErrorCode`, `Screen`) is an `iota` constant, encoded as its name from one table, so reordering constants or moving a package can't change what a stored event means. A zero is in the table only where "none" is legal (`OpNone`, `ScreenNone`); any other value without a name fails to encode, and an unknown name fails to decode. `Type` is a string, since each body names its own and nothing switches on it. |
| Which messages are events | Every message type in `tui/message`, including the UI's own (`Signal`, `Notify`, `Nav`). One rule, one test, and filters work on all of them. Bubble Tea's messages (`tea.KeyPressMsg`, `tea.PasteMsg`, `spinner.TickMsg`, `tea.BatchMsg`) aren't ours and stay as they are. |
| How an event is sent | As a value, never a pointer. `RouteIDOf` already treats a value and a pointer to it as different routes. |

### Routing and filtering

| Question | Decision |
| --- | --- |
| How `Dispatch` finds routes | Unchanged: by Go type. `Event[Changed[message.Company]]` and `Event[Changed[message.Application]]` are different `reflect.Type`s, so each entity's writes are their own route, and a model registers one route per entity, not one per action. |
| How a route filters | `Always` and `WhenFocused` take `...model.Filter`, a `func(message.Meta) bool`. The route reads `Meta` through `Metadata()`, which `Event[P]` has, and calls the handler only if every filter passes. `About(typ, id)` checks `Subject`, `From(instance)` checks `Source.Instance`, `OnOp(ops...)` checks `Op`. A filter on a type without `Metadata()`, such as `tea.KeyPressMsg`, fails `Register`, as an interface handler does now. |
| What a handler is left with | Only the payload. The delete dialog's handlers in the template take the event and return `close`, since its filters already said it's this company being deleted, or its own delete failing. |

### Payloads

| Question | Decision |
| --- | --- |
| What a payload may hold | Strings, numbers, bools, `time.Time`, and structs, arrays, slices and maps of those. Never a pointer, interface (`error`, `tea.Model`, `any`), function or channel. |
| What `tui/message` may import | The standard library for its types. Its command constructors (`ReportStatus`, `Fail`, `Pop`, …) return `tea.Cmd` and still import Bubble Tea, but no event holds anything from it. It stops importing `company` and `sync`. |
| How an event is serialised | No one format is fixed. Field names are given explicitly in the encoding (JSON tags now, a `.proto` if one is written), so renaming a Go field doesn't change the wire; `time.Time` maps to RFC 3339 or `google.protobuf.Timestamp`. On Kafka, `Meta` is the record headers, `Subject` the key and `Payload` the value. Protobuf has no generics, so each instantiation is its own wire message (`ChangedCompany`, `ReadCompanyList`). A test round-trips one of each event through JSON as the stand-in. |
| What a payload carries for an entity | A snapshot: a comparable struct in `tui/message` with the fields models read, as plain values. `company.Company.DeletedAt *time.Time` becomes `DeletedAt time.Time`, zero while not deleted. A command in `tui/model` builds it, since `tui/message` can't import the domain. |
| `Changed[T]` | `Before` and `After`, the snapshot on each side. The zero snapshot is a missing side (`Before` for `created`, `After` for `deleted`), left out when serialised. `Op` says which sides are set, and the `Created`, `Updated` and `Deleted` constructors set the right ones. A handler that cares about one field compares it across the two; no event names a field, since that would bring back a type per action. |
| `Read[T]`, `Run[T]`, `Ran[T]`, `Signal[T]` | One field of the content type `T`, which is one struct per query, job or signal (`CompanyList`, `SyncCompany`, `SyncReport`, `ContentSize`). The kind says how to treat it; `T` says what it holds. |
| What a `Ran[T]` reports | A summary a responding model can act on: counts of what succeeded and failed, and values such as `SyncReport.FetchedAt`, which the company list shows. Never the rows the job wrote: a sync can write hundreds, and another process's sync of the same company reaches the TUI only through the change log, so a model that shows those rows filters the `Ran` with `About` and reloads. Messages for the user go to the banner as a `Notify`. |
| `Error` | `Code` and `Text`. `Meta` says who asked, which entity and which op, and its `Type` is the `Code`. It's one Go type for every failure, since a route picks out the ones it cares about with `From` or `About`. `At` is when the command saw the error. |
| How an error is carried | `Code` is a string from a closed set in `tui/message` (`not_found`, `sync_in_progress`, …, `other`), and `Text` is `err.Error()`. The command maps a Go error to a code with `errors.Is`, outside `tui/message`, which can't import the errors it names. A code keeps what `Syncing.companyRefreshed` does today, `errors.Is(refreshed.Err, sync.ErrSyncInProgress)`, which text alone would lose. |
| How an error reaches the user | There is one error event, `Error`, and the command that saw the error sends it and decides nothing else. The banner has a route for every `Error` and shows a line when its `Code` is one it has text for; the rest aren't for the user, and it logs them. A requester that has closed has been `Deregister`ed and gets nothing, but the banner still hears the failure. |
| Where `Before`, `After` and `At` come from | For an update or delete, the service: it reads the row, writes, and reads again in one `store.InTx` transaction, and returns `change.Of[T]` (domain types, `internal/domain/change`). A `Before` taken from what a model last showed would be wrong whenever another process wrote in between. A create needs no service change. |
| Which writes are `Changed[T]` | Those that a model other than the one asking may show. A write only the asking model hears is a `Ran` (open question 1). |

## Adopting the existing message structs

Each struct becomes an event when the model that sends or handles it migrates (RFC 0012 section 8: a message moves when it's first touched). Content types drop `Msg`, since `Event` says what it is.

### Already in `tui/message`

| Struct | Becomes |
| --- | --- |
| `CompanyCreatedMsg`, `CompanyNameUpdatedMsg`, `CompanyDeletedMsg` | `Event[Changed[Company]]`, `Op` `created`, `updated` or `deleted`; or `Event[Error]` |
| `CompaniesLoadedMsg` | `Event[Read[CompanyList]]`: `Companies []Company`, `OpenPostings map[int64]int`; or `Event[Error]` |
| `RefreshCompanyMsg` | `Event[Run[SyncCompany]]`, `Subject` the company |
| `ToggleSyncAllMsg` | `Event[Run[SyncAll]]`: the company IDs, or stop |
| `CompanyRefreshedMsg`, `SyncAllStepMsg`, `SyncAllFinishedMsg` | `Event[Ran[SyncReport]]` for each company synced, by a refresh or a sync-all step, with the company as `Subject` and `RunID` for `Syncing` to match a step to its run; `Event[Ran[SyncAllReport]]`, the run's totals, when it ends. A refused or failed sync is `Event[Error]`, `Code` `sync_in_progress`, with the company as `Subject` |
| `BannerMsg` | `Event[Notify]`: `Status`; its errors are `Event[Error]` |
| `ErrMsg` | `Event[Error]`, `Code` `other`, sent by `Fail` |
| `FocusChangedMsg`, `ContentSizeMsg` | `Event[Signal[FocusChanged]]` (`Focused ScreenInstance`), `Event[Signal[ContentSize]]` |
| `OpenCompanyFormMsg`, `OpenCompanyEditMsg`, `OpenCompanyDeleteMsg`, `OpenCompanyPostingsMsg`, `CloseCompanyDeleteMsg`, `CancelCompanyEditMsg` | `Event[Nav]` |

### Still in `tui`

| Family | Examples | Becomes |
| --- | --- | --- |
| Write results | `applicationCreatedMsg`, `applicationStatusUpdatedMsg`, `applicationNotesUpdatedMsg`, `applicationDeletedMsg`, `postingMarkupUpdatedMsg`, `documentReviewCreatedMsg` | `Event[Changed[Application]]`, `Event[Changed[Markup]]` (subject the posting), `Event[Changed[Review]]`, or `Event[Error]` |
| Write results heard only by the asker | `companyFiltersSavedMsg`, `applicationFormSavedMsg` | `Event[Ran[T]]` (open question 1) |
| Write, reload and URL requests | `confirmApplicationDeleteMsg`, `confirmApplicationSubmitMsg`, `saveFilterSelectionMsg`, `refreshApplicationDetailMsg`, `refreshPostingDetailMsg`, `toggleHideArchivedMsg`, `openJobURLMsg` | None. The model runs the command itself. |
| Loads and reloads | `postingsLoadedMsg`, `activeApplicationsLoadedMsg`, `applicationReloadedForDeleteMsg`, `postingReloadedMsg`, … | `Event[Read[T]]`, one `T` per query |
| Change log | `changesMsg` | `Event[Changed[T]]` per row, `Source.Process` the row's origin (open question 2) |
| Jobs outside the database | `applicationExportedMsg`, `browserOpenedMsg`, `editorClosedMsg` | `Event[Ran[ExportReport]]` (the directory, the paths written and the documents skipped, which the submit screen shows), `Event[Ran[OpenBrowser]]`, and `Event[Ran[EditDocument]]`, whose command records the editor's write before it reports; or `Event[Error]` |
| Navigation | `enter…Msg`, `cancel…Msg`, `backTo…Msg`, `navigatePostingMsg` | `Event[Nav]` |
| Timers | `changeTickMsg`, `allPostingsSearchMsg` | `Event[Signal[T]]` |

### Navigation without the model

RFC 0012's `NavMsg` carries `Next tea.Model`. As a primitive, `Nav` carries its `NavOp` and the `Screen` kind to open, `Source.Instance` is RFC 0012's `From`, and `Subject` says which entity the screen opens on. `App` builds the model, numbers it and `Register`s it, as it already does for `OpenCompanyDeleteMsg`.

## Template

```go
// tui/message

type ScreenInstance int

// Kind, Op, NavOp, ErrorCode and Screen have the same shape: an iota,
// a name table, and String, MarshalText and UnmarshalText through it.
type Kind int

const (
 KindChanged Kind = iota + 1
 KindRead
 // …
)

var kindNames = [...]string{KindChanged: "changed", KindRead: "read" /* … */}

type Op int

const (
 OpNone Op = iota
 OpCreated
 OpUpdated
 OpDeleted
)

type Type string

const (
 TypeCompany      Type = "company"
 TypeContentSize  Type = "content_size"
 TypeFocusChanged Type = "focus_changed"
)

// Meta is what every event carries, and all that routing and filtering read.
type Meta struct {
 ID      string    `json:"id"`
 Kind    Kind      `json:"kind"`
 Type    Type      `json:"type"`
 Source  Source    `json:"source"`
 Subject Subject   `json:"subject,omitzero"`
 Op      Op        `json:"op,omitzero"`
 At      time.Time `json:"at"`
}

type Source struct {
 Process  string         `json:"process"`
 Instance ScreenInstance `json:"instance,omitzero"`
}

type Subject struct {
 Type Type  `json:"type"`
 ID   int64 `json:"id"`
}

// Body is what a primitive holds: a snapshot, a query's result, a job.
type Body interface{ Type() Type }

// Primitive is one of the kinds of payload.
type Primitive interface {
 Kind() Kind
 Type() Type
}

type Event[P Primitive] struct {
 Meta    Meta `json:"meta"`
 Payload P    `json:"payload"`
}

func (e Event[P]) Metadata() Meta { return e.Meta }

// New is the only way to make an event, so Kind and Type match the payload.
func New[P Primitive](src Source, subject Subject, op Op, at time.Time, p P) Event[P] {
 return Event[P]{
  Meta:    Meta{ID: NewID(), Kind: p.Kind(), Type: p.Type(), Source: src, Subject: subject, Op: op, At: at},
  Payload: p,
 }
}

type Snapshot interface {
 comparable
 Body
}

// Company is what an event carries for a company. DeletedAt is zero
// while it isn't deleted.
type Company struct {
 ID          int64     `json:"id"`
 Name        string    `json:"name"`
 Description string    `json:"description"`
 CreatedAt   time.Time `json:"created_at"`
 UpdatedAt   time.Time `json:"updated_at"`
 DeletedAt   time.Time `json:"deleted_at,omitzero"`
}

func (Company) Type() Type { return TypeCompany }

// Changed is a write that has happened. Before is zero for OpCreated and
// After for OpDeleted; build it with Created, Updated or Deleted.
type Changed[T Snapshot] struct {
 Before T `json:"before,omitzero"`
 After  T `json:"after,omitzero"`
}

func (Changed[T]) Kind() Kind { return KindChanged }
func (Changed[T]) Type() Type { var t T; return t.Type() }

func Updated[T Snapshot](src Source, id int64, at time.Time, before, after T) Event[Changed[T]] {
 c := Changed[T]{Before: before, After: after}
 return New(src, Subject{Type: c.Type(), ID: id}, OpUpdated, at, c)
}

// Read is a read's result. Run and Ran have the same shape.
type Read[T Body] struct {
 Value T `json:"value"`
}

func (Read[T]) Kind() Kind { return KindRead }
func (Read[T]) Type() Type { var t T; return t.Type() }

// Signal is a UI fact with no entity, such as the content area's size.
type Signal[T Body] struct {
 Value T `json:"value"`
}

func (Signal[T]) Kind() Kind { return KindSignal }
func (Signal[T]) Type() Type { var t T; return t.Type() }

// ContentSize is the area a screen draws in: the terminal less the header
// and banner.
type ContentSize struct {
 Width int `json:"width"`
 Rows  int `json:"rows"`
}

func (ContentSize) Type() Type { return TypeContentSize }

// FocusChanged names the model now in focus, 0 when none is.
type FocusChanged struct {
 Focused ScreenInstance `json:"focused"`
}

func (FocusChanged) Type() Type { return TypeFocusChanged }

type ErrorCode int

const (
 ErrNotFound ErrorCode = iota + 1 // "not_found"
 ErrSyncInProgress                 // "sync_in_progress"
 ErrOther                          // "other"
)

// Error is a write, load or run that didn't happen, or a fault. Meta says
// by whom, of which entity, and the op.
type Error struct {
 Code ErrorCode `json:"code"`
 Text string    `json:"text"`
}

func (Error) Kind() Kind   { return KindError }
func (e Error) Type() Type { return Type(e.Code.String()) }
```

```go
// tui/model

// Filter is a test of an event's Meta, applied before its handler runs.
type Filter func(message.Meta) bool

func Always[T tea.Msg](handle func(T) tea.Cmd, filters ...Filter) *Route
func WhenFocused[T tea.Msg](owner Focusable, handle func(T) tea.Cmd, filters ...Filter) *Route

func About(typ message.Type, id int64) Filter
func From(instance message.ScreenInstance) Filter
func OnOp(ops ...message.Op) Filter
```

A write's command in a migrated model, here the company edit's rename. `companySnapshot` and `errorOf` convert from the domain:

```go
func setName(companies *service.CompanyService, src message.Source, id int64, name string) tea.Cmd {
 return func() tea.Msg {
  c, err := companies.SetName(context.Background(), id, name)
  if err != nil {
   return message.New(src, message.Subject{Type: message.TypeCompany, ID: id}, message.OpUpdated, time.Now().UTC(),
    errorOf(err))
  }
  return message.Updated(src, id, c.At, companySnapshot(*c.Before), companySnapshot(*c.After))
 }
}
```

The command captures only values, and sends one `Error` whoever it's for: the edit and the banner each have a route for it.

The company list takes every company write through one route:

```go
func (m *companyListModel) Routes() []*model.Route {
 return []*model.Route{
  model.Always(m.companiesRead),
  model.Always(m.companyChanged),
  model.WhenFocused(m, m.keyPressed),
  model.WhenFocused(m, m.paste),
  model.Always(m.resize),
  model.Always(m.focusChanged),
 }
}

func (m *companyListModel) companyChanged(e message.Event[message.Changed[message.Company]]) tea.Cmd {
 switch e.Meta.Op {
 case message.OpCreated:
  m.companies = append(m.companies, e.Payload.After)
 case message.OpUpdated:
  if i := indexOfCompany(m.companies, e.Meta.Subject.ID); i != -1 {
   m.companies[i] = e.Payload.After
  }
 case message.OpDeleted:
  if i := indexOfCompany(m.companies, e.Meta.Subject.ID); i != -1 {
   m.companies = append(m.companies[:i], m.companies[i+1:]...)
  }
  m.clampCursor(len(m.companies))
  return nil
 }
 sortCompaniesByName(m.companies)
 return nil
}

func (m *companyListModel) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
 m.width, m.height = e.Payload.Value.Width, e.Payload.Value.Rows
 return nil
}

func (m *companyListModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
 m.focused = e.Payload.Value.Focused == m.instance
 return nil
}
```

The delete confirmation closes when its company is deleted, whoever deleted it, and when its own delete fails. Its filters say which events those are, so its handlers never look at the payload:

```go
func (m *companyDeleteModel) Routes() []*model.Route {
 return []*model.Route{
  model.WhenFocused(m, m.keyPressed),
  model.Always(m.companyDeleted, model.About(message.TypeCompany, m.company.ID), model.OnOp(message.OpDeleted)),
  model.Always(m.deleteFailed, model.From(m.instance)),
  model.Always(m.resize),
  model.Always(m.focusChanged),
 }
}

func (m *companyDeleteModel) companyDeleted(message.Event[message.Changed[message.Company]]) tea.Cmd {
 return close
}

func (m *companyDeleteModel) deleteFailed(message.Event[message.Error]) tea.Cmd {
 return close
}

func (m *companyDeleteModel) resize(e message.Event[message.Signal[message.ContentSize]]) tea.Cmd {
 m.width = e.Payload.Value.Width
 return nil
}

func (m *companyDeleteModel) focusChanged(e message.Event[message.Signal[message.FocusChanged]]) tea.Cmd {
 m.focused = e.Payload.Value.Focused == m.instance
 return nil
}
```

## Steps

Each step keeps `go build`, `go test`, the linter and `gofmt` clean.

1. **The envelope and the primitives.** Add `Event`, `Meta`, `New`, `NewID` and the eight primitives. A test round-trips one event of each primitive through JSON. Today's structs aren't checked; each later step replaces the ones it touches.
2. **Filters on routes.** `Always` and `WhenFocused` take `...Filter`; `About`, `From` and `OnOp`. Tests show a filtered-out event never reaches the handler, and that a filter on a type without `Metadata()` fails `Register`.
3. **`change.Of` and the company service.** `CompanyService.SetName` and `Delete` return `change.Of[company.Company]`, read in one transaction. Their tests show that `Before` is the row as another writer left it just before this write, and that `At` is `UpdatedAt`, or `DeletedAt` for a delete.
4. **The migrated company models.** Add `Company` and instance numbering. The company list, delete and edit register `Event[Changed[Company]]` and `Event[Error]`, the three company result structs go, and the rest of the structs in `tui/message` become events. `tui/message` stops importing `company` and `sync`.
5. **Each model as it migrates** adopts its row in the tables above. A service write it uses returns `change.Of[T]` first.

When step 4 lands, RFC 0012 changes in three places: its naming rule (messages are `Event[P]` of a primitive), `NavMsg` (`Event[Nav]`, a screen kind and a subject, not `Next`), and `FocusChangedMsg` (a `Signal` naming a `ScreenInstance`).

## Open questions

1. **Should a write only its asker hears be `Changed[T]`?** By default no: `companyFiltersSavedMsg` and `applicationFormSavedMsg` are `Ran`, since neither has one entity to be the subject.
2. **What is `Before` for another process's write?** If the change log feeds `Changed[T]`, `change.Event.Old` holds only the logged columns, not a whole snapshot. The default is the zero snapshot, with `Op` from the event. Its own writes come back through the change log too, with a different `ID` from the event its command sent; by default the TUI skips rows whose origin is its own process.
3. **Does `Meta` need a correlation ID?** `Source.Instance` ties an outcome to the dialog that asked, which is all the TUI needs. A chain across processes (a sync started by the TUI that writes postings) would need an ID that each event made because of another copies. By default there's none until a consumer needs it.
4. **Should `Subject.ID` be `int64` for every entity?** A composite key would need it to be a string.
5. **How does a payload's schema change?** `Kind` and `Type` name a payload, not a version of it. By default a payload only gains fields that are optional, and a breaking change is a new `Type`.
6. **Where do the event types live once something other than the TUI produces them?** A producer in `sync` or `mcpserver` shouldn't import `tui/message`. Since they import only the standard library, moving them to their own package is a rename. By default that waits for the producer.
7. **How does the banner keep a load's error?** `BannerMsg.Load` lets a failed load's error stay until a later load succeeds, since loads finish in any order. With errors as `Error`, the banner would need to hear the loads that succeed. By default it clears the error on its next `Error` or `Notify`.

## Out of scope

- **Old `App` code paths,** which this RFC doesn't design around. Their message structs are covered only by how they're adopted (above).
- **Routed write and load requests.** Models issue them as commands. An `update` or `load` kind is added when one model first needs another to do it.
- **Choosing a wire format, a transport or topics** (Kafka or otherwise). This RFC keeps events serialisable, and doesn't publish them.
- **The domain types' own pointers** (`company.Company.DeletedAt`, `posting.Markup`'s times). Snapshots convert them, and changing the domain types is separate.
