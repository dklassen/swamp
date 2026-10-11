# RFC 0009: Discovering postings that fit, through the agent

- **Status:** Draft
- **Date:** 2026-10-11
- **Related:** RFC 0006 (structured posting search; its open question 4 anticipated this); RFC 0008 (the change log, which carries the agent's verdicts to the TUI); RFC 0002 (the application workflow a suggestion feeds)

## Summary

There are now too many companies and postings to read through. Postings that would suit the user, and postings that changed recently, get lost in the list.

This RFC has the **agent** judge fit and **Swamp** keep the records:
- Swamp works out which postings are **due** for a look: new ones, ones whose content changed since the agent last looked, and, after the user asks for a re-run, ones assessed against an older profile.
- Through the MCP server, the agent pages through the due postings and triages them by title, department, location and workplace type. It reads the full description only for the plausible ones. It records a **verdict** for each one: *suggest* or *not a fit*, with a one-line reason.
- A new **Suggestions** view in the TUI lists the *suggest* verdicts from every company. From there the user marks a posting interested, which feeds `apply-to-posting`, or **dismisses** it from Suggestions for good.

Swamp never calls a model. Every judgment is the agent's, made in a conversation the user started.

## Problem

1. **Finding fits.** Across 44 companies there are about 2,000 open postings (1,972 on 2026-10-01; see RFC 0006), and up to 393 at one company. Today the user has to open each company and read the titles.
2. **Noticing change.** New postings and changed ones look the same as everything else. Nothing says "you haven't seen this".
3. **Reconsidering after a profile change.** When the user updates `profile.md` (a new skill, a different target role), postings that were judged before should be judged again. That has to happen when the user asks, not on every edit.

What the agent can't do today:
- `search_postings` filters on fixed values only, and never returns descriptions (#117).
- No tool returns a posting's description.
- Nothing records that a posting was looked at, or what was concluded.

## Decisions

These were settled in discussion before drafting:

| Question | Decision |
|---|---|
| Who judges fit | The agent, from `read_profile`, on a list Swamp has narrowed. Swamp computes and stores no score. |
| What counts as recent | New postings and content changes. A posting closing or reopening doesn't count. |
| Since when | Since the agent last assessed that posting. Swamp records it for each posting; there's no global cursor. |
| Profile changes | An explicit re-run. Editing `profile.md` never queues anything by itself. |
| Getting through many postings | Triage, then read: summary fields first, a description only when plausible. |
| Where suggestions show up | A new Suggestions view, across companies, with the reason. |
| Dismissal | Removes the posting from Suggestions permanently, whatever changes later. It isn't archiving: the posting is unchanged in every other view. |

## How it works

### 1. Assessments

An **assessment** is the agent's verdict on one posting. There's one per posting, and a newer one replaces it:

| Column | Meaning |
|---|---|
| `posting_id` | The posting (primary key). |
| `verdict` | `suggest` or `not_a_fit`. |
| `reason` | One line, shown in Suggestions, e.g. "Go and distributed systems, remote". |
| `content_version` | The posting's content version when it was assessed (below). |
| `generation` | The assessment generation it belongs to (below). |
| `profile_sha256` | The hash of `profile.md` it was judged against. This is for the record only: it never makes anything due. |
| `assessed_at` | UTC, `CURRENT_TIMESTAMP`. |

**Content version.** This is the ID of the posting's latest `content_updated` row in `posting_history`, or 0 if there's none. It can't be `postings.updated_at`: that also moves when a posting closes or reopens (`db/queries/postings.sql`), and a reopened posting with the same content isn't new to the user. `posting_history` rows are never deleted, so this ID only grows. (`posting_history.id` isn't `AUTOINCREMENT`. That's safe while nothing deletes its rows, and the RFC relies on that.)

**Generation.** A single counter, starting at 1. A re-run increments it.

### 2. What's due

A posting is **due** when all of these hold:
- it's open, it isn't archived, and its company isn't deleted;
- it isn't marked interested and has no application, since the user has already decided about those;
- it isn't dismissed from Suggestions;

and one of these holds:
- it has no assessment (**new**);
- its content version is newer than its assessment's (**changed**);
- its assessment's generation is older than the current one (**re-run**).

Each due posting is reported with which of the three applies, so the agent can say "3 new, 1 changed".

The rule also decides what *stops* being due. Recording an assessment at the current generation and content version takes the posting off the list. So "what's new since I last checked" doesn't need a checkpoint. It's whatever hasn't been assessed since it last changed.

### 3. Re-runs

The user asks the agent for a re-run (for example "I've updated my profile, go through everything again"), and the agent calls `start_reassessment`. That increments the generation, which makes every eligible posting due again with the reason **re-run**.

The current verdicts stay in place until the new ones replace them. During a long re-run, Suggestions keeps showing the previous suggestions instead of going blank.

The re-run is explicit because a full one means about 2,000 postings. A typo fix in `profile.md` shouldn't start that by itself.

### 4. Dismissals

Dismissing a posting from Suggestions writes a row to its own table, `suggestion_dismissals` (`posting_id`, `dismissed_at`). Dismissed postings are never due again, so they're never suggested again, even after a content change or a re-run.

A dismissal doesn't touch `posting_markup`. The posting isn't archived, stays visible in its company's posting list, and can still be marked interested from there.

### 5. MCP tools

| Tool | What it does |
|---|---|
| `list_due_postings` (new) | One page of due postings: ID, company, title, department, location, workplace type, published date, why it's due (`new`/`changed`/`rerun`), and `ContentVersion`. Keyset paged with an opaque cursor, like `search_postings` (RFC 0006); ordered by posting ID, newest first. Also returns `Total` and the current `Generation`. |
| `read_posting` (new) | One posting in full: the summary fields, the description as text, the job URL, the company's description, and `ContentVersion`. |
| `record_assessments` (new) | A batch of verdicts: `[{PostingID, Verdict, Reason, ContentVersion}]`, plus the `ProfileSHA256` read from `read_profile`. Each is stored at the current generation. Each entry succeeds or fails on its own, and the result says which. An entry is refused if its `ContentVersion` is no longer current (the posting changed since the agent saw it; it stays due) or if the posting was dismissed in the meantime. |
| `start_reassessment` (new) | Increments the generation and returns the new value and how many postings are now due. The description says to call it only when the user asks. |
| `read_profile` (changed) | Also returns the content's `SHA256`, as `read_document` already does. |

Batching matters here. A full triage involves hundreds of verdicts, and one call per verdict would cost far more than the verdicts themselves.

### 6. The TUI's Suggestions view

A new top-level view, opened from the company list (key to be decided). It lists every posting whose verdict is `suggest` and that isn't dismissed, interested, archived or closed, newest assessment first:

```
Suggestions (12)
  Acme      Senior Backend Engineer   remote   Go and distributed systems
  Globex    Platform Engineer         NYC      infra and k8s, like the SRE work
  Initech   Staff Engineer, Data      remote   data pipelines; the title is a stretch

enter: detail  i: interested  d: dismiss  x: archive  o: open in browser  esc: back
```

- **`i`** marks the posting interested. It leaves Suggestions and joins the queue `apply-to-posting` works from.
- **`d`** dismisses it (section 4).
- **`x`** archives it, as everywhere else.
- **`enter`** opens the existing posting detail.

`posting_assessments` and `suggestion_dismissals` are both logged tables (RFC 0008). When the agent records verdicts from the MCP process, a TUI that's open sees new suggestions without a manual refresh.

### 7. The skill

A new `discover-postings` skill, next to `apply-to-posting`, runs the loop:
1. Read the profile and keep its `SHA256`.
2. Page through `list_due_postings`. From the summary fields, record clear misses as `not_a_fit` in batches.
3. For the plausible ones, `read_posting`, judge, and record `suggest` or `not_a_fit` with a reason.
4. Report what it did: how many were due and why, how many were suggested, and the suggestions themselves.

The user might ask for a re-run. The skill then calls `start_reassessment` first, and says how many postings that made due before starting.

## Estimates (not measured)

- The summary fields come to roughly 30–40 tokens per posting. A full triage of about 2,000 postings is roughly 60–80k tokens of listings, across pages. That fits one session but isn't cheap. A routine run after a sync, with tens of new or changed postings, is small.
- A description is roughly 500–1,500 tokens. Triage keeps descriptions to a small fraction of the due postings.

Before wave C, measure these on a copy of the real database.

## Where it can go wrong

- **Stale verdicts after a content change.** The verdict stays shown until the posting is assessed again. The posting is due again, so the next run reassesses it. Showing the old suggestion until then seems better than hiding it.
- **A verdict recorded on content the agent didn't see.** This is what `record_assessments`'s `ContentVersion` check prevents.
- **Duplicate work with two agents at once.** Both can assess the same posting. The later verdict wins, and nothing is corrupted. That's acceptable for a single user.
- **Triage misses.** A title-only miss is recorded as `not_a_fit` and isn't looked at again until the posting changes or a re-run happens. That's the cost of triage. The reason field makes misses auditable.

## Options considered

1. **The agent marks postings interested directly.** It needs no new view, but the agent's picks can't be told apart from the user's, and unreviewed picks would fill the apply queue. Rejected.
2. **Tags (`tags`/`posting_tags`, in the schema but unused).** These are general, but a tag carries no reason, and no record of the profile or content it was based on. Rejected for this. Tags can still come later for the user's own grouping.
3. **A stored fit score (embeddings or model scoring inside Swamp).** That gives a better ranking, but it means Swamp choosing a model, paying for it, and handling stale scores. Rejected: the agent already has the profile and the judgment.
4. **A global "last checked" cursor in the change log.** It works for new and changed postings, but it can't express a re-run, and it loses track of postings skipped partway through a run. Due-ness for each posting, derived from assessments, covers both. Rejected.
5. **Keyword prefilter with FTS5 (RFC 0006, option 3).** It's the cheapest, but it misses fits the keywords don't anticipate. Not needed for this. It's still a good later addition to `list_due_postings`, as an optional filter for "only look at X this time".

## Work breakdown

### Wave A: storage (mutually independent)
1. A migration adding `posting_assessments`, `suggestion_dismissals` and the generation counter, with their `changelog.go` classification and triggers. IDs: `posting_assessments` and `suggestion_dismissals` are keyed by `posting_id`, which is never reused.
2. Store: content version for a posting; `RecordAssessments` with the version check; `DuePostings` (paged, with the due reason); `StartReassessment`; `DismissSuggestion`; `Suggestions`.

### Wave B: MCP (needs A)
3. `read_profile` returns `SHA256`.
4. `read_posting`.
5. `list_due_postings`.
6. `record_assessments`.
7. `start_reassessment`.

### Wave C: user-facing (needs B)
8. The TUI's Suggestions view, live through the change log.
9. The `discover-postings` skill.

Each item is its own issue and PR.

## Open questions

1. **Keep assessment history?** This RFC keeps only the latest verdict for each posting. A history ("suggested in March, not a fit after the profile change") is a separate table if it's ever wanted.
2. **Start a re-run from the TUI too?** This RFC only offers it through the agent. A key in Suggestions would be simple, but it would queue work the agent then has to do.
3. **Ask again when a suggested posting is still undecided after N days?** Not proposed. It stays in Suggestions until the user acts.
4. **The key for the Suggestions view**, and whether the company list shows a count of suggestions.

## Out of scope

- Free-text or keyword search (see option 5).
- Notifications, or running discovery on a schedule. Discovery runs when the user asks the agent.
- Learning from the user's accept and dismiss decisions. The agent can read them later if that's wanted.
