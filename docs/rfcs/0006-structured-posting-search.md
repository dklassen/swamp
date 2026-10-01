# RFC 0006: Finding a posting or application by what it is, not by text

- **Status:** Draft, for discussion
- **Date:** 2026-10-01
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

Two things the agent must be able to do:

1. **Resolve a named application.** The user says "my application to the Staff Developer role at Acme". The agent needs that application's posting ID, for `stage_prepare`, and its application ID, for the document tools.
2. **Find a posting at a company.** The user says "the platform role at Acme", with no application yet. The agent needs to narrow that company's postings down to the one meant.

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

1. **`list_companies`**: store query (or reuse `ListActiveCompanies` with `CountOpenPostingsByCompany`), MCP tool, tests. S.
2. **`search_postings`**: replace PR #216's in-memory search with the static query, the three sort orders, opaque cursors (encode, decode as untrusted, reject a mismatched sort or filters) and `Total`; tool schema advertises the status enum. Table tests for each filter, plus tests, for each sort order, that pages concatenate to the full result and that a posting closed mid-paging doesn't shift later pages; and cursor tests for tampering, a wrong version, and reuse with other filters. S–M.
3. **Skill**: the flow above. S.

PR #216 was closed unmerged (2026-10-01, with a comment saying why). #215 is reworked to steps 2 and 3, and its description updated to point here.

## Open questions

1. **`CompanyID` or company name as input?** An ID removes ambiguity, at the cost of one `list_companies` call first. Names in the TUI are exact, so a case-insensitive name match would usually work too.
2. **Include `Total`?** It's one window function, and the agent uses it to decide whether to narrow. But it can drift during paging, which needs saying in the tool description.
3. **Several companies at once?** One `CompanyID` covers every case so far. A list would use the same `json_each` approach as statuses.
4. **When does free text come in?** Probably when the user asks to *discover* postings by content. Then option 3 is the starting point, with descriptions indexed.
5. **Most recently changed application first?** It's a natural order for "what was I working on", but its key (the latest status change, or `updated_at`) changes whenever the user acts on an application. That's far more often than `published_at` changes, so rows would cross the cursor routinely while paging. Not proposed. Since applications fit on one page per company, the agent can sort those itself.
