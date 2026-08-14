# CLAUDE.md

Operational guidance for working in this repo: build/test commands, workflow, and standards established over the course of this project. For product scope and architecture, see `README.md`. For the chronological "why" behind decisions, see `decisions.log`.

## Issue-driven task workflow

When asked to work through a labeled group of issues:

1. Fetch the issues: `gh issue list --label "<tag>" --state open --json number,title,body,labels,url`
2. Build a task list from the results, one item per issue (number + title).
3. Work through the list in order:
   - `gh issue view <number> --comments` to pull full context before starting
   - Implement the change
   - Reference the issue number in the commit message (e.g. `fixes #123`)
   - `gh issue comment <number> --body "..."` to log what was done, if asked
4. Report progress after each issue rather than batching silently.
5. Do not close issues or push without explicit confirmation unless told otherwise.

## Testing

- TDD (red-green-refactor) is a project-wide requirement: write a failing test, watch it fail for the right reason, write minimal code to pass, watch it pass, then refactor. One behavior at a time — never write multiple tests before running any of them.
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

## Decisions log

Every non-trivial decision gets an entry in `decisions.log` — chronological, includes the "why" and alternatives considered, appended at the time the decision is made (not batched). This is the durable record independent of git history shape; commit messages capture the "why" for what shipped but don't give a single browsable trail, and can get lost in a squash/rebase. Don't forget to append to it.
