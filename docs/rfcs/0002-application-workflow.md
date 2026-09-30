# RFC 0002: Getting committed applications submitted

- **Status:** Draft, for discussion (revised 2026-09-29)
- **Date:** 2026-09-28
- **Related:** RFC 0001 (scheduled and background sync); issue #105 (a closing posting ends an early-stage application); the `apply-to-posting` skill (`.agents/skills/apply-to-posting/SKILL.md`)

## Summary

Swamp is good at collecting postings and drafting materials, but very little of that turns into submitted applications. Since the first application was started on 2026-08-24, **2 applications are at "submitted"** and **23 are stuck at "started"**. Meanwhile 7 more ended because the posting closed. The job search is limited by applications that stall after you commit to them, not by a shortage of postings or companies.

This RFC defines that problem, sets a metric for it, and proposes a short, ordered set of projects. The first step is to make the metric measurable, then clear the stalled queue, then handle what differs between application forms. Only after that do we widen the intake with triage.

An earlier draft of this RFC framed triage (2,000 unreviewed postings) as the main bottleneck, and proposed 20 projects across four areas. The data below doesn't support that framing, so most of those projects are deferred (see "Deferred").

## Problem

**Committed applications aren't getting submitted, and postings close while they wait.**

Measured on 2026-09-29 against a copy of the real database:

| Stage | Count | Notes |
|---|---|---|
| Marked interested, no application yet | 15 | open postings only |
| Started, no drafts | 10 | no `cover_letter.md` or `resume.md` on disk |
| Started, drafted, never reviewed | 8 | 7 with both documents, 1 with only a cover letter |
| Started, reviewed at least once | 5 | 8 reviews: 4 passed, 4 flagged. 1 has exported PDFs. |
| Submitted | 2 | |
| Posting closed | 7 | see below |
| Withdrawn | 2 | |

- 16 of the 23 started applications are three weeks old or more. 22 of their postings are still open.
- The stall happens at every step after committing: 10 of 23 were never drafted and 18 of 23 were never reviewed. Submission is only the last of these steps.
- **"Posting closed" hides what happened before it.** Sync moves both started *and submitted* applications to `posting_closed` (`earlyApplicationStatuses`, `sync/company.go:289`), and there's no status history. So we can't tell how many of the 7 were submitted before the posting closed. Two of them (applications 3 and 5) have exported PDFs, which suggests they were. The real submitted count is somewhere between 2 and 9, and Swamp can't say which.

### Metric

- **Primary:** applications submitted per week.
- **Secondary:** days from started to submitted, and the number of applications that end at `posting_closed` without being submitted.
- **Baseline:** 2 to 9 submitted in about 5 weeks (see above). The secondary metrics can't be measured today; that's what phase 0 fixes.

### Why the stall happens: unknown

The data shows *where* applications stop, but not *why*. Possible reasons include: drafting one posting per agent session is too slow, reviewing takes too long, the form asks questions that aren't drafted anywhere, exporting and submitting is fiddly, or you've simply lost interest in the posting. Each of these points to different projects. Phase 0 asks you to record the actual reason for each stalled application before we commit to phase 1's scope.

## Goals

1. Record every status change, so the metric above can be measured.
2. Clear the queue of 23 started applications: each one either submitted or withdrawn.
3. Keep the queue small afterwards: an application should go from started to submitted in days, not weeks.

### Success criteria

- Phase 0 is done when status history is recorded and each of the 23 stalled applications has a noted reason.
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
- **Ashby and Lever:** unverified. As far as we know, their public APIs don't expose application questions (see open questions).

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

Effort: **S** is a few hours to a day, **M** is a few days. Each project is its own issue and PR, following the repo's one-issue-per-PR workflow.

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

## Deferred

| Project | Why deferred |
|---|---|
| Finding companies (add from careers URL, guess board from name, `find-companies` skill) | 2,000 open postings already outnumber what gets submitted by orders of magnitude. More companies make the backlog worse. |
| Interview stages in the TUI, upcoming interviews, interview prep, pipeline stats | No application has reached interviewing. Revisit when one does. Status history (phase 0) is the groundwork. |
| Agent fit scoring | Useful for ordering the triage inbox, so it follows phase 3. |
| Pay import (Ashby compensation, Lever salary) | A nice filter, but it doesn't affect submissions. It fits in phase 3 if cheap. |
| Tags | No use case yet. The tables stay unused. |
| Base resume plus tailored changes; carrying review lessons into the profile | Worth it once review volume is the bottleneck, which it isn't at 8 reviews. |

