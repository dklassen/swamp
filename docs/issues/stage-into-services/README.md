# Stage into services work order

`stage` holds business logic that belongs in `internal/service`: the drafting queue, get-or-create of an application, the application form's fetch policy, document reads and writes, the deleted-company rule, the profile check. Several pieces are repeated in `tui` and `cmd/swamp` (reviews still current on disk: five copies; the form's JSON: two). This series moves that logic into the services, which now own `documents.Store` and the form fetchers. Then it moves what's left of `stage` (the agent's JSON shapes, the sort registry, cursors) under `mcpserver`. One PR per step. There's no RFC; RFC 0011's section 3 is updated in step 10.

| Step | Title | Issue | PR | State | Depends on |
| --- | --- | --- | --- | --- | --- |
| 1 | [A document's hash has one home](01-one-content-hash.md) | - | - | planned | none |
| 2 | [DocumentService owns documents; reviews come back current](02-services-own-documents.md) | - | - | planned | none |
| 3 | [Document reads and writes go through DocumentService](03-document-read-write.md) | - | - | planned | 2 |
| 4 | [Application forms are PostingService's](04-application-forms.md) | - | - | planned | none |
| 5 | [A posting at a deleted company is refused in one place](05-posting-at-live-company.md) | - | - | planned | none |
| 6 | [Preparing a posting is one service call](06-prepare.md) | - | - | planned | 2, 3, 4, 5 |
| 7 | [The drafting queue is one service query](07-drafting-queue.md) | - | - | planned | 2 |
| 8 | [Companies with their open counts are CompanyService's](08-companies-with-open-counts.md) | - | - | planned | none |
| 9 | [Scoring checks the profile; canonical files come from DocumentService](09-profile-and-canonical-files.md) | - | - | planned | 2 |
| 10 | [What's left of stage moves under mcpserver](10-stage-into-mcpserver.md) | - | - | planned | 1-9 |

## Next

- **The TUI's own `documents.Store` use.** Screens still read `docs.Status`, `docs.Path` and `docs.SHA256` directly (`application_detail.go`, `posting_detail.go`, `document_review_select.go`, `document_review_form.go`, `editor.go`, `application_export.go`), and `documents.Current` for exports (`active_applications.go`). So does `cmd/swamp`'s `runExport`. Under "services own documents" these move behind `DocumentService` too, removing `*documents.Store` from `tui`. Needed to start: steps 2 and 3, whose methods most of it reuses. It's outside `stage`, so it's a series of its own or more steps here, Bob's call.

## Decided

- Services own every store their behaviour needs, the filesystem (`documents.Store`) and board clients (form fetchers) included, not only the database (Bob, 2026-11-09). This holds beyond the series, so it's in AGENTS.md's Go style.
- Once its business and domain logic is in `internal`, what's left of `stage` is endpoint logic and moves under `mcpserver` (Bob, 2026-11-09).
- `swamp stage list` and `swamp stage prepare` are dropped (Bob, 2026-11-09), leaving `mcpserver` `stage`'s only caller. They were its only other one, and both skills use MCP.
- The steps aren't filed as GitHub issues (Bob, 2026-11-09): the issue files here are their record, so a step's PR references its file, not `Fixes #N`, and the Issue column stays `-`.
- The agent's JSON stays byte-for-byte the same through every step: the skills in `.agents/skills/` depend on its field names (the `json` tags on `Candidate` and the rest). Moving a type moves its tags with it.
- Cursors, page-size defaults and the sort-order registry are endpoint logic, so they stay with the endpoints. `Search` and `PendingPostings` share the limit and cursor code; that's two copies, so it isn't abstracted.
- Tests move with the logic. A moved behaviour's tests are rewritten as service tests, arranged and checked through service calls (AGENTS.md, Testing), not carried over through `stage`.
- `Prepare` on a posting at a deleted company is refused with the same error as `ReadPosting`, not today's bare "get company: not found" (step 5).

## Open

None.

## Follow-ups

None yet.
