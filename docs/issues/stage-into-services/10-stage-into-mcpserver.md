---
title: "[Stage into services 10] What's left of stage moves under mcpserver"
labels: [stage-into-services]
depends_on: [1, 2, 3, 4, 5, 6, 7, 8, 9]
---

## Problem

After steps 1-9, `stage` holds no rule of its own. It maps service results to the agent's JSON (`Candidate`, `Prepared`, `FullPosting`, `SearchMatch`, `PendingMatch`, `CompanySummary`, the score types) and owns the endpoint concerns: the sort-order registry and its descriptions, page-size defaults and opaque cursors. That's endpoint logic, and a separate package for it is one more layer between `mcpserver` and the services.

RFC 0011 section 3 still calls `stage` a use-case package that "composes what the agent reads and writes". `internal/service` has since taken that role.

## Change

- `stage`'s remaining types and functions move into `mcpserver` (or a package under it), unexported where nothing outside reads them. `mcpserver.New` takes the services instead of `*stage.Stage`.
- `stage` is deleted, its tests either already moved (steps 2-9) or rewritten as `mcpserver` tests of the JSON.
- `swamp stage list|prepare` go: `runStage`, `newStage`, the `stage` flag parsing in `cmd/swamp`, and their lines in `README.md`.
- RFC 0011 section 3 says use cases that span areas are service methods, and `stage` no longer exists.
- The `any` output type on `list_postings`, `stage_prepare` and `read_posting`: their comments say it's because the output embeds `posting.Posting`, which it no longer does. Check whether schema inference now works and, if so, type them. If not, fix the comment to say why.

## Decide before starting

- **One package or a subpackage**: `mcpserver` alone is about 470 lines; with `stage`'s mapping it's about 1,100. A subpackage only for the JSON shapes is an option, not a requirement.

## Acceptance criteria

- [x] No `stage` package, no import of it.
- [x] Every tool's JSON in `mcpserver_test.go` unchanged.
- [x] No `stage` subcommand: `swamp stage` is an unknown command.
- [x] RFC 0011 and README updated.
- [ ] Manual check: the apply-to-posting and score-postings skills' tools answer against real data through the MCP server.
- [x] Verification checklist clean.
