---
name: apply-to-posting
description: Draft a tailored cover letter and resume for a job posting tracked in Swamp (the job-search CLI/TUI in this repo), using the `swamp stage list`/`stage prepare` mechanism and the user's PROFILE_REFERENCE.md background file. Use this skill whenever the user asks to work on job applications, wants to draft a cover letter or resume for a posting, wants to work through their "interested" postings queue, or asks what to apply to next -- even if they don't mention "stage" or the CLI by name. Always confirm which posting to work on and show drafts for review before finishing; never submit an application or advance its status.
---

# Apply to a posting

This skill turns a posting the user has marked "interested" in Swamp into a
drafted, tailored cover letter and resume. It is deliberately
interactive-first: a human picks which posting to work on and reviews what
gets written before anything is considered done. There is no autonomous or
scheduled mode -- only run this with a person present in the session.

Run every command below from the repo root (`cd
/Users/dana/Documents/Code/lang/go/swamp` if you're not already there), and
prefix each `swamp` invocation with `direnv exec .` so `SWAMP_DB_PATH` and
`SWAMP_DOCUMENTS_PATH` are set from `.envrc`. Use `go run ./cmd/swamp
<args>` rather than assuming a built binary exists.

**If you don't have shell access to this repo** (e.g. you're running in a
container that can't reach the swamp binary or its sqlite db directly),
use the `swamp` MCP server instead -- it's declared in this repo's
`.mcp.json` and exposes the same three operations this skill needs as
tools, over the same JSON shapes documented below (except that
`list_postings` wraps the array in an object: `{"Postings": [...]}`, since
MCP requires structured tool results to be objects):

| CLI (steps below)             | MCP tool          | Arguments                                           |
| ------------------------------ | ------------------ | ---------------------------------------------------- |
| `swamp stage list`             | `list_postings`   | none                                                 |
| `swamp stage prepare <id>`     | `stage_prepare`   | `PostingID`                                          |
| writing the drafted file directly | `write_document`  | `ApplicationID`, `DocumentType` (`cover_letter`\|`resume`), `Content` |

The MCP path needs `swamp mcp-serve` actually running on the host first
(`task mcp-serve`, or `direnv exec . go run ./cmd/swamp mcp-serve`) --
unlike the CLI, this is a persistent server, not something spawned
per-call. See `decisions.log` for why MCP/Streamable-HTTP is the
mechanism here rather than gRPC or a REST API, and for the
`host.container.internal` DNS/bind-address details if the server seems
unreachable from inside a container.

## 1. Discover eligible postings

```
direnv exec . go run ./cmd/swamp stage list
```

This is read-only -- safe to run as often as you like. It prints a JSON
array of postings the user has marked interested and not archived, already
filtered to exclude applications at a dead-end status (rejected, withdrawn,
posting closed, offer declined) and anything that already has both documents written *and*
no outstanding flagged review (see `LatestReviews` below -- a flagged
document keeps its posting in this list even once both files exist):

```json
{
  "Posting": {
    "ID": 1, "CompanyID": 1, "Source": "ashby", "SourceID": "job-1",
    "Title": "Senior Backend Engineer", "Location": "Remote",
    "DescriptionText": "...", "JobURL": "...", "ApplicationURL": "...",
    "...": "other fields you likely won't need for drafting"
  },
  "CompanyName": "Acme",
  "ApplicationID": null,
  "ApplicationStatus": null,
  "ApplicationNotes": "",
  "LatestReviews": {}
}
```

`ApplicationID`/`ApplicationStatus` are `null` until an application has been
started; a non-null status here means someone already started this one
(e.g. `"application_started"`) but hasn't finished its documents.

`ApplicationNotes` is the user's free-text notes on the application (empty
string until one exists or until they've written any). `LatestReviews` is
keyed by document type (`"cover_letter"`, `"resume"`) and holds the most
recent human review of that document, if any:

```json
"LatestReviews": {
  "cover_letter": {
    "Outcome": "flagged",
    "Notes": "too generic -- mention specific Go/distributed systems experience",
    "Cycle": 1,
    "CreatedAt": "2026-09-04T10:00:00Z"
  }
}
```

`Outcome` is `"passed"` or `"flagged"`. A document with no key in
`LatestReviews` hasn't been reviewed yet. **A posting can still appear in
this list even when both documents already exist on disk, if the latest
review of either one is `"flagged"`** -- that's the signal to revise, not
draft fresh: read `LatestReviews[...].Notes` for what specifically needs
fixing, and read the existing file's content (once you commit to the
posting in step 2) as your starting point rather than writing from scratch.

