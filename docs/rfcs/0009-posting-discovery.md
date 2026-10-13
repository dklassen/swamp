# RFC 0009: Discovering postings that fit, through the agent

- **Status:** Draft
- **Date:** 2026-10-11, rewritten 2026-10-13
- **Related:** RFC 0006 (structured posting search, whose `SearchPostings` step 1 builds on; its open question 4 anticipated this); RFC 0008 (the change log, which carries new scores to the TUI); RFC 0002 (the application workflow a good fit feeds)

## Summary

There are now too many companies and postings to read through. Postings that would suit the user, and new ones, get lost because each company's postings are listed separately.

This RFC takes two steps, each useful by itself:

1. **All postings.** A new TUI view lists the open postings of every company in one place, newest first, a page at a time.
2. **Fit scores.** The agent scores postings against `profile.md`, and Swamp records each score as it's given. Every scoring belongs to a **run**, which records when it happened, which model scored, from which client, and against which version of the profile. Scores are only ever added, never changed, so the history stays. The all-postings view shows each posting's latest score and can sort by it.

Swamp never calls a model. Every score is the agent's, made in a conversation the user started.

## Problem

1. **No single list.** Across 44 companies there are about 2,000 open postings (1,972 on 2026-10-01; see RFC 0006), and up to 393 at one company. The TUI only lists postings company by company, so seeing what's new means opening each one.
2. **Finding fits.** Even in one list, 2,000 titles are too many to read. Something has to rank them.
3. **Trusting the ranking.** A score is only as good as what produced it. Later the user will want to know which model gave it, when, and against which profile, so a score from an old profile or a weaker model can be weighed or redone.

What the agent can't do today:
- No tool returns a posting's description (`search_postings` returns summary fields only, #117).
- Nothing records that a posting was assessed, what was concluded, or by what.

## Decisions

| Question | Decision |
|---|---|
| Order | Step 1 ships alone first. Scoring builds on it. |
| Where the list lives | A TUI view. The agent can already page through every company's postings with `search_postings`. |
| Sort | By when Swamp first saw the posting, newest first. That's posting ID order: IDs are `AUTOINCREMENT` and never reused (migration 00024), so the order is stable while paging. The board's published date is unreliable (missing or backdated). |
| Paging | Fixed pages that fill the window, with next and previous page keys and "page 3 of 40". |
| Who scores | The agent, from `read_profile`. Swamp stores the scores and computes none. |
| Score records | Added only, never updated. A newer score replaces an older one in the view, but the older one stays. Each belongs to a run that records when, which model, which client and which profile. |
| Profile changes | Rescoring is an explicit run the user asks for. Editing `profile.md` never queues anything by itself. |

## Step 1: All postings

### The view

A new top-level view. `tab` already switches between the two top-level views (companies and active applications); it becomes a cycle of three: companies, all postings, applications.

```
All postings (1,972)                                           page 1 of 50
  Oct 12  Acme      Senior Backend Engineer   Platform   remote
  Oct 12  Globex    Platform Engineer         Infra      New York
  Oct 11  Initech   Staff Engineer, Data      Data       remote  ★

↑/↓ (j/k): select  ←/→ (h/l): page  enter: detail  i: interested  x: archive  o: open  tab: applications
```

- **Which postings:** open, not archived, and not at a deleted company. This is `search_postings`' default, so the agent and the user see the same set.
- **Columns:** first seen, company, title, department, location (with workplace type), and a mark if interested.
- **`enter`** opens the existing posting detail. **`i`**, **`x`** and **`o`** work as they do in a company's posting list.
- **Live:** postings are a logged table (RFC 0008). A sync in another process adds rows and updates the total without a manual refresh. The page the user is on is kept: new postings appear on page 1, not under the cursor.

### Paging

Each page is one `store.SearchPostings` call with `Order: PostingOrderIDDesc`, `Limit` set to the rows that fit the window, and the previous page's last ID as the cursor. The query already exists (RFC 0006, #223), so step 1 needs no new SQL beyond one column. The TUI keeps a stack of the cursors it has used, so going back a page is one call too. `Total` gives the page count.

Two small additions to the store:
- The page row needs `first_seen_at` for the first column (it has `published_at` today).
- `posting_markup` becomes a logged table (`interested_at`, `archived_at`). This view hides archived postings and marks interested ones, and changes to those from another TUI window should show here. RFC 0008 and `changelog.go` kept it unlogged only because nothing read its changes yet.

## Step 2: Fit scores

### 1. Runs and scores

A **run** is one scoring session, started by the agent:

