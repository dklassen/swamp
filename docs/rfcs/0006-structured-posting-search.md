# RFC 0006: Finding a posting or application by what it is, not by text

- **Status:** Accepted 2026-10-02. Work breakdown filed as #218–#224 (see "Work breakdown").
- **Date:** 2026-10-01 (accepted 2026-10-02)
- **Related:** issue #215 and its closed PR #216 (a first `search_postings` with free-text matching, which this RFC replaces); RFC 0002 (the application workflow the agent drives); issue #171 (normalising workplace type)

## Summary

The agent can't find most of the user's applications. Its one listing tool, `list_postings`, returns only the drafting queue. When the user names an application they're looking at in the TUI, which shows no IDs, the agent has nothing to look it up with.

PR #216 answered that with free-text search. Reviewing it showed that the problem doesn't need text search yet. Every case we have can be resolved by filtering on values from a **controlled list**: the company, application status, open or closed, interested, archived. The agent then reads the titles in a short, paged result.

This RFC proposes:
- a `list_companies` tool, so the agent can learn the company vocabulary;
- `search_postings` with structured filters only;
- **keyset pagination** with **opaque cursors**, over a small fixed set of sort orders (by posting ID either way, or newest on the board first), so paging stays stable while data changes underneath.

Free-text and ranked search wait until there's a need these filters can't meet.

## Problem

Three things the agent must be able to do:

1. **Resolve a named application.** The user says "my application to the Staff Developer role at Acme". The agent needs that application's posting ID, for `stage_prepare`, and its application ID, for the document tools.
2. **Find a posting at a company.** The user says "the platform role at Acme", with no application yet. The agent needs to narrow that company's postings down to the one meant.
3. **Review the interested shortlist.** The user asks "what have I marked interested but not started?", or "which of my interested postings have I applied to?". Interested is a mark on the posting, not the application: it's set before an application exists, and stays after. So this is the `Interested` filter, combined with `HasApplication`. On the real data that's 19 open interested postings with no application, plus 19 started and 2 submitted, at most 7 at any one company. One page per company, or a few pages overall.

What makes these hard today:
- `list_postings` returns postings marked interested, plus started applications, that still need a document or have a flagged review. On the real database, **23 of 34 applications aren't in it**: drafted ones, submitted ones, ones whose posting closed.
- The TUI shows no IDs, and truncates long titles. Several roles at one company often share a title prefix ("Senior Software Engineer, …").
- Company and title together aren't unique either: 336 open postings share both with another posting.
- The agent can't list companies, so it can't check the name the user gave against what's stored.

Not part of this RFC: discovering postings by what they say, such as "postings mentioning Kubernetes". That's a search for things the user hasn't named. It's a different problem, and see Options for what it would cost.

## What the data says

Measured on a copy of the real database, 2026-10-01:

| | |
|---|---|
| Companies | 44 (4 deleted); boards: 28 Ashby, 9 Greenhouse, 3 Lever |
| Postings | 1,972 open, 725 closed |
| Open postings per company | 1 to 393, mean about 51; the five largest have 393, 203, 140, 125 and 111 |
| Applications | 34: 23 started, 2 submitted, 7 posting closed, 2 withdrawn |
| Applications per company | at most 6; at most 5 at one company and status |
| Marked interested | 53 |
| Archived | 12 |

Which fields have a controlled list of values:

| Field | Controlled? | Values |
|---|---|---|
| Company | yes, a table | 44 rows, each with an ID |
| Application status | yes, a Go enum | 9 (`store.ApplicationStatuses()`) |
| Listing status | yes | `open`, `closed` |
| Interested, archived | yes | true / false |
| Board (source) | yes | `ashby`, `greenhouse`, `lever` |
| Workplace type | **not yet** | 1,207 of 1,972 open postings blank, and mixed casing (`Remote`/`remote`); #171 normalises it |
| Department, location | **no** | 345 and 389 distinct free-text values across open postings |

So problem 1 never needs more than one page: filtering by company plus "has an application" returns at most 6 rows. Problem 2 needs paging: a company can have 393 open postings. Even at the largest company, though, that's 8 pages of 50, and filters like interested or open narrow it further.

## Pagination: why keyset, not offset

