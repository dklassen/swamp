# Lint work order

Enabling golangci-lint linters one themed group per PR, titled `[Lint N] <theme>: <linters>`. Each PR fixes what its linters find and enables them in `.golangci.yml`. There's no RFC; steps are picked one at a time from Next.

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
| 11 | Shared shapes: iface, dupl | - | - | in review | 1 |

## Next

Findings on `main` after step 10, from `go tool golangci-lint run --enable-only <linters> ./...`. Each needs its scope agreed with Bob before starting.

| Linter | Findings | Needed to start |
| --- | --- | --- |
| revive | 253 | Which rules. |
| paralleltest | 222 | Mostly tests without `t.Parallel()`; whether any must stay serial. |
| wrapcheck | 201 | Which packages' errors get wrapped. |
| err113 | 123 | Which dynamic `errors.New` become sentinels. |

## Decided

- A finding is fixed, not excluded: an exclusion is an admission the linter is wrong there, so it carries a one-line reason in `.golangci.yml`.
- Tests are excluded from funlen, gocognit, maintidx, goconst, forcetypeassert (from step 9), noctx and gosec: their literals, length and assertions are the point, and a failed assertion already fails the test.
- cyclop stays off: it counts each case of a key switch as a branch, and splitting those would scatter a screen's keymap.
- Before changing what a finding flags, find out why it's written that way. Migration 00027 closed its rows by hand so the read ended before its updates in the same transaction.
- A fix that grows past the linter (hundreds of call sites, a rename, a design change) is raised with Bob before it's made, and may become its own issue (#469, #471).
- Tests are excluded from prealloc too (from step 10): a test's allocations aren't its point, and sizing a `want` up front turns an empty case's `nil` into `[]`, which `cmp.Diff` reports.
- Identical interfaces in one package are merged, not excluded (from step 11): the consuming package declares an interface, one per need. The rule is in AGENTS.md, under Go style.
- Each linter's fixes are one commit, and enabling them is the last, so every commit passes the checklist. The PR body has Production, Tests, Config and Verified sections; Verified says what wasn't run against real data.
- `go tool task fmt:check` prints its script, which ends in `exit 1`; go by its exit code.

## Open

None.

## Follow-ups

- #469: generate the TUI's help lines from its key bindings.
- #471: move the documents-need-a-live-application rule from `stage` into `internal/service`. The names of application states (live, deleted, not found, active) are agreed first, so `stage.application` keeps its name until then.
