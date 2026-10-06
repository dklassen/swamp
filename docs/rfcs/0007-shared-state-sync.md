# RFC 0007: Keeping the TUI, the MCP server and the files in step

- **Status:** Draft
- **Date:** 2026-10-05
- **Related:**
  - #243 (the TUI goes stale when the MCP server changes data), #244 (`write_document` and application ID reuse)
  - RFC 0001 (P1: `store.Open` makes SQLite safe for several writers; scheduled `swamp fetch`)
  - RFC 0004 and 0005 (`documents.Current`: a review counts only while its hash matches the file)
  - `decisions.log`, 2026-09-04, "Stale reviews must not be surfaced as current"

## Summary

Using the TUI and the MCP server at the same time is a normal way to work, not an edge case. An agent drafts through `mcp-serve` while you review in the TUI, and `swamp fetch` may run on a schedule (RFC 0001). RFC 0001 made it safe for these processes to **write** the database at the same time. Nothing yet makes them **see** each other's changes, or stops one from acting on an old copy of what the other just changed.

That leads to two problems:

1. **Stale display.** The TUI loads its data and then reloads only after its own changes. A draft written through MCP still shows as flagged (#243). A new application from `stage_prepare` doesn't appear on the home list.
2. **Unsafe writes.** A writer acting on an old copy can destroy or misplace work:
   - `write_document` overwrites a draft you edited after the agent read it;
   - a draft written to a deleted application's ID ends up on a later application (#244);
   - a reader can see a half-written document.

**Recommendation:**

- **Make writes safe first (wave A).** Check that the application exists, write documents atomically, add an expected-version check to `write_document`, and stop application IDs being reused.
- **Then add one cheap change signal (wave B).** The TUI watches the database's files and, when SQLite's `PRAGMA data_version` confirms a commit, reloads the current screen (a tick at first; changed to a file watch in #259, see "Change detection"). Document writes record a row in the database, so that one signal covers files too.

No daemon, no change-feed table. The one file watcher (since #259) watches the database's directory and only wakes the `data_version` check.

## Problem

### Who writes what

| Writer | Database | Files under `assets/` |
|---|---|---|
| TUI | companies, filters, postings markup (interested, archived, notes), applications (create, status, notes, delete), reviews, exports, hand-entered forms, company sync (`r`, sync all) | `$EDITOR` on a document; export PDFs; `RemoveDir` on delete |
| `mcp-serve` | `stage_prepare`: creates an application, caches a fetched form. `add_company`: a company plus its first sync | `stage_prepare`: `EnsureDir`. `write_document`: the document itself |
| `swamp fetch` (by hand, or launchd per RFC 0001) | postings upsert, close and reopen; applications moved to and from `posting_closed`; `last_fetched_at`; sync leases | none |
| You, outside Swamp | none | any edit to a document |

### Who reads, and when

- **MCP server:** reads fresh on every tool call, so it never holds old data itself. But the **agent** driving it keeps tool results in its conversation and acts on them later, possibly much later. That copy goes stale the same way the TUI's does, and nothing checks it.
- **TUI:** loads in `Init` (`loadCompanies`, `loadActiveApplications`). After that it reloads only when it changes something itself: status, delete, review submitted. Partial exceptions:
  - `u` in application detail reloads that application's reviews, but not its document progress;
  - posting detail reloads reviews when you enter it.

  The home list has no refresh key. Closing `$EDITOR` doesn't reload anything: `editorClosedMsg` (`tui/editor.go`) isn't handled in `App.Update`, so editing a draft from the TUI itself leaves its review badge and "Next" step stale until something else triggers a reload.

### Hazards, by severity

| # | Hazard | Today | Severity |
|---|---|---|---|
| H1 | Draft written to a deleted application's ID is inherited by the next application created (#244) | `write_document` never checks that the application exists; `applications.id` has no `AUTOINCREMENT`, so the highest deleted ID is reused (reproduced) | **Misplaces work**: a cover letter for one company shows up on another's application |
| H2 | `write_document` overwrites changes made after the agent read the document | No version check. The skill's "ask before overwriting" relies on a `stage_prepare` result that may be old | **Loses work** silently |
| H3 | A reader sees a half-written document | `os.WriteFile` empties the file, then writes. Export, review and `documents.Current` can read in between | Wrong PDF exported, or a spurious "changed" result |
| H4 | `$EDITOR` and `write_document` on the same file | Whoever saves last wins. Most editors warn about outside changes on save; Swamp doesn't | Loses work. Mostly outside Swamp's control |
| H5 | Destructive TUI actions on a stale screen | The delete confirmation shows the application as loaded ("no drafts") while drafts exist. The status form shows a status that sync may have changed since | Deletes drafts unknowingly; reverses an automatic `posting_closed` |
| H6 | Stale display (#243) | Described above | Confusing. Leads to H5 |
| H7 | A review saved after the file changed underneath the form | Records the content the form opened with (`readDocumentForReview`), so `documents.Current` drops it as stale. Nothing is misrecorded | The review silently doesn't count, and you aren't told |

H7 is already correct in what it stores. It's listed because it needs only a warning.

## Measured (2026-10-05)

**`PRAGMA data_version` sees every writer.** I tested it on a scratch database opened with `store.Open` (WAL, `_txlock=immediate`), reading the pragma on one pinned `*sql.Conn`:

| Event | `data_version` |
|---|---|
| Start, then read again with no change | 2, 2 |
| A **separate process** inserts a row | 3 |
| Another connection in the **same** `*sql.DB` pool inserts | 4 |
| 1000 reads | about 1.3 ms in total |

- **What it means:** "did anything change in the database since I last looked" costs about a microsecond and catches the TUI's own background work, `mcp-serve` and `swamp fetch` alike.
- **Caveat:** the value is per connection. It must be read on a dedicated `*sql.Conn`, not through the pool, or each read compares against a different connection.

**ID reuse is real.** On the same kind of scratch database:
- insert applications for postings 10 and 11 (IDs 1 and 2);
- delete ID 2;
- insert one for posting 12: it gets **ID 2**.

## Options

### For stale display (H6)

1. **Refresh keys everywhere.** One key that reruns the current screen's loader, plus a full reload on the home list. Cheap, but you have to know you need it, which is exactly what failed in #243.
2. **Reload on navigation.** Entering a screen reruns its loader. Covers most flows (you open an application to look at it), but not a screen you're already sitting on.
3. **Reload on a timer.** Rerun the current screen's loader every N seconds. Catches everything, but rereads and rehashes documents and reruns per-application queries even when nothing changed.
4. **Reload when something changed.** A short tick (1–2 s) reads `data_version` on a dedicated connection and reruns the current screen's loader only when it moved. Catches every database writer for about a microsecond per tick.
   - **Gap:** it doesn't see files. It needs the next item, or a file check.
5. **Watch the files.** fsnotify on `assets/`. It would catch edits made outside Swamp too, but it's per-platform, has its own failure modes (macOS FSEvents batching, editors that save by renaming), and adds a dependency. Not needed if 4 covers Swamp's own writes.
6. **One process owns the data.** Run `mcp-serve` inside the TUI process, or put a daemon in front of the database, and push change events in-process.
   - The in-TUI version leaves MCP down whenever the TUI is closed, and still doesn't see `swamp fetch`.
   - A daemon changes how every command runs.
   - Too much for a single-user tool.

**Chosen: 4, with document writes made visible to it.** `write_document` records a row in the database in the same call (see wave B), so the signal covers documents written by Swamp. Combine it with 2: entering a screen reloads it anyway, which also covers edits made outside Swamp the next time you look. Finally, handle `editorClosedMsg`, which needs no signal at all.

*Revised 2026-10-06 (#259): the trigger is now a push from the database's files rather than a tick. The detection is still `data_version`. See the next section.*

### Change detection: where each strategy works and where it falls apart (2026-10-06, #259)

While building #259, the user asked for push rather than polling, then whether SQLite's own change callbacks would beat fsnotify, then what fsnotify reacts to. This section records what was tested and where each approach breaks.

#### Measured

All on Linux, `modernc.org/sqlite`, databases opened with `store.Open` (WAL), two `*sql.DB` handles on one file standing in for two processes.

| Experiment | Result |
|---|---|
| Commit through handle B; `data_version` on A's pinned connection | Moves (#257's tests) |
| Commit through A's own pool, on a connection other than the probe's | Moves (#257) |
| Read through B | Doesn't move; no file events at all |
| `PRAGMA wal_checkpoint(PASSIVE)` through B, with frames to copy | Doesn't move, but writes the database file (a file event) |
| `PRAGMA wal_checkpoint(TRUNCATE)` through B | **Moves.** Resetting the WAL changes the shared index header, so readers see a new version |
| fsnotify on the database's directory, commit through B | Signal about 140 ms later (100 ms debounce), 10 of 10 runs |
| `sqlite3_commit_hook` on A's connection, commit through B | **0 calls.** A commit on A's own connection: 1 call |
| `sqlite.RegisterConnectionHook` adding a commit hook to each new connection, three commits on three pooled connections | Every connection got the hook; 3 of 3 counted |

Not tested here: macOS, and Docker bind mounts written from the host. Claims about them below are from the libraries' and kernels' documented behavior, marked *unverified*.

#### The strategies

**A. Poll `data_version` on a tick** (this RFC's original choice 4).

- **Works:** any writer in any process on the same machine, including ones that know nothing about Swamp (the `sqlite3` CLI, a script, an older binary). About a microsecond per check; one query only when something changed. No dependency, same on every platform.
- **Falls apart:**
  - Latency is the interval: about half of it on average.
  - A WAL reset (`TRUNCATE`/`RESTART` checkpoint) reads as a change: one harmless extra reload.
  - The TUI's own commits read as changes (see "Telling the TUI's own writes apart").

**B. fsnotify on the database's directory, confirmed with `data_version`** (user decision for #259).

- **Works:** every writer A sees, with about 100–150 ms latency and no work while nothing happens. Watching the directory, not the files, survives SQLite recreating and truncating `-wal`. The `data_version` confirmation filters out events that aren't commits: checkpoints copying pages (measured) and anything else touching the files.
- **Falls apart:**
  - **Writes the kernel doesn't see as file writes.** A writer on another machine (network filesystem) or across a VM boundary (Docker Desktop bind mounts written from the host, *unverified*) produces no inotify event. In practice WAL mode needs shared memory between readers and writers and doesn't work across those boundaries either, so Swamp can't be used that way anyway.
  - **Dropped events.** If the kernel's event queue overflows, fsnotify reports an error instead of the events. The watcher surfaces it (`Errors()`), and the TUI shows it, but changes in that gap are missed until the next one arrives.
  - **Platform differences** (*unverified*): on macOS fsnotify uses kqueue, which opens a descriptor per file in the watched directory; Swamp's `db/` directory holds a handful of files, so that's fine, but it's the one backend not exercised here. Windows isn't a target.
  - Same false positives as A: a WAL reset, and the TUI's own commits.
  - A dependency (`github.com/fsnotify/fsnotify`, already in the module graph).

**C. SQLite change callbacks** (`sqlite3_update_hook`, `sqlite3_commit_hook`, the pre-update hook).

- **Works:** exact, synchronous notice of every change made **through the connection that registered it**. With `RegisterConnectionHook`, that's every connection in this process.
- **Falls apart:** they never fire for another process's commits (measured: 0 calls). Since the TUI, `mcp-serve` and `swamp fetch` are separate processes, a callback can't be the change signal. It is the right tool for the opposite question: "did *I* just commit?" (below).

**D. Writers notify readers** (a Unix socket or HTTP ping from `mcp-serve` and `swamp fetch` to running TUIs).

- **Works:** instant, and could say what changed.
- **Falls apart:** every writer has to know about every reader and how to reach it, and any writer that doesn't (a script, a migration, an older binary, a crash between commit and notify) is silently missed. It's the change-feed machinery rejected above, for a single-user tool.

**E. One process owns the data** (option 6 above): rejected for the same reasons.

**F. fsnotify on `assets/`** (option 5 above) remains unnecessary for Swamp's own document writes, since #258 records them in the database. It would only add edits made outside Swamp, with the editor-rename and batching problems noted there.

#### Telling the TUI's own writes apart

A and B both see the TUI's own commits, because they go through pool connections other than the probe's. The reload is harmless: the TUI already reloads after its own changes. The message isn't: about 100 ms after you save, "Updated from another window or the agent" would replace "Application form saved", and it would be false.

| Option | Works | Falls apart |
|---|---|---|
| **Count own commits with a commit hook** on every connection in the process (`RegisterConnectionHook`), and compare the count at each confirmed change | Exact for the common case; verified to reach every pooled connection | When the TUI's commit and another process's land in the same debounce window, `data_version` can't say there were two. The screen still reloads, and only the message is wrong (it's skipped). A hook returning non-zero would roll the commit back, so it must always return 0. The hook is process-wide: harmless in the TUI, which opens one database |
| **Compare before and after:** show the message only if the reload changed what's on screen | Never mislabels | A comparison per screen, written and maintained for every screen. Misses "updated" when another process wrote the same values |
| **Time window:** ignore changes within N ms after the TUI's own action | Simple | Guesswork: too short mislabels, too long hides real changes |
| **Say nothing** | Simplest | The user chose a status line so a row moving under the cursor isn't a surprise |

**Recommended:** count own commits with a commit hook. Always reload on a confirmed change; show the message only when commits other than the TUI's own happened in that window.

#### Recommendation

- **Signal:** B (fsnotify, confirmed with `data_version`), as decided.
- **Own writes:** commit-hook counter, as above.
- **Gaps in B:** when the watcher reports an error (an overflow, or it couldn't start), fall back to A's tick until it recovers, so a missed event can't leave the screen stale indefinitely. Polling is cheap enough that the fallback costs nothing.
- **Not used:** C as a signal (can't see other processes), D and E (machinery), F (covered by #258).

### For unsafe writes (H1–H5)

These don't depend on the display choice. Each is small and worth doing alone:

- **H1: check the application exists, and stop reusing IDs (#244).** `write_document` and `read_document` look up the application first and return a tool error if it's gone. A migration adds `AUTOINCREMENT` to `applications.id`, with the table rebuilt the same way as migration 00004.
- **H2: an expected version on writes.** `stage_prepare` and `read_document` return each document's `SHA256`, empty when the document doesn't exist. `write_document` takes an optional `ExpectedSHA256`. When it's given and doesn't match the file, the write is refused with an error saying the document changed since it was read. An empty value means "expect no document", which stops a fresh draft from overwriting one that appeared in the meantime. The `apply-to-posting` skill always passes it.
  - Optional rather than required, so a client can still write unconditionally when the user asks it to.
- **H3: atomic writes.** Write to a temporary file in the same folder, then `os.Rename` it over the document. It goes in `documents.Store` (a `Write` method), so `stage.WriteDocument` (behind `write_document` since #248) stops calling `os.WriteFile` itself.
- **H4: rely on the editor, then reload.** Swamp can't see inside `$EDITOR`, and after it closes there's no telling whether a change on disk came from the editor or from `write_document`. Most editors already warn when a file changed on disk since it was opened. What Swamp can do is reload when the editor closes (step 6), so what you see afterwards is what's actually on disk, and rely on H2 so an agent never overwrites your edit. A lock file wouldn't help: editors and agents would both ignore it.
- **H5: re-check before acting.** The delete confirmation reloads the application's document status when it opens, and shows what is actually on disk. The status form re-reads the status when it opens. The posting-closed case could also go through an expected-status check, but that's probably more than needed.
- **H7: warn on save.** When a review is saved, compare the form's content hash with the file. If they differ, say so ("the document changed while you were reviewing; this review applies to the version you saw").

## Proposal

**Wave A: safe writes.** H1, H2, H3, H5, H7. No new machinery. A and B are independent, but A comes first because it prevents lost work.

**Wave B: change signal.**

- **Detecting changes:** a `store` method returning the current `data_version` from a dedicated `*sql.Conn`, held open by the TUI for its lifetime.
- **The trigger:** originally a `tea.Tick` every 1–2 s. Since #259, fsnotify on the database's directory, confirmed with the probe, with the tick as a fallback when the watcher fails (see "Change detection").
- **Making documents visible:** `documents.Store.Write` (from H3) also records the write in the database, so writing a document moves `data_version`.
  - The simplest form is touching `applications.updated_at`.
  - A small `document_writes` table (application, type, SHA-256, time) would also give history. It's the cheaper choice if H2's hashes are wanted without rereading files.
  - Either is fine. Decide during the work.
- **Reload on entry and after the editor:** entering a screen reloads it (option 2), and `editorClosedMsg` reloads the application.

**Rules for a background reload,** whatever triggers it:

- keep the cursor **by ID**, not by index;
- keep any filters, including the company `/` search, and any scroll position;
- never rebuild a screen with a form in progress (review notes, application notes, status, company form). Reload the screens beneath it, or mark them stale and reload them when the form closes;
- if the item on screen was deleted underneath you, go back one screen and say so in the status line.

## Work breakdown

Effort: **S** is a few hours to a day, **M** a few days. Each task is its own issue and PR, labelled `rfc-0007` and `rfc0007-step-N`.

**Ship point:** after wave A, no concurrent writer can lose or misplace a draft. After wave B, the TUI shows MCP and sync changes within a couple of seconds.

### Wave A: safe writes (mutually independent)

1. **Application existence and `AUTOINCREMENT` (#244).** *Done:* a test that `write_document` to a deleted application fails and creates no folder; a migration test that a new application never gets a deleted ID. **S.**
2. **Atomic `documents.Store.Write`.** Temp file plus rename; `stage.WriteDocument` uses it (#251). *Done:* a test that the file is never seen empty or partial (a reader running alongside a writer loop). **S.**
3. **`ExpectedSHA256` on `write_document`.** Hashes in `stage_prepare` and `read_document` output; refuse on mismatch; update the `apply-to-posting` skill to pass it. *Done:* table tests for match, mismatch, expect-absent, and omitted. *Deps:* 2. **S.**
4. **Re-check before destructive TUI actions.** Delete confirmation and status form re-read on entry. *Done:* a test where state changes between list load and confirmation, and the confirmation shows the new state. **S.**
5. **Warn on a review of changed content.** *Done:* a test that saving a review after the file changed shows the warning and still records the snapshot. **S.**

### Wave B: change signal

6. **Handle `editorClosedMsg`.** Reload the application's reviews and progress. *Done:* a test that the badge updates after an edit. **S.** Could ship on its own straight away; it's a bug.
7. **`data_version` probe in `store`.** A dedicated connection, and a method that reports whether it moved. *Done:* a test with two `*sql.DB` handles on one file. **S.**
8. **Document writes recorded in the database.** *Done:* `write_document` moves `data_version`. *Deps:* 2, 7. **S.**
9. **Change trigger and per-screen reload.** Rerun the current screen's loader on change, following the reload rules above. *Done:* tests that each screen reloads on a change and keeps cursor, filters and open forms. *Deps:* 7. **M.**
10. **Reload on screen entry.** Where a screen doesn't already. *Done:* a test per screen. **S.**

## Open questions

1. **Tick interval.** 1 s or 2 s? A tick costs about a microsecond and the reload only runs on change, so this is about how fast a change should appear, not cost. Proposed: 1 s. *Superseded (#259): the trigger is a file watch; a tick remains only as the fallback, where 1 s is still proposed.*
2. **Hash storage.** Should H2's hashes come from rereading files, or from a `document_writes` table? Rereading is simpler and can't disagree with disk; a table adds history. Proposed: reread, and add the table only if step 8 wants it anyway.
3. **Edits made outside Swamp.** Is reloading on screen entry enough, or is fsnotify (option 5) worth it later?
4. **Should the TUI show that a reload happened?** For example "updated" in the status line, so a row moving under the cursor isn't a surprise.
5. **Status writes and `posting_closed`.** Should the status form refuse to overwrite a status that sync changed since the form opened (an expected-status check), or is re-reading on entry (step 4) enough?

## Out of scope

- **Several people sharing one database.** Swamp is single-user; this RFC is about one person's several processes.
- **Real-time MCP notifications.** MCP has resource subscriptions, but clients don't act on them in the middle of a turn, and nothing in the skill would use them.
- **Locking documents while they're open.** Neither editors nor agents would honour a lock.
