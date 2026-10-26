---
title: "[RFC 0013 step 3] CompanyService.SetName and Delete return change.Of, read in the write's transaction"
labels: [rfc-0013, rfc0013-step-3]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Decisions: Payloads (Where Before, After and At come from)", "Steps: 3"]
depends_on: []
---

RFC 0013, step 3. See `docs/rfcs/0013-tui-change-messages.md`.

## Problem

A `Changed[Company]` event (step 8) needs the company before and after the write, and when it happened. `CompanyService.SetName` (`internal/service/company.go:214`) returns only the row after, and `Delete` (`:126`) returns only an error. A model can't fill in `Before` from what it last showed, because another process may have written in between.

## Change

- `change.Of[T]` in `internal/domain/change`, holding `Before`, `After` and `At`.
- `SetName` returns `*change.Of[company.Company]`: read, write, read again, in one `store.InTx` transaction. `At` is the new `UpdatedAt`.
- `Delete` returns `*change.Of[company.Company]` the same way. `After` is the soft-deleted row and `At` is its `DeletedAt`.
- Both return `nil` with any error, as AGENTS.md requires.
- Update the callers: `tui/app.go:629`, `:637`, the company delete confirmation's delete command, and anything in `mcpserver` or `cmd/swamp` that `grep` finds. Each one keeps its behaviour and reads `.After` where it read the company before.

## Acceptance criteria

- [ ] `Before` is the row as another writer left it just before this write. The test changes the description through a second service call between create and `SetName`, and `Before.Description` shows that change.
- [ ] `At` equals `After.UpdatedAt` for `SetName`, and `After.DeletedAt` for `Delete`, both UTC.
- [ ] A missing company returns `store.ErrNotFound` and a nil `Of`.
- [ ] Tests arrange and assert through the service, not the repository or SQL.
- [ ] No caller's behaviour changes (existing tests pass).
- [ ] Verification checklist clean.
