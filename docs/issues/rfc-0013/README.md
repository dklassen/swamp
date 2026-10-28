# RFC 0013 work order

Tracking issues for [RFC 0013](../../rfcs/0013-tui-change-messages.md), the router's messages as events. Scope: the messages that go through `model.MsgRouter`, the 18 types in `tui/message` and the components that register for them (the company list, the company delete confirmation, `model.Syncing`), plus the company form and edit, whose results the list also hears (steps 1 to 12). Steps 13 to 29 are RFC step 5: the models still on `App`'s old paths, one per step, after RFC 0012's stack.

Each step is one PR and keeps the verification checklist clean. Do them in order, since each depends on the steps before it.

| Step | Issue | RFC step | Depends on |
| --- | --- | --- | --- |
| 1 | [The envelope and the primitives](01-envelope-and-primitives.md) | 1 | none |
| 2 | [Filters on routes](02-route-filters.md) | 2 | 1 |
| 3 | [`change.Of` from the company service](03-change-of-company-service.md) | 3 | none |
| 4 | [Instance numbering](04-instance-numbering.md) | 4 | 1 |
| 5 | [Size and focus are signals](05-signals.md) | 4 | 1, 4 |
| 6 | [Errors and notices](06-errors-and-notices.md) | 4 | 1, 4 |
| 7 | [Company form and edit register routes](07-company-form-edit-routes.md) | 4 (prerequisite) | 4 |
| 8 | [Company writes are `Changed[Company]`](08-company-changed.md) | 4 | 2, 3, 6, 7 |
| 9 | [The company list's load is `Read[CompanyList]`](09-company-list-read.md) | 4 | 6, 8 |
| 10 | [Syncs are `Run` and `Ran`](10-sync-run-ran.md) | 4 | 6, 8 |
| 11 | [Company navigation is `Nav`](11-company-nav.md) | 4 | 4, 8 |
| 12 | [`tui/message` is plain data; RFC 0012 amended](12-close-out.md) | 4 | 1 to 11 |
| 13 | [The navigation stack](13-navigation-stack.md) | 5 (prerequisite) | 11 |
| 14 | [The posting list is stacked](14-posting-list.md) | 5 | 10, 13 |
| 15 | [The filter picker](15-filter-select.md) | 5 | 13, 14 |
| 16 | [`change.Of` from the posting and application services](16-change-of-posting-application.md) | 5 | 3 |
| 17 | [Markup writes are `Changed[Markup]`](17-changed-markup.md) | 5 | 14, 16 |
| 18 | [Application writes are `Changed[Application]`](18-changed-application.md) | 5 | 16 |
| 19 | [Posting detail](19-posting-detail.md) | 5 | 13, 14, 17, 18 |
| 20 | [Status and notes](20-status-and-notes.md) | 5 | 13, 18 |
| 21 | [All postings](21-all-postings.md) | 5 | 17, 19 |
| 22 | [The home screen](22-active-applications.md) | 5 | 18, 20 |
| 23 | [Application detail and the editor](23-application-detail.md) | 5 | 19, 22 |
| 24 | [Application delete](24-application-delete.md) | 5 | 18, 22, 23 |
| 25 | [Document reviews are `Changed[Review]`](25-document-review.md) | 5 | 19, 23 |
| 26 | [Export and submit](26-export-and-submit.md) | 5 | 18, 22, 23 |
| 27 | [The application form](27-application-form.md) | 5 | 13, 23 |
| 28 | [The change log's rows are `Changed[T]`](28-change-log.md) | 5 | 17, 18, 25 |
| 29 | [Every message is an event](29-step-5-close-out.md) | 5 | 13 to 28 |

Steps 1 and 3 don't depend on each other, so either can go first. Step 16 depends only on 3, so it can go any time before 17.

Decided for steps 13 to 29: RFC 0012's stack is built first (step 13), the posting list is stacked (RFC 0012 open question 1), and the change log's `Before` is the zero snapshot, skipping the TUI's own rows (RFC 0013 open question 2).

Tags: every issue carries `rfc-0013` and `rfc0013-step-N` ("RFC 0013 work order: step N of 29"), the same as earlier RFCs' work orders. Steps 7, 11 and 12 also carry `rfc-0012`, since they change what RFC 0012 describes.
