---
name: apply-to-posting
description: Draft a tailored cover letter and resume for a job posting tracked in Swamp (the job-search tool in this repo), using the `swamp` MCP server's tools, the user's profile and their canonical resume. Use this skill whenever the user asks to work on job applications, wants to draft a cover letter or resume for a posting, wants to work through their "interested" postings queue, or asks what to apply to next -- even if they don't mention "stage" or MCP by name. Always confirm which posting to work on and show drafts for review before finishing; never submit an application or advance its status.
---

# Apply to a posting

This skill turns a posting the user has marked "interested" in Swamp (or
started an application for) into a
drafted, tailored cover letter and resume. It is deliberately
interactive-first: a human picks which posting to work on and reviews what
gets written before anything is considered done. There is no autonomous or
scheduled mode -- only run this with a person present in the session.

All Swamp data access goes through the `swamp` MCP server, declared in
this repo's `.mcp.json`. Its tool definitions are the source of truth for
arguments and behavior -- this skill only says which tool to use when.
Don't read or write the sqlite db or the assets directory directly.

The server has to be running on the host before the session starts
(`task mcp-serve`). If the `swamp` tools aren't available or calls fail to
connect, stop and tell the user to start it rather than falling back to
anything else. See `decisions.log` for the `host.container.internal`
DNS/bind-address details if the server seems unreachable from inside a
container.

## 1. Discover eligible postings

Call `list_postings`. It's read-only, so call it as often as you like.

Each result's `Posting` is a summary (ID, title, department, location,
workplace type, application URL), without the description: that comes
from `stage_prepare` once a posting is picked.

Each result may carry `ApplicationNotes` (the user's free-text notes) and
`LatestReviews`, the most recent human review per document type. **A
posting with a `"flagged"` review stays in the list even when both
documents exist** -- that's the signal to revise, not draft fresh; the
review's `Notes` say what needs fixing (see step 2).

Show the user the list -- title, company, location, and whether anything's
flagged is usually enough -- and ask which one to work on. Don't pick for
them: committing to a posting is the user's call, not an inference you
make from the queue.

If the user asks to work through several at once -- e.g. "draft the
started applications that have no drafts" -- follow the batch flow
below instead of picking one.

### When the user names a posting or application

The user often names one in their own words, e.g. "my application to the
Staff Developer role at Acme", copied from the TUI, which shows no IDs.
`list_postings` is only the drafting queue: an application that's
drafted, submitted or closed isn't in it. Find it by what it is:

1. Call `list_companies` and match the company name the user gave,
   ignoring case. If none matches, or several could, ask.
2. Call `search_postings` with that `CompanyID`, plus filters the user's
   words imply:
   - "my application", "the one I applied to": `HasApplication: true`;
   - a status ("the one I submitted"): `ApplicationStatuses`;
   - a posting that may have closed: `ListingStatus: "any"`.
3. Read the full titles in the results (the TUI truncates them, and
   several roles at one company often share a prefix) and match the
   user's words against them:
   - one match: confirm it with the user -- company, full title,
     location, application status -- before going on;
   - several: show them with full titles and ask which;
   - none: say so, and retry with `ListingStatus: "any"` or without the
     application filters before giving up.
4. If `NextCursor` isn't null, there are more pages: pass it back as
   `Cursor`, with the same filters, for the next. If `Total` is large,
   narrow the filters rather than paging through everything.

The match's `Posting.ID` is what `stage_prepare` takes, and its
`ApplicationID` (when it has one) what `read_document` and
`write_document` take.

For the user's shortlist -- "what have I marked interested but not
started?" -- call `search_postings` with `Interested: true` and
`HasApplication: false` (or `true` for "interested ones I've started"),
paging the same way.

## 2. Commit to the posting

Once the user names a posting, call `stage_prepare` for it. This is the
one mutating step before drafting.

If it fails with "posting is closed", the posting is no longer listed on
its board, so there's no form left to submit and Swamp won't start an
application on it. Tell the user, don't retry, and offer to pick another
posting.

Its `Documents` field has one entry per document type, keyed by the
type's name (`cover_letter`, `resume`), each with a `Path` and whether it
`Exists`. Use those names as `DocumentType` with `read_document` and
`write_document`. Treat the paths as informational -- they're locations
on the host running the server, not something to open or write yourself.

Its `ApplicationForm` is what the posting's application form asks for, when
Swamp has it: fetched from Greenhouse, or entered by hand in the TUI for
Ashby and Lever postings (application detail, `f`). It's `null` when Swamp
has neither:

- `ApplicationForm.Documents` gives each document as `"required"`,
  `"optional"` or `"absent"` (a document missing from it is absent).
- `ApplicationForm.Questions` lists the form's custom questions, each with
  `Label`, `Required`, `Type` and, for a select, its `Options`. A question
  entered by hand has an empty `Type` and no `Options`: treat it as free
  text.
- If `ApplicationFormError` is set, the form couldn't be read: say so, and
  carry on as if it were `null`.

If a document already exists, check `LatestReviews` first:

- A flagged review with `Notes` set: this is a **revision**, not a fresh
  draft. Read the existing draft with `read_document`, then write a
  version that addresses the notes -- don't silently start from a blank
  page.
- No review yet, or the only review passed: tell the user and ask before
  overwriting it -- don't silently clobber drafted work you can't see.

## 3. Read the background sources

Two files the user writes and maintains themselves. Both are read-only
for you: never edit them, and don't look for them on disk.

**The profile: call `read_profile`.** It holds the user's real
experience, skills, and usually a "Voice & Style Notes" section or
similar covering tone and conventions to write in. Read and follow
whatever guidance is actually in it rather than assuming its structure in
advance; it's the user's document and may change.

**If `Exists` is false, stop and tell the user** (the README says how to
set it up, from `docs/canonical/profile.md.example`) rather than drafting
from general knowledge or assumptions about their background. The entire
point of this step is that the draft comes from real, user-provided
material -- a cover letter written without it isn't a shortcut, it's a
different (and much worse) task.

**The canonical resume: call `read_canonical_resume`** when you'll draft
a resume. It's the user's maintained, best-version resume: the baseline
you tailor, not something to rewrite. If `Exists` is false, carry on
drafting the resume from the profile alone, and tell the user that
setting one up (from `docs/canonical/resume.md.example`) would give
steadier resumes.

## 4. Draft the documents

Write each document the form uses, in markdown, tailored to this specific
posting:

- With an `ApplicationForm`: draft the `"required"` documents, and the
  `"optional"` ones too (an optional cover letter is still worth sending),
  telling the user which were optional. Don't draft an `"absent"` one: the
  form has nowhere to put it.
- Without one: draft every document in `Documents`, as the form is
  unknown. For an Ashby or Lever posting, you can mention that
  the user can enter its form in the TUI (application detail, `f`) so you
  draft only what it asks for.

For each document:

- Pull the posting's actual content (title, company, description, any
  specifics worth responding to) from what `stage_prepare` returned. The
  description is its plain-text `DescriptionText`.
