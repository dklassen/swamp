# RFC 0001: Scheduled and background posting sync

- **Status:** Implemented in part, 2026-09-30. P1–P4 and option 2 shipped (#147–#153: PRs #155–#161; see "What shipped"). Option 1 (launchd) is not filed yet and waits on open questions 1 and 4.
- **Date:** 2026-09-28 (updated 2026-09-29: #146 merged; option 2 design and P4 added; P4 reproduced, per-company lock added as P4d, issues filed. Updated 2026-10-01: what shipped)
- **Related:**
  - `decisions.log` entries "Refresh is manual only in v1, no scheduler/daemon" (2026-08-10) and "SyncAll: sequential, not a goroutine fan-out/fan-in pipeline" (2026-08-11)
  - Done: #136 (P1, PR #137), #142 (P2, PR #143), #145 (P3, PR #146), #140 (`published_at`, PR #141), #138 (status line visible on every screen, PR #144)
  - Open: #139 (config file), where this RFC's new settings should end up

## Summary

Postings only refresh when someone presses `r` on a company in the TUI or runs `swamp fetch`. This RFC looks at two ways to take that manual step away:

- running the sync on a **schedule**;
- **queuing a sync of every company** that runs in the background while the TUI stays usable.

It ranks seven approaches from simplest to most involved. Every approach first needs a small set of prerequisites. When this RFC was written the codebase wasn't safe to write from two places at once. That fix (P1), a board fetch timeout (P2) and a failing exit status for `swamp fetch` (P3) have all since merged. A fourth, P4, was found while designing option 2: a company's sync can be left half-done by an interruption or an overlapping sync. **The recommendation is:** fix P4, add a "sync all" key to the TUI (option 2), and run the existing `swamp fetch` from launchd (option 1). Together these cover both requests with little new code.

## Problem

- **Manual only.** Refreshing is a manual step, one company at a time in the TUI (`r` on the company list). There's no TUI action that syncs everything.
- **Silent staleness.** New postings don't show up until someone remembers to refresh. Postings that were taken down don't get closed, so their applications don't move to `posting_closed` either.
- **The CLI isn't a full substitute.** `swamp fetch` syncs every active company, but only when run by hand.

The 2026-08-10 decision kept v1 manual on purpose, and noted that scheduling could be added later without reshaping anything. This RFC is that later step.

## How sync works today

- **Core call:** `sync.Syncer.SyncAll` (`sync/sync.go`) loops over the active companies one at a time and calls `SyncCompany` for each. Each company's error goes into its own `Result`, so one failure doesn't stop the batch. Every board fetch runs under `sync.Config.FetchTimeout` (P2).
- **Database:** every process opens the database through `store.Open` (P1), so the TUI, `swamp fetch` and `mcp-serve` can write at the same time.
- **CLI:** `swamp fetch` (`cmd/swamp/main.go`, `runFetch`) calls `SyncAll` and prints one line per company. Since P3 it exits 1 if any company failed, and writes errors and a summary line (`41 companies, 1 failed (Outschool)`) to stderr.
- **TUI:** `refreshCompany` (`tui/app.go`) runs `SyncCompany` for a single company as a `tea.Cmd`. When it finishes, the result shows in the status line. (Until #138 the company list was drawn too tall and the status line was cut off, so the result never actually showed. It does now.)
- **Last-fetched time:** `companies.last_fetched_at` (migration 00011) is set after each successful fetch and shown on the company list. It's the only record of sync health in the database; beyond it there's only `swamp fetch`'s output, which lasts only if something logs it.
- **MCP server:** `swamp mcp-serve` is a long-running process on the host. It already holds a `Syncer`, which `add_company` uses.

### Measured (2026-09-28, copy of the real database, before P1 and P2)

| Measurement | Result |
|---|---|
| Full `swamp fetch`, 41 active companies | **about 12 s** wall-clock, sequential |
| Companies failing in a single run | 2 or 3 (see below) |
| Two `swamp fetch` runs at once, connection settings before P1 | **39 of 41 and 5 of 41 companies failed** with `database is locked (SQLITE_BUSY)` |
| Same, with `busy_timeout(5000)` and `journal_mode(WAL)` set | **No lock errors** in either run; only the data errors remained |

What this means:

- **Speed isn't the problem.** 12 seconds sequentially is fine, so there's no need to make sync concurrent (the 2026-08-11 decision still holds). "Background" is about keeping the TUI responsive and not needing a person, not about going faster.
- **Concurrent writes were the problem.** Before P1, a scheduled sync that ran while the TUI (or `mcp-serve`) was writing lost that race most of the time. P1 fixed this.

**After P1, P2 and the `published_at` fix** (a full `swamp fetch` against all real boards, recorded in `decisions.log` under #142): 40 of 41 companies synced and nothing timed out. The only failure was Outschool's removed board (404).

## Prerequisites (needed by every option)

These are small, independent changes. Each is worth doing even if no scheduling ever ships.

| | Prerequisite | Status |
|---|---|---|
| P1 | SQLite safe for more than one writer | **Done** (#136, PR #137) |
| P2 | Timeout on job board fetches | **Done** (#142, PR #143) |
| P3 | Failures visible when no one is watching | **Done** (#145, PR #146) |
| P4 | A company's sync survives interruption and overlap | **Done** (#147 PR #155, #148 PR #156, #149 PR #157, #150 PR #158) |

### P1. Make SQLite safe for more than one writer (#136, done)

- **Before:** `sql.Open("sqlite", "file:"+dbPath)` set no pragmas. A second writer got `SQLITE_BUSY` straight away instead of waiting.
- **Change (shipped):** the database is opened through `store.Open` (`store/open.go`), which sets three modernc.org/sqlite DSN parameters:
  - `busy_timeout` makes writers wait for the lock instead of failing. It's the one tunable setting (`store.Config`, default 5 s).
  - WAL lets readers keep reading while a write is in progress, so the TUI doesn't stall during a sync.
  - `_txlock=immediate` makes transactions take the write lock when they begin. `UpsertPosting` and `CreateCompany` read and then write inside one transaction. Without this, a commit from another process in between makes the write fail immediately, and `busy_timeout` can't retry it.
  - WAL and immediate transactions are named constants, not settings, because a different value would bring the bug back.
  - `store.Open` later also gained `_time_format=sqlite` and `_timezone=UTC` (`store.Config.TimeFormat` and `Timezone`) as the fix for the `published_at` bug below.
- **Verified:** against a database copy.
  - Two concurrent `swamp fetch` runs: no lock errors.
  - Starting an application in the TUI during a background fetch: no errors in either.
- **Caveat (measured):** the first time a database is switched to WAL, the switch needs exclusive access. When two processes start at the same moment on a not-yet-WAL database, one fails its startup migration check. This happened 3 out of 3 times. Once the database is in WAL, simultaneous starts were clean 3 out of 3 times. The switch happens once per database (WAL is stored in the file), so start one process on its own the first time after upgrading. The `db/swamp.db` in this checkout has made the switch (WAL as of 2026-09-29).
- **Side effect:** WAL adds `swamp.db-wal` and `swamp.db-shm` files next to the database. They're now in `.gitignore`. Backups must copy all three files, or use `VACUUM INTO`.
- **Also fixed:** a TUI `r` refresh runs in a goroutine and shares the connection pool with the TUI's other writes, so it could already collide within one process.

### P2. Give the job board clients a timeout (#142, done)

- **Before:** `ashby`, `greenhouse` and `lever` all default to `http.DefaultClient`, which has no timeout. The add-company form put a 15-second deadline on its own check for exactly this reason.
- **Risk:** one board that hangs stalls an unattended run forever, and nobody is watching.
- **Change (shipped):** every board fetch `Syncer` makes (sync, the add-company checks, import) runs under `context.WithTimeout(sync.Config.FetchTimeout)`, default 30 s, about 6× the slowest real board seen. A timeout fails only that company; `SyncAll` carries on. Verified with the real Ashby client against a server that never answers and one that stalls mid-body: both stop at the deadline. `sync.New` now takes a `sync.Config`, and `swamp fetch` passes `sync.DefaultConfig()`.

### P3. Make failures visible when no one is watching (#145, done)

- **Before:** `swamp fetch` exited 0 even when companies failed, and printed per-company errors to stdout. The only lasting sign of trouble was a stale "last fetched" time.
- **Change (shipped, PR #146):** `swamp fetch` exits 1 if any company failed. Each company's error goes to stderr, followed by a one-line summary that always prints ("41 companies, 1 failed (Outschool)", or "40 companies, 0 failed"). Successful company lines stay on stdout. Verified on a database copy: exit 1 with Outschool active, exit 0 with it soft-deleted. The output is built by `reportFetch` in `cmd/swamp/main.go`, which has a unit test.
- **Cleanup (done 2026-09-29):** Outschool, whose board is gone, was removed, leaving 40 active companies, so a clean run exits 0 again.
- **Not in #145:** a `sync_runs` table (started, finished, counts, errors as JSON) that the TUI could show ("last full sync: 2h ago, 1 failed"). A history of sync runs is really the first piece of a general record of background job executions, which feeds into option 7 (a job queue with a worker). It needs its own design discussion first; see open question 2.

### P4. Make a company's sync survive interruption and overlap (done)

**Shipped (2026-09-30):**
- `store.ClosePosting` (#147), `store.IngestPosting` and `store.ReopenPosting` (#148) each write a change and its history (and, for a close, the application) in one transaction. Each write happens only if the row is still in the expected state.
- `SyncCompany` ignores cancellation after its fetch (#149).
- **Per-company sync lease (#150):** `SyncCompany` holds it from before the fetch until after the last write (`companies.sync_lease_token` / `sync_lease_at`, migration 00013, `sync.Config.LeaseTimeout`, default 5 min).
  - A sync that finds the lease held returns `sync.ErrSyncInProgress` without fetching.
  - `swamp fetch` lists it as skipped and doesn't count it as a failure.
  - The TUI shows "already syncing elsewhere" as status, not as an error.
- Verified with two `swamp fetch` runs at once against real boards, and in the real TUI.

The problem as found:

Found while designing option 2. `SyncCompany` (`sync/company.go`) is not one transaction. After the fetch, it makes many separate writes, each committed on its own:
- a `posting_history` row, then the upsert, close or reopen it records;
- then, for postings that have disappeared, `MarkPostingClosed` followed by closing the posting's application;
- finally `MarkCompanyFetched`.

Anything that stops it partway leaves damage the next sync doesn't repair. That includes a cancelled context, an error, `SQLITE_BUSY` after the busy timeout, quitting the TUI, or a killed `swamp fetch`.

- **Measured by prototype (c):** a context that cancels at its k-th check, swept over every point where `SyncCompany` checks ctx, followed by a clean re-sync. 53 of 164 cut points left damage:
  - **Application never closed:** a cut after `MarkPostingClosed` but before the application is closed leaves the application at `application_started` for good. The next run skips the posting because it's no longer `open`. This is the worst case, since it breaks #105's guarantee.
  - **Duplicate history rows:** a cut between a history insert and its matching change means the next run records the same `content_updated` or `closed` history again.
  - **`last_fetched_at`:** not updated, which is correct.
  - **Posting upserts:** each is its own transaction, so no half-written rows.
- **Overlap, also measured:** two `SyncCompany` runs on one company, held at a barrier after the fetch.
  - With 30 postings to close, all 30 of 30 trials wrote duplicate `closed` history rows.
  - With one new posting, `Created` was counted twice in 30 of 30 trials, though the row itself is fine because `UpsertPosting` re-checks inside its transaction.
  - There were no errors or deadlocks: P1 prevents crashes, but not these logical races.
  - Overlap happens with a TUI run plus `r` or a filter save, and with a launchd `swamp fetch` while the TUI syncs.
- **Reproduced on 2026-09-29** against current code, with throwaway tests (the examples are in the issues):
  - A failed application update after `MarkPostingClosed`, followed by a clean sync, leaves the posting `closed` and the application at `application_started`.
  - Two overlapping closes of 5 postings wrote duplicate `closed` history in 20 of 20 trials.
  - Overlapping reopens and content changes wrote duplicate history in 20 of 20.
  - One new posting was counted as `Created` twice in 20 of 20.
  - A cancellation sweep (93 cut points, 2 postings with applications) left an application stuck in 18 cases and duplicate history in 23.
- **Overlap from different fetches (found 2026-09-29).** Making writes conditional doesn't cover two runs that fetched the board at different moments:
  1. Run A fetches a board that briefly dropped posting X.
  2. Run B fetches later, sees X and reopens it.
  3. Run A then closes X and moves its application to `posting_closed`.

  The "only if still open" check passes. The next sync reopens the posting, but reopening never restores the application, so the user's status is lost. Reproduced: an application at `application_submitted` ended at `posting_closed`, with the posting open again. Only stopping the overlap prevents this, hence P4d.
- **Change,** one issue each:
  - **P4a (#147):** close a posting, record its history and close its application in one transaction. The close is conditional (`UPDATE … WHERE listing_status = 'open'`), and history is written only if a row changed.
  - **P4b (#148):** record `reopened` and `content_updated` history in the same transaction as the change, and only when the change happens. The reopen is conditional. `UpsertPosting` reports whether it created the row, so `Created` isn't counted twice.
  - **P4c (#149):** in `SyncCompany`, apply `context.WithoutCancel` after the fetch, so a caller's cancellation can't cut the write phase. **Preventive:** every current caller passes `context.Background()`. It protects the signal handling or stop-cancels that background and scheduled sync are likely to add.
  - **P4d (#150):** exclusion at two levels.
    - **Across processes:** a per-company lease in the database (`companies.sync_lease_token`, `sync_lease_at`). It's taken with a conditional `UPDATE` before the fetch, released after the last write (only by its owner), and expires after `sync.Config.LeaseTimeout`, so a killed process can't hold it forever. A sync that finds the lease held returns `sync.ErrSyncInProgress` without fetching.
    - **Within the TUI:** `App` refuses a second trigger (`r`, `R`, the sync after a filter save) for a company that's already syncing (#152, #153).
- **Why it's a prerequisite now:** a background TUI run and a scheduled `swamp fetch` both make interruption and overlap far more likely than today's manual `r`. Option 2's in-process guard covers one TUI; only P4d's lease covers two processes.

### Found along the way: a `published_at` bug (#140, fixed)

Every run in the original tests failed Mattermost (`update posting`) and Instacart (`create posting`) with:

```
sql: Scan error on column index 14, name "published_at": unsupported Scan, storing driver.Value type string into type *time.Time
```

- **Cause:** the driver wrote a `time.Time` with `t.String()`, which only reads back when the offset matches the machine's local timezone. This had nothing to do with scheduling, but a scheduled run would have failed those companies every time with nobody noticing.
- **Fix (PR #141):** `store.Open` writes times with a numeric offset, in UTC, and migration 00012 rewrote existing values. Every timestamp column now stores UTC (a rule in `AGENTS.md`). That matters for anything this RFC adds that compares times in SQL, such as option 3's staleness check or a `sync_runs` table.
- **The third failure** was Outschool: its Greenhouse board returns 404. That's a real board that was taken down, and the company has since been removed (see P3).

## Options, ranked by simplicity

| Rank | Option | New code | On a schedule | Sync all from the TUI | Works with the TUI closed |
|---|---|---|---|---|---|
| 1 | launchd/cron runs `swamp fetch` | Very little (plist and a task) | Yes | No | Yes |
| 2 | TUI "sync all" key, running in the background | Small (mostly TUI; ~90 lines plus small `sync` changes) | No | **Yes** | No |
| 3 | Auto-sync when the TUI starts, if stale | Very small once 2 exists | Sort of | Automatic | No |
| 4 | Periodic timer inside the TUI | Small once 2 exists | Yes, while open | Yes | No |
| 5 | `swamp fetch --every <duration>` loop | Small | Yes | No | Yes, if kept alive |
| 6 | Scheduler and `sync_all` tool in `mcp-serve` | Moderate | Yes | Through MCP only | Yes, while the server runs |
| 7 | Persistent job queue with a worker | Large | Yes | Yes (enqueue) | Depends on the worker's host |

### 1. launchd (or cron) runs the existing `swamp fetch` (simplest)

- **What:** a checked-in launchd agent template, e.g. `deploy/launchd/com.swamp.fetch.plist`. It runs the built binary's `fetch` subcommand on a schedule. Add `task fetch:schedule:install` and `task fetch:schedule:uninstall`, which copy the plist into `~/Library/LaunchAgents` and run `launchctl bootstrap` / `bootout gui/$(id -u)`.
- **Plist contents:**
  - `ProgramArguments` with absolute paths;
  - `EnvironmentVariables` for `SWAMP_DB_PATH` and `SWAMP_DOCUMENTS_PATH` (launchd doesn't read `.envrc`). If #139's config file lands first, the job could use that instead; the env vars keep working either way;
  - `StartCalendarInterval` (e.g. 08:00, 12:00 and 17:00) or `StartInterval`;
  - `StandardOutPath` and `StandardErrorPath` pointing at a log file.
- **Why launchd over cron on a Mac:** if the machine is asleep when a `StartCalendarInterval` job is due, launchd runs it once on wake. cron skips it, which matters on a laptop.
- **Pros:**
  - No new Go code: the plist and two tasks are the whole change.
  - A failed run shows in the job's log and exit status (P3), and launchd records the last exit status (`launchctl print gui/$(id -u)/com.swamp.fetch`).
  - Runs whether or not the TUI is open.
  - The OS handles scheduling, logging and restarts.
- **Cons:**
  - Setup is per machine and outside the app.
  - Nothing shows in the TUI except the updated "last fetched" times.
  - The plist points at `bin/swamp`, so a broken or wrong-platform build silently breaks the schedule. `bin/swamp` recently became a macOS binary while work was happening in a Linux container, which shows this can happen. Consider installing a copy (e.g. `~/.local/bin/swamp`) instead.
- **Needs:** P1 (the TUI or `mcp-serve` may be writing when the job fires), P2 and P3 (all done), and P4, since a scheduled run can overlap a TUI sync.
- **First run after upgrading:** P1's WAL caveat applies. If the database hasn't been opened since `store.Open` shipped, open it once on its own before installing the job, so the job doesn't race the TUI on the one-time switch to WAL. The simplest way is to run `swamp fetch` once by hand.

### 2. A TUI "sync all" key that runs in the background

**Shipped (2026-09-30), as designed below:**
- `sync.Result.Name` and `sync.Summarize` (#151); `r` became a request handled by `App` (#152).
- `R` (#153) runs chained `tea.Cmd`s with the run's state in `App` (`tui/sync_all.go`).
  - `R` again stops the run after the company in flight.
  - During a run, `r` and filter saves are refused (not queued).
  - "Last fetched" isn't updated live; the company list reloads once at the end.
- **Found while verifying:** quitting mid-run abandoned the in-flight company and left its lease held until expiry. `main` now calls `Syncer.ReleaseHeldLeases` after the TUI exits, before closing the database.
- Option 1 (launchd) is next and not yet filed.

The design as proposed:

- **What:** `R` on the company list (still unused there; `r` refreshes one company) syncs every active company without blocking the TUI. The status line shows `Syncing 12/40: Kong…` and ends with the same summary `swamp fetch` prints (`40 companies, 1 failed (Kong)`). Since #138 every screen sizes itself under the status line, so progress stays visible.
- **Pros:** directly covers "queue up the entire sync", with visible progress. Almost all the code is in the TUI.
- **Cons:** only runs while the TUI is open.
- **Needs:** P1 and P2 (done), and P4 below for robustness against interruption and overlap. The detailed design follows.

#### Option 2 in detail: Go structure

Explored on 2026-09-29 with three throwaway prototypes, all passing build, tests and lint:
- (a) chained `tea.Cmd`s;
- (b) one worker goroutine with a progress channel and context cancellation;
- (c) the `sync` package side: iterator vs callback APIs, a shared summary, and probes of cancellation and overlap.

**Recommended: chain one `tea.Cmd` per company, with the run's state owned by `App`.** Bubble Tea already runs each `tea.Cmd` in its own goroutine and delivers its result as a message. So a loop of "sync one company, return a message, `Update` issues the next command" gives background work, progress and cancellation without any goroutines, channels or contexts of our own.

```go
// tui/sync_all.go
type syncAllState struct {
    runID     int             // bumped on every start; stale messages are dropped
    companies []store.Company // snapshot of a.companies when R was pressed
    next      int             // index of the company in flight
    results   []sync.Result
    running   bool            // true until the in-flight company reports back, even after stop
    stopping  bool            // stop requested: don't issue the next command
}

type syncAllStepMsg struct {
    runID  int
    result sync.Result
}

func syncAllStep(syncer *sync.Syncer, runID int, c store.Company) tea.Cmd // runs SyncCompany, returns syncAllStepMsg
func (a *App) startSyncAll() tea.Cmd                     // R: snapshot companies, issue step 0
func (a *App) handleSyncAllStep(msg syncAllStepMsg) tea.Cmd // record, then next step or finish
func (a *App) stopSyncAll()                              // set stopping; the in-flight company finishes
```

- **Who owns what:** `App` owns `syncAllState`, following the existing pattern where App owns domain data and the status line. `companyListModel` only turns keys into intents (`startSyncAllMsg{}`, and `refreshCompanyMsg{}` for `r`, which is currently a direct `tea.Cmd`). The run carries on if the user leaves the company list, and the summary still shows. The prototype tested this.
- **Progress before each company:** `Update` sets `Syncing k/N: <name>…` as it issues step k, so the status names the company being fetched, not the one that just finished.
- **Stopping:** `stopping` means "don't issue the next command". The company in flight always finishes, because `SyncCompany` isn't one transaction (see P4). `running` stays true until that company's message arrives, so a quick stop-then-`R` can't overlap two runs. The status reads `Stopped after 12/40: …` using the same summary.
- **Stale messages:** `runID` is one int that drops a late or duplicate message from an earlier run. With the "running until the in-flight company reports" rule it's mostly insurance, but the prototype's stale-message test fails without it, so it does real work.
- **Its own message type, not `companyRefreshedMsg`:** reusing that handler would reload the company list 40 times, overwrite the progress text with per-company lines, and has no run ID. With its own type, the list reloads once at the end, plus the posting list if the selected company synced. The cost is that "Last fetched" doesn't update live; one `loadCompanies` per step would fix that if wanted.
- **Guarding other syncs of the same company:** while a run is active, App refuses `r` and the sync that follows saving a filter (`companyFiltersAppliedMsg`), with a short status message. Two overlapping `SyncCompany` calls on one company aren't safe today (see P4).
- **Tests:** synchronous and deterministic. A `step()` helper executes one command and feeds its message back, so tests can press stop or `R` between companies. The existing `applyCmd` helper drains the whole chain. `newTestSyncer`/`fakeFetcher` cover the fetches. The prototype came to about 90 lines of production code and 6 tests.
- **Quit mid-run:** Bubble Tea's `handleCommands` never waits for running commands; it abandons them (checked in v1.3.10 source). `main` then closes the database, and the in-flight company fails partway. The `r` refresh already has this exposure. P4 makes it harmless; an explicit "wait for the in-flight company on quit" isn't worth building on top.

**Changes to the `sync` package:**
- **`Result.Name`:** `SyncCompany` fills it from the company it already loads. Callers stop building `names` maps (`runFetch`), and `refreshCompany`/`applyCompanyFilters` stop passing the name alongside.
- **`sync.Summarize([]Result) Summary` with `Summary.String()`:** moves the `40 companies, 1 failed (A, B)` wording out of `package main` so `reportFetch` and the TUI share it. It also fixes "1 companies" to "1 company".
- **Cancellation only during the fetch:** in `SyncCompany`, `ctx = context.WithoutCancel(ctx)` right after the fetch, since nothing has been written before it. A cancelled caller then never leaves a company half-written, while a slow fetch can still be abandoned. This belongs with P4.
- **Not needed for option 2:** a `SyncEach` iterator (`iter.Seq2[Progress, error]`). Prototype (c) built one, and it's a good fit for the CLI: `reportFetch` could print each company as it finishes. But the TUI can't range over a blocking iterator inside `Update`. It could drive one with `iter.Pull2`, one `next()` per command, but that's more machinery than calling `SyncCompany` per command. Worth adding only if streaming CLI output is wanted.

**Considered and rejected:**
- **Worker goroutine + progress channel + `context.CancelFunc`** (prototype b). It works: an unbuffered channel closed by its only sender, a `select` on `ctx.Done()` on every send so nothing leaks, a `done` channel to wait for the worker, and an `App.Shutdown()` called from `main`. But it costs about 100 more lines of concurrency code, and its tests need to be pumped by hand, gated fetchers, a sleep, and `Shutdown` in every cleanup. Its one real advantage, waiting for the in-flight company on quit, is better solved by P4. It also starts the goroutine as a side effect inside `Update`.
- **`program.Send` from a goroutine:** App would need the `*tea.Program` injected after construction, a chicken-and-egg problem, and tests that drive `Update` directly couldn't use it.
- **A callback API** (`SyncAll(ctx, func(Progress))`) or **a channel-returning API:** both still need a goroutine plus a channel on the TUI side, and the channel version hands goroutine lifetime and draining to every caller.

**Open UX points:**
- **Stop key:** Esc on the company list already means "back". The prototype made the first Esc during a run mean "stop", which overloads it. A dedicated key (e.g. pressing `R` again to stop) keeps Esc predictable. See open question 7.
- **Banner priority:** `a.err` takes precedence over `a.status`, so an error from another action hides the progress until the user changes screen.
- **Viewing failures afterwards:** the summary names them. The per-company error could also show in the company's `i` info box until its next successful sync.
- **Cursor jumps:** `postingsLoadedMsg` resets the posting list's cursor, so the end-of-run reload can move the cursor under someone browsing that company. This is an existing behaviour, but more likely to be hit with a background run.

**Suggested split into PRs:**
1. The `sync` changes: `Result.Name`, `Summarize` (with `reportFetch` using it) and fetch-only cancellation.
2. The TUI `R` run as above, including the guard on `r` and filter saves.

P4 can go before or alongside these.

### 3. Sync automatically when the TUI starts, if the data is stale

- **What:** on `Init`, if the oldest `last_fetched_at` among active companies is older than a threshold (e.g. 6 h), start option 2's sync automatically.
- **Pros:** about 20 lines on top of option 2, and data is fresh whenever it's actually looked at.
- **Cons:** only runs when the TUI starts, and adds network calls at startup. Make the threshold configurable, including a way to turn it off, following the `store.Config`/`sync.Config` pattern so #139 can pick it up.
- **Note:** `last_fetched_at` is set with `CURRENT_TIMESTAMP`, so it's UTC. Compare it against `time.Now().UTC()`, or do the comparison in SQL.

### 4. A periodic timer inside the TUI

- **What:** a `tea.Tick` every N hours that starts option 2's sync.
- **Pros:** small on top of option 2.
- **Cons:** only works while the TUI stays open. The 2026-09-08 decision chose a manual `u` key over automatic polling for document refresh, and the same reasoning applies here. Most useful for someone who leaves the TUI open all day. Probably not worth it if option 1 exists.

### 5. `swamp fetch --every <duration>` as a long-running loop

- **What:** a flag that makes `fetch` loop: sync, sleep, repeat, stop on SIGINT/SIGTERM.
- **Pros:** portable (the same command works under launchd, systemd, tmux or a container) and doesn't depend on the OS scheduler.
- **Cons:**
  - Something still has to keep it running (launchd with `KeepAlive`), which is option 1 plus a process to babysit.
  - Misses runs across sleep unless it compares against `last_fetched_at` on wake.
  - Only better than option 1 if a non-launchd environment appears.

### 6. Scheduler and a `sync_all` tool inside `mcp-serve`

- **What:**
  - `mcp-serve` already runs on the host and holds a `Syncer`. Add a goroutine with a ticker that calls `SyncAll`.
  - Add a `sync_all` MCP tool so an agent can queue a full sync.
  - Guard against overlapping runs with a mutex or single-flight.
- **Pros:** reuses a process that's already running, and gives agents a way to trigger a sync.
- **Cons:**
  - Mixes two unrelated jobs, agent hand-off and scheduled ingestion, in one process.
  - Only runs while the server is up, and the server only needs to be up for agent sessions.
  - Harder to test and to reason about shutting down.
- **Worth it if** agent-triggered syncing becomes a real need. Even then, a `sync_all` tool that runs synchronously (about 12 s) is enough without any scheduler.

### 7. A persistent job queue with a worker (most involved)

- **What:**
  - A `sync_jobs` table (`id`, `company_id` or NULL for all, `status`, `attempts`, `run_after`, `error`, timestamps).
  - Producers (TUI, CLI, MCP, a scheduler) insert rows.
  - A worker (option 5's loop, or a goroutine in `mcp-serve`) claims rows with an `UPDATE … RETURNING`, runs `SyncCompany`, and records the outcome, with retries and backoff.
  - The TUI shows a queue and history view.
- **Pros:**
  - True "queue up and walk away" behavior, from any producer.
  - Retries, history and per-company scheduling.
  - Survives restarts.
- **Cons:**
  - The most code by far: a migration, sqlc queries, worker lifecycle, stuck-job recovery, and a TUI view.
  - Most of that machinery is for a job that takes 12 seconds and is safe to run again.
- **Revisit when:** there are many more companies, or boards start rate-limiting and need per-company pacing and retries.

## Recommendation

1. **P1 (#136), P2 (#142), P3 (#145) and the `published_at` bug (#140) are done.** Outschool, whose board was gone, has been removed, so a scheduled run's exit status now reflects real failures.
2. **P4 (a company's sync survives interruption and overlap)** next: #147, #148, #149 and #150 (the per-company lock). It fixes an existing bug where an interrupted sync can leave an application never closed, and the stale-fetch race that closes a reopened posting's application. Background and scheduled syncs make both more likely.
3. **Option 2 (TUI `R` = sync all in the background)**, built as chained `tea.Cmd`s with state in `App` (see "Option 2 in detail"). It's three PRs: the `sync` package changes (#151), routing `r` through `App` (#152), then the `R` run (#153).
4. **Option 1 (launchd running `swamp fetch`)** for the schedule. It needs almost no new code and runs whether or not the TUI is open. With P3 done, a failed run shows in its exit status and log. It should wait for P4, since a scheduled run can overlap a TUI sync.
5. **Option 3** is a cheap follow-up if data still feels stale when the TUI opens.
6. **Defer options 4 to 7.** Revisit option 6 if agents need to trigger syncs, and option 7 if scale or rate limits require retries and pacing.

Suggested order: ~~P1~~ → ~~P2~~ → ~~P3~~ → ~~P4 (#147–#150)~~ → ~~option 2 (#151–#153)~~ → option 1. Option 2 comes before option 1 because it's useful on its own and exercises P1 inside one process first. Option 1 has no code dependency on option 2, though, so they can go in either order if the schedule matters more. Each step is its own issue and PR, following the repo's one-issue-per-PR workflow.

## Work breakdown

Filed 2026-09-29. Every issue carries the `rfc-0001` label, and an `rfc0001-step-N` label giving the order to work in. List them with `gh issue list --label rfc-0001`.

| Step | Issue | Part | Work | Size | Depends on |
|---|---|---|---|---|---|
| 1 | #147 (done) | P4a | Close a posting, its history and its application in one transaction; conditional close | S–M | — |
| 2 | #148 (done) | P4b | Reopen and content-update history in the same transaction as the change; conditional reopen; `Created` from `UpsertPosting` | M | #147 (same loop) |
| 3 | #149 (done) | P4c | `context.WithoutCancel` after the fetch (preventive) | S | — |
| 4 | #150 (done) | P4d | Per-company sync lease across processes; `ErrSyncInProgress`; skipped companies in `swamp fetch`'s summary | M | #147, #148 |
| 5 | #151 (done) | Option 2 | `Result.Name`, `sync.Summarize` (fixes "1 companies"), `reportFetch` uses both | S | — |
| 6 | #152 (done) | Option 2 | `r` becomes a request message handled by `App` (refactor only) | S | — |
| 7 | #153 (done) | Option 2 | `R` syncs every company in the background; guard on `r`, `R` and filter saves; stop with `R` | M | #151, #152; after P4 |

- Steps 3, 5 and 6 have no code dependency on the steps before them and could go in parallel. The step labels give the agreed order.
- Option 1 (launchd) isn't filed yet. It follows step 7 and needs open questions 1 and 4 answered.
- For #153, the issue takes the defaults this RFC leaned towards: `R` again to stop (question 7), refuse filter saves during a run (question 8), and no live "Last fetched" (question 6).

## What shipped

The prerequisites, P4 and option 2, in the order of the work breakdown. Each step has a `decisions.log` entry.

**Prerequisites P1–P3** (before the breakdown): safe concurrent writes (#136, PR #137), a board fetch timeout (#142, PR #143), and a failing exit status for `swamp fetch` (#145, PR #146).

**P4: a company's sync survives interruption and overlap.**

1. **Atomic close** (#147, PR #155). `store.ClosePosting` closes a posting, writes its history and ends an early-stage application in one transaction. The close is conditional, so an overlapping sync that got there first changes and counts nothing. The #105 early-status policy stays in `sync` and is passed in, leaving RFC 0003's question of where it lives open.
2. **Atomic create, update and reopen** (#148, PR #156). `store.IngestPosting` and `store.ReopenPosting` compare against the row read inside the transaction, so overlapping syncs no longer double-count `Created`, write duplicate history, or leave a history row for a failed update. Each history row now snapshots the state just before the change it records.
3. **Cancel stops the fetch, never the writes** (#149, PR #157). `SyncCompany` switches to `context.WithoutCancel` once the fetch returns. Preventive: no caller cancels yet.
4. **Per-company sync lease** (#150, PR #158). A lease on the company row (migration 00013), held from before the fetch to after the last write, works across processes (TUI, `swamp fetch`, a future launchd run). `sync.ErrSyncInProgress` is reported as a skip, not a failure. This closes the stale-fetch race that could end a reopened posting's application.

**Option 2: `R` syncs every company in the background.**

5. **`Result.Name` and `sync.Summarize`** (#151, PR #159). Callers stop carrying company names alongside results, the TUI can reuse the CLI's summary line, and "1 companies" is fixed.
6. **`r` as a request to `App`** (#152, PR #160). A refactor so `App` can refuse a refresh during a run.
7. **`R` sync-all** (#153, PR #161). A chain of `tea.Cmd`s with the run's state in `App` (`tui/sync_all.go`): progress per company, the shared summary at the end, one reload when it finishes. During a run, `r` and filter saves are refused. Found while verifying: quitting mid-run left the in-flight company's lease held until expiry, so `Syncer.ReleaseHeldLeases` now frees held leases on exit.

Open questions 6, 7 and 8 were answered by #153 (see below). Not done: option 1 (launchd), and options 3–7, which stay deferred per the recommendation.

## Open questions

1. **Schedule:** fixed times (e.g. 08:00, 12:00, 17:00, suited to reading results over coffee) or a fixed interval (every 4 h)?
2. **History of runs:** is a status line in the TUI and a log file enough, or do we want a persistent history so the TUI can say "last full sync 2h ago, 1 failed"? If we do, should it be a sync-specific `sync_runs` table, or a general log of background job executions that sync is the first user of? The general version is the foundation option 7's job queue would build on (its `sync_jobs` table already carries status, attempts and error), so it's worth designing the two together.
3. **New postings:** should a scheduled sync highlight postings created since the user last looked (e.g. a "new" marker on the posting list)? That's a separate feature, but it's what makes scheduled sync actually useful.
4. **Where the binary lives:** should the launchd job run `bin/swamp` (picks up new builds, can break with a bad one) or an installed copy (stable, needs a reinstall step)?
5. **Configuration:** the schedule and option 3's stale threshold are new settings. Add them as `Config` fields now and let #139 move them into the config file later, or wait for #139?
6. **TUI picking up background changes:** Screens reload their data when entered. Is that fresh enough after a background sync, or should the company list reload itself when a sync finishes (only possible for option 2)? Option 2's design reloads once at the end of a run. Should "Last fetched" also update live, at one extra query per company? *Answered by #153: reload once at the end of a run; no live "Last fetched".*
7. **Option 2's stop key:** Overload Esc on the company list (the first Esc stops the run, a second goes back), or use a dedicated key (e.g. `R` again) so Esc always means "back"? The design above leans towards a dedicated key. *Answered by #153: `R` again; Esc keeps meaning "back".*
8. **Option 2 and filter saves:** While a run is active, should saving a filter selection be refused (simplest), or queued until the run finishes? *Answered by #153: refused during a run.*

## Out of scope

- Syncing companies concurrently. 12 s sequentially doesn't justify it, and the 2026-08-11 decision stands.
- Notifications outside the terminal (macOS notifications, email).
- Syncing across machines or to a remote database.
