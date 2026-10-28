---
title: "[RFC 0013 step 16] Posting markup and application writes return change.Of"
labels: [rfc-0013, rfc0013-step-16]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Decisions: Payloads (Where Before, After and At come from)", "Steps: 3, 5"]
depends_on: [3]
---

RFC 0013, step 5: "A service write it uses returns `change.Of[T]` first." The same as step 3 for the posting and application services.

## Problem

`PostingService.SetInterested`, `UnmarkInterested`, `SetArchived`, `Unarchive` and `SetNotes` return `*posting.Markup`; `ApplicationService.SetStatus` and `SetNotes` return `*application.Application`, and `Delete` only `error`. None reads the row before writing, so an event can't say what it was before.

## Change

- Each returns `*change.Of[T]`, read, written and read again in one `store.InTx`, as `CompanyService.SetName` and `Delete` do. A missing or deleted row is `store.ErrNotFound` with a nil `Of`.
- Callers in `tui` and `mcpserver` take `.After` (or nothing, for a delete) until their events use the rest.
- `PostingService.ApplyTo` is a create, and needs no change.

## Acceptance criteria

- [ ] For each write, a test shows `Before` is the row as another writer left it just before, and `At` is its `UpdatedAt`, or `DeletedAt` for `Delete`.
- [ ] A delete of an already-deleted application is `ErrNotFound`.
- [ ] Verification checklist clean.
