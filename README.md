# Swamp

Automate the collection, curation, and application of job postings from various job boards. i.e. Ashby.

This README is the living product doc for the project. It reflects the decisions made so far and gets updated as scope evolves — treat it as the source of truth, not a snapshot.

## Problem Statement

Job hunting across multiple companies' Ashby boards means manually re-checking each company's careers page, losing track of what's changed, and re-reading postings from scratch to figure out if/how to tailor a resume and cover letter to them. There's no single place to collect postings from companies of interest, filter down to relevant roles, and keep notes/status on where things stand. Or worse using LinkedIn....

## Solution

A Go CLI/TUI application, backed by local SQLite storage, that:

- Tracks a set of companies of interest and pulls their published postings from Ashby's public job board API
- Filters postings at fetch time so only relevant roles are stored
- Presents postings in a terminal UI for browsing, filtering, and marking up (status, notes, tags, interview progress)
- Keeps a history of posting changes over time rather than silently overwriting

Resume/cover-letter generation and autonomous agents (see original vision below) are explicitly **out of scope** for this version — this phase is about getting clean, curated, well-organized posting data. The data model should stay normalized enough not to preclude that future work, but nothing is built for it yet.

## V1 Scope

- Ashby only (public, unauthenticated `posting-api/job-board/{slug}` endpoint — no API key required)
- Companies and their filters are managed in the database, via the TUI (no YAML config)
- Manual refresh only, via a CLI command — no built-in scheduler/daemon yet
- TUI for browsing, filtering, and marking up postings

## User Stories

1. As a user, I want to add a company by its Ashby slug through the TUI, so that its postings start being tracked.
2. As a user, I want to set filter criteria (department, location) on a company, so that only relevant postings are stored.
3. As a user, I want filters to be extensible beyond department/location, so that I can refine matching criteria as I learn what matters.
4. As a user, I want changing a company's filters to trigger a fresh fetch, so that previously-excluded postings that now match aren't missed.
5. As a user, I want to manually trigger a refresh from the CLI, so that I control when new data is pulled.
6. As a user, I want postings that disappear from a company's feed to be marked closed (not deleted), so that my history and notes on them are preserved.
7. As a user, I want to see when a posting's content changes between fetches, so that I know if a role I'm tracking has been updated.
8. As a user, I want to browse and filter postings in a TUI, so that I can quickly find roles worth pursuing.
9. As a user, I want to view a posting's full detail (description, location, employment type, etc.), so that I can evaluate fit.
10. As a user, I want to open a posting's application URL in my browser directly from the TUI, so that I can apply without retyping a link.
11. As a user, I want to set a status on a posting (new, interested, applied, interviewing, rejected, archived), so that I can track where each opportunity stands.
12. As a user, I want to record interview stages/rounds on a posting (e.g. recruiter screen, technical, onsite) with date and outcome, so that I can track progress through a company's process.
13. As a user, I want to attach free-text notes to a posting, so that I can capture context that doesn't fit a status field.
14. As a user, I want to tag postings with my own labels, so that I can organize them flexibly beyond the built-in status field.
15. As a user, I want to pause a company (stop fetching new postings) without losing its stored postings or my markup on them, so that I can deprioritize without losing history.

## Implementation Decisions

> **Every non-trivial decision also gets an entry in [`decisions.log`](decisions.log)** — chronological, includes the "why" and alternatives considered, and survives independent of git history shape (unlike commit messages, which can get squashed away). This section is a current-state summary; `decisions.log` is the trail of how we got here. Don't forget to append to it.