Show the user the list -- title, company, location, and whether anything's
flagged is usually enough -- and ask which one to work on. Don't pick for
them: the whole point of this step being separate from the next one is that
committing to a posting is the user's call, not an inference you make from
the queue.

## 2. Commit to the posting

Once the user names a posting (by its `Posting.ID`):

```
direnv exec . go run ./cmd/swamp stage prepare <posting-id>
```

This is the one mutating step in the whole workflow, and it's idempotent --
safe to re-run if you need to fetch this information again later in the
same session. It creates the application record if one doesn't exist yet
and makes sure the assets directory is there, then prints:

```json
{
  "Posting": { "...": "same shape as above" },
  "CompanyName": "Acme",
  "ApplicationID": 1,
  "CoverLetter": { "Path": "/abs/path/assets/1/cover_letter.md", "Exists": false },
  "Resume": { "Path": "/abs/path/assets/1/resume.md", "Exists": false },
  "ApplicationNotes": "",
  "LatestReviews": {}
}
```

`ApplicationNotes`/`LatestReviews` are the same shape as step 1's -- read
again here since this is the object you're about to draft from. If
`CoverLetter.Exists` or `Resume.Exists` is already `true`, someone (you, in
an earlier run, or the user directly) already wrote that file. Check
`LatestReviews` first:

- A flagged review with `Notes` set: this is a **revision**, not a fresh
  draft. Read the existing file's current content, read what the notes say
  needs fixing, and write a revised version that addresses it -- don't
  start from a blank page.
- No review yet, or the only review passed: tell the user and ask before
  overwriting it -- don't silently clobber drafted work you can't see the
  value of from the JSON alone.

## 3. Read the background source

Read `PROFILE_REFERENCE.md` at the repo root. This is the user's own,
untracked file -- it holds their real experience, skills, and usually a
"Voice & Style Notes" section or similar covering tone and conventions to
write in. Read and follow whatever guidance is actually in the file rather
than assuming its structure in advance; it's the user's document and may
change.

**If the file doesn't exist, stop and tell the user** rather than drafting
from general knowledge or assumptions about their background. The entire
point of this step is that the draft comes from real, user-provided
material -- a cover letter written without it isn't a shortcut, it's a
different (and much worse) task.

**No MCP tool exposes this file yet.** The `swamp` MCP server (see the
table above) only covers steps 1, 2, and 4's write -- if you're on the
MCP path because you have no filesystem access at all, there's currently
no way to complete this step, and the skill can't proceed past it as-is.
Tell the user rather than drafting without the profile.

## 4. Draft the documents

Write a cover letter and a resume, both in markdown, tailored to this
specific posting:

- Pull the posting's actual content (title, company, description, any
  specifics worth responding to) from what `stage prepare` returned.
- Pull background, framing, and voice from `PROFILE_REFERENCE.md` --
  don't invent experience, skills, or achievements that aren't in there.
  If the posting wants something the profile doesn't cover, that's worth
  surfacing to the user rather than papering over.
- Match the voice/style guidance in the profile file as closely as you
  can; it exists precisely so drafts don't need a separate editing pass
  to sound like the user.

Write the cover letter to `CoverLetter.Path` and the resume to
`Resume.Path` from step 2's output -- directly, if you have filesystem
access to the repo. Otherwise, call the `write_document` MCP tool once per
document (`ApplicationID` from step 2's output, `DocumentType` set to
`"cover_letter"` or `"resume"`, `Content` the full document text), which
has the same effect.

## 5. Review checkpoint

Show the user what you wrote -- the content itself, or at minimum a
summary plus the two file paths -- and stop there. Do not:

- mark the application submitted or change its status (that's a manual
  action in the Swamp TUI, entirely outside this skill's scope)
- move on to the next posting from the list without being asked
- treat a draft as finished before the user has actually seen it

This is what "interactive-first" means in practice: the workflow produces
a draft for a human to react to, not a finished output to hand off.