| Column (`scoring_runs`) | Meaning |
|---|---|
| `id` | `INTEGER PRIMARY KEY AUTOINCREMENT`. |
| `model` | The model ID the agent reports for itself, e.g. `claude-opus-5-5`. Required. Swamp can't check it. |
| `client_name`, `client_version` | The MCP client's name and version (e.g. `claude-code`, `2.4.1`), from the request's client info (`ServerRequest.ClientInfo` in the go-sdk). Swamp records these itself. |
| `profile_sha256` | The hash of `profile.md` the agent scored against (below). |
| `scope` | `unscored` or `all` (section 3). |
| `started_at` | UTC, `CURRENT_TIMESTAMP`. |

A **score** is one posting's score within a run:

| Column (`posting_scores`) | Meaning |
|---|---|
| `id` | `INTEGER PRIMARY KEY AUTOINCREMENT`. |
| `run_id` | The run. |
| `posting_id` | The posting. `UNIQUE (run_id, posting_id)`: once per run. |
| `score` | 0–100. |
| `reason` | One line, shown with the score, e.g. "Go and distributed systems, remote". |
| `content_sha256` | The posting's content hash when it was scored (section 2). |
| `scored_at` | UTC, `CURRENT_TIMESTAMP`. |

Rows are only ever inserted. A posting's **current score** is its newest row (highest `id`). One `record_scores` call is one database transaction: every valid entry in it is committed together, or none is if the write fails.

### 2. Content hash

A score is tied to the content the agent saw. `postings.content_sha256` is a new column: the SHA-256 of the fields the agent reads (title, department, location, workplace type, description text). `IngestPosting` sets it whenever it writes a posting.

It can't be the `posting_history` ID. A `content_updated` row is written whenever any ingested field changes, including the raw payload (`store/posting.go`), and Greenhouse's raw payload has its own `updated_at` and other fields Swamp never shows. A change the agent can't see shouldn't make a posting need scoring again.

SQLite has no built-in SHA-256, so existing rows are backfilled by a Go migration. It would be the first one: goose supports them, but every migration so far is SQL.

### 3. What's pending in a run

A posting is **eligible** when it's open, not archived, and its company isn't deleted, and it isn't marked interested and has no application (the user has already decided about those).

Within a run, an eligible posting is **pending** when it has no score in that run and either:
- the run's scope is `unscored`, and the posting has no score anywhere with its current `content_sha256` (it's new, or its content changed since it was scored); or
- the run's scope is `all` (a rescore after a profile change, or with a different model).

So "what's new since I last checked" needs no cursor: it's whatever has no score on its current content. And an interrupted run picks up where it stopped. An `all` run is continued by its ID. A new `unscored` run finds the same postings anyway.

### 4. MCP tools

| Tool | What it does |
|---|---|
| `read_profile` (changed) | Also returns the content's `SHA256`. |
| `read_posting` (new) | One posting in full: the summary fields, the description as text, the job URL, the company's description, and `ContentSHA256`. |
| `start_scoring_run` (new) | Takes `Model`, `ProfileSHA256` and `Scope`. Refuses if `ProfileSHA256` isn't the hash of `profile.md` right now, so a run can't claim a profile the agent didn't read. Returns the run ID and how many postings are pending. The description says to use `Scope: all` only when the user asks for a rescore. |
| `list_scoring_runs` (new) | Recent runs with their metadata, how many postings each scored, and how many are still pending. For resuming a run. |
| `list_pending_postings` (new) | One page of a run's pending postings: ID, company, title, department, location, workplace type, published date, `ContentSHA256`, and why it's pending (`new`, `changed`, `rescore`). Keyset paged with an opaque cursor, like `search_postings`. Also returns `Total`. |
| `record_scores` (new) | A run ID and a batch of `[{PostingID, Score, Reason, ContentSHA256}]`, stored in one transaction. An entry is refused, and the result says so, if its `ContentSHA256` isn't current (the posting changed since the agent saw it; it stays pending) or the posting already has a score in this run. |

Batching matters. A full scoring involves hundreds of scores, and one call each would cost far more than the scores themselves.

### 5. The TUI

- The all-postings view gets a **score** column: the current score, dimmed if the posting's content changed since it was scored.
- **`s`** switches the sort between first seen and score (highest first, unscored last). This is a new `PostingOrderScoreDesc` for `SearchPostings`, keyset paged on `(score, id)` like `PostingOrderPublishedDesc`.
- The posting detail shows the current score with its reason, model and date, and the earlier scores below it.
- `posting_scores` is a logged table (inserts only). Scores recorded by the agent in the MCP process show up in an open TUI as they're recorded.

