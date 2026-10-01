# RFC 0002: Getting committed applications submitted

- **Status:** Implemented in part, 2026-10-01. Waves A–C and D1–D2 shipped (#162–#168, #178–#180, and the related #188; D1 was RFC 0004). Open: C3 (#181), D3 (#169), D4 (#170), manual requirements for Ashby/Lever (#184), and the related #174–#176. Wave E (#171–#173) waits on phase 1's success criteria.
- **Date:** 2026-09-28 (revised 2026-09-29; status updated 2026-10-01)
- **Related:** RFC 0001 (scheduled and background sync); issue #105 (a closing posting ends an early-stage application); the `apply-to-posting` skill (`.agents/skills/apply-to-posting/SKILL.md`)

## Summary

Swamp is good at collecting postings and drafting materials, but very little of that turns into submitted applications. Since the first application was started on 2026-08-24, **2 applications are at "submitted"** and **23 are stuck at "started"**. Meanwhile 7 more ended because the posting closed. The job search is limited by applications that stall after you commit to them, not by a shortage of postings or companies.

This RFC defines that problem, sets a metric for it, and proposes a short, ordered set of projects. The first step is to make the metric measurable, then clear the stalled queue, then handle what differs between application forms. Only after that do we widen the intake with triage.

An earlier draft of this RFC framed triage (2,000 unreviewed postings) as the main bottleneck, and proposed 20 projects across four areas. The data below doesn't support that framing, so most of those projects are deferred (see "Deferred").

## Problem

**Committed applications aren't getting submitted, and postings close while they wait.**

Measured on 2026-09-29 against a copy of the real database:

| Stage                                 | Count | Notes                                                |
| ------------------------------------- | ----- | ---------------------------------------------------- |
| Marked interested, no application yet | 15    | open postings only                                   |
| Started, no drafts                    | 10    | no `cover_letter.md` or `resume.md` on disk          |
| Started, drafted, never reviewed      | 8     | 7 with both documents, 1 with only a cover letter    |
| Started, reviewed at least once       | 5     | 8 reviews: 4 passed, 4 flagged. 1 has exported PDFs. |
| Submitted                             | 2     |                                                      |
| Posting closed                        | 7     | see below                                            |
| Withdrawn                             | 2     |                                                      |

- 16 of the 23 started applications are three weeks old or more. 22 of their postings are still open.
- The stall happens at every step after committing: 10 of 23 were never drafted and 18 of 23 were never reviewed. Submission is only the last of these steps.
- **"Posting closed" hides what happened before it.** Sync moves both started _and submitted_ applications to `posting_closed` (`earlyApplicationStatuses`, `sync/company.go:289`), and there's no status history. So we can't tell how many of the 7 were submitted before the posting closed. Two of them (applications 3 and 5) have exported PDFs, which suggests they were. The real submitted count is somewhere between 2 and 9, and Swamp can't say which.

### Metric

- **Primary:** applications submitted per week.
- **Secondary:** days from started to submitted, and the number of applications that end at `posting_closed` without being submitted.
- **Baseline:** 2 to 9 submitted in about 5 weeks (see above). The secondary metrics can't be measured today; that's what phase 0 fixes.

### Why the stall happens: unknown

The data shows _where_ applications stop, but not _why_. Possible reasons include: drafting one posting per agent session is too slow, reviewing takes too long, the form asks questions that aren't drafted anywhere, exporting and submitting is fiddly, or you've simply lost interest in the posting. Each of these points to different projects. Phase 0 asks you to record the actual reason for each stalled application before we commit to phase 1's scope.

## Goals

1. Record every status change, so the metric above can be measured.
2. Clear the queue of 23 started applications: each one either submitted or withdrawn.
3. Keep the queue small afterwards: an application should go from started to submitted in days, not weeks.

### Success criteria

- Phase 0 is done when status history is recorded and each of the 23 stalled applications has a noted reason. (2026-10-01: phase 0 closed without reasons; all 23 were kept, see #163.)
- Phase 1 is done when the started queue is under 5, and the median time from started to submitted, measured over new applications, is under 7 days.
- Phase 2 is done when a Greenhouse application can be submitted without writing an answer that isn't already drafted.
- Only then is widening the intake (phase 3) worth it: more interested postings help only if they get submitted.

## Where things stand

### Tracking

- One status per application (started → submitted → interviewing → offer/rejected/withdrawn/posting closed) plus notes. There's no status history, only `updated_at`.
- `interview_stages` has full create/update/delete in `store`, but nothing in the TUI or MCP uses it, and it has 0 rows. No application has reached interviewing yet.

### Documents

- `apply-to-posting` drafts a cover letter and a resume, from `PROFILE_REFERENCE.md`, for one posting per session.
- There's a review loop (passed/flagged) and PDF export.
- Swamp doesn't know whether a posting needs a cover letter, or what extra questions its form asks.

### Application forms (checked live on 2026-09-29)

- **Greenhouse:** `GET /v1/boards/{token}/jobs/{id}?questions=true` returns the full form. For one GitLab job it returned:
  - a required resume and an optional cover letter;
  - required screening questions: country of residence, location, visa sponsorship, post-employment restrictions, prior GitLab employment;
  - role-specific yes/no questions (scripting proficiency, LLM ecosystem);
  - optional demographic and accessibility questions.

  Swamp's list request (`/jobs?content=true`) doesn't include any of this.

- **Ashby and Lever:** not through their supported public APIs (checked 2026-09-30, #167). Both expose the form unofficially: Ashby through its hosted board's undocumented GraphQL, and Lever as JSON embedded in the hosted apply page. Swamp deliberately doesn't use either, so their requirements are entered by hand (#184).

### Intake

This isn't where the problem is today, but it's where phase 3 will work.

- 40 active companies: 28 Ashby, 9 Greenhouse, 3 Lever. 24 have no filters (Stripe alone has 414 open postings).
- 2,043 open postings. 36 are interested and 1 is archived. The other 2,006 have no decision recorded.
- **Swamp can't tell "seen" from "not seen."** Every posting gets an empty `posting_markup` row when it's created (`store/posting.go:172`). So "2,006 undecided" doesn't mean "2,006 never looked at," and a triage inbox needs its own "seen" state.
- Workplace type is missing on 1,258 open postings:
  - 1,067 are Greenhouse, which has no workplace field. 594 of those say "remote" in their location.
  - 191 are Ashby, where `workplaceType` and `isRemote` are **both** null. So falling back to `isRemote` wouldn't help.
- Casing is mixed: Ashby uses `Remote`/`OnSite`/`Hybrid`, and Lever uses lowercase.
- Pay data:
  - Ashby returns structured pay summaries with `?includeCompensation=true` (checked live). Swamp requests `?listedOnly=true` only.
  - Lever has `salaryRange` on 1 of 44 postings.
  - About 1,400 open descriptions mention pay, but none of it is structured.

## Proposal

Effort: **S** is a few hours to a day, **M** is a few days. Each project is its own issue and PR, following the repo's one-issue-per-PR workflow. The **ordered** task list with dependencies and per-task definitions of done is in [Work breakdown: ordered tasks](#work-breakdown-ordered-tasks) below — read that for sequencing; the phases here explain the *why* for each project. (Note: phase 1's "submit checklist" is split there into C2/`1.3a` and C3/`1.3b`; the split is the 2026-09-29 decision.)

### Phase 0: Measure (S)

1. **Status history.**
   - **What:** an `application_status_history` table recording each status change with its time (UTC, `CURRENT_TIMESTAMP`). It's written in the same transaction as the status update, including sync's move to `posting_closed`.
   - **Why:**
     - It makes the metric measurable.
     - It stops `posting_closed` from hiding whether an application was submitted.
     - It's the prerequisite for any "stalled for N days" view.
2. **Diagnose the 23 stalled applications (no code).**
   - **What:** for each one, write down why it stopped, in the application's notes. Then either withdraw it or keep it.
   - **Why:** it tells us which phase 1 and phase 2 projects matter, and it shrinks the queue before building anything.

### Phase 1: Clear the queue (S–M)

The final scope depends on what phase 0 finds. The expected projects are:

1. **Stalled-applications view.**
   - **What:** on the home screen, list started applications, oldest first. Show how long each has been started, what it's missing (no drafts, not reviewed, flagged, not exported) and whether the posting is still open.
   - **Why:** it makes the queue and its age visible every time you open Swamp, and shows the next step for each application.
2. **Draft several applications in one go.**
   - **What:** let `apply-to-posting` work through the started applications with no drafts (and optionally the interested ones), drafting each one and stopping for review at the end.
   - **Why:** 10 started applications have no drafts at all.
   - **Guardrail:** it still never submits anything or changes an application's status.
3. **Submit checklist.**
   - **What:** a submit flow on application detail. It opens the apply page, exports only the PDFs this posting needs, shows the answers ready to copy, and then marks the application submitted.
   - **Why:** it turns a reviewed draft into a submitted application in one pass.

### Phase 2: Match what each form asks for (M)

1. **Store each posting's application requirements (Greenhouse).**
   - **What:** when you commit to a posting (`stage_prepare`), fetch its form and store:
     - whether the resume and cover letter are required, optional or absent;
     - the custom questions (label, required, field type, options).

     `stage_prepare` returns these, and the skill drafts only what's needed.

   - **Why at commit time:** one extra request per posting you apply to, instead of one for every posting on every sync.
2. **"Answers" document type.**
   - **What:** `answers.md`, next to the cover letter and resume, with one section per question. It gets the same read/write tools, review loop and staleness check.
3. **Reusable standard answers.**
   - **What:** answers to questions that repeat across forms, stored once and reused: country, location, visa sponsorship, work authorization, notice period, post-employment restrictions, salary expectations, preferred name, LinkedIn. The GitLab form alone asks six of these.

Run the Ashby/Lever spike alongside this phase. If their APIs don't expose questions, requirements can be entered by hand.

### Phase 3: Widen the intake (S–M)

Start this only after phase 1 meets its criteria.

1. **A "seen" state and a triage inbox.**
   - **What:** a `seen_at` on posting markup. Then a TUI screen listing every open, unseen posting across all companies, newest first. One key each for interested, archive and skip.
   - **Guardrail:** the inbox should show the started-queue size, so triage doesn't outrun what gets submitted.
2. **Global title and workplace rules.**
   - **What:** include/exclude title patterns and allowed workplace types, applied across all companies at display time.
3. **Reliable workplace type.**
   - **What:**
     - normalize the casing;
     - for Greenhouse, detect "remote" from the location;
     - drop the Ashby `isRemote` fallback idea, since it's always null when `workplaceType` is.
   - **Why:** without this, a workplace rule silently hides or keeps 1,258 postings arbitrarily.

## Work breakdown: ordered tasks

Order of operations for the projects above. Each task is **one branch / issue / PR** — the repo's one-issue-per-PR rule, TDD, the four-step verification checklist, and a `decisions.log` entry wherever a choice is actually made. **S** = hours to a day, **M** = a few days.

**Confirmed 2026-09-29:**

- **1.3 is split.** `1.3a` (submit checklist, needs no requirements data) lands in Wave C now; `1.3b` (export *only* what this posting asks) waits on D2 (Greenhouse requirements) because it is the one piece that reads stored requirements.
- **RFC 0004** (the `documents` type-dispatch refactor) is **strongly recommended** as a prerequisite of D3 (`answers.md`) — recommended, **not** a hard gate. It is the work that makes a third document type cheap; skipping it makes D3 the expensive path by design.
- **Hygiene findings** (deleted companies' postings staying `open`; Mattermost/Livekit not fetched since 2026-09-24) are folded in here as task A3 rather than left as separate issues.
- **Phase 3 is a hard gate.** It does not start until phase 1's success criteria are met: started queue under 5 **and** median started→submitted (measured over new applications) under 7 days.

### Wave A — foundations (mutually independent, can all start now)

**A1 — Status history (phase 0.1).** The `application_status_history` table (application id, status, `changed_at` UTC default) plus a sqlc query that inserts the row **in the same transaction** as the status update. There are **two** write paths in the main tree and both must insert in-tx: `store/application.go:100` (`Store.UpdateApplicationStatus`, the TUI path) and `store/posting.go:317` (the `qtx` path). Include sync's move to `posting_closed`. *Done:* a test proves the history row and the status change commit or roll back together, and a second test covers the `posting_closed` move from sync. The metric becomes computable from the table. Deps: none. **S**.

**A2 — Ashby/Lever questions spike (phase 2 open question, pulled early).** Timeboxed reconnaissance, no production code: do the public Ashby and Lever APIs expose application/screening questions at all (open Q4)? If not, D2's fallback is manual entry. *Done:* a short note (issue comment or `decisions.log`) answering Q4, so it is already settled when D2 is designed. Deps: none; run early because its finding changes D2's design. **S**.

**A3 — Hygiene fixes ("Related findings").** (a) A deleted company's postings should not keep counting as `open` (Outschool: 6 postings still `open` after deletion) — decide the intended behavior and fix; (b) confirm Mattermost and Livekit simply have not been fetched since 2026-09-24, and re-fetch now that `swamp fetch` reports failures (#145). *Done:* deleted companies' postings excluded from open totals (or documented as intended) and all active companies freshly fetched. Each is its own small issue/PR. Deps: none. **S**.

### Wave B — make the queue visible (needs A1)

**B1 — Diagnose the 23 stalled applications (phase 0.2, no code).** For each started application: write in its notes why it stopped, then withdraw it or keep it. *Done:* every one of the 23 has a reason note and a keep/withdraw decision; the keep-list is C1's work list. Deps: A1 (so "how long stalled" is measurable, not guessed). **S**.

**B2 — Stalled-applications view (phase 1.1).** On the home screen, list started applications oldest-first with their age, what each is missing (no drafts / not reviewed / flagged / not exported), and whether the posting is still open. *Done:* the home screen shows the queue and each application's next step; the "what's missing" logic composes the existing `documents.Status` + `store.LatestDocumentReviews` (read-only, no new store work). Deps: A1, B1. **S**.

### Wave C — clear the queue (work list comes from B1)

**C1 — Batch drafting (phase 1.2).** Let `apply-to-posting` walk B1's keep-list of applications with no drafts, drafting each and stopping for review. *Guardrail:* it still never submits anything or changes an application's status. *Done:* the no-draft keep-list from B1 has cover-letter/resume (and, once D3 lands, answers) drafts. Deps: B1. **S**.

**C2 — Submit checklist v1, `1.3a` (phase 1.3, split).** A submit flow on application detail: open the apply page, export the PDFs that already exist (cover letter / resume, whichever are present), show the drafted answers ready to copy, then mark the application submitted. *Done:* a reviewed draft reaches "submitted" in one pass on the TUI, without needing stored requirements. Deps: B2 (helps navigation, not a hard dep). **S**.

**C3 — Export only what this posting asks, `1.3b`.** The half of the submit flow that reads the stored application requirement (D2's output) and exports only what this form actually asks. *Done:* for a Greenhouse posting the exported set matches the stored required/optional/absent. Deps: **D2** — this is the only submit-flow piece that needs requirements data. **S**.

### Wave D — match what each form asks (phase 2)

**D1 — `documents` type-dispatch refactor (RFC 0004).** `documents` gains the type→document accessor (and, per RFC 0004, the single `CurrentReviews` owner); optionally co-land its small siblings (RFC 0003's option-3 contract test, RFC 0005's presence method). *Why first:* it is the change that makes adding a third document type (D3) cheap — doing D3 without it touches six dispatch sites by hand. Deps: none. **Strongly recommended before D3.** **M**.

**D2 — Greenhouse application requirements (phase 2.1, the keystone).** Add the Greenhouse job-detail call with `?questions=true` (the client today only does `/jobs?content=true`, `greenhouse/client.go:87`), parse it, and at `stage_prepare` time store whether the cover letter/resume is required, optional, or absent plus the custom questions (label, required, field type, options); `stage_prepare` returns them so the skill drafts only what's needed. *Done:* committing to a Greenhouse posting stores its requirements + questions and the skill reads them back to draft only what is asked. Deps: A2 (it decides the API-vs-manual-entry shape). **M**.

**D3 — `answers.md` document type (phase 2.2).** A third document type beside the cover letter and resume, one section per question, with the same read/write tools, the same review loop (passed/flagged), and the same staleness check. *Done:* the answers document round-trips — written, reviewed, and re-checked for staleness when its content changes. Deps: D1 (strongly recommended), D2 (for the question set to draft against). **M**.

**D4 — Reusable standard answers (phase 2.3).** Decide the home first (open Q3: a section in `PROFILE_REFERENCE.md`, free text the agent adapts, vs a structured file the TUI can show beside each question) — one line in `decisions.log` — then store-and-reuse the answers that repeat across forms (country, location, visa sponsorship, work authorization, notice period, post-employment restrictions, salary expectations, preferred name, LinkedIn). *Done:* a standard answer edits once and is reused across drafts; `apply-to-posting` references it. Deps: D3. **S**.

### Wave E — widen the intake (phase 3, hard-gated)

**Gate:** phase 3 does not start until phase 1's success criteria are met — started queue under 5 **and** median started→submitted (over new applications) under 7 days. "More interested postings help only if they get submitted."

**E1 — Reliable workplace type (phase 3.3, first in the wave).** Normalize casing; for Greenhouse, detect "remote" from the location; drop the Ashby `isRemote` fallback (verified always-null when `workplaceType` is null). *Why first:* E2's workplace rules are footguns on this data — 1,258 open postings have no usable workplace type as it stands. *Done:* workplace type is consistent across sources and the Greenhouse "remote" gap is closed. Deps: none. **S**.

**E2 — Global title & workplace rules (phase 3.2).** Include/exclude title patterns and an allowed-workplace-types list, applied across all companies at display time. *Done:* a single rule set filters postings company-wide and only changes what is displayed (no store mutation). Deps: E1 (the workplace half needs its data fixed first). **S**.

**E3 — "Seen" state + triage inbox (phase 3.1).** A `seen_at` on posting markup, then a TUI screen listing every open, unseen posting across all companies, newest first, with one key each for interested / archive / skip. *Guardrail:* the inbox shows the started-queue size so triage never outruns what gets submitted. *Done:* an unseen open listing is promoted to "interested" with one key press and its `seen_at` set. Deps: B2 (for the guardrail display). **S**.

### Dependency map

```
A1  status history ─┬─ B1 ─┬─ C1  batch drafting
                    │      └─ B2 ─┬─ C2  submit checklist v1 (1.3a)
                    │            └────── E3  (after the phase-3 gate)
                    └─ the metric / stall-age / "submitted-then-closed" become measurable
A2  Ashby/Lever spike ──────────────── D2 (API vs manual-entry fallback)
A3  hygiene fixes (independent, can ship alone anytime)
D1  documents type-dispatch (RFC 0004) ─(strongly recommended)─ D3 (a 3rd type is cheap with it)
D2  Greenhouse requirements ─┬─ C3  export-only-what's-asked (1.3b)
                             └─ D3  the question set to draft answers against
D3  answers.md ─────────────────────── D4  reusable standard answers
E1  reliable workplace ─────────────── E2  workplace rules
```

**Order in one line:** `A1, A2, A3` → `B1, B2` → `C1, C2` → `D1, D2` → `D3, D4` (and `C3` right after D2) → gate → `E1, E2, E3`.

**Issues** (filed 2026-09-30; labels `rfc-0002` plus `rfc0002-step-N` in the order above; list with `gh issue list --label rfc-0002`):

| Task | Issue | Step label |
| --- | --- | --- |
| A1 | #162 | 1 |
| A2 | #167 (done: not in supported APIs; manual entry, #184) | 2 |
| A3a | #178 | 3 |
| A3b | #179 (done: both companies fetch normally) | 4 |
| B1 | #163 (done: all 23 kept, no reasons recorded) | 5 |
| B2 | #164 | 6 |
| C1 | #165 | 7 |
| C2 | #166 | 8 |
| D1 | #180 | 9 |
| D2 | #168 | 10 |
| C3 | #181 | 11 |
| D3 | #169 | 12 |
| D4 | #170 | 13 |
| E1 | #171 | 14 |
| E2 | #173 | 15 |
| E3 | #172 | 16 |

Found while filing, outside the waves (label `rfc0002-related`): #174 (open question 1, and whether reopening a posting restores its application), #175 (an application can be started on an already-closed posting), #176 (`last_seen_at` only changes with content, which blocks open question 5). Filed from A2: #184 (enter Ashby/Lever application requirements by hand; depends on D2).

## Deferred

| Project                                                                                 | Why deferred                                                                                                             |
| --------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| Finding companies (add from careers URL, guess board from name, `find-companies` skill) | 2,000 open postings already outnumber what gets submitted by orders of magnitude. More companies make the backlog worse. |
| Interview stages in the TUI, upcoming interviews, interview prep, pipeline stats        | No application has reached interviewing. Revisit when one does. Status history (phase 0) is the groundwork.              |
| Agent fit scoring                                                                       | Useful for ordering the triage inbox, so it follows phase 3.                                                             |
| Pay import (Ashby compensation, Lever salary)                                           | A nice filter, but it doesn't affect submissions. It fits in phase 3 if cheap.                                           |
| Tags                                                                                    | No use case yet. The tables stay unused.                                                                                 |
| Base resume plus tailored changes; carrying review lessons into the profile             | Worth it once review volume is the bottleneck, which it isn't at 8 reviews.                                              |

## Related findings (now folded into the plan as task A3, except the already-fixed one)

- **Deleted companies' postings stay open** (→ A3a). Outschool was deleted on 2026-09-29, but its 6 postings are still `open`, so they count toward open totals.
- **Two companies haven't been fetched since 2026-09-24** (→ A3b). Mattermost and Livekit, while others were fetched on 09-28 and 09-29. It may just be how those fetches were run. Worth checking now that `swamp fetch` reports failures (#145).
- **Already fixed:** the `published_at` scan error that an earlier draft listed as a blocker was fixed in #141 (migration 00012).

## Open questions

1. **Should a closing posting still move a _submitted_ application to `posting_closed`?** Issue #105 made that deliberate. With status history the submission isn't lost, but "submitted, then posting closed" arguably means "still waiting on a response."
2. **Phase 1 scope:** which projects do the phase 0 notes actually call for?
3. **Where standard answers live:** a section in `PROFILE_REFERENCE.md` (free text, the agent adapts it) or a structured file (exact reuse, and the TUI could show it next to each question)?
4. **Ashby/Lever questions:** answered by the spike (#167, 2026-09-30). Neither supported public API exposes them. Unofficial sources exist but aren't used, so manual entry (#184) is the path for both.
5. **Time pressure:** how long do postings stay open? A rough estimate from `published_at` to `last_seen_at` is about a month. Some applications' `last_seen_at` falls _before_ they were started, though, so what `last_seen_at` records needs checking before relying on it.

## Out of scope

- Submitting applications automatically. It's against the "never submit" rule in `apply-to-posting`, and fragile across boards.
- Email or calendar integration.
