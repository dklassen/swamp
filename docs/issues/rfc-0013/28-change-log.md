---
title: "[RFC 0013 step 28] The change log's rows are Event[Changed[T]]"
labels: [rfc-0013, rfc0013-step-28, rfc-0008]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Adopting: Still in tui (Change log)", "Open questions: 2", "Decisions: The envelope (ID)"]
depends_on: [17, 18, 25]
---

RFC 0013, step 5 (the change log), as RFC 0013 open question 2 decided.

## Problem

`App` polls the change log on `changeTickMsg`, reads `changesMsg`, and calls each screen's `ReloadFor`, which reloads by entity (`applicationDetailReloadedMsg`, `postingReloadedMsg`, `postingScoresLoadedMsg`). A screen hears another process's write differently from its own.

## Change

- The tick is `Signal[ChangeTick]`. Each row becomes the `Changed[T]` for its table: `ID` from the row, `Source.Process` its origin, `Op` from the event, `Before` the zero snapshot, `After` read for the row. Rows from the TUI's own process are skipped, since its command already sent the event.
- Screens hear another process's write on the routes they have for their own; `ChangeFollower`, `ReloadFor` and the `Load` path for the company list go.
- Remove `changeTickMsg`, `changesMsg` and the three reload structs. RFC 0012: section 6's change-log bullet updated.

## Acceptance criteria

- [ ] A write by the MCP server (or a second TUI) to a posting, application or company shows on the screen that shows it.
- [ ] The TUI's own write is applied once, not twice.
- [ ] The same row read twice gives the same event `ID`.
- [ ] Manual check against real data with a second process writing.
- [ ] Verification checklist clean.
