# RFC 0011: Reads and writes that span areas

- **Status:** Accepted, 2026-10-15, with the open questions' defaults: `pursuit`, `sync` owns company deletion, the order as listed (companies moved after postings; see step 6).
- **Date:** 2026-10-15
- **Related:** RFC 0010 (areas own their queries; an area never imports another area); #379 (document records, blocked on this RFC)

## Summary

RFC 0010 split `store` into areas that each own their types and queries, with one rule: an area never imports another area. Company filters, scoring and posting markup have moved under that rule. What's left in `store` includes five writes and one read that span areas, and the rule says nothing about where those go.

This RFC proposes:

1. **SQL may cross areas; Go types may not.** A query may read any table. Its result is the owning area's own type, built only from pure types. This is already how `SearchPostings` and `scoring.Pending` work.
2. **Use-case packages sit above the areas.** A use-case package may import several areas. It owns the rule that spans them, and runs the areas' steps in one `store.InTx`. Areas expose single-area steps that take a `store.Tx`.
3. **The use-case packages mostly exist already.** `sync` already owns companies' and postings' lifecycle, and `stage` already composes what the agent sees. Only the active-applications view needs a new home.

## Problem

These `store` operations span areas. All of this is from reading the code.

| Operation | Areas it touches | Callers |
|---|---|---|
| `ClosePosting` | postings (status, history), applications (ends early ones, their status history) | `sync` |
| `ReopenPosting` | postings, applications (restores the ones closing ended) | `sync` |
| `IngestPosting` | postings (create/update, history), posting markup (creates the row) | `sync` |
| `SoftDeleteCompany` | companies, postings (closes the company's open ones) | `tui` |
| `CreateApplication` | applications; reads postings (refuses a closed one) | `stage`, `tui` |
| `ListActiveApplications` | returns `ApplicationView`: an application with its `Posting`, company name and latest `DocumentReview`s | `stage`, `tui` |

Each write is one transaction today, and that's what keeps them correct. RFC 0003 and #147/#148 made closing and reopening atomic for exactly this reason. Once postings and applications are separate areas, nothing in either one may do the other's half.

`ListActiveApplications` is the read that blocks #379. `ApplicationView` carries a `DocumentReview`, so whichever package owns the view must be able to import document records. While the view is in `store`, `store` can't import `docrecord`, because `docrecord` imports `store`.

## Proposal

### 1. Reads: SQL crosses, types don't

- A query may join or read any table.
- Its result type belongs to the area that owns the query. It may contain pure types (`applicationstatus.Status`, `filter.Filter`), never another area's type.
  - **Already true of `SearchPostings`:** a posting listing carries its application's status and its markup flags as plain fields.
  - **Already true of `scoring.Pending`:** it reads postings, markup and applications to decide what's pending.
- `CreateApplication` refusing a closed posting is a read of the postings table inside an applications query, so it stays in the applications area.

A read that has to return *another area's type*, as `ApplicationView` does with `Posting` and `DocumentReview`, belongs one layer up (section 3).

### 2. Writes: areas expose steps, use cases compose them

Each area exposes the single-area half of a cross-area write as a function that takes a `store.Tx`:

```go
// postings area
func Close(ctx context.Context, tx store.Tx, postingID int64) (Closed, error)   // status + posting history
func Reopen(ctx context.Context, tx store.Tx, postingID int64) (Reopened, error)
func Ingest(ctx context.Context, tx store.Tx, p IngestParams) (Ingested, error)

// applications area
func EndForPosting(ctx context.Context, tx store.Tx, postingID int64, from []applicationstatus.Status) error
func RestoreForPosting(ctx context.Context, tx store.Tx, postingID int64) error

// posting markup area
func Create(ctx context.Context, tx store.Tx, postingID int64) error

// companies area
func SoftDelete(ctx context.Context, tx store.Tx, companyID int64) error
```

The rule that joins them lives in the use-case package that owns it, which runs the steps in one transaction:

```go
// sync
err := store.InTx(ctx, s.db, func(tx store.Tx) error {
	if _, err := postings.Close(ctx, tx, id); err != nil {
		return err
	}
	return applications.EndForPosting(ctx, tx, id, earlyApplicationStatuses)
})
```

Nesting `InTx` (RFC 0010's savepoints) keeps each step atomic within the whole, and the `Tx` parameter means no step can be called outside a transaction.

### 3. The use-case packages

The layers, from the bottom:

1. pure packages
2. `store`
3. areas
4. use-case packages
5. `tui`, `cmd/swamp`, `mcpserver`

An area never imports another area or a use-case package. A use-case package may import any area and other use-case packages. Nothing imports `tui`, `cmd/swamp` or `mcpserver`.

| Cross-area operation | Owner | Why there |
|---|---|---|
| Close a posting and end its early applications | `sync` | `sync` is its only caller and already owns `earlyApplicationStatuses` |
| Reopen a posting and restore its applications | `sync` | Same |
| Ingest a posting and create its markup row | `sync` | Only caller |
| Delete a company and close its postings | `sync` | `sync` already owns company lifecycle (`Syncer.CreateCompany`, `AddCompany`). Today `tui` calls `store.SoftDeleteCompany` directly; it would call `sync` instead, as it does to create one |
| The active-applications view (`ApplicationView`) | a new use-case package, `pursuit` (Q1) | Both `tui` and `stage` read it, and it combines applications, postings, companies and document records |

`stage` is already a use-case package: it composes what the agent reads and writes. It stays one, and it reads the view from `pursuit` rather than building its own.

## Options considered

1. **Let areas import each other, as long as there are no cycles.** Rejected. Postings and applications already need each other (close ends applications; creating an application checks the posting), so a cycle comes almost at once. Each new feature would also have to decide which way the arrow points.
2. **Interfaces or callbacks between areas** (postings calls an `ApplicationEnder` it's given). Rejected. It adds indirection for one implementation, and hides a business rule behind a hook instead of naming it where it's decided.
3. **Leave cross-area operations in `store` for good.** Rejected. `store` would keep knowing postings, applications and companies, which is what RFC 0010 set out to end, and #379 stays blocked.
4. **One `domain` package for everything that spans areas.** Rejected. It would just become `store` again, with every rule in one package.
5. **This RFC:** SQL crosses areas but types don't, and cross-area rules live in the use-case package that owns them.

## Work breakdown

Each step is its own PR, built test-first, with the full checklist.

1. **The `pursuit` package.** `store.ListActiveApplications` keeps the application, posting, company name and status-since, without reviews. `pursuit` adds each application's latest reviews, through `store` until #379. `tui` and `stage` read the view from `pursuit`. This unblocks #379, because `store` no longer holds a `DocumentReview`.
2. **#379: document records.**
3. **The applications area**, with `EndForPosting` and `RestoreForPosting` as `Tx` steps. `store.ClosePosting` and `ReopenPosting` can't import an area, so they keep their own application updates, through the generated queries, until postings move.
4. **#364 and #365**, fixed in `store` before the postings move. #366 and #367 fold into the move.
5. **The postings area.** `Close`, `Reopen` and `Ingest` become `Tx` steps, and `sync` composes close, reopen and ingest. `store.ClosePosting`, `ReopenPosting` and `IngestPosting` are deleted.
6. **The companies area**, with `SoftDelete`. `sync.DeleteCompany` composes it with `postings.Close` for each open posting, `tui` calls `sync`, and `store.SoftDeleteCompany` is deleted. Companies comes last: until postings are an area, nothing outside `store` could close a company's postings in the same transaction.

## Test impact

- **Atomicity tests move to `sync`, against real SQLite.** Close or reopen leaves the posting and its applications consistent when either half fails. These are the RFC 0003 / #147 / #148 tests.
- **Each area's step is tested on its own.**
- **`pursuit`'s view test** replaces `store`'s `ListActiveApplications` tests.

## Where it can go wrong

- **A caller skipping the use case.** Nothing stops `tui` calling `postings.Close` directly and leaving applications open. The steps take a `Tx`, so it can't happen by accident outside a transaction. But only review and naming keep the policy in `sync`.
- **`sync` grows.** It becomes the owner of posting and company lifecycle as well as syncing. If that's too much, the lifecycle half can split into its own use-case package later, without touching the areas.
- **The view's query.** `ListActiveApplications` is one sqlc query today. `pursuit` keeps it as one query, running it through `store.Read`, rather than composing it from three area reads one row at a time.

## Open questions

1. **What should the view package be called?** `pursuit` is a placeholder for "what the user is actively pursuing". Something else?
2. **Should `sync` own company deletion?** It already creates companies, so that keeps their lifecycle in one place. Or would you rather have a separate lifecycle package?
3. **The order of steps 3–6.** Applications before companies and postings, as listed, or postings first, after the two bug fixes?

4. **`store.Read` and `store.Write` promise a read/write split they don't enforce.** Both hand the function every generated query, and `postingmarkup` already sends `UPDATE … RETURNING` through `Read`. The options are honest names (`Query`/`Exec`, or one function) or a read-only connection pool for `Read` on a `DB`. Deferred until the moves are done.

## Out of scope

- **Moving the change-log reader** (`ChangeFeed`, `LatestChangeEventID`, `ChangeEventsAfter`). It's infrastructure and stays in `store`.
- **Deleting the remaining single `Store` methods with no outside callers.** They move with their area, or get deleted then, case by case.
