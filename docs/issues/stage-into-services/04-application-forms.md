---
title: "[Stage into services 4] Application forms are PostingService's"
labels: [stage-into-services]
depends_on: []
---

## Problem

`PostingService.Form` and `SaveForm` deal in a JSON string, so each caller encodes and decodes `jobboard.ApplicationForm` itself: `stage/form.go:52` and `:73`, `tui/application_form.go:232` and `:251`.

The policy for when a form is reused or fetched is in `stage.applicationForm` (`stage/form.go:46`):

- A source with no fetcher (Ashby, Lever) gets whatever was entered by hand, however old.
- A stored form under `formMaxAge` (7 days) is reused.
- Otherwise the form is fetched, bounded by the fetch timeout, and stored.
- A failed fetch isn't an error: the stale form comes back, if there is one, with a message saying what failed.

`stage` also holds the `FormFetcher` interface, the fetchers keyed by source and the clock (`WithFormFetchers`).

## Change

- `Form` and `SaveForm` take and return `jobboard.ApplicationForm`. The TUI's and stage's encode/decode go.
- The fresh-or-fetch policy becomes a `PostingService` method. `FormFetcher`, the fetchers, the timeout and the clock move to the service, which declares the interface (AGENTS.md: the consumer declares it). `cmd/swamp` hands them to `NewPostingService` and not to `stage.New`.
- A stored form is never fetched again (Bob, 2026-11-10): the weekly refetch rested on no evidence that forms change apart from their postings. With no staleness there's no clock, and a failed fetch never has a stale form to fall back on.

## Decide before starting

- **How a failed fetch is returned.** Today it's three values: form, a message, error. Under AGENTS.md a struct with an error is a pointer, `nil` when the error isn't. Options: a result `{Form *jobboard.ApplicationForm; FetchError string}` where a fetch failure isn't an `error`, or returning the stale form beside a wrapped fetch error the caller tests for. Recommended: the result struct, as drafting goes ahead either way.
- **How the fetchers and clock reach the service.** Options on `NewPostingService`, or parameters. Sync's constructor takes its fetchers as a map; follow that.

## Acceptance criteria

- [x] No `json.Marshal`/`Unmarshal` of an `ApplicationForm` outside `internal`.
- [x] Service tests for each branch of the policy: a stored form is read without a fetch (with and without a fetcher), no stored form is fetched and stored, a fetch failure reads no form, no fetcher and nothing stored reads no form. Moved from `stage/form_test.go`, run one at a time.
- [x] `stage/form.go` is gone or calls through.
- [ ] Manual check: the TUI's application form screen loads and saves a form against real data.
- [x] Verification checklist clean.