- Pull background, framing, and voice from the profile -- don't invent
  experience, skills, or achievements that aren't in it or the canonical
  resume. If the posting wants something neither covers, that's worth
  surfacing to the user rather than papering over.
- Match the voice/style guidance in the profile as closely as you can;
  it exists precisely so drafts don't need a separate editing pass to
  sound like the user.

For the resume, when there's a canonical one, **tailor it rather than
writing a new one**: keep its structure, facts, dates and titles, and
change only what fits it to the posting -- reorder roles' bullets so the
most relevant lead, trim what doesn't serve this posting, rephrase a
bullet toward the posting's language, and adjust the profile summary.
Material from the profile may replace or add a bullet when it fits the
posting better. Tell the user what you changed from the canonical
version, briefly, so they review a diff rather than a whole new resume.

A revision (a flagged review, step 2) still starts from the existing
draft, not the canonical resume: the notes are about that draft.

Save each one with `write_document`.

The custom questions aren't drafted yet (an answers document is coming,
#169). List the required ones for the user in the review checkpoint, so
they aren't a surprise on the form.

## 5. Review checkpoint

Show the user what you wrote -- the content itself, or at minimum a
summary plus where each document was saved -- and stop there. Do not:

- mark the application submitted or change its status (that's a manual
  action in the Swamp TUI, entirely outside this skill's scope)
- move on to the next posting from the list without being asked (the
  batch flow below is the user asking, for the postings they confirmed)
- treat a draft as finished before the user has actually seen it

This is what "interactive-first" means in practice: the workflow produces
a draft for a human to react to, not a finished output to hand off.

## Batch: several postings in one session

Use this only when the user asks for it. It runs the same steps for each
posting, with one confirmation up front and one review at the end
instead of one of each per posting.

1. **Build the list.** From `list_postings`, take what the user asked
   for -- usually the started applications (`ApplicationStatus` is
   `"application_started"`) with no drafts yet. You can't see which
   documents exist from the list; `stage_prepare` tells you, so a
   posting that turns out to already have drafts is reported, not
   redrafted (see 4).
2. **Confirm it.** Show the list (title and company per posting) and ask
   the user to confirm or trim it. Don't start until they do. Work only
   on the postings they confirmed.
3. **Read the background sources once** (step 3), before the first
   posting: `read_profile`, and `read_canonical_resume` if any posting
   needs a resume. If the profile is missing, stop before drafting
   anything.
4. **For each posting, in order:** call `stage_prepare` (step 2), then
   draft and save its documents (step 4). The step-2 rules still apply
   to every posting:
   - a flagged review means **revise** the existing draft with
     `read_document`, not start over;
   - a document that already exists without a flagged review is **not
     overwritten**: skip it and note it for the summary, rather than
     stopping the batch to ask.
   If something fails for one posting, note it and carry on with the
   next.
5. **Stop with one summary** (step 5): per posting, what was drafted,
   revised or skipped and why, with where each document was saved. The
   user reviews from there. Don't start another batch or anything else
   unasked.

The guardrails don't change in a batch: never submit anything, never
change an application's status, never overwrite a draft without asking.
