---
name: score-postings
description: Score how well postings tracked in Swamp (the job-search tool in this repo) fit the user, from 1 (no fit) to 5 (apply now), using the `swamp` MCP server's tools and the user's profile. Use this skill whenever the user asks to score, rank, rate or triage postings, wants to know which new postings are worth a look, asks to score what's pending after a sync, or asks for a rescore after editing their profile -- even if they don't mention scores or MCP by name. Never marks a posting interested, archives it, or starts an application.
---

# Score postings

This skill works through the postings Swamp says are still to score,
gives each a fit score from 1 to 5 with a one-line reason, and records
them. The scores show in the TUI's Fit column (`s` sorts by it) and in
each posting's detail. Run it only with a person present in the session.

All Swamp data access goes through the `swamp` MCP server, declared in
this repo's `.mcp.json`. Its tool definitions are the source of truth for
arguments and behavior -- this skill only says which tool to use when.
Don't read or write the sqlite db directly.

The server has to be running on the host before the session starts
(`task mcp-serve`). If the `swamp` tools aren't available or calls fail to
connect, stop and tell the user to start it rather than falling back to
anything else. If the server answers on the host but is unreachable from
inside a container, have the user run `task container:dns` on the host:
it recreates the `host.container.internal` DNS entry.

## 1. Read the profile

Call `read_profile`, and keep its `Version`: every other call takes it as
`ProfileVersion`.

**If `Exists` is false, stop and tell the user** (the README says how to
set it up, from `docs/canonical/profile.md.example`). A score is a
judgment against the user's real background; without the profile there's
nothing to judge against.

Read it for what decides fit: the kinds of role the user is looking for,
their seniority, where they can work from, and what they say they don't
have. Read whatever is actually in it rather than assuming its structure;
it's the user's document and may change.

## 2. See what's pending

Call `list_pending_postings` with the profile's `Version` as
`ProfileVersion`. Pass `Rescore: true` only when the user asked for a
rescore: it lists every posting with no score against this profile, which
after a profile edit is all of them.

Postings come newest first, the order the TUI's all-postings view shows
by default, so "the first 10" is the newest 10 pending.

Tell the user how many are pending (`Total`). If they haven't said how
many to score, ask, since a full scoring of a large `Total` takes several
sessions. When they ask for a number up to 100, pass it as `Limit` and
score just that page. Otherwise page through: pass `NextCursor` back as
`Cursor`, with the same `ProfileVersion` and `Rescore`, until it's null or
the user's number is reached.

## 3. Score

The scale, judged against the profile:

- **5, apply now:** the kind of role the user wants, at their level, where
  they can work, asking for what the profile shows.
- **4, strong:** a good fit with one real gap, e.g. a skill the profile
  says the user doesn't have, or a level a step off.
- **3, maybe:** worth the user's look, but with several gaps or a lot
  unknown.
- **2, weak:** the right field, but the wrong role, level or emphasis.
- **1, no fit:** a different job altogether, or somewhere the user can't
  work.

For each posting on the page:

- **A clear miss from the summary alone** (a different job, an
  internship, a location the profile rules out): score it 1 or 2 from the
  summary, with the listed `Version` as its `PostingVersion`. Say in the
  reason that it was judged from the title, e.g. "Product design role; from
  the title".
- **Anything that could be a 3 or more:** call `read_posting` first and
  score from the description, with the `Version` that `read_posting`
  returned as its `PostingVersion`. A title alone underrates or overrates
  too often to score a plausible posting on it.

When in doubt, read the description. It costs about 1.5k tokens; a good
posting buried at 2 costs the user the posting.

Several postings sometimes share a title, company and `Version` (a
board listing one role more than once). Read the description once and
give each the same score; each ID still needs its own.

Write the reason for the user scanning the Fit column: one line saying
what decided the score, e.g. "Senior Go backend on ClickHouse pipelines,
remote Canada" or "Staff, but AWS-heavy and the profile says GCP".

## 4. Record

Call `record_scores` with your own model ID as `Model`, the profile's
`Version` as `ProfileVersion`, and the page's scores in one batch. Batch
them; one call per score costs far more than the scores themselves.

Each entry comes back recorded or refused, with why:

- `content_changed`: the posting changed since you read it. It stays
  pending. Read it again with `read_posting` and score it again, once.
- `already_scored`: a score from you on that content, against that
  profile, exists. Nothing to do.
- `no_such_posting`, `score_out_of_range`, `no_reason`: a mistake in the
  batch. Fix the entry and send it again.

If the whole call is refused because the profile changed, the user edited
it during the session. Stop and tell them; scoring against a profile you
haven't read isn't a score of their fit. Start again from step 1 if they
want to go on.

## 5. Report

Stop with one summary:

- how many were scored, and how many of those from the title alone;
- the 4s and 5s: title, company, score and reason, best first;
- anything refused and not recorded, with why;
- how many are still pending (the next `list_pending_postings`' `Total`).

Point the user to the TUI to look further: the all-postings view's Fit
column, `s` to sort by it, and the posting detail for each score's reason.

Don't mark postings interested, archive them, or start applications:
choosing what to pursue is the user's call. Don't start another page or
another batch unasked.
