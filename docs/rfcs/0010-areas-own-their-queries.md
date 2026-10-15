# RFC 0010: Areas own their queries; `store` hands out the database

- **Status:** Accepted, 2026-10-15. Work items: #368 (step 1), #369 (step 2), #370 (step 3), #371 (step 4).
- **Date:** 2026-10-14 (revised 2026-10-15)
- **Related:** #364–#367 (the posting-visibility change, the example that motivated this RFC); RFC 0006 (the static sqlc search queries); RFC 0008 (change-origin stamping on the connection)

## Summary

`store` knows the structure of every table and every domain object. It has about 75 exported methods over 83 sqlc queries, in 18 files, and each new way of reading or writing something has meant a new `store` method. Rules that belong to an area end up spread across those methods and their queries, because `store` is organised by table and no package owns an area.

This RFC changes which package knows what, and nothing else:

1. **`store` hands out the database and owns how queries run on it.** It gains:
   - `DB` (the database) and `Tx` (a transaction), both opaque;
   - `Runner`, a sealed interface either one satisfies;
   - `Read`, `Write` and `InTx`, the only ways to run anything on them.

   That's alongside what `Open` already guarantees for every connection. It stops gaining area methods.
2. **An area is a package that owns its types and its queries.** Its functions take a `store.Runner` instead of being methods on `Store`, or a `store.Tx` when their statements must succeed together: `companyfilter.Replace(ctx, tx, companyID, filters)`, not `st.ReplaceCompanyFilters(ctx, companyID, filters)`.
3. **One area moves, as the worked example: company filters.** It's the smallest area with real callers, and moving it fixes a bug: `ReplaceCompanyFilters` isn't atomic.

SQL still comes from sqlc, exactly as today.

## Problem

`store/store.go`'s package comment describes it as the layer that turns database rows into clean domain types for every table. In practice that makes `store` the owner of every area's knowledge:

