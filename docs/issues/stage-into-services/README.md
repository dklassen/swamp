# Stage into services work order

`stage` holds business logic that belongs in `internal/service`: the drafting queue, get-or-create of an application, the application form's fetch policy, document reads and writes, the deleted-company rule, the profile check. Several pieces are repeated in `tui` and `cmd/swamp` (reviews still current on disk: five copies; the form's JSON: two). This series moves that logic into the services, which now own `documents.Store` and the form fetchers. Then it moves what's left of `stage` (the agent's JSON shapes, the sort registry, cursors) under `mcpserver`. One PR per step. There's no RFC; RFC 0011's section 3 is updated in step 10. Steps 11, 13 and 14 came out of step 4: application forms get their own `FormService`, which sync uses to fetch the forms its company's open postings lack, and drafting only reads them.

| Step | Title | Issue | PR | State | Depends on |
| --- | --- | --- | --- | --- | --- |
| 1 | [A document's hash has one home](01-one-content-hash.md) | - | #486 | merged | none |
| 2 | [DocumentService owns documents; reviews come back current](02-services-own-documents.md) | - | #487 | merged | none |
| 3 | [Document reads and writes go through DocumentService](03-document-read-write.md) | - | #488 | merged | 2 |
| 4 | [Application forms are PostingService's](04-application-forms.md) | - | #489 | merged | none |
| 5 | [A posting at a deleted company is refused in one place](05-posting-at-live-company.md) | - | #490 | merged | none |
| 6 | [Starting to draft a posting is one DraftingService call](06-prepare.md) | - | #495 | merged | 2, 3, 4, 5, 12 |
| 7 | [The drafting queue is one DraftingService query](07-drafting-queue.md) | - | #496 | merged | 2, 12 |
| 8 | [Companies with their open counts are CompanyService's](08-companies-with-open-counts.md) | - | #493 | merged | none |
| 9 | [Scoring checks the profile; canonical files come from DocumentService](09-profile-and-canonical-files.md) | - | #497 | merged | 2 |
| 10 | [What's left of stage moves under mcpserver](10-stage-into-mcpserver.md) | - | #498 | merged | 1-9 |
| 11 | [Application forms have their own FormService](11-form-service.md) | - | #500 | in review | 6 |
| 12 | [The latest review of each document type is a repository read](12-latest-reviews.md) | - | #492 | merged | none |
| 13 | [Syncing a company fetches the forms its open postings lack](13-form-job.md) | - | - | planned | 11 |
| 14 | [Starting to draft reads the stored form](14-drafting-reads-forms.md) | - | - | planned | 13 |

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
- Drafting is a process, so it gets its own service, `DraftingService` (Bob, 2026-11-10): the queue (step 7) and starting or resuming a posting (step 6) read postings, companies, applications, reviews, forms and files, and own none of them. Steps 6 and 7 go there rather than on `PostingService`. Reading, writing and reviewing documents stay `DocumentService`'s, since the TUI does them outside drafting.
- A service calls repositories and its non-database stores, never another service (Bob, 2026-11-10): a repository holds the primitives for the data it represents, and a service is a process over them. So `DraftingService` writes its own get-or-create of an application instead of calling `ApplyTo`, and a primitive that services rebuild by hand moves into its repository (step 12). Not in AGENTS.md yet, by Bob's say-so.
- `CurrentForm` and the form fetchers move from `PostingService` to `DraftingService` in step 6 (Bob, 2026-11-10), replacing step 4's placement: starting to draft is its only caller. Steps 11, 13 and 14 replace this placement in turn.
- Names: `DraftingService.Prepare` (step 6), matching `stage_prepare` and `Prepared`, and `DraftingService.Queue` (step 7). Step 12's `Latest` is one SQL query, not the per-type loop moved into the repository (Bob, 2026-11-10).
- Step 8 moves the TUI's company list onto the new `CompanyService` query in the same PR, since it makes the same two reads (Bob, 2026-11-10).
- Step 7 merges the queue in Go rather than one SQL query, with the reviews of every application in one batch query (Bob, 2026-11-10): the interested and started halves follow different rules, so one query would be a UNION repeating the company-list filters. Started applications not marked interested are ordered by company name, then application ID, dropping the status-since tie-break, which cost a history read per application. `PostingService.Interested` went with it, having no other caller.
- Step 9: `documents.Store` reads the profile and canonical resume (`ReadProfile`, `ReadCanonicalResume`), and both `ScoringService` and `DocumentService` call it, since neither may call the other; `NewScoringService` takes the store as a plain parameter (Bob, 2026-11-10). `DocumentService.Profile` and `CanonicalResume` are two methods, not one over an `iota` kind, since nothing switches on which file it is. They return a `CanonicalFile` with `Exists`, not `store.ErrNotFound`: a missing file is a state the skill acts on, and the tool still names its path (Bob, 2026-11-10).
- Step 9: `ScoringService.Pending` takes the profile hash on every call plus a `rescore` flag, replacing the empty rescore hash that meant none, since it now checks the profile either way. The flag picks which postings are listed against one profile, not a second intent.
- Step 10: `stage`'s leftovers go into `mcpserver` itself, not a subpackage for the JSON shapes (Bob, 2026-11-10): the step removes a layer, and a subpackage would put one back. Each `stage` method folds into its tool handler, so the option types between them go, and the shapes are unexported.
- Step 10: five tools still return `any`. Schema inference panics on `posting.OptionalTime`, which embeds `time.Time`, whose schema is a string where an embedded struct's must be an object; and enums that marshal as names would be advertised as integers. Typing them needs schema overrides for those types, as `documentInputSchema` does for `document.Type`: not in this step.
- Application forms are their own concern, not drafting's (Bob, 2026-11-10): one job downloads postings, one downloads and stores the forms a board has, and a form can be made by hand and linked to a posting. So they get a `FormService` holding storing, reading and fetching (step 11 starts it), and drafting only reads the stored form (step 14).
- Sync fetches forms, not drafting (Bob, 2026-11-10, step 13): each company sync, once its postings are written, fetches the form of each open posting that has none. That one rule covers new postings, retries a failed fetch on the next sync, and backfills postings stored before step 13 in a one-off burst.
- Form fetches run after the lease is released, up to 4 at a time (Bob, 2026-11-10, step 13): the largest Greenhouse company has hundreds of open postings, each form its own request under a 30-second timeout, which can't fit in the 5-minute lease. Two syncs of a company may then fetch the same form; the job's write stores only where none exists, so the cost is a wasted request, not a lost form.
- A stored form isn't refetched when sync reports its posting updated (Bob, 2026-11-10), keeping step 4's rule and a form saved by hand.

## Open

None.

## Follow-ups

None yet.
