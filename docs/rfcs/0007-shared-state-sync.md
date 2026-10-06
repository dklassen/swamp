# RFC 0007: Keeping the TUI, the MCP server and the files in step

- **Status:** Safe writes (wave A) and steps 6–8 implemented. Seeing other processes' changes is specified by RFC 0008.
- **Date:** 2026-10-05
- **Related:** #243, #244; RFC 0001 (several processes writing one database), RFC 0004 and 0005 (a review counts only while it matches the file), RFC 0008 (the change log)

## Summary

Swamp isn't one program. While you review drafts in the TUI, an agent is drafting through `mcp-serve`, and `swamp fetch` may be syncing postings on a schedule. These are separate processes. They share one SQLite database and the document files under `assets/`, and nothing else: they can't talk to each other.

RFC 0001 made it safe for them to write the database at the same time. That isn't enough, because each process works from a copy of what it last read, and that copy goes out of date without it knowing. Two things follow:

1. **Work gets lost or misplaced.** A process acting on an out-of-date copy can overwrite your edit, attach a draft to the wrong application, or read a half-written file.
2. **The TUI shows the past.** It reloads only after its own actions, so it never shows what the agent or a sync just did.

This RFC fixes the first problem directly, with a set of small checks at the moment of writing. The second needs a way for one process to learn what another changed; that's RFC 0008's change log.

## What goes wrong

Three situations that happened, or could:

- **Your edit disappears.** The agent reads your cover letter, you then improve it in your editor, and the agent saves its revision of the version it read. Your edit is gone, and nothing says so.
- **A draft lands on the wrong job.** You delete an application while the agent is still working on it. The agent saves the draft anyway, which recreates the application's folder. Later you start an application for a different posting; SQLite gives it the same ID, and it "already has" the old draft (#244).
- **The screen lies.** The agent revises a cover letter you'd flagged. The TUI still shows it as flagged, because it never looked again (#243).

### Who writes what

| Process | Writes to the database | Writes files under `assets/` |
|---|---|---|
| TUI | companies, filters, posting markup (interested, archived, notes), applications (start, status, notes, delete), reviews, exports, application forms, company syncs | your edits in `$EDITOR`; exported PDFs |
| `mcp-serve` (the agent) | starts applications (`stage_prepare`), records document writes, adds companies | the drafts themselves (`write_document`) |
| `swamp fetch` | postings: new, changed, closed, reopened; moves applications to and from `posting_closed` | none |
| You, outside Swamp | none | any edit to a document |

The agent is the hard case: `mcp-serve` reads fresh data on every call, but the agent keeps the answers in its conversation and acts on them much later.

## Safe writes (wave A)

Each hazard has a check made at the moment of writing. A check has to happen then: knowing something changed a second ago doesn't help if it changes again before you write.

