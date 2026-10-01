# RFC 0003: Split `SyncCompany`'s two loops and state what a failed sync leaves behind

- **Status:** Revised 2026-10-01 against the code after RFC 0001's P4 work; ready for work. Step 1: #209. Step 2: #210. See "Work breakdown".
- **Date:** 2026-09-28 (revised 2026-10-01: re-checked against the code; much of the earlier problem was fixed by #147/#148, see "Corrections to the earlier draft")
- **Related:** RFC 0001 P4 (#147 atomic close, #148 atomic ingest/reopen, #150 sync lease); issue #105 (a closing posting ends an early-stage application); #174 (should a closing posting end a submitted application, and should reopening restore it); `decisions.log` 2026-09-29, "Close a posting and its application in one transaction (#147)"

## Summary

`syncHeld` (`sync/company.go:140`) is a sync's work once it holds the company's lease. It does two things in one function:

- **Ingest:** save each fetched posting that matches the company's filters (create, update or reopen).
- **Close:** close each stored open posting the fetch didn't return, and end its application if it's still at an early stage (#105).

RFC 0001's P4 work moved every write into one transaction per posting (`store.IngestPosting`, `ReopenPosting`, `ClosePosting`), with the history rows and the application change inside them. That fixed most of what the earlier draft complained about. Two things are left:

1. **What a failed sync leaves behind isn't written down or tested.** Any error after the fetch returns at once. Postings already saved stay saved, the close pass doesn't run, and `MarkCompanyFetched` doesn't run. Each posting's change is atomic; the sync as a whole is not. Nothing says this is intended, and only the fetch-failure case is tested.
2. **The two loops aren't named, and the close policy isn't next to the loop that uses it.** `earlyApplicationStatuses` (`sync/company.go:299`) sits between `ImportCompanies` and `AddCompany`, not beside the close pass that passes it to `store.ClosePosting`.

This RFC proposes fixing (1) with a doc comment and a test, then (2) by splitting `syncHeld` into two named methods and moving the close pass and its policy into `sync/reconcile.go`. Where the close policy should finally live is left to #174.

## How it works today

```go
func (s *Syncer) SyncCompany(ctx, companyID) (Result, error) {
	// get company, pick fetcher, acquire lease (ErrSyncInProgress if held),
	// defer release
	return s.syncHeld(ctx, company, fetcher, result)
}

func (s *Syncer) syncHeld(ctx, company, fetcher, result) (Result, error) {
	fetched := fetch(...)               // the only step ctx can cancel (#149)
	rules := FilterRules(ListCompanyFilters(...))

	// ingest
	for _, p := range fetched {
		if !filter.Match(p, rules) { continue }
		ingested := store.IngestPosting(...)     // one tx: create or update + history
		if ingested.Posting.ListingStatus == "closed" {
			store.ReopenPosting(...)             // one tx: reopen + history
		}
	}

	// close
	for _, existing := range ListPostingsByCompany(...) {
		if existing is open and not in fetched {
			store.ClosePosting(existing.ID, earlyApplicationStatuses)
			// one tx: close + history + application -> posting_closed + status history
		}
	}

	MarkCompanyFetched(...)
	return result, nil
}
```

Every `err != nil` in `syncHeld` returns `result, err`. On an error, callers show the error and not the counts: the TUI shows `msg.err` (`tui/app.go:968`), and `swamp fetch` prints `error: ...` and leaves the counts out (`cmd/swamp/main.go:206`).

## Options

### 1. A `reconciler` type that owns both loops (the earlier draft's option 1)

Not worth it now. Its purpose was to make the close policy testable without a fetcher. `store.TestClosePosting` (`store/posting_test.go:284`) already does that for the mechanism, table-driven. What's left in `sync` is one slice of statuses.

### 2. Split `syncHeld` into two named methods (recommended, step 2)

```go
func (s *Syncer) syncHeld(ctx, company, fetcher, result) (Result, error) {
	fetched, rules, err := ... // fetch, sanitize, filters: as today
	result, err = s.ingest(ctx, company, fetched, rules, result)
	if err != nil {
		return result, err // the close pass doesn't run: the contract step 1 pins
	}
	result, err = s.closeMissing(ctx, company, fetched, result)
	if err != nil {
		return result, err
	}
	// MarkCompanyFetched as today
}
```

The calls stay sequential, with an error check between them: running the close pass after a failed ingest would change what step 1 pins.

`closeMissing` and `earlyApplicationStatuses` move to a new `sync/reconcile.go`. `company.go` keeps `SyncCompany`, `syncHeld`, `ingest`, the conversion helpers and company management. Tests stay at `SyncCompany`'s boundary: `ingest` and `closeMissing` are unexported, so they aren't tested directly (AGENTS.md: test behavior, not unexported functions).

**Pros:** each loop has a name; the close pass sits next to the only policy it uses; no change to signatures or behavior.
**Cons:** it's a readability change only. No test gets simpler.

### 3. Write down and pin the partial-failure contract (recommended, step 1)

A doc comment on `SyncCompany` stating what a failed sync leaves, and a test that proves it. No design risk, and it protects step 2's refactor. Do it first.

## Work breakdown

**Step 1: pin what a failed sync leaves behind (#209).** S. No dependencies.
- Doc comment on `SyncCompany`. On an error after the fetch:
  - every posting change already made stays committed (each was its own transaction, #147/#148);
  - postings not yet reached are untouched;
  - the close pass doesn't run;
  - `MarkCompanyFetched` doesn't run, so "Last fetched" stays stale;
  - `Result`'s counts cover only what ran, and callers don't report them.
  The next clean sync finishes the job.
- Test: two fetched postings, plus a stored open posting the fetch no longer returns. A trigger fails saving the second fetched posting. Assert:
  - `SyncCompany` returns an error;
  - the first posting is saved;
  - the second is not;
  - the missing posting is still open, with no history;
  - `LastFetchedAt` is unchanged;
  - `Result` has `Created` 1 and `Closed` 0.
  Then drop the trigger, sync again, and assert all of it completes.
- Should pass without code changes. If it doesn't, the behavior has drifted from the description: stop and decide which is right before going on.

**Step 2: name the two loops and move the close pass into `sync/reconcile.go` (#210).** S. After step 1.
- Option 2 above. A pure refactor: no test changes beyond those step 1 added, and every test in `sync/` passes unmodified.

**Not filed: where the close policy lives.** #174 decides whether a closing posting still ends a submitted application, and whether a reopened posting restores one. If it chooses to restore (its option 2b), that's a second rule about applications in the sync path. Look then at whether the rules belong in one named type, or with the application lifecycle in `store`. Until then, a slice passed to `ClosePosting` is enough.

## Test impact

- **New (step 1):** the partial-failure test above.
- **Already exists (no work needed):**
  - The close mechanism, without a fetcher: `store.TestClosePosting`.
  - Per-posting atomicity: `TestClosePosting_HistoryFailure_LeavesPostingAndApplicationOpen` (`store/application_status_history_test.go:163`), `TestSyncCompany_PostingCloses_ApplicationUpdateFails_NothingHalfClosed` and `TestSyncCompany_ContentUpdateFails_NoHistoryUntilItHappens` (`sync/company_test.go:545`, `:728`).
  - A failed fetch leaves "Last fetched" alone: `TestSyncCompany_RecordsLastFetchedAtOnlyOnSuccess`.
- **Unchanged (step 2):** the `TestSyncCompany_PostingCloses_*` tests (`sync/company_test.go:424`–`:545`) keep going through `SyncCompany`. They test which statuses a closing posting ends, and that is `sync`'s policy.

## Corrections to the earlier draft

| Earlier draft said | Actually (2026-10-01) |
|---|---|
| `SyncCompany` is 102 lines doing everything | `SyncCompany` (`:103`) takes the lease; `syncHeld` (`:140`) does the work (#150). |
| `recordHistory` calls sit between state changes in both loops | Gone. History rows are written inside `store.IngestPosting`/`ReopenPosting`/`ClosePosting`'s transactions (#147, #148). |
| `closeApplicationForClosedPosting` (`:303`) is the only code that changes applications | Gone. `store.ClosePosting` does it in the posting's transaction; `sync` passes `earlyApplicationStatuses` in. `store.SoftDeleteCompany` also closes postings (#178) but leaves applications alone. |
| The close policy can only be tested by running a fake fetcher | The mechanism is tested without one: `store.TestClosePosting`. Only the choice of statuses still goes through `SyncCompany`, which is the right boundary for it. |
| LOOP 1 is a `switch` over a lookup, with independent update and reopen checks | One `IngestPosting` call, then `ReopenPosting` if the stored posting was closed. Both can still happen in one pass. |
| An error strands the database "partially synced" | Still true per sync, but each posting's change is now atomic. What's half-done is the set of postings, never one posting. |
| Proposed test injects a `recordHistory` failure | No such call. Fail a posting write with a trigger, as the existing failure tests do. |
| Tests can call `reconcileClosed` directly | Unexported; tests stay at `SyncCompany`. |
| Option 1 is the follow-up if a second rule appears | That rule may come from #174 (restore on reopen). Decide then where the rules live, not necessarily a `reconciler` in `sync`. |

## Open questions

1. **Should a failed sync do more before it stops?** This RFC pins today's behavior, not changes it. The close pass decides "missing" from the fetch, not from what was saved, so running it after a failed ingest wouldn't wrongly close the posting that failed. Running it anyway, or carrying on past one failed close, is plausible, but it's a behavior change for its own issue.
2. **Should `Result.ApplicationsClosed` be separate from `Closed`?** It's the only `Result` field about applications rather than postings. Leave it: it's public, and the TUI, CLI and MCP read `Result`.

## Out of scope

- Making a whole sync one transaction, or continue-on-error (open question 1).
- Moving the close policy into `store` (#174 decides first).
- `ApplyCompanyFilters`, `CreateCompany`, `AddCompany` and `ImportCompanies`. They share `s.fetchers` with `SyncCompany` but not its loops.