## Work breakdown

Filed 2026-09-30. Every issue carries the `rfc-0002` label, and an `rfc0002-step-N` label giving the order to work in. List them with `gh issue list --label rfc-0002`.

| Step | Issue | Phase | Work | Size | Depends on |
|---|---|---|---|---|---|
| 1 | #162 | 0 | Application status history, written on every status-changing path, with a backfill | S–M | — |
| 2 | #163 | 0 | Note why each started application stalled; withdraw or keep (you, no code) | S | — |
| 3 | #164 | 1 | Home screen: age at status and next step, oldest first | S–M | #162 |
| 4 | #165 | 1 | `apply-to-posting` drafts several started applications per session; `list_postings` includes started applications that aren't marked interested | M | #117 |
| 5 | #166 | 1 | Submit flow on application detail: open, export, confirm, mark submitted | M | #162 |
| 6 | #167 | 2 | Spike: Ashby/Lever application forms (half a day) | S | — |
| 7 | #168 | 2 | Greenhouse application requirements fetched and stored at `stage_prepare` | M | — |
| 8 | #169 | 2 | Answers document type | M | RFC 0004 steps 1–3 (not yet filed), #168 |
| 9 | #170 | 2 | Reusable standard answers | S–M | Open question 3 decided |
| 10 | #171 | 3 | Reliable workplace type (normalize; remote from Greenhouse locations) | S | Phase 1 criteria met |
| 11 | #172 | 3 | Seen state and a cross-company triage inbox | M | Phase 1 criteria met |
| 12 | #173 | 3 | Global title and workplace rules at display time | M | #171 |

- **Found reviewing this RFC** (label `rfc0002-related`, not in the order above):
  - #174: decide open question 1, closing a submitted application, and whether reopening a posting restores its application;
  - #175: an application can be started on an already-closed posting;
  - #176: `last_seen_at` only changes with content, which blocks open question 5.
- **Changes from the proposal above:**
  - Reliable workplace type moved first in phase 3, since the workplace rule depends on it.
  - The stalled-applications view extends the existing home screen.
  - Batch drafting also fixes `list_postings` missing started applications that aren't marked interested.
- **Resolved since the RFC was written:** Mattermost and Livekit fetch normally again (2026-09-30). The deleted-companies finding is folded into #172, since nothing visible is affected today.
- **The answers document (#169) needs RFC 0004's work first,** and that isn't filed yet. It should be broken into issues before step 8.

## Related findings (separate issues, not part of this RFC)

- **Deleted companies' postings stay open.** Outschool was deleted on 2026-09-29, but its 6 postings are still `open`, so they count toward open totals.
- **Two companies haven't been fetched since 2026-09-24.** Mattermost and Livekit, while others were fetched on 09-28 and 09-29. It may just be how those fetches were run. Worth checking now that `swamp fetch` reports failures (#145).
- **Already fixed:** the `published_at` scan error that an earlier draft listed as a blocker was fixed in #141 (migration 00012).

## Open questions

1. **Should a closing posting still move a *submitted* application to `posting_closed`?** Issue #105 made that deliberate. With status history the submission isn't lost, but "submitted, then posting closed" arguably means "still waiting on a response."
2. **Phase 1 scope:** which projects do the phase 0 notes actually call for?
3. **Where standard answers live:** a section in `PROFILE_REFERENCE.md` (free text, the agent adapts it) or a structured file (exact reuse, and the TUI could show it next to each question)?
4. **Ashby/Lever questions:** is a timeboxed spike worth it, before designing manual entry?
5. **Time pressure:** how long do postings stay open? A rough estimate from `published_at` to `last_seen_at` is about a month. Some applications' `last_seen_at` falls *before* they were started, though, so what `last_seen_at` records needs checking before relying on it.

## Out of scope

- Submitting applications automatically. It's against the "never submit" rule in `apply-to-posting`, and fragile across boards.
- Email or calendar integration.
- Job boards beyond Ashby, Greenhouse and Lever.