| Hazard | What could happen | What Swamp does now |
|---|---|---|
| **H1** A draft for a deleted application | It's inherited by a later, unrelated application | `write_document` and `read_document` refuse an application that doesn't exist or was deleted, instead of recreating its folder. Application IDs are never reused (`AUTOINCREMENT`), and deleted applications are kept as tombstones, so an old ID can't name a new application (#244). |
| **H2** The agent overwrites an edit it never saw | Your work is lost silently | `stage_prepare` and `read_document` give the agent a fingerprint (`SHA256`) of each document. `write_document` takes it back as `ExpectedSHA256` and refuses if the file has changed since, telling the agent to read it again. The skill always sends it (#253). |
| **H3** Reading a half-written document | An export of half a cover letter; a review that looks out of date | Documents are written to a temporary file and renamed into place, so a reader sees the old version or the new one, never a mix (#251). |
| **H4** You and the agent edit the same file at once | Whoever saves last wins | Swamp can't see inside your editor. Most editors warn when the file changed on disk; H2 stops the agent overwriting your edit; and the TUI reloads when the editor closes (#256). |
| **H5** Acting on a stale screen | Saving a status form undoes a change sync made; deleting something that changed | The status form and the delete confirmation re-read the application when they open, and say so if it was deleted meanwhile (#254). |
| **H7** Reviewing a document that changes while you read it | The review is saved against a version that no longer exists, so it doesn't count, and the agent never sees your notes | Saving checks the file first. If it changed, nothing is saved: the form keeps your notes, shows what changed (a diff) and who changed it, and your next save reviews the current version (#255). |

## Seeing other processes' changes (wave B)

The TUI needs to know when another process has changed something, and ideally what and who, so it can update the right screen and say why it moved.

This is harder than it sounds, because SQLite doesn't tell one process about another's changes. The options considered:

| Approach | Why it was or wasn't chosen |
|---|---|
| A refresh key | You have to know you need it, which is exactly what failed in #243 |
| Reload on a timer | Redoes all the work (queries, rereading and hashing documents) whether or not anything changed |
| Ask SQLite "has anything changed?" (`PRAGMA data_version`) | Cheap, and sees every process. But it only says *that* something changed: not what, not who, and it can't tell the TUI's own saves from anyone else's. Built as #257; being replaced |
| SQLite's change callbacks | Only report changes made by the same process, so they can't see the agent or a sync |
| Watch the database's files for writes | Faster to notice, but a dependency with platform quirks (untested on the macOS host), and it still only says *that* something changed |
| Have each process announce its changes | Any writer that doesn't (a script, an older binary, a crash at the wrong moment) is silently missed |
| Run everything in one process, or a daemon | The agent's server would be down whenever the TUI is closed, or every command would change how it runs |
| **Record every change in the database (RFC 0008)** | **Chosen.** Each change is written, in the same transaction, as an event saying what changed, from what to what, and which process did it. Every process can read the events since it last looked. |

With a record of changes in the database, a process that looks late still sees everything it missed, so checking twice a second is enough; nothing needs to watch files. Each event names the process that made it, so the TUI can ignore its own.

The rest of wave B is in place:

- **After `$EDITOR`:** closing the editor reloads the application's reviews (an edit can make a review out of date) and shows the editor's error if it failed (#256).
- **Document writes in the database:** a file write is invisible to the database, so each write Swamp makes is also recorded as a row, with who made it: the agent (`write_document`) or you (`$EDITOR`). RFC 0008 turns these into change events like any other (#258).
- **Reloading screens** when events arrive is #259, and reloading a screen whenever you enter it is #260. The second also covers document edits made outside Swamp, which nothing records.

**How a background reload must behave**, whatever triggers it:

- the cursor stays on the same item (by ID, not by position);
- filters, the company `/` search and scroll position stay as they were;
- a form you're filling in is never rebuilt under you; the screens beneath it reload when you close it;
- if the item you're looking at was deleted, go back a screen and say so;
- the status line says who made the change.

## Work breakdown

Effort: **S** is a few hours to a day, **M** a few days. Labels `rfc-0007` and `rfc0007-step-N`.

| Step | Issue | State |
|---|---|---|
| 1. Application existence and `AUTOINCREMENT` | #244 | Done |
| 2. Atomic document writes | #251 | Done |
| 3. `ExpectedSHA256` on `write_document` | #253 | Done |
| 4. Re-read before destructive TUI actions | #254 | Done |
| 5. A review of a changed document reloads with the diff | #255 | Done |
| 6. Reload after `$EDITOR` | #256 | Done |
| 7. `PRAGMA data_version` probe | #257 | Done; replaced by RFC 0008 and removed in #270 |
| 8. Document writes recorded in the database | #258 | Done |
| 9. Reload the screens a change touches | #259 | RFC 0008, step 5 |
| 10. Reload a screen on entry | #260 | To do (**S**) |

## Open questions

1. **Should the status form refuse to save over a status that changed while it was open?** It re-reads when it opens; a check at save time would close the remaining few seconds.

## Out of scope

- **Several people sharing one database.** Swamp is single-user; this is about one person's several processes.
- **Locking documents while they're open.** Neither editors nor agents would honour a lock.

## Evidence

All with Swamp's SQLite driver (`modernc.org/sqlite`), databases opened the way Swamp opens them (`store.Open`: WAL, immediate transactions). Two database handles on one file stand in for two processes.

**Application IDs were reused.** Insert applications for postings 10 and 11 (IDs 1 and 2), delete ID 2, insert one for posting 12: before `AUTOINCREMENT`, it got ID 2.

**`PRAGMA data_version`**, read on one dedicated connection:

| Event | Changes? |
|---|---|
| Another process commits | Yes |
| Another connection in the same process commits | Yes, so the TUI's own saves count as changes |
| A read | No |
| An automatic (`PASSIVE`) checkpoint | No |
| A `TRUNCATE` checkpoint (which resets the WAL) | Yes, though no data changed |
| 1000 checks | about 1.3 ms in total |

**SQLite's commit callback** on one connection fired 0 times for another handle's commit and once for its own.

**Watching the files:** a commit produced two write events on the `-wal` file, about 140 ms after the commit with a 100 ms debounce, on Linux. Not tested on macOS, where the file watcher works differently (kqueue).