- **Language/runtime**: Go.
- **Storage**: SQLite, accessed via `database/sql` + [sqlc](https://sqlc.dev/) (hand-written SQL, generated type-safe Go — chosen over an ORM like GORM to keep queries explicit and testable, matching Go idioms rather than ActiveRecord-style abstraction).
- **Migrations**: [goose](https://github.com/pressly/goose), with migration files embedded via `go:embed`.
- **Foreign keys**: no `ON DELETE CASCADE` unless there's a specific justification, even though it's the SQL default reflex. Companies are soft-paused and postings are marked closed rather than hard-deleted (see below), so cascading deletes would only ever fire accidentally — and could silently wipe owned data (notes, tags, interview history) as a side effect. Default to `NO ACTION` (SQLite's default — the delete fails loudly if dependent rows exist) everywhere. The one exception: `posting_tags.tag_id → tags` cascades, because deleting a tag you created should remove its associations — that's an explicit, expected user action, not a surprise. Requires `PRAGMA foreign_keys = ON` per connection, since SQLite doesn't enforce FKs by default.
- **TUI**: [Bubble Tea](https://github.com/charmbracelet/bubbletea) (+ Bubbles/Lipgloss for components/styling).
- **Ashby integration**: public `GET https://api.ashbyhq.com/posting-api/job-board/{slug}` endpoint — no authentication required, one call per company returns all published postings.
- **Company config**: stored in the database, added/edited via the TUI. No YAML.
- **Filtering**: evaluated at fetch time; postings that don't match a company's filters are never stored. v1 filter fields: department, location — designed to be extended with more fields later. Changing a company's filters triggers a full re-fetch for that company.
- **Refresh model**: manual only in v1, via a CLI command (e.g. `swamp fetch`). No background scheduler/daemon.
- **Change handling**: postings are upserted with status + history tracked, not deleted. A posting missing from a fetch is marked closed rather than removed.
- **Markup model**: status field (new / interested / applied / interviewing / rejected / archived), free-text notes, user-defined tags, and a list of interview stage records (name, date, outcome) per posting.
- **Company lifecycle**: soft pause/resume only in v1; no hard delete. Paused companies stop being fetched but retain all stored postings and markup.
- **Future-proofing**: no schema/tables added now for capabilities or resume/cover-letter generation. Keep the posting/company data model clean and normalized so that future work isn't blocked, without building anything for it yet.

### Modules

- **`ashby`** — client for the Ashby public job-board API. Given a company slug, returns normalized postings. All HTTP/JSON handling is encapsulated behind this single call.
- **`filter`** — pure function(s) that decide whether a raw posting matches a company's filter rules. No I/O.
- **`store`** — sqlc-generated queries wrapped in a repository interface: companies, filters, postings (with status + history), tags, notes, interview stages.
- **`sync`** — orchestrates a refresh: pulls from `ashby` per active company, applies `filter`, upserts into `store`, transitions posting status and records history. Triggered by the manual CLI command or a filter change.
- **`tui`** — Bubble Tea views: company management, posting list/detail, filter editing, markup editing (status/tags/notes/interview stages), open-in-browser keybinding.
- **`cmd/swamp`** — entrypoint; launches the TUI by default, exposes a `fetch` subcommand, runs goose migrations on startup.

## Testing Decisions

- TDD (red-green-refactor) is a project-wide requirement.
- Full automated test suites required for the core/orchestration modules: `ashby`, `filter`, `store`, `sync`.
  - `ashby`: tested against recorded/fixture HTTP responses, no live network calls in tests.
  - `filter`: pure function, tested exhaustively against posting/filter combinations.
  - `store`: tested against a real sqlite file (temp db per test), not mocked.
  - `sync`: tested with fakes for `ashby` and a real `store`, verifying fetch → filter → upsert → status-transition behavior end to end.
- `tui` Update-function logic (state transitions) is tested where practical; the View/render layer is not held to automated coverage and is verified manually.
- `cmd/swamp` wiring is verified manually, not unit tested.

## Out of Scope (this phase)

- Job boards other than Ashby
- Scheduled/background refresh (daemon, cron-like behavior)
- Resume and cover letter generation
- Capabilities configuration (YAML or otherwise)
- Autonomous agents operating on postings/resumes

## Further Notes / Original Vision

The long-term vision for this project (unchanged, just deferred beyond this phase):

- Connect to multiple job board APIs beyond Ashby
- Refresh listings on a schedule automatically
- Automatically generate a cover letter and resume tailored to a posting, based on configured capabilities
- Capabilities stored as configuration (YAML or agent-friendly format)
- Agents operating autonomously to build/improve tailored cover letters and resumes based on feedback

## Requirements

- Golang
- SQLite for local storage
- TDD development iteration
