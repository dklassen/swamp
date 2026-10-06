# RFC 0008: A change log in the database, watched by each process

- **Status:** Draft (exploration)
- **Date:** 2026-10-06
- **Related:**
  - RFC 0007 (keeping the TUI, the MCP server and the files in step), and its "Change detection" section on #259's branch
  - #243 (stale TUI), #257 (`data_version` probe), #258 (`document_writes`), #259 (reload on change, paused for this RFC), #260
  - `decisions.log`, 2026-10-06, "#259: change detection by file watch"

## Summary

Swamp runs as several processes: the TUI (possibly more than one window), `mcp-serve`, and `swamp fetch`. The SQLite file is the only thing they share, so it's the only place they can coordinate. RFC 0007 made their **writes** safe and gave the TUI a bare "something changed" signal (`data_version`). That signal can't say **what** changed or **who** changed it, which is why the TUI must reload whole screens and can't tell its own writes from an agent's.

This RFC explores the alternative: an append-only **change log**. Each insert, update or delete on a table we care about adds an event row, in the same transaction, with the old and new values of the columns that matter and the process that made it. Each process keeps a cursor ("last event ID I've seen") and, when woken, reads the new events it cares about.

**Recommendation:** feasible. Every hard question was settled by experiment (below). Build it as the trigger for #259 instead of the `data_version` probe, with SQL triggers writing the events and a per-connection TEMP trigger stamping who wrote them.

## Relationship to RFC 0007: not mutually exclusive

This RFC **builds on RFC 0007 and replaces one piece of its wave B**. It doesn't undo or compete with the rest.

