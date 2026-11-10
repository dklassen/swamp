# Stage into services work order

`stage` holds business logic that belongs in `internal/service`: the drafting queue, get-or-create of an application, the application form's fetch policy, document reads and writes, the deleted-company rule, the profile check. Several pieces are repeated in `tui` and `cmd/swamp` (reviews still current on disk: five copies; the form's JSON: two). This series moves that logic into the services, which now own `documents.Store` and the form fetchers. Then it moves what's left of `stage` (the agent's JSON shapes, the sort registry, cursors) under `mcpserver`. One PR per step. There's no RFC; RFC 0011's section 3 is updated in step 10. Step 11 came out of step 4: once `PostingService` owns the form fetchers, sync fetches a posting's form when the posting lands.

| Step | Title | Issue | PR | State | Depends on |
| --- | --- | --- | --- | --- | --- |
| 1 | [A document's hash has one home](01-one-content-hash.md) | - | #486 | merged | none |
| 2 | [DocumentService owns documents; reviews come back current](02-services-own-documents.md) | - | #487 | merged | none |
| 3 | [Document reads and writes go through DocumentService](03-document-read-write.md) | - | #488 | merged | 2 |
| 4 | [Application forms are PostingService's](04-application-forms.md) | - | #489 | merged | none |
| 5 | [A posting at a deleted company is refused in one place](05-posting-at-live-company.md) | - | #490 | in review | none |
| 6 | [Preparing a posting is one service call](06-prepare.md) | - | - | planned | 2, 3, 4, 5 |
| 7 | [The drafting queue is one service query](07-drafting-queue.md) | - | - | planned | 2 |
| 8 | [Companies with their open counts are CompanyService's](08-companies-with-open-counts.md) | - | #493 | in review | none |
| 9 | [Scoring checks the profile; canonical files come from DocumentService](09-profile-and-canonical-files.md) | - | - | planned | 2 |
| 10 | [What's left of stage moves under mcpserver](10-stage-into-mcpserver.md) | - | - | planned | 1-9 |
| 11 | [A posting's application form is fetched when the posting lands](11-forms-with-postings.md) | - | - | planned | 4 |

## Next

- **The TUI's own `documents.Store` use.** Screens still read `docs.Status`, `docs.Path` and `docs.SHA256` directly (`application_detail.go`, `posting_detail.go`, `document_review_select.go`, `document_review_form.go`, `editor.go`, `application_export.go`), and `documents.Current` for exports (`active_applications.go`). So does `cmd/swamp`'s `runExport`. Under "services own documents" these move behind `DocumentService` too, removing `*documents.Store` from `tui`. Needed to start: steps 2 and 3, whose methods most of it reuses. It's outside `stage`, so it's a series of its own or more steps here, Bob's call.
- **`DocumentService.Write`'s signature.** Its `source` is always `document.SourceWriteDocument`; the version is spelled `NoDocumentYet` in the service and `""` in `documents.Store`; and `applicationID, documentType` go together through nearly every document method. Options: drop `source`, a typed `document.Version`, a `document.Key{ApplicationID, Type}`. Bob put it off until this series is done (2026-11-10).

## Decided

- Services own every store their behaviour needs, the filesystem (`documents.Store`) and board clients (form fetchers) included, not only the database (Bob, 2026-11-09). This holds beyond the series, so it's in AGENTS.md's Go style.
- Once its business and domain logic is in `internal`, what's left of `stage` is endpoint logic and moves under `mcpserver` (Bob, 2026-11-09).
- `swamp stage list` and `swamp stage prepare` are dropped (Bob, 2026-11-09), leaving `mcpserver` `stage`'s only caller. They were its only other one, and both skills use MCP.
- The steps aren't filed as GitHub issues (Bob, 2026-11-09): the issue files here are their record, so a step's PR references its file, not `Fixes #N`, and the Issue column stays `-`.
- The agent's JSON stays byte-for-byte the same through every step: the skills in `.agents/skills/` depend on its field names (the `json` tags on `Candidate` and the rest). Moving a type moves its tags with it. A tool's input changes only on Bob's say-so, with the skill updated in the same PR: step 3 made `write_document`'s `ReplacesVersion` required.
- Cursors, page-size defaults and the sort-order registry are endpoint logic, so they stay with the endpoints. `Search` and `PendingPostings` share the limit and cursor code; that's two copies, so it isn't abstracted.
- Tests move with the logic. A moved behaviour's tests are rewritten as service tests, arranged and checked through service calls (AGENTS.md, Testing), not carried over through `stage`.
- `Prepare` on a posting at a deleted company is refused with the same error as `ReadPosting`, not today's bare "get company: not found" (step 6, through step 5's method).
- `DocumentService.LatestReviews` and `ApplicationService.Active` return only current reviews rather than a `CurrentReviews` beside them (Bob, 2026-11-09, step 2): no caller wanted the unfiltered map. `LatestReview` and `Reviews` stay history.
- A service takes the stores it needs as plain constructor parameters, as it takes `db` (Bob, 2026-11-09, step 2), since none works without them.
- A document write always names the version it read, and there's no unconditional write (Bob, 2026-11-10, step 3). Its only use was the user asking to overwrite, which a caller does by reading first, and it allowed a blind overwrite of an edit made meanwhile. So `write_document`'s `ReplacesVersion` is required.
- The document errors and `NoDocumentYet` live in `internal/service`, beside the methods that return them (step 3): `mcpserver` is above it and can match them there.
- Functions are named for intent, not the conditions they run under (Bob, 2026-11-10, step 3): `WriteIfUnchanged` folded into `Write`, and the application check is `requireApplication`. The rule is in AGENTS.md's Go style.
- Step 4 returns a failed form fetch in a result struct with the form and a fetch error message, not as an `error`, since drafting goes ahead without the form; the fetchers reach `NewPostingService` as a map by source, as `sync`'s do (Bob, 2026-11-10).
- Step 4: `NewPostingService` takes the fetchers and a `FormConfig` holding the fetch timeout, mirroring `sync.New`; callers that never fetch pass `nil` and `FormConfig{}`. `PostingService.CurrentForm` takes the posting and company values `Prepare` already has, leaving how the company is looked up to step 5 (Bob, 2026-11-10).
- A stored application form is never fetched again (Bob, 2026-11-10, step 4). The weekly refetch carried over from stage assumed forms change apart from their postings, with nothing to show they do; that's also why it doesn't refetch when the posting's `updated_at` moves, which may not even reflect form edits. Step 11 fetches the form when the posting lands instead (Bob, 2026-11-10), which also keeps a form saved by hand from blocking the board's.
- Step 5's method returns the posting with its `*company.Company` (Bob, 2026-11-10): `PostingService.GetWithCompany`, refusing a deleted company with `CompanyDeletedError` after one query joining the posting to its company (Bob, 2026-11-10: the work that removes complexity is done now, not deferred), typed like `ApplicationDeletedError` so `mcpserver` can match it and the message keeps naming the company. `Prepare` moves onto it in step 6, as its issue file says.
- Step 8 moves the TUI's company list onto the new `CompanyService` query in the same PR, since it makes the same two reads (Bob, 2026-11-10).

## Open

- Step 11 needs host-side counts of open Greenhouse postings, in total and at the largest company, to decide where its form fetches run.

## Follow-ups

None yet.
