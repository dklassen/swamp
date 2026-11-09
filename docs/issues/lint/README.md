# Lint work order

Enabling golangci-lint linters one themed group per PR, titled `[Lint N] <theme>: <linters>`. Each PR fixes what its linters find and enables them in `.golangci.yml`. There's no RFC. The series is closed after step 13: the linters left off, and why, are under Decided.

| Step | Title | Issue | PR | State | Depends on |
| --- | --- | --- | --- | --- | --- |
| 1 | The linters already passing, in CI | - | #461 | merged | none |
| 2 | The auto-fixable linters | - | #462 | merged | 1 |
| 3 | Test hygiene: thelper, tparallel, contextcheck, noctx | - | #463 | merged | 1 |
| 4 | Enum switches: exhaustive | - | #464 | merged | 1 |
| 5 | Exits run deferred cleanup: gocritic | - | #465 | merged | 1 |
| 6 | Bound MCP header reads, keep files private: gosec | - | #466 | merged | 1 |
| 7 | Bound function length and nesting: funlen, gocognit, nestif | - | #467 | merged | 1 |
| 8 | Repeated strings: goconst, keys as `key.Binding` | - | #468 | merged | 1 |
| 9 | Unused and unchecked code: unparam, forcetypeassert, sqlclosecheck, musttag, sloglint | - | #470 | merged | 1 |
| 10 | Small fixes: dupword, embeddedstructfieldcheck, dogsled, prealloc | - | #474 | merged | 1 |
| 11 | Shared shapes: iface, dupl | - | #475 | merged | 1 |
| 12 | Unused parameters, package comments: revive without `exported` | - | #477 | merged | 1 |
| 13 | Every test in parallel: paralleltest | - | #478 | merged | 1 |

## Decided

- A finding is fixed, not excluded: an exclusion is an admission the linter is wrong there, so it carries a one-line reason in `.golangci.yml`.
- Tests are excluded from funlen, gocognit, maintidx, goconst, forcetypeassert (from step 9), noctx and gosec: their literals, length and assertions are the point, and a failed assertion already fails the test.
- cyclop stays off: it counts each case of a key switch as a branch, and splitting those would scatter a screen's keymap.
- Before changing what a finding flags, find out why it's written that way. Migration 00027 closed its rows by hand so the read ended before its updates in the same transaction.
- A fix that grows past the linter (hundreds of call sites, a rename, a design change) is raised with Bob before it's made, and may become its own issue (#469, #471).
- Tests are excluded from prealloc too (from step 10): a test's allocations aren't its point, and sizing a `want` up front turns an empty case's `nil` into `[]`, which `cmp.Diff` reports.
- Identical interfaces in one package are merged, not excluded (from step 11): the consuming package declares an interface, one per need. The rule is in AGENTS.md, under Go style.
- Each linter's fixes are one commit, and enabling them is the last, so every commit passes the checklist. The PR body has Production, Tests, Config and Verified sections; Verified says what wasn't run against real data.
- revive runs its default rules except `exported` (from step 12), which stays off (below). It reports one unused parameter per function, so fixing one can surface the next.
- An exported method returning an unexported type exports the type when the package's exported API already takes it (`StackScreen`, from step 12).
- A test that needs process-wide state (`t.Setenv`, which panics beside `t.Parallel`) gets it passed in instead, so it runs in parallel too (from step 13): the export screens take their home directory lookup.
- The series closes after step 13 with err113, wrapcheck and revive `exported` off: each would add code that catches no bug this app has.
  - err113 (123 findings) is for libraries whose callers tell errors apart with `errors.Is`. Swamp's errors end in a banner, CLI output or an MCP reply, and where a caller does branch on one there's already a sentinel or type (`store.ErrNotFound`, `*BehindError`).
  - wrapcheck (201) mostly flags errors crossing swamp's own packages, where wrapping at every layer only stutters (`service: repository: store: ...`). The dozen stdlib errors it finds returned bare are too few to carry a linter.
  - revive `exported` (211) asks for a doc comment on every exported name, but names here are exported for the app's other packages, not a public API, and 100 are `tui/message` types a comment would only restate.

## Open

None.

## Follow-ups

- #469: generate the TUI's help lines from its key bindings.
- #471: move the documents-need-a-live-application rule from `stage` into `internal/service`. The names of application states (live, deleted, not found, active) are agreed first, so `stage.application` keeps its name until then.