| RFC 0007 piece | Status | Under this RFC |
|---|---|---|
| Wave A: safe writes (#244, #251, #253, #254, #255) | Merged | **Unaffected.** Atomic writes, `ExpectedSHA256` and re-reading before acting are about not losing work, not about noticing changes. Still needed. |
| Step 6: reload after `$EDITOR` (#256) | Merged | Unaffected. |
| Step 7: `data_version` probe (#257) | Merged | **Superseded as the change test**: "are there events past my cursor?" is cheaper to reason about and exact. The probe can stay as a pre-check, or be removed once nothing uses it. |
| Step 8: `document_writes` (#258) | Merged | **Kept, and becomes an event source.** Files can't fire SQL triggers; a document write only reaches the database as a `document_writes` row, and a trigger on that table turns it into an event like any other. |
| #259: `store.ChangeWatcher` (fsnotify on the database's directory) | On the paused branch | **Kept** as the wake-up ("look now"). Its confirmation step changes from the probe to reading the log past the cursor. |
| #259: TUI `dataChangedMsg`, status line, per-screen reload | On the paused branch | **Kept**, and refined: the message can carry the events, so a screen reloads only for changes it shows, and the status line can name who made them. |
| #259: commit-hook counter for the TUI's own writes (RFC 0007's recommendation) | Not built | **Not needed.** Each event carries its origin; the TUI skips its own. |
| #260: reload on screen entry | Not started | Unaffected. Still covers edits made outside Swamp to document files. |

In short: RFC 0007's wave A stands as is, and this RFC is a better wave B signal that reuses what wave B already built.

### Why a change log can't replace wave A

A change log records what already happened. Wave A's fixes are guarantees at the instant of writing: a check and a write that can't be interleaved. Knowing about a change and then acting on it always leaves a gap where something else can change first.

| RFC 0007 item | What it guarantees | Could the log do it? |
|---|---|---|
| #244 `AUTOINCREMENT` + existence check | A draft can't land on a recycled or deleted application | No. The log would record the deletion, but the write must still check. ID reuse is a schema property, and the log needs `AUTOINCREMENT` itself, or its `row_id`s become ambiguous. |
| #251 atomic file writes | No reader sees half a document | No. It's a filesystem property (temp file plus rename). |
| #253 `ExpectedSHA256` | `write_document` can't overwrite an edit it didn't see | Only the token's form: "latest event ID for this document" could replace the hash. The compare-and-write is still needed, and edits made outside Swamp produce no event, so the file hash is the more reliable token. |
| #254 re-read before destructive actions | You don't undo a change you never saw | The mechanism ("events on this row since I loaded it?") could replace the re-read; the check before acting stays. |
| #255 diff on a changed document | A review describes the version on disk | No. It compares file contents; the log only adds who changed it, which #258 already gives documents. |
| #256 reload after `$EDITOR` | The badge is current after an edit | Yes, once editor writes are events (via #258). A few lines either way. |

So the log replaces #257's probe and the unbuilt own-writes counter outright. #254 and #256 could be re-expressed as event checks if the log becomes the TUI's single way of asking "is my copy stale?"; rewriting them only for that isn't worth it on its own.

## Measured (2026-10-06)

All with `modernc.org/sqlite` (Swamp's driver) on a WAL database opened with `store.Open`. Two `*sql.DB` handles on one file stand in for two processes, as in RFC 0007.

| # | Experiment | Result |
|---|---|---|
| 1 | A Go function `swamp_origin()` registered with `sqlite.RegisterScalarFunction`, called from a trigger | Works; each event tagged |
| 2 | `AFTER INSERT/UPDATE/DELETE` triggers writing `json_object(...)` of `OLD.*` / `NEW.*` | Works: e.g. `{"status":"application_started"}` → `{"status":"interviewing"}`. A `WHEN OLD.x IS NOT NEW.x ...` clause skips no-op updates |
| 3 | A rolled-back transaction | Leaves no event: the log is atomic with the change |
| 4 | **A writer whose connection lacks the function** (as the `sqlite3` CLI or an older binary would) | **The write fails**: `no such function`. Guarding the call with `CASE WHEN EXISTS (SELECT 1 FROM pragma_function_list ...)` doesn't help: SQLite resolves functions when it compiles the statement |
| 5 | A main-schema trigger reading a per-connection `temp` table | Refused: `cannot reference objects in database temp` |
| 6 | TEMP triggers installed per connection | Work, but only that connection's writes are logged; another connection's write succeeds **unlogged** |
| 7 | **Hybrid:** main-schema triggers log every change with `origin = NULL`; each Swamp connection adds one TEMP trigger on `main.changes` that fills `origin` from its `temp` table | **Works.** A Swamp connection's change is logged with its origin; an outsider's is logged with `origin = NULL`; neither write is blocked |
| 8 | `sqlite.RegisterConnectionHook` | Reaches every connection the pool opens (from #259's exploration), so step 7's setup can run on each one |
| 9 | Overhead: 2000 committed single-row updates, with and without a logging trigger | 213 vs 160 ms and 293 vs 184 ms (two rounds): roughly 25–55 µs per write. A first run in the other order showed the opposite, so treat it as "small", not a precise figure |
| 10 | A watcher reading 100 new events past its cursor, filtered by table and origin, out of 2004 | 75 µs |

From the real database (read-only):

| Measure | Value |
|---|---|
| Tables | 14 |
| Postings | 2962; `raw_payload` averages 13.7 KB |
| `posting_history` (content changes, closes, reopens) | 2023 rows over 22 days with any: about 92 a day, busiest day 387 |
| Postings whose `last_seen_at` the latest sync set | 2141 |
| Applications / status history / reviews | 39 / 62 / 30 |

**What the volume means:** meaningful changes are tens to hundreds a day. But each sync sets `last_seen_at` on every listed posting (#176), so a trigger that logged every `postings` update would add about 2000 events per fetch, all noise. Triggers must list the columns that matter and skip updates that only touch the others. `raw_payload` must never be copied into an event.

### Deployment and throughput

**Where Swamp runs:** the user runs every Swamp process (TUI windows, `mcp-serve`, `swamp fetch`) on one macOS host. That's what WAL needs: one machine, one memory-mapped `-shm` index. The development VM sees the repository through a `virtiofs` share; it must never open the live database while the host has it open, since WAL's shared memory and locks aren't reliable across that boundary. (This RFC's real-data figures came from read-only opens made from the VM before that was known; the host's `PRAGMA integrity_check` was `ok`. Future data comes from host-side queries or copies.)

**The single writer isn't a throughput limit.** Measured in the VM (`virtiofs` and local ext4; the host's APFS numbers will differ but not by orders of magnitude):

| | ext4 | virtiofs |
|---|---|---|
| Single-row commits, no trigger | 5,000–10,500/s | 4,500–4,800/s |
| With a change-log trigger that fires | ~9,000/s | ~1,900/s |

A fetch is about 2000 one-posting commits; a trigger skips `last_seen_at`-only updates, so a fetch costs what it does today, and ~100 meaningful changes a day add tens of milliseconds a day. Sync commits each posting separately (`IngestPosting`, `ClosePosting`, `ReopenPosting`), so another writer waits at most one posting's transaction, never a whole fetch, far inside `busy_timeout` (5 s).

**File events:** in the VM, one commit produced two `WRITE` events on `-wal`, on both ext4 and `virtiofs`. On the macOS host, fsnotify uses **kqueue**, which reports writes per file, not per directory: fsnotify opens each file in a watched directory to cover that, including files created later (SQLite recreates `-wal`). Not yet verified on the host: run `go test ./store/ -run 'TestChangeWatcher|TestChangeProbe' -count=5 -v` there (on #259's branch) before relying on it.

## Design

### The event table

```sql
CREATE TABLE change_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,  -- the cursor; never reused (#246)
    table_name TEXT    NOT NULL,                   -- applications, postings, ...
    row_id     INTEGER NOT NULL,                   -- the changed row's id (posting_id for keyed tables)
    op         TEXT    NOT NULL,                   -- insert, update, delete
    old        TEXT,                               -- JSON of the logged columns before; NULL for insert
    new        TEXT,                               -- JSON after; NULL for delete
    origin     TEXT,                               -- e.g. "tui:4120", "mcp:3981", "fetch:5512"; NULL: outside Swamp
    at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_change_events_table ON change_events(table_name, id);
```

- **Order:** SQLite has one writer at a time (`_txlock=immediate`), so IDs are handed out in commit order. A reader that has seen ID N has seen everything committed before N+1.
- **Atomic:** events are written by triggers inside the writer's transaction (experiment 3). There's no event for a change that didn't happen, and no change without its event.

### Writing events: triggers, in two layers

1. **Main-schema triggers**, created by migrations, one set per logged table. They never call a Go function, so no writer can be broken by them (experiment 4). They log with `origin = NULL`.
2. **A per-connection TEMP trigger**, installed by each Swamp binary on every connection it opens (`RegisterConnectionHook`): a TEMP table holding this process's origin, and one `AFTER INSERT ON main.change_events WHEN NEW.origin IS NULL` trigger that stamps it (experiment 7).

Writers outside Swamp (the `sqlite3` CLI, a script) still produce events, with `origin = NULL`, shown as "outside Swamp". A Swamp process that somehow writes through a connection without the TEMP trigger is attributed the same way: misattributed, never lost.

Which columns each table logs (first cut, to settle during the work):

| Table | Logged columns | Not logged |
|---|---|---|
| `applications` | `status`, `notes`, `deleted_at` | |
| `application_status_history` | inserts only | |
| `document_reviews`, `document_exports`, `document_writes` | inserts only (`document_type`, `outcome` or `source`, hash) | snapshots, paths |
| `postings` | `listing_status` (open, closed), `title`, `department`, `team`, `location`, `workplace_type`, `employment_type`, `published_at` | `raw_payload`, `description_html`, `description_text` (large: a hash at most), `last_seen_at`, `updated_at` (set by every sync) |
| `posting_markup` | `interested_at`, `archived_at`, `notes` | `updated_at` |
| `companies` | `name`, `description`, `deleted_at` | `last_fetched_at`, `sync_lease_token`, `sync_lease_at`, `updated_at` |
| `company_filters`, `tags`, `posting_tags`, forms | to decide | |

### Watching

- **Cursor:** each process starts at `max(id)` when it starts (it doesn't need history from before it was running).
- **Wake-up:** `store.ChangeWatcher` (#259) keeps watching the database's directory. When the files settle, it reads `SELECT ... FROM change_events WHERE id > ? ORDER BY id`, advances the cursor, and hands the events to the subscriber. No events: nothing happens, so checkpoints and WAL resets stop causing reloads (they did with `data_version`).
- **Missed wake-ups lose nothing.** Unlike `data_version`, the log is durable: if fsnotify drops events (queue overflow) or the watcher restarts, the next read past the cursor returns everything since. A slow fallback poll of `max(id)` (every few seconds) makes even a dead watcher self-heal.
- **Filtering:** each subscriber says what it cares about, e.g. the TUI: `origin IS NOT <mine>`, and per screen, `table_name IN (...)` and `row_id = ?`. Column-level interest can use `json_extract(old, '$.status') IS NOT json_extract(new, '$.status')`.

### What each process would do with it

- **TUI:** reload only the screens whose data an event touches; skip its own events; status line naming the source ("Updated by the agent", "by `swamp fetch`", "outside Swamp"). The review form's "who changed it" (#258) generalises to every table.
- **`mcp-serve`:** later, MCP can push notifications to the agent ("the user flagged your cover letter for Acme"), so an agent acting on an old conversation learns what changed. Not needed for #259.
- **Possible later uses**, not proposed here: an activity view, undo of a status change, deriving `application_status_history` from events. Each would need its own decision, and each is a reason to keep old/new values honest.

## Where it falls apart

| Risk | Effect | Mitigation |
|---|---|---|
| **Schema drift:** a column added to a table but not to its trigger | Changes to it aren't logged; subscribers don't reload | Generate triggers from a per-table column list in one Go/SQL source, and a test (like #246's AUTOINCREMENT rule) that every logged table has all three triggers and that they mention exactly the listed columns |
| **Table rebuilds** (the 00004/00019 pattern: create new, copy, drop, rename) | Dropping the old table drops its triggers | The same test fails until the migration recreates them; note it in AGENTS.md next to the AUTOINCREMENT rule |
| **Noise from sync** | ~2000 `last_seen_at` updates per fetch | Column lists plus `WHEN` clauses (measured volume above) |
| **Growth** | One row per meaningful change; at ~100–400 a day, small, but unbounded | Prune events older than N days (e.g. 30) on `swamp fetch` or TUI start. Cursors start at `max(id)`, so nothing depends on old events |
| **Writers outside Swamp** | `origin = NULL` | Shown as "outside Swamp"; by design |
| **Large values** | Copying `raw_payload` (13.7 KB) or snapshots would bloat the log | Never logged; events carry the small columns or a hash |
| **Write overhead** | ~25–55 µs per write (experiment 9) | Negligible at this scale; recheck if a bulk import ever matters |
| **Driver coupling** | The origin stamping relies on `RegisterConnectionHook` and TEMP triggers | Both are plain SQLite plus one driver hook; a driver change would need the hook re-done, nothing else |
| **kqueue on macOS** (the host) | If fsnotify missed writes to a recreated `-wal`, wake-ups would stop | Verify on the host first (see "Deployment and throughput"). The fallback poll of `max(id)` heals any missed wake-up, since the log loses nothing |
| **Document files edited outside Swamp** | No row, so no event | Unchanged from RFC 0007: reload on entry (#260) and the review form's diff (#255) cover it |

## Options compared

| | `data_version` probe (RFC 0007 / #259 as built) | Change log (this RFC) |
|---|---|---|
| Sees other processes | Yes | Yes |
| Says what changed | No | Yes, with old/new |
| Says who | No (needs a commit-hook counter for "me vs not me") | Yes, per event |
| False positives | WAL resets, own writes | None |
| Missed changes | A missed wake-up is caught on the next change only | None: the cursor catches up |
| Cost | No schema; one pragma per check | A table, triggers per logged table, pruning, a drift test |
| Reload granularity | Whole current screen | Per screen, per row |

## Proposal and work breakdown

Build it as #259's signal, in this order (each its own issue and PR):

1. **Feasibility spike in code** (#267, S): the `change_events` table and the two-layer triggers on `applications` only, the connection hook stamping origin, and tests for experiments 3, 4 (outsider writes still succeed) and 7. Proves the mechanism in the real store.
2. **Trigger generation and the drift test** (#268, S/M): one place listing logged tables and columns; the test that fails on a missing or stale trigger; AGENTS.md rule.
3. **The remaining tables** (#269, S), per the column table above, with the sync-noise test (a fetch that only refreshes `last_seen_at` logs nothing). Events name rows by ID, so a table whose IDs can be reused needs `AUTOINCREMENT` first (#246); that matters for tables with hard deletes (`company_filters`, rebuilt on every sync; `interview_stages`).
4. **Watcher reads the log** (#270, S): `ChangeWatcher` confirms by reading past its cursor instead of the `data_version` probe, which is then removed (#257); a fallback poll of `max(id)`; pruning events older than 30 days.
5. **#259 on top** (M): the TUI consumes events, skips its own, reloads per screen, names the source.

## Decided (2026-10-06, user)

- **Retention:** 30 days.
- **Origin:** `kind:pid`, e.g. `tui:4120`, `mcp:3981`, `fetch:5512`. No per-window names.
- **The `data_version` probe (#257) is removed** in step 4, once the watcher reads the log instead.
- **Work items filed** for steps 1–4; step 5 is #259 itself.

## Open questions

1. **Where to prune:** `swamp fetch`, TUI start, or both? Proposed: both; it's one `DELETE` on an indexed column.
2. **Columns per table:** the first cut above, especially for `postings`; settled in step 3.
3. **Agent notifications:** worth an MCP follow-up once the log exists?

## Out of scope

- Replacing `application_status_history` or `posting_history` with events.
- Cross-machine sync, or any writer the kernel and SQLite's WAL can't see (see RFC 0007's "Change detection").
- A general audit or undo feature.
