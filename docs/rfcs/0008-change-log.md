# RFC 0008: A change log in the database, read by each process

- **Status:** Implemented (#273, #267–#270, #259). Pruning deferred (#272).
- **Date:** 2026-10-06
- **Related:** RFC 0007 (keeping the TUI, the MCP server and the files in step), #243, #246, #258

## Summary

Swamp's processes (the TUI, possibly in several windows; `mcp-serve`, through which an agent drafts; `swamp fetch`) share one SQLite database and nothing else. When one of them changes something, the others don't find out. The TUI goes on showing an application as "started" after the agent moved it on, or a cover letter as "flagged" after the agent revised it.

SQLite can tell a process *that* the database changed, but not *what* changed or *who* changed it. Without the what, the TUI can only reload everything. Without the who, it can't tell its own saves from the agent's, so it can't say "updated by the agent" truthfully.

**The idea:** keep a log of changes inside the database. Whenever a row we care about is inserted, updated or deleted, the database itself adds an event saying which row, what it was before, what it is now, and which process did it. The event is written in the same transaction as the change, so the log can never disagree with the data. Each process remembers the last event it has seen and, twice a second, reads anything newer.

For example, the agent revising a cover letter and you moving an application to "interviewing" would add:

| id | table | row | change | before | after | by |
|---|---|---|---|---|---|---|
| 812 | `document_writes` | 31 | insert | | `{"application_id":12, "document_type":"cover_letter", "source":"write_document"}` | `mcp:3981` |
| 813 | `applications` | 12 | update | `{"status":"application_started"}` | `{"status":"interviewing"}` | `tui:4120` |

The TUI window with process ID 4120 ignores event 813 (its own) and reloads application 12's documents because of 812, saying "updated by the agent".

## How it works

### 1. The database records each change

A trigger on each logged table writes the event. Doing it in the database, not in Swamp's Go code, means no write path can forget, including writes from outside Swamp, such as the `sqlite3` command-line tool. A table is logged only when something reads its changes and acts on them; the appendix lists which.

Only the columns that matter are logged, and an update that changes none of them adds nothing. This matters: every sync touches about 2000 postings just to record that they're still listed (`last_seen_at`). Logging that would bury the real changes, which run at tens to a few hundred a day. Large values, such as a posting's raw payload (13.7 KB on average), are never copied into an event.

```sql
CREATE TABLE change_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,  -- the order of events; never reused
    table_name TEXT    NOT NULL,
    row_id     INTEGER NOT NULL,                   -- the changed row's ID (posting_id for tables keyed by it)
    op         TEXT    NOT NULL,                   -- insert, update or delete
    old        TEXT,                               -- the logged columns before, as JSON; NULL for an insert
    new        TEXT,                               -- after; NULL for a delete
    origin     TEXT,                               -- which process: "tui:4120", "mcp:3981", "fetch:5512"; NULL outside Swamp
    at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_change_events_table ON change_events(table_name, id);
```

SQLite lets only one process write at a time, so event IDs follow the order changes were committed: a process that has read up to event 812 has seen every change committed before 813.

### 2. Each event says which process made it

The obvious way, a trigger asking Swamp's code "which process is this?", breaks every writer that isn't Swamp: the `sqlite3` tool, a script or an older binary would get an error on every write, because the function the trigger calls doesn't exist for them. SQLite checks that when it prepares the statement, so no condition in the trigger can avoid it.

So it's done in two layers:

- **The database's triggers** log every change with no origin. They use only plain SQL, so any writer works.
- **Each Swamp connection** adds its own private trigger (a SQLite `TEMP` trigger, which exists only on that connection) that fills in the origin, `kind:pid`, on events its own writes create.

A change from outside Swamp is still logged, as "outside Swamp", and never blocked.

For this to work, the log's table must already exist when a Swamp connection opens, because the private trigger refers to it. So migrations are an explicit step, `swamp migrate`, and every other command refuses to run against a database that's behind or ahead of it (#273).

### 3. Each process reads what's new

Each process starts from the newest event when it starts; it doesn't need history from before it was running. Every 500 ms it reads the events past the last one it saw (`store.ChangeFeed`): one query on the primary key, a few microseconds when there are none.

This doesn't need to be faster, or pushed: the log is kept, so a process that looks late still gets everything it missed. Watching the database's files would notice changes about 100 ms sooner, at the cost of a dependency and a file-watching backend on the macOS host that's never been tested.

### 4. What the TUI does with the events

- Ignores its own (the origin is its own `kind:pid`).
- Reloads only the screens showing the changed data: the home list for applications and documents, posting screens for postings, the company list for companies, application detail for that application.
- Reloads quietly, with no status line: one pushed the table down on every change. (The events say who made each change, if an unobtrusive indicator is wanted later.)
- Follows RFC 0007's rules for a background reload: the cursor stays on the same item, filters and scroll stay, a form in progress isn't rebuilt, and if what you're looking at was deleted, it goes back a screen and says so.

### 5. The agent: internal for now, pull when exposed

The change log isn't exposed through `mcp-serve` yet: nothing on the agent's side would act on it. When something does, it should be **pulled**, not pushed.

- **MCP can push.** Streamable HTTP keeps a stream open to the client, and the server can send resource-update and log notifications.
- **Agents can't act on a push.** An agent works in turns. A notification arriving between tool calls has nothing in the conversation to respond to it, and clients don't surface it to the model mid-turn.
- **What the agent needs is context when it acts.** It may be working from an old conversation. The tool results it uses to act (`stage_prepare`, `read_document`), or a `changes_since` tool, would return that application's events since its last look: "flagged by you at 14:05: 'tighten the intro'", "status moved to interviewing", "posting closed". The log's durability makes that reliable.

Wave A's checks (`ExpectedSHA256`, refusing deleted applications) already stop an out-of-date agent from writing over newer work. This would only tell it why.

## What the change log is not for

It doesn't replace RFC 0007's safe writes. A log records what has already happened; preventing a bad write needs a check at the moment of writing, because something can change between learning about a change and acting on it.

| Safe write (RFC 0007) | Why the log can't do it |
|---|---|
| Refusing drafts for deleted applications, never reusing IDs (#244) | The write itself still has to check. The log also depends on IDs not being reused, or old events would point at the wrong row. |
| Atomic document writes (#251) | That's about files, not the database. |
| `write_document`'s `ExpectedSHA256` (#253) | The comparison still has to be made at write time, and edits made outside Swamp leave no event, so the file's own fingerprint is the reliable one. |
| Re-reading before a destructive action (#254) | Still a check just before acting. |
| Refusing a review of a changed document (#255) | It compares file contents. The log only adds who made the change, which document writes already record. |

What it does replace is RFC 0007's `PRAGMA data_version` probe (#257), which said only that something changed; the probe is gone (#270).

## Where it can go wrong

| Risk | What would happen | How it's handled |
|---|---|---|
| A column is added to a table but not to its trigger | Changes to it go unseen | One list of logged tables and columns, and a test that fails when a trigger is missing or doesn't match it (#268) |
| A migration rebuilds a table | Dropping the old table drops its triggers | The same test fails until the migration recreates them |
| A table reuses deleted rows' IDs | Old events point at a different row | Tables that delete rows need `AUTOINCREMENT` first (#246); `company_filters` and `interview_stages` stay unlogged until then |
| The log grows forever | Slowly: tens to hundreds of events a day | Delete events older than 30 days (#272, deferred). Nothing depends on old events. |
| A write from outside Swamp | Logged with no origin | Shown as "outside Swamp" |
| A document edited outside Swamp | No event at all | Reloading a screen on entry (#260), and the review form's diff (#255) |
| Extra cost per write | About 0.3 ms more per logged change on the development VM's shared folder | A sync's `last_seen_at` updates aren't logged, so a fetch costs what it does now; a few hundred logged changes a day add well under a second |

## The work

| Step | Issue | What |
|---|---|---|
| 0 | #273 | `swamp migrate` as an explicit step; processes refuse a mismatched schema |
| 1 | #267 | The log's table and the two layers of triggers, on `applications` only |
| 2 | #268 | One list of logged columns, and the test that triggers match it |
| 3 | #269 | Log postings, companies and document writes, reviews and exports, without sync noise |
| 4 | #270 | Read the log every 500 ms; remove the `data_version` probe. Needs only step 1, so it can come before step 3 |
| 5 | #259 | The TUI reloads the screens an event touches and says who made the change |
| later | #272 | Delete events older than 30 days |

## Open questions

1. **Where to delete old events:** when `swamp fetch` runs, when the TUI starts, or both (#272).

## Out of scope

- Replacing the existing history tables (`application_status_history`, `posting_history`) with events.
- An activity view, or undo.
- Processes on different machines. SQLite's WAL mode needs every process on the same machine; see "Where Swamp runs" below.

## Appendix: evidence

### Where Swamp runs

All of Swamp's processes run on one macOS machine. That's what SQLite's WAL mode needs: the processes coordinate through a shared-memory file and locks, which don't work across machines or virtual-machine boundaries. The development VM sees the repository through a shared folder, so it must never open the live database; it works on copies made on the host.

### Experiments

With Swamp's SQLite driver (`modernc.org/sqlite`), on databases opened the way Swamp opens them; two database handles on one file stand in for two processes.

| Question | Result |
|---|---|
| Can triggers log before/after values as JSON? | Yes, for inserts, updates and deletes; an update that changes no logged column logs nothing |
| Does a rolled-back change leave an event? | No |
| What if a trigger calls a function a writer doesn't have? | **Every write by that writer fails** (`no such function`), even behind a condition that checks for it |
| Can a database trigger read a connection's private table? | No (`cannot reference objects in database temp`) |
| Do per-connection private triggers work alone? | Yes, but other connections' writes go unlogged |
| **Both layers together** | A Swamp connection's change is logged with its origin; an outsider's is logged without one; neither write is blocked |
| Can a private trigger be created before the log's table exists? | No (`no such table`), hence `swamp migrate` (#273) |
| Does SQLite's connection hook reach every connection a process opens? | Yes |
| How long does "what's the newest event?" take? | About 3 µs, with 100,000 events |
| How long does reading 100 new events take? | About 75 µs |

### Volume (from the real database)

| | |
|---|---|
| Tables | 14 |
| Postings | 2962; raw payload 13.7 KB on average |
| Posting changes, closes and reopens | 2023 over 22 days: about 92 a day, 387 on the busiest |
| Postings whose `last_seen_at` the latest sync set | 2141 |
| Applications, status changes, reviews | 39, 62, 30 |

### Throughput

SQLite allows one writer at a time. That isn't a limit here. On the development VM, single-row commits ran at 4,500–4,800 a second on the shared folder (5,000–10,500 on local disk), and about 1,900 a second when each also logged an event. A sync commits one posting at a time, about 2000 commits per fetch, so another process waits at most one posting's commit, never a whole fetch. The host's disk will give different numbers, but not different conclusions.

### What's logged

A table is logged only when something reads its changes and acts on them. `db/migrations/changelog.go` is the list, and tests check the triggers against it.

| Table | Logged | Not logged |
|---|---|---|
| `applications` | `status`, `notes`, `deleted_at` | `posting_id`, timestamps |
| `postings` | `company_id`, `listing_status`, `title`, `department`, `team`, `location`, `workplace_type`, `employment_type`, `published_at`, `updated_at` | `raw_payload`, `description_html`, `description_text`, URLs, `last_seen_at` |
| `companies` | `name`, `description`, `deleted_at` | source, `last_fetched_at`, sync lease, timestamps |
| `document_writes` | inserts: application, document type, source, hash | time |
| `document_reviews` | inserts: application, document type, outcome | snapshot, hash, notes, cycle |
| `document_exports` | inserts: application, document type, hash | path, time |

A posting's `updated_at` changes only with its content, never when a sync just sees it again, so logging it marks a description change without copying the description.

Not logged:

- `posting_markup`, `tags`, `posting_tags`, application forms: no reader acts on their changes yet.
- `application_status_history`, `posting_history`: they duplicate the events of the tables they record.
- `company_filters`, `interview_stages`: their rows are deleted and their IDs reused (until `AUTOINCREMENT`, #246), so an old event would name a different row.
