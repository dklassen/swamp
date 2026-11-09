# CI checks work order

The gaps a review of the linters left after the lint series (`docs/issues/lint/`) closed: import grouping, prints that would garble the TUI, and known vulnerabilities in dependencies. One PR per step, each adding its check to `.golangci.yml` or CI. There's no RFC.

| Step | Title | Issue | PR | State | Depends on |
| --- | --- | --- | --- | --- | --- |
| 1 | Import groups: gci | - | #480 | merged | none |
| 2 | No prints outside the CLI: forbidigo | - | #481 | merged | none |
| 3 | Known vulnerabilities: govulncheck in CI | - | #482 | in review | none |

## Decided

- forbidigo bans `fmt.Print*`, `print` and `println` outside `cmd/swamp`, whose output is the CLI's (none elsewhere today). MCP serves over HTTP, so stdout matters only while the TUI holds the terminal.
- govulncheck runs as a `go.mod` tool in its own CI job. Today it finds nothing swamp calls, and 2 vulnerabilities in imported packages it doesn't.
- Imports are grouped standard library, other modules, then swamp's own packages: what all but 6 files already did. gofmt and the pre-commit hook don't fix it; `go tool golangci-lint fmt` does (in AGENTS.md).
- `-race` stays out of CI: Bob runs it on the host.
- Left off after the review, each catching nothing worth its findings: govet `shadow` (100, all `if err := ...` handled on the next line), gocritic's performance checks (`hugeParam`, `rangeValCopy`), golines (165 files rewritten), gochecknoinits (2 `init`s that register). staticcheck with every check already passes. The linters with no findings that would only guard against future bugs (nilnesserr, wastedassign, recvcheck and others) weren't taken up.

## Open

- Whether `task fmt` and the pre-commit hook move from gofmt to `golangci-lint fmt`, so they fix gci and gofumpt too. Slower per commit; blocks nothing.
- `main` isn't `go mod tidy`: tidy drops `canonicalheader`, `go-difflib` and 177 `go.sum` lines. Left out of step 3 as unrelated; whether to tidy, and check it in CI, blocks nothing.

## Follow-ups

- #483: bump `golang.org/x/net` to v0.62.0, fixing GO-2026-6866 and GO-2026-6860 in `html` (found by step 3; swamp calls neither).