- **Rules get copied, not owned.** "Visible posting" was restated in five sqlc queries and a Go pipeline in the TUI, and two copies drifted into bugs (#364, #365). #366 fixes that one rule with a SQL view. But the place a rule would naturally live, the area it belongs to, doesn't exist as code, so the next rule can go the same way.
- **Shallow methods.** Many methods just call the generated query (e.g. `store/posting.go`, `company.go`, `tag.go`), or convert rows and map `sql.ErrNoRows` to `ErrNotFound`. That mapping is written about 33 times.
- **Types that only exist to convert.** Company filters have three representations:
  - `store.CompanyFilter`, which `store` returns;
  - `filter.Filter`, which `filter.Match` reads;
  - `sync.FilterRules` (`sync/company.go:15`), which exists only to convert the first into the second, and is called only by `tui` (`tui/app.go:885`).
- **No transactions across methods.** Each method runs on its own. `ReplaceCompanyFilters` (`store/company_filter.go:72-83`) deletes a company's filters and then inserts the new ones one by one, with no transaction. If an insert fails partway, the company is left with some or none of its filters, and no filters means its list shows every posting. A caller can't wrap two methods in one transaction because `store` offers no way to.
- **One package for everything.** Changing one area means editing a package that also owns postings, applications, documents, scoring, tags and change events.

The useful part of `store` is real and should stay in one place. `Open` makes every connection convert times to UTC, carry the registered `equal_fold`/`contains_fold`, and stamp change-log origin through temp triggers (`store/open.go`). Every area needs that; no area should own it.

Only `store` imports the sqlc-generated code (today `store/db`), so areas can move one at a time, and packages outside `store` only notice when their calls change.

## Proposal

### 1. `store`: owns how every query runs

```go
package store

// DB is the database. Statements run on it one at a time, each committed
// on its own.
type DB struct{ sqlDB *sql.DB }

// Tx is a transaction (or a savepoint within one). Statements run on it
// commit together or not at all. Only InTx makes one.
type Tx struct{ tx *sql.Tx }

// Runner is a DB or a Tx: what a statement can run on. Only store's types
// implement it.
type Runner interface{ queries() (*sqlcdb.Queries, error) }

// Read runs fn on r and maps sql.ErrNoRows to ErrNotFound.
func Read[T any](ctx context.Context, r Runner, fn func(*sqlcdb.Queries) (T, error)) (T, error)

// Write is Read for statements whose result the caller doesn't need.
func Write(ctx context.Context, r Runner, fn func(*sqlcdb.Queries) error) error

// InTx runs fn atomically: in a new transaction when r is a DB, or in a
// savepoint when r is a Tx. If fn returns an error, its writes are undone
// and the caller's are kept.
func InTx(ctx context.Context, r Runner, fn func(Tx) error) error

func Open(path string, cfg Config) (*sql.DB, error) // unchanged
func New(sqlDB *sql.DB) *Store                      // unchanged
func (s *Store) DB() DB
```

An area runs a named query like this:

```go
rows, err := store.Read(ctx, r, func(q *sqlcdb.Queries) ([]sqlcdb.CompanyFilter, error) {
	return q.ListCompanyFilters(ctx, companyID)
})
```

- **`DB` and `Tx` are separate types**, so a signature says what it needs:
  - **A function that runs one statement takes a `Runner`**, and works on the database or inside a transaction.
  - **A function whose statements must succeed together takes a `Tx`.** It can't be called outside a transaction, and it can't be handed the database from inside one. Both are compile errors.
- **`Runner` is sealed.** Its one method is unexported, so only `DB` and `Tx` implement it.
  - `queries()` binds sqlc's generated query type to the `*sql.DB` or the `*sql.Tx` on each call (`sqlcdb.New(…)`, one small allocation). There's no stored copy to get out of step with its source.
  - It returns an error for a zero `DB` or `Tx`, so `Read`, `Write` and `InTx` return `store: zero DB` / `store: zero Tx` rather than panicking on a nil field.
- **`DB` and `Tx` have no exported methods.** Everything that runs on one is a `store` function that takes it: `Read`, `Write`, `InTx`. That's the same principle areas follow: functions take the database rather than being methods on it. There's no embedding, so adding a sqlc query doesn't change `store`'s contract.
- **Every query an area runs passes through `store`'s code.** `store` binds it to the database or the transaction and maps `sql.ErrNoRows` to `ErrNotFound`. Anything later that every query needs (logging, timing, retrying on `SQLITE_BUSY`) goes in one place.
- **Areas never see `sqlcdb.New`, `*sql.DB`, `*sql.Tx` or a SQL string.** How a query runs can change inside `store` without touching any area.
- **`InTx` means the same thing for every `Runner`.**
  - On a `DB` it begins a transaction. Transactions begin `IMMEDIATE`, as `Open` already sets.
  - On a `Tx` it opens a `SAVEPOINT`, and rolls back to it if `fn` fails. Savepoints use one fixed name, because SQLite allows duplicate names and `ROLLBACK TO`/`RELEASE` act on the most recent one. Step 2's nested test confirms that.
  - Either way, a failing `fn` leaves none of its writes behind, even if its caller then carries on.
- **One mistake the types don't catch.** Inside `InTx`, a single-statement function still accepts the outer `DB`, because it takes a `Runner`. That statement isn't part of the transaction:
  - a write on it waits for the transaction's write lock until `busy_timeout` (5s), then fails with `SQLITE_BUSY`;
  - a read on it doesn't see the transaction's writes.

  Everything that has to be part of the transaction takes a `Tx`, so this can only affect single statements, and step 2's test makes the write case fail loudly.
- **The existing `Store` methods keep working.** As they're touched, they move their `sql.ErrNoRows` mapping onto the same unexported helper `Read` uses.

Two limits, accepted:
- **Areas still see sqlc's generated parameter and row types** (e.g. `sqlcdb.CompanyFilter`, `sqlcdb.CreateCompanyFilterParams`), so they import `store/sqlcdb` for those types. Converting them into the area's own types is the area's job.
- **The `*sqlcdb.Queries` an area is handed can run any named query, not just the area's own.** `store` owns every call, but not which queries an area may call. One sqlc package per area would narrow that, but this RFC doesn't need it.

**The generated package is renamed from `store/db` to `store/sqlcdb`.** Callers name a `store.DB` variable `db`, which would shadow a package called `db`. In that scope, `db.CompanyFilter` would then refer to the variable, not the generated type, and wouldn't compile. The new name also says what the package is.

### 2. When to use `InTx`

Use `InTx` when **several statements must hold the write lock together**. SQLite allows one writer at a time, so holding the write lock across statements is how nothing else ever sees or commits a half-done change. That happens in two cases:

- **Writes that must all happen or none.** `companyfilter.Replace`'s delete plus inserts; closing a posting and ending its applications.
- **A write based on something just read.** `UpsertPosting` reads, then writes. Another process committing in between would make the write stale. That's why `Open` begins transactions `IMMEDIATE`, taking the lock at the start (`store/open.go:66-70`).

Don't use it for:
- **A single statement.** SQLite commits each one atomically, and a single write takes and releases the lock on its own.
- **Plain reads.** Under WAL they run alongside writers. Because transactions begin `IMMEDIATE`, a read wrapped in `InTx` would take the write lock and queue behind every sync and MCP write.

Who calls it:
- **A function whose statements must succeed together takes a `Tx`**, and its caller opens the transaction:

  ```go
  err := store.InTx(ctx, st.DB(), func(tx store.Tx) error {
  	return companyfilter.Replace(ctx, tx, companyID, filters)
  })
  ```

  The requirement is in the signature, visible at every call site. There's no convenience version that opens the transaction for the caller. That would give each atomic operation two functions, to hide what the `Tx` parameter is there to show.
- **A caller combining several calls into one change**, such as closing a posting and ending its applications, passes the same `Tx` to each. If one of those calls needs to be undone on its own while the rest carry on, it runs in a nested `InTx` (a savepoint).

Not covered: **several reads that must see one consistent snapshot** need a transaction but no write lock. Under `IMMEDIATE` there's no way to get one without the lock. Nothing needs this today. If something does, it gets its own function that begins a deferred transaction.

### 3. Packages and how they depend on each other

There are two kinds of domain package:

- **Areas** own queries. An area owns its types, its rules and its reads and writes, as functions that take a `store.Runner`, or a `store.Tx` when their statements must succeed together. It imports `store` and `store/sqlcdb`. Example: `companyfilter`.
- **Pure packages** own rules and value types and do no I/O. They import nothing else from this project. Examples: `filter` (`filter.Match`, `filter.Filter`), and later a package for `ApplicationStatus` when two areas need it.

The rules:
- An area may import pure packages, never another area. A value type two areas need moves into a pure package.
- A pure package imports neither areas nor `store`.
- New reads and writes for a moved area go in the area, never on `Store`.
- An area runs the named queries for its tables through `store.Read`/`store.Write`, never by constructing sqlc's `Queries` itself. Splitting sqlc output into one package per area is possible later (a second `sql:` entry in `sqlc.yaml`), but this RFC doesn't need it.
- An area wraps the errors it returns with its name, as the rest of the repo does: `fmt.Errorf("companyfilter: replace: %w", err)`.

**Areas depend on `store`'s types and sqlc's generated code, on purpose.** An area depends on `store.Runner`/`store.Tx` and on sqlc's generated `*sqlcdb.Queries` and row types, rather than on interfaces it defines. `Runner` is an interface, but a sealed one, with no other implementations: it's how one function accepts either `store` type, not a seam for substitution. This repo adds substitutable interfaces when there's a second implementation or a consumer that needs to swap one in. Here there's neither: tests run against real SQLite, and there's one engine. The opaque `DB` and `Tx` are the seam: `store` can change what's behind them without areas noticing.

### 4. The first area: company filters

```go
package companyfilter

func List(ctx context.Context, r store.Runner, companyID int64) ([]filter.Filter, error)
func Replace(ctx context.Context, tx store.Tx, companyID int64, filters []filter.Filter) error
```

- **Returns `filter.Filter`**, the type `filter.Match` already reads. That makes `store.CompanyFilter` and `sync.FilterRules` unnecessary, *if* nothing in production reads a filter's `ID`, `CompanyID` or `CreatedAt`. Step 3 checks that before relying on it.
- **`Replace` takes a `Tx`**: its delete and inserts must succeed together, so it can only run inside a transaction. That fixes its missing transaction, and the compiler enforces it.
- **`List` is one statement**, so it takes a `Runner` and uses `store.Read`.
- **Callers:** `tui/app.go` passes the store's `DB()` to `List` (:823 and :938), and wraps `Replace` (:955) in `store.InTx`.
- **Deleted from `store`:** `CreateCompanyFilter`, `ListCompanyFilters`, `DeleteCompanyFilters`, `ReplaceCompanyFilters`, `CompanyFilter`.
- **Unchanged:** the sqlc queries in `db/queries/company_filters.sql`; the SQL that reads `company_filters` inside posting queries (the search queries, and #366's view), which stays posting knowledge; the `filter` package itself, which stays pure.

What this area proves:
- free functions over a `Runner` or `Tx`, and an area owning its type;
- an area importing a pure package;
- `Read` and `Write` from inside an area, and a `Tx`-taking function called through `InTx`;
- `ErrNotFound` mapped in one place;
- callers and tests moving without behaviour change.

What it doesn't prove: reads that join several areas, paging, or sort order. Those come when postings moves.

### 5. Where `Store` ends up

While areas move, `Store` does two jobs: it hands out a `DB`, and it keeps the methods of areas that haven't moved yet. Each move deletes some of those methods. When the last area has moved, `Store` only hands out a `DB`, and transactions come from `InTx`. At that point `store.New` returns a `DB` directly and the `Store` type is deleted. Each area move works towards that, and none adds anything to `Store`.

## Options considered

1. **Status quo: methods on `Store`.** Cheapest. `store` keeps owning every area, and transactions across methods stay impossible.
2. **A `Reader`/`Writer` interface per area that `store` implements (classic ports and adapters).** Rejected. It would be an interface with one implementation and no fakes (tests use real SQLite), and `store` would still own every area's structure.
3. **`DB` embeds sqlc's `Querier`.** Rejected: `DB`'s surface becomes whatever sqlc generates, so `store` loses ownership of its own contract, and calls never pass through `store`'s code.
4. **`DB` with one hand-written method per query.** `store` owns the list completely, but it means about 83 pass-through methods eventually: the same shallow layer this RFC moves away from. Worth it only if `store` must restrict which queries each area may run, and splitting sqlc output per area does that better.
5. **`InTx` joins the caller's transaction instead of opening a savepoint.** Rejected. A failing inner `fn` would leave its writes in the caller's transaction, and if the caller carried on, they'd be committed. That's the half-done `Replace` this RFC fixes.
6. **One type for both the database and a transaction, with a convention to use the inner one inside `InTx`.** Rejected. Nothing in a signature would say a function needs a transaction. Passing the outer database to an atomic function inside `InTx` would compile, and the bug `Replace` has today could come back through any caller.
7. **Atomic functions that open their own transaction** (`Replace(ctx, r Runner, …)` calling `InTx` itself, plus a `ReplaceTx` for callers that combine calls). Rejected. Every atomic operation would need two functions, and the convenient one hides the requirement the `Tx` type exists to show.
8. **Naming the database type `Conn`, or one combined type `Tx`.** Rejected:
   - `Conn` reads as one pinned connection, as `sql.Conn` is, but a `DB` is the pool.
   - A combined `Tx` would promise atomicity it only has inside `InTx`.
   - Making every call a real transaction to keep the name true would make plain reads take the write lock, because transactions begin `IMMEDIATE`.
9. **`InTx` as a method on `DB` and `Tx`.** Rejected for consistency. `Read` has to be a function, since Go methods can't take type parameters. With `InTx` a function too, `DB` and `Tx` have no exported methods, and everything that runs on them reads the same way.
10. **This RFC: areas as packages of functions over `store.Runner` (or `store.Tx` when atomic), running sqlc queries through `store.Read`/`Write`.**

## Work breakdown

Each step is its own PR, built test-first. Existing tests stay green throughout.

1. **Rename the generated package `store/db` to `store/sqlcdb`.** Change `package` and `out` in `sqlc.yaml`, regenerate, and update the imports in `store`'s 18 files. No behaviour change. *Done:* `go tool task sqlc:generate` produces `store/sqlcdb`, nothing imports `store/db`, and the full checklist passes.
2. **`store.DB`, `store.Tx`, `store.Runner`, `store.Read`, `store.Write`, `store.InTx` and `Store.DB()`.** *Done:* tests that:
   - a write in `InTx` is rolled back when `fn` returns an error;
   - a failing `InTx` on a `Tx` undoes only its own writes, and the outer transaction's earlier writes commit; the same holds two levels deep, using the one savepoint name;
   - a write on the outer `DB` inside `InTx` fails with `SQLITE_BUSY` once `busy_timeout` passes, using a short timeout in the test;
   - change-origin stamping applies inside a transaction and a savepoint;
   - `Read` maps `sql.ErrNoRows` to `ErrNotFound`;
   - a zero `DB` or `Tx` returns an error from `Read`, `Write` and `InTx`.
3. **The `companyfilter` package: `List` (takes a `Runner`) and `Replace` (takes a `Tx`).**
   - *Done:* `Replace` leaves the old filters in place when an insert fails, both in its own `InTx` and as part of a caller's transaction that then commits. This is the regression test for the missing transaction.
   - Confirm nothing in production reads a filter's `ID`, `CompanyID` or `CreatedAt`. If something does, `companyfilter` gets its own type instead of `filter.Filter`.
4. **Callers move.** `tui` calls `companyfilter.List` with the store's `DB()`, and wraps `companyfilter.Replace` in `store.InTx`. Delete `sync.FilterRules`, or move it into `tui` if #367 hasn't removed its last caller yet. Delete the four `store` methods and `store.CompanyFilter`.

## Test impact

- **New:** `store` tests for `DB`, `Tx`, `Read`, `Write` and `InTx`; `companyfilter` tests for `List`, `Replace` and both rollback cases.
- **Moved:** `store/company_filter_test.go` becomes `companyfilter` tests.
- **Fixtures:** `store` tests that set up filters (`posting_search_test.go`, `scoring_test.go`) are in package `store`. They can't import `companyfilter`, because it imports `store` and that would be an import cycle. They get an unexported helper that inserts filter rows directly. The same applies to every later area whose tables `store`'s remaining tests use.
- **Updated:** `sync`, `tui` and `stage` tests that create filters call `companyfilter.Replace`.
- **Unchanged:** the migration and change-log tests, and `store/open_test.go`.

## Where it can go wrong

- **Two styles during the move.** `Store` methods and area functions coexist until every area has moved, and each later move has to finish its area. If moves stall, `store` has a hole in it rather than a cleaner design.
- **Test fixtures inside `store`.** Every moved area's tables still have to be set up by `store`'s own tests, through helpers, until those tests move too. If that grows large, moving `store`'s tests to an external test package (`package store_test`) is the fix.
- **Import cycles between areas.** If two areas can't avoid needing each other's types, the split is in the wrong place, or a type belongs in a pure package. Company filters has no such neighbours; postings will be the real test.
- **Using the outer `DB` for a single statement inside `InTx`.** Functions that must be atomic take a `Tx`, so the compiler stops the outer `DB` reaching them. A single-statement function takes a `Runner`, so it still accepts the outer `DB`. That write fails loudly with `SQLITE_BUSY` (step 2's test), and that read misses the transaction's own writes. If that read case ever bites, the fix is a function that takes the `Tx`.
- **Callers write `InTx` around every atomic call.** That's deliberate. If wrapping the same call becomes common, the fix is a caller-side helper in that caller, not a second function in the area.
- **Savepoints add a code path.** Nested `InTx` is a `SAVEPOINT`/`RELEASE`/`ROLLBACK TO` sequence on the transaction's connection. It's small, but it's the part of `store` most worth testing directly.

## Open questions

1. **Order with #364–#367.** Those change posting queries, not filter queries, so they don't conflict. But step 1's package rename touches every file those issues change in `store`. Should this RFC wait for them, or go in parallel and rebase?
2. **Which area moves next?** Postings reads, once #366 and #367 land, would test reads across areas. Or another small area first?

## Out of scope

- **Moving any area other than company filters**, and splitting sqlc output per area.
- **The posting-visibility change** (#364–#367).
- **Deleting `store` APIs with no production callers**: tags, interview stages, `RestoreCompany`, `MarkPostingClosed`/`MarkPostingReopened`, `ChangeFeed`. Worth an issue of their own.