### 6. The skill

A new `score-postings` skill, next to `apply-to-posting`, runs the loop:
1. `read_profile`, and keep its `SHA256`.
2. `start_scoring_run` with the agent's model ID and `Scope: unscored`, or `all` if the user asked for a rescore. Say how many are pending.
3. Page through `list_pending_postings`. Score clear misses from the summary fields alone, in batches.
4. For the plausible ones, `read_posting`, then score with a reason.
5. Report how many were scored, and list the best ones.

## Estimates (not measured)

- The summary fields come to roughly 30–40 tokens per posting. Scoring all of about 2,000 postings is roughly 60–80k tokens of listings, across pages. A routine run after a sync, with tens of new postings, is small.
- A description is roughly 500–1,500 tokens. Scoring from the summary first keeps descriptions to a small fraction.

Measure both on a copy of the real database before writing the skill.

## Where it can go wrong

- **A score on content the agent didn't see.** `record_scores`' `ContentSHA256` check prevents it.
- **A misreported model.** `model` is whatever the agent says. `client_name` and `client_version` come from the protocol, not the agent, so at least the client is reliable.
- **Scores from a summary only.** A posting scored from its title alone may be underrated. That's the cost of not reading every description. The reason makes it auditable, and a rescore can fix it.
- **Two agents at once.** Each has its own run, so both may score the same posting. The newer score is current, and both are kept.
- **Scores across runs aren't comparable.** A 70 from one model or profile isn't the same as a 70 from another. The view shows each posting's newest score regardless. Sorting by score across mixed runs is approximate, and the detail shows which run each score came from.

## Options considered

1. **One verdict per posting, replaced in place** (this RFC's first draft: `suggest` or `not_a_fit`, plus a generation counter for rescores). It's simpler, but it keeps no history and nothing about what produced a verdict. Replaced by runs and scores added only.
2. **The agent marks postings interested directly.** It needs no new storage, but the agent's picks can't be told apart from the user's, and unreviewed picks would fill the apply queue. Rejected.
3. **Tags (`tags`/`posting_tags`, in the schema but unused).** A tag carries no score, reason or record of what produced it. Rejected.
4. **Swamp scores with embeddings or a model.** Ranking would be automatic, but Swamp would choose a model, pay for it and handle stale scores. Rejected: the agent already has the profile and the judgment.
5. **Offset paging in the TUI.** Simpler, but a sync adding postings while the user reads shifts every page. The keyset query already exists. Rejected.
6. **Keyword prefilter with FTS5 (RFC 0006, option 3).** It's cheap, but it misses fits the keywords don't anticipate. It could come later as a filter on `list_pending_postings`.

## Work breakdown

### Step 1: all postings
1. Store: `first_seen_at` on the search page row; log `posting_markup` (migration, `changelog.go`, triggers).
2. TUI: the all-postings view, paged and live.

### Step 2a: storage (needs nothing from step 1)
3. Migration: `postings.content_sha256` with a Go backfill; `IngestPosting` sets it.
4. Migration: `scoring_runs` and `posting_scores`, with their `changelog.go` classification (`posting_scores` logged, inserts only; `scoring_runs` not logged, since nothing reads its changes) and triggers.
5. Store: `StartScoringRun`, `ScoringRuns`, `PendingPostings` (paged, with why), `RecordScores`, and the current score on search rows.

### Step 2b: MCP (needs 2a)
6. `read_profile` returns `SHA256`.
7. `read_posting`.
8. `start_scoring_run` and `list_scoring_runs`.
9. `list_pending_postings`.
10. `record_scores`.

### Step 2c: user-facing (needs 1, 2 and 2b)
11. TUI: score column, sort by score, score history in the posting detail.
12. The `score-postings` skill.

Each item is its own issue and PR.

## Open questions

1. **The score scale.** 0–100 is proposed. A coarser scale (1–5) would make scores from different runs more comparable, and give the agent less false precision to invent.
2. **Should the view hide low scores** (a threshold), or only sort by them?
3. **Excluding interested postings and those with applications** keeps runs small, but leaves them without a score. Is that wanted, or should they be scored too, for the record?
4. **A way to set postings aside** (the first draft's "dismiss") without archiving them. Not proposed until the view is in use.

## Out of scope

- Free-text or keyword search (see option 6).
- Notifications, or scoring on a schedule. Scoring runs when the user asks the agent.
- Learning from the user's interested and archive decisions.
