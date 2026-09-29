# RFC 0003: Deepen `SyncCompany`'s reconcile loop into a dedicated module

- **Status:** Draft, for discussion
- **Date:** 2026-09-28
- **Related:** issue #105 (a closing posting ends an application that's still at an early stage); `decisions.log` entries on `closeApplicationForClosedPosting` (the one place sync reaches past postings into application state)

## Summary

`SyncCompany` (`sync/company.go`, 102 lines) conflates two responsibilities in one straight-line procedure: **ingesting** what the board returned (fetch, gate each fetched posting through the company's filters, upsert — create, update, or reopen) and **reconciling** what's stored against it (find every stored-open posting that didn't come back in the fetch, close it, and — the one deliberate exception — end any application still at an early stage). The second half is where the only application-state touch in the whole sync path lives, and it's not separable from the first half without reading through all of it: it's a `switch` over the create/update/reopen lookup with `if existing.ListingStatus == "closed"` nested inside the stored-posting branch, plus a second `for` loop for the closing pass, with `recordHistory` calls wedged between the state changes in both, and the "leave an application alone if it's past this stage" rule lives in an unexported `var`, not a function, so testing it means running a fake fetcher, seeding a company, and checking `Result` fields after the fact.

This RFC proposes splitting the loop that does (create/update/reopen) and the loop that does (close posting, and possibly close its application) into two named `*Syncer` methods, moving the close-policy pieces (`closeApplicationForClosedPosting`, `earlyApplicationStatuses`) and one of the `recordHistory` call sites into a new `sync/reconcile.go` file, and — independently of any of that — writing down and pinning by test the partial-failure behavior (what happens to the database and to `Result` when the fetch or a `recordHistory`/store write fails partway through) since it's currently implied by the shape of the code but never stated anywhere.

## Problem

- **The close-and-close-application policy is the only cross-domain behavior in the sync path, and it's the hardest to reach.** `closeApplicationForClosedPosting` (`sync/company.go:303`) and `earlyApplicationStatuses` (`sync/company.go:289`) are the only code anywhere that mutates `applications` as a side effect of a sync. To test "an application at `started`/`submitted` gets moved to `posting_closed`, and one at `interviewing`/`offer_received`/`offer_accepted`/`withdrawn` does not," the only available hook is the `TestSyncCompany_PostingCloses_*` tests (`sync/company_test.go:422`–`:512`), each of which builds a company, does a successful `SyncCompany` first, then empties the mock fetcher's postings and runs `SyncCompany` again. There's no way to exercise the policy without a working fetcher in scope.
- **There's no shared unit for "is this application at a stage where ending the posting ends it."** The rule is a `slices.Contains` over an unexported slice (`sync/company.go:312`), not a function you can point a test at on its own. If a second "close-related" application rule ever shows up (e.g. a future rule about which statuses count as "still early" for some other purpose), it has nowhere to live other than inside `SyncCompany` again.
- **The partial-failure contract is implied, not stated.** Every step in `SyncCompany` does `return result, fmt.Errorf(...)` — the fetch, the filter list, every upsert/history call, the closing loop, the final `MarkCompanyFetched`. That means an error at step 3 of 11 strands the database partially-synced (some postings created, none reconciled, `MarkCompanyFetched` never run, `Result` half-filled) with no test asserting that shape and no comment saying "this is intended." A reader has to trace the `switch`/`if` chain to discover it.

## How it works today

Abridged body of `SyncCompany` (`sync/company.go:112`–`213`), with the two policy-relevant pieces highlighted:

```go
func (s *Syncer) SyncCompany(ctx context.Context, companyID int64) (Result, error) {
	result := Result{CompanyID: companyID}
	// 1. resolve company, pick fetcher from s.fetchers[company.Source],
	//    fetch, sanitize each posting, set result.Fetched
	// 2. ListCompanyFilters -> FilterRules(filters)

	// LOOP 1: for each fetched posting
	for _, p := range fetched {
		if !filter.Match(...) { continue }
		existing, err := GetPostingBySourceAndSourceID(...)
		switch {
		case not found:    UpsertPosting(...)                                          -> Created++
		case lookup error: return result, err
		default:           // already stored -- two INDEPENDENT checks, both may fire in one pass:
			if content changed: recordHistory(...); UpsertPosting(...)             -> Updated++
			if ListingStatus == "closed": recordHistory(...); MarkPostingReopened(...) -> Reopened++
		}
	}

	// LOOP 2: for each stored, open posting missing from the fetch
	for _, existing := range existingPostings {
		if existing.ListingStatus != "open" || alreadySeen { continue }
		recordHistory(existing, "closed")
		MarkPostingClosed(existing.ID)              -> Closed++
		closed := closeApplicationForClosedPosting(existing.ID)  // <- the one application-state change
		if closed { result.ApplicationsClosed++ }
	}

	MarkCompanyFetched(companyID)
	return result, nil
}
```

The close policy is grouped with its own rule — `earlyApplicationStatuses` (`sync/company.go:289`) immediately precedes `closeApplicationForClosedPosting` (`sync/company.go:303`), the two together occupying :283–:322 — but that block sits **sandwiched between two company-creation functions** (`ImportCompanies` declared :259; `AddCompany` declared :352): filed inside the company-management code and not next to `SyncCompany`, the only code that calls it. (Also note the LOOP 1 sketch above is faithful to the real control flow, not a simplification: the `default` branch's two checks are independent, so a posting that is both content-changed and closed is updated **and** reopened in the same pass, incrementing both `Updated` and `Reopened`.)

## Options

### 1. Extract the closed-posting/application logic into a `reconciler` type

```go
type reconciler struct {
	store *store.Store
}

func (r *reconciler) reconcile(ctx context.Context, company *store.Company,
	fetched []jobboard.Posting, rules []filter.Filter) (Result, error)
```

`SyncCompany` becomes: resolve company/fetcher/fetch, build `rules`, hand `fetched` to `newReconciler(s.store).reconcile(...)`, set `result.Fetched`, return. `reconcile` owns LOOP 1, LOOP 2, `recordHistory`, `closeApplicationForClosedPosting`, and `shouldCloseApplication(status) bool` (a genuine, testable, fetcher-free helper the current inline `slices.Contains` doesn't give us).

**Pros:** the close-policy becomes independently unit-testable with zero fetcher in scope; `Result` stays the public shape for all callers (TUI, CLI, MCP); `SyncCompany` drops to ~15 lines.

**Cons:** a new unexported type in a package that currently has none; the "who owns the fetch, who owns the reconcile" line has to be drawn deliberately rather than inherited from one big function.

### 2. Split into two named methods on `*Syncer`, and group the close-policy in its own file

```go
func (s *Syncer) SyncCompany(ctx context.Context, companyID int64) (Result, error) {
	// fetch setup as-is: resolve company, fetcher, fetch, sanitize, build rules
	result, err := s.ingestFetched(ctx, company, fetched, rules)
	if err != nil {
		// Mid-ingest failure abandons the close pass -- the exact contract
		// option 3 pins. `result` is the half-filled value, as today.
		return result, err
	}
	return s.reconcileClosed(ctx, company, fetched, result)
}
```

The two halves run **sequentially and share one `Result`** (ingest sets `Fetched/Created/Updated/Reopened`; reconcile appends `Closed/ApplicationsClosed`) and one error chain. That ordering is deliberate, not incidental: the close pass must *not* run after an ingest error. A tempting `return s.ingestFetched(...), s.reconcileClosed(...)` would run both unconditionally — Go's `return f(), g()` evaluates `g()` even when `f()` returned an error — and would silently change the contract option 3 pins. The sketch is sequential for exactly that reason.

`ingestFetched` is LOOP 1; `reconcileClosed` is LOOP 2. `recordHistory`, `closeApplicationForClosedPosting`, and `earlyApplicationStatuses` move to a new `sync/reconcile.go`; `company.go` keeps `SyncCompany`, `FilterRules`, the `to*` helpers, and the company-creation code. The close policy stays one method call away from being pure, but is now at least one named call (`reconcileClosed`) instead of buried under an inline `if` in a 10-step chain, and it can be tested through `reconcileClosed` directly with a seeded `store.Store`.

**Pros:** smaller diff than option 1 — no new type, `SyncCompany`'s signature and behavior unchanged, and the file-level split ("company management" in `company.go`, "close reconciliation" in `reconcile.go`) is the actual thing that was hard to read.

**Cons:** less than option 1 in terms of making the close-policy a genuinely independent, fetcher-free unit — a second caller of `shouldCloseApplication`-style logic would still need to find `closeApplicationForClosedPosting`, which is still a method, not a free function.

### 3. Do nothing structural, just write down the partial-failure contract and pin it with a test

Cheapest possible change, zero new code: add one doc comment to `SyncCompany` stating that an early error leaves the database partially synced (created/updated postings committed, closing loop and `MarkCompanyFetched` not run), and add a `TestSyncCompany_MidLoopError_LeavesDBHalfSynced_AndSkipsMarkCompanyFetched` that proves it. This is worth doing in any case — it's the one piece of the four options above that carries no downside — but on its own it doesn't fix the "has to run a fetcher to test the close policy" problem.

## Test impact

- **New, regardless of option:** a test that injects a failure at a `recordHistory` call mid-loop and asserts the database has some `created` postings, *none* of the closed-posting reconciliation ran, `MarkCompanyFetched` was skipped, and `Result`'s `Closed`/`Reopened`/`ApplicationsClosed` are all zero. This is the only test that currently doesn't exist and that every option above should add, since it's the actual contract nobody wrote down.
- **Simpler under options 1 or 2:** `TestSyncCompany_PostingCloses_EarlyStageApplicationClosedToo` and `:LiveApplicationLeftAlone` (`sync/company_test.go:422`, `:458`) keep their assertions but shed the "run a successful `SyncCompany` first, then empty the fetcher" setup, since they can call `reconcileClosed` (or `reconciler.reconcile`) directly with a seeded store.
- **Unchanged:** every other test in `sync/` — `SyncAll` per-company isolation (`sync_all_test.go`), `SyncCompany` create/update/reopen flows, `company_*_test.go` — passes unmodified since no signature or observable behavior changes.

## Recommendation

Go with **option 2** (two named `*Syncer` methods, close-policy helpers grouped into `sync/reconcile.go`) for this pass — it's the smallest change that actually separates the two loops, it doesn't introduce a new type into a package that currently has none, and it leaves `company.go` free to keep the genuinely company-management code. Treat option 1 (`reconciler` type) as the follow-up if a second "close-related, reaches past postings into application state" rule ever shows up — that's the signal the inline-`method` scoping of option 2 is too small and the close policy has genuinely grown into its own thing.

Do **option 3's** part (write down and pin the partial-failure contract) as a standalone, no-dependency change regardless of which of options 1/2 gets picked for the loop split — that test and comment carry no design risk and the highest payoff-per-line of anything in this RFC.

## Open questions

1. **Is the closing-loop error behavior actually the right one?** This RFC pins it, not changes it. But "first error in the create/update/reopen loop abandons the closing loop entirely" is a design choice worth a second look on its own (e.g. should a failure closing posting #1 of #3 leave postings #2 and #3 open, or should the closing loop be best-effort-continue?) — that's a behavioral change, out of scope here, but the pinned test makes the current choice at least *visible*.
2. **Should `Result.ApplicationsClosed` be a separate field from `Result.Closed` at all?** It's currently the only field in `Result` that reflects application state, not posting state. It's useful for the TUI's sync summary line to say "closed 3 postings, 2 applications" — but it also means `Result` quietly spans two domains. Leave it as-is (it's already public and changing it is a breaking API change for TUI/CLI/MCP), but worth a one-line note in any doc that explains `Result`.

## Out of scope

- Refactoring the create/update/reopen loop (LOOP 1) into itself its own thing, separate from the close loop. It's less entangled with application state than LOOP 2 and doesn't have the same "has to run a fetcher to test it" problem; splitting it is polish, not fix.
- Making the failure behavior transactional or "continue-on-error" (see open question 1). That's a behavioral change with real user-visible consequences and belongs in its own issue.
- `ApplyCompanyFilters`, `CreateCompany`, `AddCompany`, `ImportCompanies` — all share `s.fetchers` with `SyncCompany` but not the reconcile loop, and are already cleanly separated.
