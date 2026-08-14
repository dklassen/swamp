# CLAUDE.md

Operational guidance for working in this repo: build/test commands, workflow, and standards established over the course of this project. For product scope and architecture, see `README.md`. For the chronological "why" behind decisions, see `decisions.log`.

## Testing

- TDD (red-green-refactor) is a project-wide requirement: write a failing test, watch it fail for the right reason, write minimal code to pass, watch it pass, then refactor. One behavior at a time — never write multiple tests before running any of them.
- Full automated test suites required for the core/orchestration modules: `ashby`, `filter`, `store`, `sync`.
  - `ashby`: tested against recorded/fixture HTTP responses, no live network calls in tests.
  - `filter`: pure function, tested exhaustively against posting/filter combinations.
  - `store`: tested against a real sqlite file (temp db per test), not mocked.
  - `sync`: tested with fakes for `ashby` and a real `store`, verifying fetch → filter → upsert → status-transition behavior end to end.
- `tui` Update-function logic (state transitions) is tested where practical; the View/render layer is not held to automated coverage and is verified manually instead (see "Manual verification" below).
- `cmd/swamp` wiring is verified manually, not unit tested.
- When a pattern of test setup repeats 3+ times, extract a helper — not before (see e.g. `mustCreateCompany`, `openPostingList`/`openPostingDetail`, `sendKey`).
- Prefer `github.com/google/go-cmp/cmp` (`cmp.Diff`) over manual field-by-field comparison or `==` on structs containing `time.Time` (`==` on `time.Time` is a known footgun — see Go's own docs).

## Feature branch workflow

Each task/feature gets its own branch off `main`, merged back when complete:

1. `git checkout -b <descriptive-branch-name>` off `main`.
2. Build via TDD, committing in small, well-documented commits as logical chunks complete — not one giant commit at the end. No `Co-Authored-By` trailer in commit messages (explicit standing preference).
3. Run the full verification checklist (below) before every commit.
4. For any change touching the TUI or other runtime-visible behavior, do a manual verification against real data (below) before considering the work done.
5. Confirm with the user before merging into `main`, and before deleting a branch — even one that's already merged. (Cherry-picks break git's own "already merged" safety check, so this matters more here than in a typical repo; see decisions.log for why cherry-picks came up at all.)
6. After merging, re-verify `main` (build/test/lint) before deleting the branch.

## Verification checklist (run before every commit)

```sh
direnv exec . go build ./...
direnv exec . go test ./...
direnv exec . ./bin/golangci-lint run ./...
direnv exec . ./bin/task fmt:check
```

If `fmt:check` fails, run `gofmt -w <files>`. The pre-commit hook auto-formats staged files too, but running it explicitly before committing avoids surprises. All four must be clean before a commit is considered done.

## Tooling

- **direnv**: `.envrc` sets `GOBIN`/`PATH` to a project-local `bin/` and `SWAMP_DB_PATH`. It does **not** auto-hook into non-interactive/scripted shell calls — prefix commands with `direnv exec .` explicitly, or they run against the ambient environment instead of the project one.
- **Taskfile.yml**:
  - `task fmt` / `task fmt:check` — gofmt, format or check-only.
  - `task lint` — golangci-lint (standard linter set: errcheck, govet, ineffassign, staticcheck, unused).
  - `task sqlc:generate` — regenerate `store/db` from `db/queries/*.sql` + `db/migrations`.
  - `task migrate:up` / `task migrate:down` / `task migrate:status` — goose migrations against the local sqlite db.
  - `task hooks:install` — point git at `.githooks/` (run once per clone; `core.hooksPath` itself can't be version-controlled).
  - `task build` — build the `swamp` binary to `./bin/swamp`.
  - `task test` — `go test ./...`.
- **go.mod**: don't fight `go mod tidy`/`go get` when they strip the `toolchain` line — that's intentional (Go 1.21+ auto-selects a compatible toolchain from the `go` directive's minimum version). Don't re-add an explicit `toolchain` pin.
- **golangci-lint** findings get fixed properly (explicitly discarding/asserting on errors, etc.), not suppressed via exclude rules or `//nolint`, unless there's a specific documented justification.

## Manual verification for runtime/TUI changes

`tui`'s View layer and `cmd/swamp` aren't unit tested, so changes there need to be verified by actually driving the code against real data before considering them done:

- For `ashby`/`sync` behavior: a throwaway `go run` script (not committed) constructing a real `store.Store` + `sync.Syncer` with the real `ashby.NewClient()`, hitting the live Ashby API against a real company slug.
- For `tui` behavior: a throwaway script driving a real `*tui.App` through `Init`/`Update`/`View` (manually executing returned `tea.Cmd`s and feeding results back in, the same way the Bubble Tea runtime would) and printing `View()` output at each step to inspect visually.
- Clean up scratch scripts/db files after verifying (`rm` them) — they're not part of the repo.
- When in doubt, ask the user to verify in a real terminal via `task build && ./bin/swamp`. This has caught real bugs that scripted verification alone missed (a stale-binary false alarm, and a genuine filter-narrowing bug that only manifested after a background re-sync completed).

## Decisions log

Every non-trivial decision gets an entry in `decisions.log` — chronological, includes the "why" and alternatives considered, appended at the time the decision is made (not batched). This is the durable record independent of git history shape; commit messages capture the "why" for what shipped but don't give a single browsable trail, and can get lost in a squash/rebase. Don't forget to append to it.
