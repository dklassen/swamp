---
title: "[Stage into services 13] Syncing a company fetches the forms its open postings lack"
labels: [stage-into-services]
depends_on: [11]
rfc: 0002
rfc_sections: ["Phase 2: Match what each form asks for", "Wave D — match what each form asks (phase 2)"]
---

## Problem

A Greenhouse posting's form is fetched only when drafting starts on it. Until then Swamp doesn't know what the posting asks for, and a form saved by hand first stops the board's from ever being fetched. RFC 0002 put the fetch there to avoid "one for every posting on every sync": the board's postings list has no questions, so each form is its own request (`/jobs/{id}?questions=true`).

## Change

- `FormService` owns the form fetchers and their `FormConfig`, and gains a method that fetches and stores the form of each open posting at a company that has none, up to 4 fetches at a time, each under the fetch timeout.
- Its write stores a form only where none exists, so it never replaces one saved by hand meanwhile. `Save` still replaces.
- `SyncCompany` runs it for the company once the postings are written and the lease is released. A sync that didn't take the lease (`ErrSyncInProgress`) or failed fetches no forms.
- A failed form fetch doesn't fail the sync: `Result` counts them in `FormsFetched` and `FormsFailed`, which `swamp fetch` prints as `forms=N forms_failed=N` and the TUI's refresh status as `forms N`, with `(N failed)` when any did.
- A source without a fetcher makes no request.
- RFC 0002's phase 2 item 1 and task D2 say a form is fetched by sync, after the postings, and why.

## Acceptance criteria

- [x] Service and sync tests, each watched fail first, or against a mutation where an earlier test's code already had the behaviour: a new posting's form is fetched and stored; a posting with a stored form, fetched or saved by hand, isn't fetched; a form saved by hand during the fetch stays; a failed fetch still syncs the postings and is counted, and the next sync fetches it; a closed posting isn't fetched; a source without a fetcher makes no request; four fetches run at a time; the forms are fetched with the lease released; a sync that didn't take the lease, or whose board failed, fetches no forms.
- [x] RFC 0002 updated in the same PR.
- [ ] Manual check: on a copy of the real database, a sync of the Greenhouse company with the most open postings stores their forms, and a second sync fetches none.
- [x] Verification checklist clean.
