---
title: "[RFC 0013 step 28] The change log's rows are Event[Changed[T]]"
labels: [rfc-0013, rfc0013-step-28, rfc-0008]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Adopting: Still in tui (Change log)", "Open questions: 2", "Decisions: The envelope (ID)"]
depends_on: [17, 18, 25]
---

RFC 0013, step 5 (the change log), as RFC 0013 open question 2 decided.

## Problem

`App` polls the change log on `changeTickMsg` for the screen on top, keeping each screen's place in the log (`seen`), reads `changesMsg`, and decides from the tables touched which `Load[T]` to send that screen. A screen hears another process's write differently from its own.

## Change

- The tick is `Signal[ChangeTick]`. Each row becomes the `Changed[T]` for its table: `ID` from the row, `Source.Process` its origin, `Op` from the event, `Before` the zero snapshot, `After` read for the row, once per table for the page of rows; a row whose entity is gone since is left out. Rows from the TUI's own process are skipped, since its command already sent the event. Postings, scores, and documents' writes and exports get snapshots.
- `App` reads from one place in the log and sends each event to every model with a route for it; `seen` goes. Screens hear another process's write on the routes they have for their own, and a model reloads at most once at a time for a run of them. `reloadFor` and every `Load[T]` go.
- Remove `changeTickMsg` and `changesMsg`. RFC 0012: section 6's change-log bullet updated.

## Acceptance criteria

- [ ] A write by the MCP server (or a second TUI) to a posting, application or company shows on the screen that shows it.
- [ ] The TUI's own write is applied once, not twice.
- [ ] The same row read twice gives the same event `ID`.
- [ ] Manual check against real data with a second process writing.
- [ ] Verification checklist clean.
