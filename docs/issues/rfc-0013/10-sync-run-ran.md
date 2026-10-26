---
title: "[RFC 0013 step 10] Sync requests are Run events and their results Ran; tui/message stops importing sync"
labels: [rfc-0013, rfc0013-step-10]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: [Primitives, "Decisions: Payloads (What a Ran[T] reports)", "Adopting: Already in tui/message"]
depends_on: [6, 8]
---

RFC 0013, step 4 (`Syncing`'s messages). See `docs/rfcs/0013-tui-change-messages.md`.

## Problem

`model.Syncing` (`tui/model/syncing.go:42`) registers `ToggleSyncAllMsg`, `SyncAllStepMsg`, `RefreshCompanyMsg` and `CompanyRefreshedMsg`, and sends `SyncAllFinishedMsg`. These carry `company.Company`, `sync.Result` (which holds an `error`) and `error`. `Syncing.companyRefreshed` recognises a refused refresh with `errors.Is(refreshed.Err, sync.ErrSyncInProgress)`.

## Change

- Bodies: `SyncCompany` (the company, enough for `Syncing`'s banner text), `SyncAll` (company IDs, or stop), `SyncReport` (`RunID`, counts fetched/created/updated/closed/reopened, `FetchedAt`), `SyncAllReport` (the run's totals).
- `RefreshCompanyMsg` is `Event[Run[SyncCompany]]` with the company as `Subject`. `ToggleSyncAllMsg` is `Event[Run[SyncAll]]`.
- Each company synced, by a refresh or a sync-all step, is `Event[Ran[SyncReport]]` with the company as `Subject`. The end of a run is `Event[Ran[SyncAllReport]]`. A refused or failed sync is `Event[Error]`, with `Code` `sync_in_progress` for a refusal, and the company as `Subject`.
- `Syncing` matches a step to its run by `SyncReport.RunID`, and a refusal by `Code`.
- The company list's `R` and `r`, and the posting list's `r` (`tui/app.go:1626`), send the `Run` events.
- `App`'s reloads on a finished sync (`tui/app.go:1118`, `:1129`) match the new types and read `Subject.ID` where they read `CompanyID`. This keeps the old path working. Moving the reloads into the listeners is RFC 0012 work, outside this step.
- Remove the five structs. `tui/message` stops importing `sync`.

## Acceptance criteria

- [ ] Sync all: the bar, per-company progress, stop, and the final summary behave as before (`tui/model/syncing_test.go`, `tui/sync_all_test.go`).
- [ ] A refresh during a run is refused with the same banner line (test).
- [ ] After a refresh or a run, the company list and the synced company's posting list reload (`tui/posting_refresh_test.go`).
- [ ] Manual check against real data: sync all, stop mid-run, refresh one company during a run.
- [ ] Verification checklist clean.
