# RFC 0013 work order

Tracking issues for [RFC 0013](../../rfcs/0013-tui-change-messages.md), the router's messages as events. Scope: the messages that go through `model.MsgRouter`, the 18 types in `tui/message` and the components that register for them (the company list, the company delete confirmation, `model.Syncing`), plus the company form and edit, whose results the list also hears. The models still on `App`'s old paths are RFC step 5 and aren't broken down here.

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

Steps 1 and 3 don't depend on each other, so either can go first.

Tags: every issue carries `rfc-0013` and `rfc0013-step-N` ("RFC 0013 work order: step N of 12"), the same as earlier RFCs' work orders. Steps 7, 11 and 12 also carry `rfc-0012`, since they change what RFC 0012 describes.
