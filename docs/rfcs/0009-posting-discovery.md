# RFC 0009: Discovering postings that fit, through the agent

- **Status:** Accepted (work items #310–#326; #309 dropped)
- **Date:** 2026-10-11, rewritten 2026-10-13
- **Related:** RFC 0006 (structured posting search, whose `SearchPostings` part 2 builds on; its open question 4 anticipated this); RFC 0008 (the change log, which carries new postings and scores to the TUI); RFC 0002 (the application workflow a good fit feeds); #56 (filters gate ingestion, so changing them re-syncs); #61 (one matching path for sync and display)

## Summary

There are now too many companies and postings to read through. Postings that would suit the user, and new ones, get lost because each company's postings are listed separately. Worse, some are never stored at all: a company's filters decide what a sync keeps, so a posting the filters leave out is lost for good.

This RFC takes four parts, each useful by itself:

1. **Store every posting.** Every sync stores every posting on the board. Company filters stay as they are, but they only decide what's shown. A filter that's gone stale then hides postings without losing them, and Swamp can say when one has gone stale.
2. **All postings.** A new TUI view lists the open postings of every company in one place, newest first, a page at a time.
3. **Search.** `/` in that view searches every company's postings by title, company, department and location. The same search is on `search_postings`, so the agent can use it too.
4. **Fit scores.** The agent scores postings against `profile.md`, and Swamp records each score as it's given. Each score records when it was given, which model gave it, from which client, and against which version of the profile and the posting. Scores are only ever added, never changed, so the history stays. The all-postings view shows each posting's latest score and can sort by it.

Swamp never calls a model. Every score is the agent's, made in a conversation the user started.

## Problem

1. **Filters lose postings silently.** A company's filters decide which postings a sync stores (`sync/company.go`, `ingest`), matching exactly but ignoring case (`filter.Match`). If a company renames its "Engineering" department to "Eng", a filter on "engineering" stops matching:
   - Every new engineering posting is skipped.
   - Postings already stored stop getting updates. They aren't closed, because the board still lists them (`sync/reconcile.go`), so they stay open with stale content.
   - The company's list still looks normal, so it looks like the company stopped hiring.
   - It can't be fixed from the TUI. The filter screen offers only values from stored postings (`ListDistinctDepartmentsForCompany`), and "Eng" was never stored. The only way to find out is to read the company's job board.
2. **Postings that come and go while filtered out are never seen.** A posting let in later gets "first seen" set to when the filter widened, and its earlier changes have no history.
3. **Stored postings and shown postings already disagree.** Narrowing a filter doesn't close what's stored. A company's list applies the current filters again when it displays (`filterPostingsByCompanyFilters`), but `SearchPostings`, and so `search_postings`, doesn't. So the agent sees postings the user's views hide.
4. **No single list.** Across 44 companies there are about 2,000 open postings (1,972 on 2026-10-01; see RFC 0006), and up to 393 at one company. The TUI only lists postings company by company, so seeing what's new means opening each one.
5. **Finding fits.** Even in one list, 2,000 titles are too many to read. The user needs to look things up ("platform", "Dublin") across every company, and something has to rank the rest. Today the only search is the company list's `/`, over company names. And whatever ranks them can't consider a posting a filter kept out, such as a platform role filed under "Infrastructure" when the filter is "Engineering".
6. **Trusting the ranking.** A score is only as good as what produced it. Later the user will want to know which model gave it, when, and against which profile, so a score from an old profile or a weaker model can be weighed or redone.

What the agent can't do today:
- No tool returns a posting's description (`search_postings` returns summary fields only, #117).
- Nothing records that a posting was assessed, what was concluded, or by what.

## Decisions

| Question | Decision |
|---|---|
| Order | Part 1 (store every posting), part 2 (all postings), part 3 (search), part 4 (scores). Each ships alone. |
| Search | A substring match in SQL over the summary fields, in `SearchPostings`, so the TUI and `search_postings` share it. No full-text index until plain matching proves too limited. |
| What a sync stores | Every posting the board lists. Filters play no part in syncing. |
| What filters do | Decide which of a company's postings are shown, in the TUI and to the agent. The rules stay the same: values within a field are alternatives, and every field must match one, ignoring case. |
| Changing filters | Saves them. No re-sync: there's nothing new to fetch. |
| Where the list lives | A TUI view. The agent can already page through every company's postings with `search_postings`. |
| Sort | By when Swamp first saw the posting, newest first. That's posting ID order: IDs are `AUTOINCREMENT` and never reused (migration 00024), so the order is stable while paging. The board's published date is unreliable (missing or backdated). |
| Paging | Fixed pages that fill the window, with next and previous page keys and "page 3 of 40". |
| Who scores | The agent, from `read_profile`. Swamp stores the scores and computes none. |
| Score records | Added only, never updated. A newer score replaces an older one in the view, but the older one stays. Each records when, which model, which client, which profile and which posting content. |
| Profile changes | Rescoring is an explicit request the user makes. Editing `profile.md` never queues anything by itself. |

## Part 1: Store every posting

### 1. Syncing

`SyncCompany` stops reading the company's filters. `ingest` stores every fetched posting. `closeMissing` already compares against the whole fetch, so closing doesn't change.

`ApplyCompanyFilters` becomes a store write only. The re-sync it does today (#56) existed only because filters controlled what was stored.

### 2. Showing

Filters are applied wherever postings are listed:
- **A company's posting list:** as today (`filterPostingsByCompanyFilters`). It now works on everything stored, so widening a filter takes effect immediately.
- **The filter screen:** offers every value the stored postings have, which is now everything on the board, with a count of open postings for each value.
- **`SearchPostings`** (and so `search_postings` and part 2's view) applies each company's filters by default. A new `IgnoreCompanyFilters` option turns this off. The matching is in SQL: for each field, either the company has no filter on that field, or one of its values equals the posting's, ignoring case (`EXISTS` against `company_filters`).

The rules then live in two places: `filter.Match` in Go, and the SQL. One test runs both against the same table of cases, so they can't drift apart (#61).

### 3. Stale filters

A filter value that matches none of a company's open postings is almost certainly stale. Every posting is now stored, so this can be computed from the database at any time, with no work during a sync:
- The filter screen marks such a value: `☑ engineering (0 open)`.
- The company list marks a company with a stale filter value, so a rename doesn't go unnoticed until you happen to open the filter screen.

### 4. The first sync after deploying

The first sync of each company stores every posting its filters used to skip, all at once:
- **"First seen" is the time of that sync.** On part 2's view, they appear as new. That's a one-time burst. It can't be told apart from postings that really are new, since Swamp has no earlier record of them.
- **Each one adds a `change_events` row** (RFC 0008), and the TUI picks them up in its twice-a-second check, in batches rather than once per posting. How the burst feels in an open TUI should be checked when item 2 is built.
- **Postings that froze after a rename** get their content updated, with a `content_updated` history row.

### 5. What it costs

Every posting is stored whatever this costs: part 1 is a decision, not conditional on measuring first (#309, closed). These are estimates, to watch for once it ships.

- **Storage:** the raw payload alone averages 13.7 KB per posting (measured over 2,962 postings for RFC 0008), about 40 MB today. Each description is stored three times: in `description_html`, in `description_text`, and inside `raw_payload`. Every `content_updated` history row also snapshots the whole posting. So a posting is more like 20 KB or more, plus history: 10,000 postings would be a few hundred MB. That's no problem for disk space. 10,000 is a guess.
- **Scan speed:** `listing_status`, `first_seen_at` and `last_seen_at` come after `raw_payload` in `postings`. Reading them means following each large row's overflow pages, so a scan that reads them can read most of the table's bytes. The index on `listing_status` may avoid this, but that hasn't been measured. If paging or search (part 3) is slow at the real size, the fix is to move `description_html`, `description_text` and `raw_payload` to their own table, joined only when the full posting is read. Watch for it once part 1 ships.
- **Sync time:** one ingest transaction per fetched posting instead of per matching posting. Unchanged postings are compared and skipped, so this grows with board size, not with how much changes.
- **Scoring (part 4):** a full scoring grows with the number of postings (see Estimates).

## Part 2: All postings

### The view

A new top-level view. `tab` already switches between the two top-level views (companies and active applications); it becomes a cycle of three: companies, all postings, applications.

```
All postings (1,972)                                           page 1 of 50
  Oct 12  Acme      Senior Backend Engineer   Platform   remote
  Oct 12  Globex    Platform Engineer         Infra      New York
  Oct 11  Initech   Staff Engineer, Data      Data       remote  ★

↑/↓ (j/k): select  ←/→ (h/l): page  enter: detail  i: interested  x: archive  o: open  tab: applications
```

- **Which postings:** open, not archived, not at a deleted company, and matching that company's filters. This is `search_postings`' default after part 1, so the agent and the user see the same set.
- **Columns:** first seen, company, title, department, location (with workplace type), and a mark if interested.
- **`enter`** opens the existing posting detail. **`i`**, **`x`** and **`o`** work as they do in a company's posting list.
- **Live:** postings are a logged table (RFC 0008). A sync in another process adds rows and updates the total without a manual refresh. The page the user is on is kept: new postings appear on page 1, not under the cursor.

### Paging

Each page is one `store.SearchPostings` call with `Order: PostingOrderIDDesc`, `Limit` set to the rows that fit the window, and the previous page's last ID as the cursor. The query already exists (RFC 0006, #223), so part 2 needs no new SQL beyond one column. The TUI keeps a stack of the cursors it has used, so going back a page is one call too. `Total` gives the page count.

Two small additions to the store:
- The page row needs `first_seen_at` for the first column (it has `published_at` today).
- `posting_markup` becomes a logged table (`interested_at`, `archived_at`). This view hides archived postings and marks interested ones, and changes to those from another TUI window should show here. RFC 0008 and `changelog.go` kept it unlogged only because nothing read its changes yet.

## Part 3: Search

### What it matches

The query is split on spaces into terms. A posting matches when **every** term appears somewhere in its title, company name, department or location, ignoring case. So `platform dublin` finds a Platform Engineer in Dublin at any company, and `acme backend` finds Acme's backend roles. It's a substring match, like the company list's `/`: `eng` matches "Engineering" and "Engineer".

Search narrows the same set the view shows (open, not archived, matching company filters), and keeps its sort and paging. `Total` becomes the number of matches.

### The store

`PostingSearch` gets a `Query string`. `SearchPostings` splits it into terms and passes them as one JSON array, read with `json_each`, the way it already passes application statuses (sqlc can't mix a list parameter with other bound parameters on SQLite). One more condition goes in the inner query of each `SearchPostingsBy*`:

```sql
AND NOT EXISTS (
    SELECT 1 FROM json_each(sqlc.narg('terms')) AS t
    WHERE (postings.title || ' ' || companies.name || ' ' ||
           postings.department || ' ' || postings.location)
          NOT LIKE '%' || t.value || '%' ESCAPE '\'
)
```

"No term is missing" is the same as "every term is present", and an empty or absent array matches everything. Go escapes `%`, `_` and `\` in each term, so they're matched literally. SQLite's `LIKE` ignores case for ASCII letters only, so `é` and `É` don't match each other. That's acceptable for titles and places.

This scans the summary columns of a few thousand rows (or tens of thousands after part 1), with no index. That's fast enough to run on every keystroke.

### The TUI

In the all-postings view, `/` opens the search line above the table, as in the company list (`searchLine`). Each keystroke reloads page 1 with the query, after a short pause in typing (about 150 ms) so fast typing doesn't queue a load per key. The line shows `12 of 1,972`. `esc` clears the search and returns to the full list. Paging and the sort work on the matches.

### The agent

`search_postings` gets an optional `Query` with the same meaning. The agent can then look something up ("any Go roles in Dublin?") without paging through everything.

### Not included yet

- **Descriptions.** Matching them would find skills that aren't in the title ("Kafka"), but descriptions are dozens of times larger than the summary fields, so a scan per keystroke could be noticeably slower. Adding `description_text` to the `LIKE` is a one-line change, so measure it once part 1's posting count is known (open question 8).
- **Word matching and ranking** (FTS5, option 9).

## Part 4: Fit scores

### 1. Scores

A **score** is one posting's score, with everything about what produced it. There are no runs: each score carries its own provenance, so a single posting can be scored on its own, and a session of hundreds is just many scores.

| Column (`posting_scores`) | Meaning |
|---|---|
| `id` | `INTEGER PRIMARY KEY AUTOINCREMENT`. |
| `posting_id` | The posting. |
| `score` | 1–5 (open question 4). |
| `reason` | One line, shown with the score, e.g. "Go and distributed systems, remote". |
| `content_sha256` | The posting's content hash when it was scored (section 2). |
| `profile_sha256` | The hash of `profile.md` it was scored against. |
| `model` | The model ID the agent reports for itself, e.g. `claude-opus-5-5`. Required. Swamp can't check it. |
| `client_name`, `client_version` | The MCP client's name and version (e.g. `claude-code`, `2.4.1`), from the session's client info. Swamp records these itself; empty when the client gives none. |
| `scored_at` | UTC, `CURRENT_TIMESTAMP`. |

Rows are only ever inserted. A posting's **current score** is its newest row (highest `id`). One `record_scores` call is one database transaction: every valid entry in it is committed together, or none is if the write fails.

A posting can't be scored twice for nothing: an entry is refused if the posting already has a score on the same content, against the same profile, from the same model.

### 2. Content hash

A score is tied to the content the agent saw. `postings.content_sha256` is a new column: the SHA-256 of the fields the agent reads (title, department, location, workplace type, description text). `IngestPosting` sets it whenever it writes a posting.

It can't be the `posting_history` ID. A `content_updated` row is written whenever any ingested field changes, including the raw payload (`store/posting.go`), and Greenhouse's raw payload has its own `updated_at` and other fields Swamp never shows. A change the agent can't see shouldn't make a posting need scoring again.

SQLite has no built-in SHA-256, so existing rows are backfilled by a Go migration. It would be the first one: goose supports them, but every migration so far is SQL.

### 3. What's pending

A posting is **eligible** when it's open, not archived, its company isn't deleted and its filters let it through, and it isn't marked interested and has no application (the user has already decided about those).

An eligible posting is **pending** when it has no score on its current `content_sha256` (it's new, or its content changed since it was scored). That's the default, and it's what "score what's new" means. Editing `profile.md` doesn't make anything pending by itself.

A **rescore** is asked for explicitly, with the profile it's against: an eligible posting is pending when it has no score on its current content *against that profile*. After a profile edit, that's every eligible posting, and it shrinks as scores against the new profile are recorded.

So nothing needs a cursor or a session to resume. "What's new since I last checked" is whatever has no score on its current content. An interrupted rescore picks up where it stopped, because what it already did has scores against the new profile.

A change of model alone doesn't make anything pending. With a 1–5 scale, a score from one model is close enough to another's; a per-model rescore can be added if that turns out wrong.

### 4. MCP tools

| Tool | What it does |
|---|---|
| `read_profile` (changed) | Also returns the content's `SHA256`. |
| `read_posting` (new) | One posting in full: the summary fields, the description as text, the job URL, the company's description, and `ContentSHA256`. |
| `list_pending_postings` (new) | One page of pending postings: ID, company, title, department, location, workplace type, published date, `ContentSHA256`, and why it's pending (`new`, `changed`, `rescore`). Takes `ProfileSHA256` and an optional `Rescore`; the description says to rescore only when the user asks. Keyset paged with an opaque cursor, like `search_postings`. Also returns `Total`. |
| `record_scores` (new) | `Model`, `ProfileSHA256` and a batch of `[{PostingID, Score, Reason, ContentSHA256}]`, stored in one transaction, with the client recorded from the session. A batch of one scores a single posting. An entry is refused, and the result says why, if its score isn't 1–5, its reason is empty, its `ContentSHA256` isn't current (the posting changed since the agent saw it; it stays pending), or the posting already has a score on that content, against that profile, from that model. |

Both refuse a `ProfileSHA256` that isn't the hash of `profile.md` right now, so a score can't claim a profile the agent didn't read.

Batching matters. A full scoring involves hundreds of scores, and one call each would cost far more than the scores themselves.

### 5. The TUI

- The all-postings view gets a **score** column: the current score, dimmed if the posting's content changed since it was scored.
- **`s`** switches the sort between first seen and score (highest first, unscored last). This is a new `PostingOrderScoreDesc` for `SearchPostings`, keyset paged on `(score, id)` like `PostingOrderPublishedDesc`.
- The posting detail shows the current score with its reason, model and date, and the earlier scores below it.
- `posting_scores` is a logged table (inserts only). Scores recorded by the agent in the MCP process show up in an open TUI as they're recorded.

### 6. The skill

A new `score-postings` skill, next to `apply-to-posting`, runs the loop:
1. `read_profile`, and keep its `SHA256`.
2. `list_pending_postings` with that `SHA256`, and `Rescore` only if the user asked for one. Say how many are pending (`Total`).
3. Page through it. Score clear misses from the summary fields alone, in batches of `record_scores` with the agent's model ID.
4. For the plausible ones, `read_posting`, then score with a reason.
5. Report how many were scored, and list the best ones.

## Estimates (not measured)

- The summary fields come to roughly 30–40 tokens per posting. Scoring all of about 2,000 postings is roughly 60–80k tokens of listings, across pages. Once part 1 stores every posting, the count may be several times that: 10,000 postings would be about 300–400k tokens, across several sessions. Routine scoring after a sync, with tens of new postings, stays small. Eligibility respects company filters (open question 1), which keeps a full scoring to what the user sees.
- A description is roughly 500–1,500 tokens. Scoring from the summary first keeps descriptions to a small fraction.

Measure token counts on a copy of the real database before writing the skill.

## Where it can go wrong

- **Filter matching drifting between Go and SQL.** If `filter.Match` and `SearchPostings`' SQL disagree, the agent and the TUI disagree about what's shown. The shared test table guards this.
- **Large boards.** A company listing thousands of postings, most of them irrelevant, costs storage and sync time with nothing to show for it. Deleting the company is the way out. A per-company option to keep filters deciding what's stored would bring back the silent loss, so it isn't proposed.
- **Greenhouse jobs in several departments** are stored as one value joined with `;` (`greenhouse/client.go`), and an exact match on one department misses them. Part 1 doesn't change this, but it becomes visible once the filter screen offers every value.
- **A score on content the agent didn't see.** `record_scores`' `ContentSHA256` check prevents it.
- **A misreported model.** `model` is whatever the agent says. `client_name` and `client_version` come from the protocol, not the agent, so at least the client is reliable.
- **Scores from a summary only.** A posting scored from its title alone may be underrated. That's the cost of not reading every description. The reason makes it auditable, and a rescore can fix it.
- **Two agents at once.** Both may score the same posting. The newer score is current, and both are kept; one from the same model on the same content and profile is refused as a repeat.
- **Scores from different models or profiles aren't quite comparable.** A 4 from one isn't exactly a 4 from another. The view shows each posting's newest score regardless; the 1–5 scale keeps the difference small, and the detail shows each score's model and date.

## Options considered

1. **Keep filters deciding what's stored, and warn on stale values.** Smaller: the sync counts matches for each filter value on the fetch and warns on zero. But postings are still lost between the rename and the user noticing the warning, and the agent still can't consider what's skipped. Rejected.
2. **Store everything, but only summary fields for postings that don't match.** It saves the payload for those, but makes two kinds of posting that every reader has to handle, and widening a filter needs a re-fetch again. Rejected.
3. **Close postings that stop matching** (keeping filters deciding what's stored). It fixes problem 3 but makes problem 1 worse: a rename would close the postings that are still listed. Rejected.
4. **One verdict per posting, replaced in place** (this RFC's first draft: `suggest` or `not_a_fit`, plus a generation counter for rescores). It's simpler, but it keeps no history and nothing about what produced a verdict. Replaced by scores added only, each recording what produced it.
5. **The agent marks postings interested directly.** It needs no new storage, but the agent's picks can't be told apart from the user's, and unreviewed picks would fill the apply queue. Rejected.
6. **Tags (`tags`/`posting_tags`, in the schema but unused).** A tag carries no score, reason or record of what produced it. Rejected.
7. **Swamp scores with embeddings or a model.** Ranking would be automatic, but Swamp would choose a model, pay for it and handle stale scores. Rejected: the agent already has the profile and the judgment.
8. **Offset paging in the TUI.** Simpler, but a sync adding postings while the user reads shifts every page. The keyset query already exists. Rejected.
9. **FTS5 full-text search (RFC 0006, option 3).** It would give word matching, prefix search and ranking, and make searching descriptions cheap. But it needs a separate index table kept in step with `postings` by triggers, and a new way of writing queries. Deferred until the substring search in part 3 proves too limited. As a prefilter for scoring, it would miss fits the keywords don't anticipate.
10. **Scoring runs** (this RFC's second draft, built in #318, #319 and #322): a run grouped a session's scores and recorded the model, client and profile once, and `scope: all` defined a rescore, resumed by run ID. But each score can record that itself, and "pending against this profile" defines a rescore and resumes it with no session to keep. Runs only added a concept, a table and two tools, and made scoring a single posting awkward. Dropped by #346 before anything was scored.

## Work breakdown

### Part 1: store every posting
1. ~~(#309) **Measure** the full boards first.~~ Dropped: every posting is stored regardless.
2. (#310) **Store every posting:** `SyncCompany` ingests everything, and `ApplyCompanyFilters` stops re-syncing.
3. (#311) **`SearchPostings` applies company filters by default** (`IgnoreCompanyFilters` to turn that off), with the test shared with `filter.Match` (needs 2).
4. (#312) **Stale filters:** counts on the filter screen, and a mark on the company list (needs 2).

### Part 2: all postings
5. (#313) Store: `first_seen_at` on the search page row; log `posting_markup` (migration, `changelog.go`, triggers).
6. (#314) TUI: the all-postings view, paged and live (needs 3 and 5).

### Part 3: search
7. (#315) Store and MCP: `Query` on `PostingSearch` and `search_postings`, matched in every `SearchPostingsBy*` query. Needs nothing before it.
8. (#316) TUI: `/` in the all-postings view (needs 6 and 7).

### Part 4a: storage (needs nothing from parts 1–3)
9. (#317) Migration: `postings.content_sha256` with a Go backfill; `IngestPosting` sets it.
10. (#318) Migration: `posting_scores` (with the `scoring_runs` that #346 drops), logged, inserts only, with its trigger.
11. (#319) Store: `PendingPostings` (paged, with why), `RecordScores`, and the current score on search rows (needs 9 and 10). Built with runs; #346 reworks it without them.

### Part 4b: MCP (needs 11)
12. (#320) `read_profile` returns `SHA256`.
13. (#321) `read_posting`.
14. (#322) `start_scoring_run` and `list_scoring_runs`. Built, then dropped by #346.
14a. (#346) Drop scoring runs: each score carries its own model, client and profile; `PendingPostings` rescores against a profile; the two run tools go (needs 11 and 14).
15. (#323) `list_pending_postings` (needs 14a).
16. (#324) `record_scores` (needs 14a).

### Part 4c: user-facing
17. (#325) TUI: score column, sort by score, score history in the posting detail (needs 6 and 11).
18. (#326) The `score-postings` skill (needs 2, 12, 13, 15 and 16).

Each item is its own issue and PR.

## Open questions

1. **What should the agent see by default?** Decided (#319): company filters apply everywhere by default, `search_postings` and scoring alike, so scoring covers what the user sees. `IgnoreCompanyFilters` exists for search; scoring could gain an "ignore filters" scope later if a filter is hiding good fits.
2. **Should the all-postings view have a key to show postings the company filters hide?**
3. **Should a company with no filters show everything?** That's today's behavior, and part 1 keeps it.
4. **The score scale.** Decided (#319): 1–5. It makes scores from different sessions and models more comparable, and gives the agent less false precision to invent than 0–100. Ties are expected, so a sort by score needs a tie-breaker.
5. **Should the view hide low scores** (a threshold), or only sort by them?
6. **Excluding interested postings and those with applications** keeps scoring small, but leaves them without a score. Is that wanted, or should they be scored too, for the record?
7. **A way to set postings aside** (the first draft's "dismiss") without archiving them. Not proposed until the view is in use.
8. **Should search include descriptions?** Measure the scan time on the real posting count after part 1 first.

## Out of scope

- Full-text search with ranking (see option 9).
- Filters shared across companies (a standard set of departments or locations). Each board uses its own words. Once filters only decide what's shown (part 1), these can be added as more filters on the view, later.
- Notifications, or scoring on a schedule. Scoring runs when the user asks the agent.
- Learning from the user's interested and archive decisions.
