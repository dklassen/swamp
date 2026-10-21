# Swamp 🐊

> Wade through job boards so you don't have to.

Swamp collects job postings from the boards companies actually post on
(Ashby, Greenhouse and Lever), keeps them in a local SQLite database, and
gives you a terminal UI for sorting the promising from the murky. When
something looks good, it hands the posting to an AI agent over
[MCP](https://modelcontextprotocol.io) to score the fit and draft a
tailored resume and cover letter, which you can then export to PDF.

Everything runs on your machine. Your data, your profile and your drafts
stay local.

## Features

- **Fetch** postings from every company you track on Ashby, Greenhouse
  and Lever, on demand or on a schedule.
- **Triage** them in a keyboard-driven TUI: search, mark interested,
  archive, and track each application from first look to final answer.
- **Score** how well each posting fits you, 1 (no fit) to 5 (apply now),
  with the `score-postings` agent skill.
- **Draft** a tailored resume and cover letter with the
  `apply-to-posting` skill, built from your own profile and resume.
- **Export** an application's documents to PDF.

## Getting started

### Prerequisites

| Dependency | Why | Notes |
| --- | --- | --- |
| [Go](https://go.dev/dl/) **1.26.1+** | Builds Swamp and runs its dev tools | The only hard requirement. |
| [Git](https://git-scm.com/) | Cloning the repo | |
| [SQLite CLI](https://sqlite.org/cli.html) (`sqlite3`) | `go tool task db:backup` / `db:restore` | Optional. Ships with macOS. |
| [direnv](https://direnv.net/) | Loads the env vars in `.envrc` | Optional; you can export them yourself. |
| An MCP-capable agent (e.g. [Claude Code](https://claude.com/claude-code)) | Scoring and drafting | Optional; fetching and triage work without one. |

The dev tools
(`task`, `golangci-lint`, `sqlc`, `goose`) are pinned in `go.mod` and run
with `go tool <name>`, so there's nothing else to install.

### Up and running

```sh
# 1. Clone
git clone https://github.com/dklassen/swamp.git
cd swamp

# 2. Point Swamp at a database and documents directory
#    (or just `direnv allow`, which reads .envrc)
export SWAMP_DB_PATH="$PWD/db/swamp.db"
export SWAMP_DOCUMENTS_PATH="$PWD/assets"

# 3. Build
go tool task build          # -> ./bin/swamp

# 4. Create the database schema

./bin/swamp migrate up      # or: go tool task migrate:up

# 5. Dive in
./bin/swamp                 # or: go tool task run (adds the MCP server)
```

In the seed file, `source` is the job board (`ashby`, `greenhouse` or
`lever`) and `source_ref` is the company's slug on it: `acme` in
`jobs.ashbyhq.com/acme`. `seed/data/companies.yaml` is gitignored, so your
list stays yours.

### Hooking up an agent

Scoring and drafting go through Swamp's MCP server. Run it alongside the
TUI, or on its own:

```sh
./bin/swamp --mcp           # TUI with the MCP server in-process (or: go tool task run)
./bin/swamp mcp-serve       # MCP server alone (or: go tool task mcp-serve)
```

It listens on Streamable HTTP at `127.0.0.1:8787` by default
(`SWAMP_MCP_ADDR` to change it). Point your agent there, then set up your
profile (below). The agent skills live in `.claude/skills/`.

## Running things with Task

Day-to-day chores are wrapped in [`Taskfile.yml`](Taskfile.yml). Task is
pinned in `go.mod`, so run it as `go tool task <name>`: no install, and
everyone gets the same version. Tasks that read the database use
`SWAMP_DB_PATH`, defaulting to `./swamp.db`.

| Task | What it does |
| --- | --- |
| `build` | Build the binary to `./bin/swamp`. |
| `run` | Launch the TUI with the MCP server in-process (`./bin/swamp --mcp`). |
| `mcp-serve` | Run the MCP server alone. |
| `migrate:up` / `migrate:down` | Apply pending migrations, or roll back the newest. Build first. |
| `migrate:status` | Show which migrations are applied. |
| `db:backup` | Back up the database to `<db>.<UTC time>.bak` and check it. Safe while Swamp is running. |
| `db:restore -- <file>.bak` | Restore a backup, backing up the current database first. Stop Swamp first. |
| `test` / `test:race` | Run the test suite, optionally under the race detector (needs cgo). |
| `lint` | Run golangci-lint. |
| `fmt` / `fmt:check` | Format the Go code, or just check it. |
| `sqlc:generate` | Regenerate `store/sqlcdb` from `db/queries`. |
| `hooks:install` | Point git at `.githooks/` (once per clone). |

`go tool task --list` prints them all with descriptions.

## Configuration

| Variable | Default | What it does |
| --- | --- | --- |
| `SWAMP_DB_PATH` | `swamp.db` | The SQLite database. |
| `SWAMP_DOCUMENTS_PATH` | `assets` | Your profile, resume and drafted documents. Gitignored. |
| `SWAMP_LOG_PATH` | `swamp.log` next to the database | The TUI's log file. |
| `SWAMP_MCP_ADDR` | `127.0.0.1:8787` | Where the MCP server listens. |
| `EDITOR` | | Used to open documents for editing. |

## Commands

```text
swamp [--mcp]                  run the TUI, optionally with the MCP server in-process
swamp migrate up|down          apply pending migrations, or roll back the newest
swamp fetch                    sync every active company
swamp stage list               print postings eligible for hand-off
swamp stage prepare <posting-id>
swamp export <application-id>  convert an application's documents to PDF
swamp import <seed-file.yaml>  bulk-create companies
swamp mcp-serve                run the MCP server alone
```

`fetch` exits non-zero if any company fails to sync, so it's safe to drop
into cron or a launchd job.

## Your profile and canonical resume

The `apply-to-posting` skill drafts from two files you write and maintain
yourself. Both live under the documents directory (`SWAMP_DOCUMENTS_PATH`,
default `assets/`), which is gitignored, so they never get committed:

| File | What it is | Template |
| --- | --- | --- |
| `assets/canonical/profile.md` | Your background: experience, skills, framing options, and voice/style notes. The source every draft comes from. **Required**: the skill stops without it. | `docs/canonical/profile.md.example` |
| `assets/canonical/resume.md` | Your best-version resume in markdown. The skill tailors it for each posting (reorders, trims, rephrases) instead of writing a resume from scratch. **Optional**: without it, resumes are drafted from the profile alone. | `docs/canonical/resume.md.example` |

The `score-postings` skill scores postings' fit against the profile alone,
and stops without it too.

To set them up:

```sh
mkdir -p assets/canonical
cp docs/canonical/profile.md.example assets/canonical/profile.md
cp docs/canonical/resume.md.example assets/canonical/resume.md
```

Then replace the placeholder text with your own. If you set
`SWAMP_DOCUMENTS_PATH`, use `$SWAMP_DOCUMENTS_PATH/canonical/` instead of
`assets/canonical/`.

Agents read them through the MCP server with `read_profile` and
`read_canonical_resume`, not from disk. Both tools are read-only: edit the
files yourself and the next draft picks up the change, no restart needed.

Keep the resume complete rather than tuned for one role. Tailoring only
cuts and rephrases, and never adds claims that aren't in either file, so
anything left out of both won't appear in a draft.

## Upgrading

Only `swamp migrate up` and `swamp migrate down` change the database's
schema. To upgrade:

1. Stop the TUI, `mcp-serve` and any scheduled `fetch`. When the last one
   closes cleanly, SQLite folds its write-ahead log (`swamp.db-wal`) back
   into the database file, so a copy of that file is a complete backup.
   If a `-wal` file is still there, something is running or crashed:
   find it first.
2. Build, back up, and migrate:

   ```sh
   go tool task build
   go tool task db:backup     # or: cp "$SWAMP_DB_PATH" "$SWAMP_DB_PATH.bak"
   ./bin/swamp migrate up     # or: go tool task migrate:up
   ```

3. Start them again.

Until the database is migrated, the TUI, `mcp-serve` and `fetch` refuse to
start and say so. They also refuse a database migrated by a newer binary
than themselves.

`go tool task run` and `go tool task mcp-serve` don't migrate; they only
check. `./bin/swamp migrate down` (or `go tool task migrate:down`) rolls
back the newest migration. `go tool task db:restore -- <file>.bak` puts a
backup back, after backing up the current database first.

## Contributing

Pull requests welcome. Before sending one:

```sh
go tool task hooks:install     # once per clone: gofmt on commit
go build ./...
go test ./...
go tool golangci-lint run ./...
go tool task fmt:check
```

Run `go tool task --list` for everything else. Design decisions live in
[`docs/rfcs/`](docs/rfcs/), and [`AGENTS.md`](AGENTS.md) covers the
project's conventions for humans and coding agents alike.

Happy hunting. 🐊