A deterministic order is necessary for paging, but it isn't sufficient. With offset paging ("rows 51–100"), any row that enters or leaves the result between two calls shifts every row after it:
- a sync closes a posting;
- the user changes an application's status in the TUI;
- a sync adds postings.

The next page then skips a row or repeats one, even though the order itself never changed.

Keyset pagination asks for "rows after the last one I saw" instead. Ordered by posting ID, a page is `WHERE postings.id > :after ORDER BY postings.id LIMIT :limit`. That's stable here because posting IDs:
- **never change:** they're the row ID;
- **only increase:** they're SQLite rowids without `AUTOINCREMENT`, so a new row gets the current maximum plus one;
- **are never reused:** nothing deletes postings. Companies are soft-deleted, and their postings are filtered out, not removed.

So a posting added mid-paging lands on a later page, and no row's position depends on any other row. One effect remains: a posting whose own filter membership changes while the agent pages (its application moves out of the requested status, say) may or may not be seen. That's inherent without a snapshot, and acceptable for an agent looking for a record.

**Why posting ID and not company then title?** Titles change when a board edits a posting (#148 records it as `content_updated`), and companies can be renamed. A keyset on mutable columns can skip or repeat rows when one changes mid-paging. Creation order isn't a useful order to show the user, but the agent filters and reads titles anyway; it can present matches in whatever order suits.

### When stability matters

How much paging stability matters depends on what the caller does with the pages.

**Enumeration: "every record matching these filters".** This is what this RFC is for, and stability matters:
- When the agent resolves a record the user named, a skipped row is a false "it doesn't exist".
- When it acts on each item (batch drafting walks a list, as in RFC 0002; or "withdraw every application whose posting closed"), a skip is a missed item and a repeat is work done twice.
- When it lists the shortlist, an incomplete page reads as the whole list.

**Discovery: "postings like X", ranked by relevance.** Stability matters much less:
- Readers take the top few results, and refine the query rather than page deep.
- Ranked order shifts as the index changes anyway.
- A miss costs little, because the result never claimed to be complete.

A future full-text search can use a capped, ranked result with plain offset paging, or no paging at all, and promise no stability. It shouldn't inherit this section's requirements.

**Cost decides how far to go.** Keyset paging costs the same as offset paging (one `WHERE` clause, plus an opaque cursor), so enumeration gets it now. The expensive guarantees aren't worth building until there's enumeration over an order that changes often, and none is planned:
- server-side snapshots;
- "as of" bounds;
- stability on keys that change.

In practice the risk is small today anyway. Most results fit on one page: at most 6 applications, or 7 interested postings, per company. A one-page result can't shift. Paging only matters when browsing a large company's postings, and the data changes only a few times a day (syncs, the user's own edits).

### Sort orders

Keyset paging works for any order whose key ends with a unique column that never changes, here the posting ID. Its stability is only as good as the leading columns' immutability. So `search_postings` offers a small fixed set of orders rather than arbitrary sort columns:

| `Sort` | Key | Page condition | Stability |
|---|---|---|---|
| `id_asc` (default) | posting ID | `id > :id` | Full. Postings added mid-paging land on later pages. |
| `id_desc`: newest to Swamp first | posting ID | `id < :id` | Full. Postings added mid-paging sort ahead of page 1, so they aren't seen until the agent starts over. |
| `published_desc`: newest on the board first | `published_at`, then ID | `(published_at, id) < (:published_at, :id)` | Unless a board changes a posting's publish date mid-paging. That one posting can then cross the cursor, and be missed or seen twice. |

ID order isn't a stand-in for publish order. Among open postings, 732 neighbours by ID are out of publish order, because adding a company ingests all its postings at once, old and new alike.

SQLite compares row values like `(published_at, id) < (?, ?)` directly. `published_at` allows NULL, though every open posting has one today. Postings without one sort last, by ID: the page condition handles NULL explicitly rather than relying on SQLite's NULL ordering.

### Opaque cursors

The agent never sees or builds a sort key. Each page returns `NextCursor`, a token it passes back unchanged as `Cursor`. This is the common practice (page tokens in Google's APIs, cursors in GitHub's GraphQL API). Here it buys three things:
- **The key can change shape without breaking callers.** `id_asc` needs one value; `published_desc` needs two.
- **A cursor can't be used with a different query.** The token records its sort order and a hash of the filters. A cursor passed with a different sort or different filters is a tool error ("this cursor belongs to a different search; start again without one"), not a silently wrong page.
- **The agent can't construct or adjust one,** which removes guessed-cursor and off-by-one mistakes.

The token is versioned, base64url-encoded JSON, e.g. `{"v":1,"sort":"published_desc","filters":"<hash>","id":812,"published_at":"2026-09-30T14:00:00Z"}`.

Opaque isn't secret: anyone can decode it, so decoding treats it as untrusted input. An unknown version, a bad value or a mismatched sort or filters is an error. There's nothing sensitive in it, so it isn't signed. And keyset cursors don't go stale on their own: there's no server-side snapshot to expire, so a cursor stays valid until the result changes in the ways described above.

### Adding sort orders later

Ordering by other fields, statuses or timestamps will come up: application status in pipeline order, last status change, interested since. Most of those keys change, which is what the design has to allow for. The opaque cursor already keeps the tool's interface fixed when a key changes shape. The rest:

1. **Sort orders are entries in one fixed registry.** Each gives:
   - its columns and directions, ending with the posting ID as tie-breaker;
   - where NULLs go;
   - how stable it is.

   Enum-like keys sort by a defined rank (e.g. statuses in pipeline order), not alphabetically. Adding an order is one entry, plus a (key, ID) index once the data is big enough to need it.
2. **A key that changes pages "mostly stable" by default.** A row whose key changes mid-paging may be skipped or repeated, while every other row keeps its place. That's acceptable for finding a record. The order's description in the tool says it's that kind.
3. **For exact paging on a changing key, an "as of" bound.** The cursor records when paging began, and later pages exclude rows changed after it (e.g. `updated_at <= :as_of`). A row that moves mid-paging drops out instead of repeating, and a re-run picks it up. Nothing is stored on the server.
4. **A server-side snapshot of the matching IDs** gives fully stable pages for any order. But it needs stored state and expiry, so it's only worth it if 3 isn't enough.

None of these is proposed now; the three orders above cover today's needs.

## Proposal

### `list_companies` (new)

It's read-only and takes no input. It returns every company the user hasn't deleted, ordered by name:

```json
{"Companies": [{"ID": 3, "Name": "Acme", "Source": "ashby", "OpenPostings": 51}]}
```

44 rows fit in one response, so it has no paging. The agent matches the name the user gave against this list, ignoring case, and asks when that's ambiguous. Then it passes the ID on. `store.CountOpenPostingsByCompany` already provides the open count.

### `search_postings` (replaces PR #216's version)

| Input | Meaning | Default |
|---|---|---|
| `CompanyID` | one company, from `list_companies` | any |
| `HasApplication` | with (true) or without (false) an application | either |
| `ApplicationStatuses` | any of these; advertised in the tool schema as an enum of the 9 names | any |
| `Interested` | marked interested, or not | either |
| `ListingStatus` | `open`, `closed` or `any` | `open` |
| `IncludeArchived` | include archived postings | false |
| `Sort` | `id_asc`, `id_desc` or `published_desc` (see Sort orders) | `id_asc` |
| `Cursor` | the `NextCursor` of the previous page, passed back unchanged with the same `Sort` and filters | from the start |
| `Limit` | rows per page, at most 100 | 50 |

Output, in the requested order:

```json
{
  "Postings": [{"Posting": {"ID": 812, "Title": "...", "Department": "...", "Location": "...",
                            "WorkplaceType": "...", "ApplicationURL": "...",
                            "PublishedAt": "2026-09-30T14:00:00Z"},
                "CompanyName": "Acme", "ListingStatus": "open", "Interested": true, "Archived": false,
                "ApplicationID": 34, "ApplicationStatus": "application_started", "ApplicationNotes": ""}],
  "NextCursor": "eyJ2IjoxLCJzb3J0IjoiaWRfYXNjIiwiaWQiOjgxMn0",
  "Total": 57
}
```

- `NextCursor` is `null` on the last page.
- `PublishedAt` is added to the posting summary so newest-first results show why they're in that order.
- `Total` is the number of matches when the page was read: a hint for whether to narrow the search, not a promise about later pages.
- There's no free-text input. The agent reads full titles (never truncated) and picks out the right one with the user.

### Bounded by construction

- **One static sqlc query.** Every filter is an optional parameter, and `ORDER BY postings.id` with `LIMIT` runs in SQL, so a call never reads more than `Limit` rows. That fixes PR #216's unbounded read of every posting into Go.
- **Application statuses** are passed as one JSON array read with `json_each`, not `sqlc.slice`, which can't be mixed with other bound parameters on sqlite (see `ListActiveApplications`).
- **`Total`** is a `COUNT(*) OVER ()` in the same query (a window function runs before `LIMIT`), so no second query is needed.
- **Summary columns only.** No description or payload (#117).

### Extending it later

The pattern is a filter on a controlled value: an optional parameter and one `AND` clause, with the value's vocabulary advertised in the tool schema or discoverable through a tool. Candidates, each its own change:
- **Board:** a fixed list, cheap.
- **Workplace type:** once #171 normalises it to a fixed list.
- **Department and location per company:** `store.ListDistinctDepartmentsForCompany` and `ListDistinctLocationsForCompany` already give the per-company lists the TUI's filter screen uses. A tool exposing them makes these controlled values within a company.
- **Interested or applied since a date.**

### Skill

When the user names a posting or application:
1. Call `list_companies` and match the company name they gave.
2. Call `search_postings` with that `CompanyID`, plus `HasApplication: true` if they said "my application".
3. Page by passing `NextCursor` back as `Cursor` until it's null, or narrow with `Interested` or statuses if `Total` is large. For "the newest ones", use `Sort: "published_desc"` and read the first page.
4. Match the user's words against the full titles:
   - one match: confirm it with the user;
   - several: show them with full titles and ask;
   - none: say so, and try `ListingStatus: "any"`.

When the user asks about their shortlist, call `search_postings` with `Interested: true` (and `HasApplication: false` for "not started yet"), paging as above.

`list_postings` stays the drafting queue for step 1 of the skill.

## Options considered

1. **Structured filters with keyset paging (recommended).** Solves both problems as measured, stays bounded, and is static sqlc SQL. It can't find a posting by words the user remembers from its title without a company to start from; that's acceptable, since the user names the company in every case we've seen.
2. **Substring query words (PR #216).** Each word must appear in company, title, department or location.
   - PR #216 read every posting into Go to do it.
   - The SQL version needs a fixed slot per word, since sqlc can't generate a variable number of conditions. It also can't rank, and each new searchable field adds a clause to every slot.
   - Dropped.
3. **SQLite full-text search (FTS5).** Tested on the real data:
   - indexing company, title, department and location adds about 300 KB;
   - adding descriptions adds about 23 MB to a 126 MB database;
   - a description search runs in about 1 ms;
   - prefix matching and accent folding work.

   But sqlc can only express a full-text match on a single column, so a multi-column search has to be a hand-written query. Relevance-ranked results can also reorder between calls as the index changes, which makes stable paging harder. Worth it when there's a discovery problem to solve; not for resolving a record the user already named.
4. **Offset pagination.** Same cost as keyset, but rows shift when data changes mid-paging (see above). Rejected.

## Work breakdown

Effort: **S** is a few hours to a day, **M** a few days. Each task is its own issue and PR (one issue per PR), labelled `rfc-0006` and `rfc0006-step-N`. Tasks are numbered in the order to do them. A wave's tasks are independent of each other unless a dependency is listed.

**Ship point:** after step 5, the agent can resolve any posting or application the user names, and review the interested shortlist (problems 1–3). Steps 6–7 add newest-first ordering, and can wait until someone asks for it.

### Wave A: foundations (mutually independent)

**1. Bounded search query (store), #218.** `store.SearchPostingListings(ctx, filter)`, one static sqlc query:
- optional filters: company ID, has an application, application statuses (a JSON array read with `json_each`), interested, listing status, include archived;
- the `id_asc` keyset (`id > :after`), `ORDER BY postings.id`, `LIMIT`, and `COUNT(*) OVER ()` for the total;
- summary columns only;
- postings of deleted companies never included.

*Done:* table tests for each filter. Pages read in sequence concatenate to the unpaged result. Closing a posting between two pages doesn't shift the next page. A call never returns more than its limit. *Deps:* none. **M.** The stashed work from PR #216 has the `json_each` and window-function parts, already proven to generate under sqlc.

**2. Opaque cursors, #219.** Encode and decode in `stage`: versioned, base64url JSON carrying the sort name, a hash of the filters, and the key values. Decoding treats the token as untrusted.

*Done:* round-trip tests, plus errors for:
- a bad encoding;
- an unknown version;
- a missing or out-of-range key value;
- a cursor reused with a different sort or different filters.

*Deps:* none. **S.**

**3. `list_companies`, #220.** An MCP tool returning every company the user hasn't deleted: ID, name, board and open posting count, ordered by name. It reuses `ListActiveCompanies` and `CountOpenPostingsByCompany`.

*Done:* an MCP test showing deleted companies are left out and the counts are right. *Deps:* none. **S.**

### Wave B: the tool (needs wave A)

**4. `search_postings`, ID order, #221.**
- `stage.Search` maps the tool's input to step 1's filter, encodes and decodes step 2's cursors, applies the `Limit` default (50) and cap (100), and returns `NextCursor` (null on the last page) and `Total`.
- The MCP tool's schema advertises application statuses and listing status as enums, and `Sort` with only `id_asc` for now.

*Done:* MCP tests:
- a drafted application missing from `list_postings` is found by company plus `HasApplication`;
- the interested shortlist (`Interested: true`, `HasApplication: false`) is returned;
- three pages through a company concatenate to the whole result;
- a cursor reused with other filters is a tool error.

*Deps:* 1, 2. **S–M.** Closes #215.

**5. Skill, #222.** The `apply-to-posting` flow from "Skill" above:
- resolve a named posting or application through `list_companies`, then `search_postings`;
- confirm a single match; with several, show full titles and ask;
- the shortlist use case.

*Done:* a manual check through the real MCP server, on a copy of the real database. Find, by the names the user would say:
- a drafted application;
- a submitted one;
- one whose posting closed;
- the interested shortlist.

*Deps:* 3, 4. **S.** **Ship point.**

### Wave C: sort orders (after the ship point, when wanted)

**6. Sort registry and `id_desc`, #223.** Move the sort order into the registry described in "Adding sort orders later". Each entry gives its columns, direction, NULL placement and stability note. Add `id_desc` (`id < :before`).

*Done:* the paging tests from step 4 pass for both orders. The tool schema lists the orders from the registry, so a new entry appears without editing the schema. *Deps:* 4. **S.**

**7. `published_desc`, #224.**
- The composite keyset `(published_at, id)`, with postings that have no publish date sorted last, by ID.
- `PublishedAt` added to the posting summary.

*Done:* the paging tests pass for this order. Postings without a publish date come last and page correctly. The stability caveat (a changed publish date can cross the cursor) is in the order's description. *Deps:* 6. **S.**

### Not scheduled

Each is its own future issue, under "Extending it later" and "Adding sort orders later":
- a board filter;
- workplace type, after #171;
- per-company department and location lists;
- further sort orders;
- an "as of" bound for changing sort keys.

### Housekeeping, when the RFC is accepted

- Update #215's description to point here.
- File steps 1–7 as issues.
- Add a "What shipped" section as steps merge, as RFC 0004 and RFC 0005 did.

## Open questions

1. **`CompanyID` or company name as input?** An ID removes ambiguity, at the cost of one `list_companies` call first. Names in the TUI are exact, so a case-insensitive name match would usually work too.
2. **Include `Total`?** It's one window function, and the agent uses it to decide whether to narrow. But it can drift during paging, which needs saying in the tool description.
3. **Several companies at once?** One `CompanyID` covers every case so far. A list would use the same `json_each` approach as statuses.
4. **When does free text come in?** Probably when the user asks to *discover* postings by content. Then option 3 is the starting point, with descriptions indexed.
5. **Most recently changed application first?** It's a natural order for "what was I working on", but its key (the latest status change, or `updated_at`) changes whenever the user acts on an application. That's far more often than `published_at` changes, so rows would cross the cursor routinely while paging. Not proposed. Since applications fit on one page per company, the agent can sort those itself.
