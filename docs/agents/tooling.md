# Tooling

- **Dev tools (golangci-lint, sqlc, goose, task, govulncheck) are `go.mod` tool dependencies**, not separately-installed binaries. Run them as `go tool <name>` (e.g. `go tool golangci-lint run ./...`) -- `go` builds/caches the pinned version from `go.mod`'s `tool (...)` block automatically on first use, so there's no install step for a fresh clone or CI. To add or bump one: `go get -tool <import path>@<version>` (e.g. `go get -tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.0`), then commit the `go.mod`/`go.sum` diff. Note golangci-lint specifically adds a large `go.sum` diff when pinned/bumped this way (it bundles its linters as library deps) -- expected, not a mistake.
- **Formatting**: lint checks formatting too, with the formatters in `.golangci.yml` (gofumpt, gci); `task fmt`, `task fmt:check` and the pre-commit hook use the same ones. If lint reports a file as not properly formatted, run `go tool task fmt` (`go tool task fmt:check` shows the diff). The hook auto-formats staged files too, but running it explicitly before committing avoids surprises.
- **direnv**: `.envrc` only sets `SWAMP_DB_PATH` (used when actually running the app, e.g. `direnv exec . ./bin/swamp`). It does **not** auto-hook into non-interactive/scripted shell calls — prefix commands with `direnv exec .` explicitly if a command needs `SWAMP_DB_PATH`, or it runs against the ambient environment instead of the project one. The verification checklist doesn't need it.
- **Taskfile.yml** (invoke as `go tool task <name>`):
  - `task fmt` / `task fmt:check` — `golangci-lint fmt` with the formatters in `.golangci.yml`, format or check-only (`--diff`).
  - `task lint` — golangci-lint (standard linter set: errcheck, govet, ineffassign, staticcheck, unused).
  - `task sqlc:generate` — regenerate `store/sqlcdb` from `db/queries/*.sql` + `db/migrations`.
  - `task migrate:up` / `task migrate:down` / `task migrate:status` — `migrate:up`/`migrate:down` run `./bin/swamp migrate up|down` (build first); `migrate:status` is raw goose. `task run`/`task mcp-serve` never migrate, only check.
  - `task hooks:install` — point git at `.githooks/` (run once per clone; `core.hooksPath` itself can't be version-controlled).
  - `task build` — build the `swamp` binary to `./bin/swamp`.
  - `task test` — `go test ./...`.
  - `task vuln` — govulncheck: fails when code swamp calls has a known vulnerability (CI runs it). Vulnerabilities only in imported packages are reported but don't fail it.
- **go.mod**: don't fight `go mod tidy`/`go get` when they strip the `toolchain` line — that's intentional (Go 1.21+ auto-selects a compatible toolchain from the `go` directive's minimum version). Don't re-add an explicit `toolchain` pin.
- **golangci-lint** findings get fixed properly (explicitly discarding/asserting on errors, etc.), not suppressed via exclude rules or `//nolint`, unless there's a specific documented justification.
