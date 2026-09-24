---
name: apply-to-posting
description: Draft a tailored cover letter and resume for a job posting tracked in Swamp (the job-search tool in this repo), using the `swamp` MCP server's `list_postings`/`stage_prepare`/`write_document` tools and the user's PROFILE_REFERENCE.md background file. Use this skill whenever the user asks to work on job applications, wants to draft a cover letter or resume for a posting, wants to work through their "interested" postings queue, or asks what to apply to next -- even if they don't mention "stage" or MCP by name. Always confirm which posting to work on and show drafts for review before finishing; never submit an application or advance its status.
---

# Apply to a posting

This skill turns a posting the user has marked "interested" in Swamp into a
drafted, tailored cover letter and resume. It is deliberately
interactive-first: a human picks which posting to work on and reviews what
gets written before anything is considered done. There is no autonomous or
scheduled mode -- only run this with a person present in the session.

All Swamp data access goes through the `swamp` MCP server, declared in
this repo's `.mcp.json`. Don't read or write the sqlite db or the
assets directory directly -- these tools are the only interface this
skill uses:

| Step                   | MCP tool         | Arguments                                                              |
| ---------------------- | ---------------- | ---------------------------------------------------------------------- |
| 1. Discover postings   | `list_postings`  | none                                                                   |
| 2. Commit to a posting | `stage_prepare`  | `PostingID`                                                            |
| 4. Save each document  | `write_document` | `ApplicationID`, `DocumentType` (`cover_letter`\|`resume`), `Content` |

The server has to be running on the host before the session starts
(`task mcp-serve`) -- it's a persistent Streamable HTTP server, not
something spawned per call. If the `swamp` tools aren't available or calls
fail to connect, stop and tell the user to start it rather than falling
back to anything else. See `decisions.log` for why MCP is the mechanism
here, and for the `host.container.internal` DNS/bind-address details if
the server seems unreachable from inside a container.

## 1. Discover eligible postings

Call `list_postings` (no arguments).

This is read-only -- safe to call as often as you like. It returns
`{"Postings": [...]}`: the postings the user has marked interested and not
archived, already filtered to exclude applications at a dead-end status
(rejected, withdrawn, posting closed, offer declined) and anything that
already has both documents written *and* no outstanding flagged review
(see `LatestReviews` below -- a flagged document keeps its posting in this
list even once both documents exist). Each element looks like:

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
this list even when both documents already exist, if the latest review of
either one is `"flagged"`** -- that's the signal to revise, not draft
fresh: read `LatestReviews[...].Notes` for what specifically needs fixing
(see step 2 for how to handle the revision).

Show the user the list -- title, company, location, and whether anything's
flagged is usually enough -- and ask which one to work on. Don't pick for
them: the whole point of this step being separate from the next one is that
committing to a posting is the user's call, not an inference you make from
the queue.

## 2. Commit to the posting

Once the user names a posting, call `stage_prepare` with `PostingID` set to
its `Posting.ID`.

This is the one mutating step before drafting, and it's idempotent -- safe
to call again if you need to fetch this information later in the same
session. It creates the application record if one doesn't exist yet and
makes sure the document directory is there, then returns:

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

The `Path` values are locations on the host running the server --
informational only, not something to open or write to yourself.
`ApplicationNotes`/`LatestReviews` are the same shape as step 1's -- read
again here since this is the object you're about to draft from. If
`CoverLetter.Exists` or `Resume.Exists` is already `true`, someone (you, in
an earlier run, or the user directly) already wrote that document. Check
`LatestReviews` first:

- A flagged review with `Notes` set: this is a **revision**, not a fresh
  draft. No MCP tool returns a document's current content yet, so ask the
  user to paste the existing draft (or confirm you should redraft from
  scratch), then write a version that addresses what the notes say needs
  fixing -- don't silently start from a blank page.
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

This file isn't Swamp data and no MCP tool serves it -- read it from the
repo checkout as a plain file.

**If the file doesn't exist or you can't read it, stop and tell the user**
rather than drafting from general knowledge or assumptions about their
background. The entire point of this step is that the draft comes from
real, user-provided material -- a cover letter written without it isn't a
shortcut, it's a different (and much worse) task.

## 4. Draft the documents

Write a cover letter and a resume, both in markdown, tailored to this
specific posting:

- Pull the posting's actual content (title, company, description, any
  specifics worth responding to) from what `stage_prepare` returned.
- Pull background, framing, and voice from `PROFILE_REFERENCE.md` --
  don't invent experience, skills, or achievements that aren't in there.
  If the posting wants something the profile doesn't cover, that's worth
  surfacing to the user rather than papering over.
- Match the voice/style guidance in the profile file as closely as you
  can; it exists precisely so drafts don't need a separate editing pass
  to sound like the user.

Save each one by calling `write_document` -- once per document, with
`ApplicationID` from step 2's output, `DocumentType` set to
`"cover_letter"` or `"resume"`, and `Content` the full document text. It
replaces whatever is there and returns the `Path` it wrote to.

## 5. Review checkpoint

Show the user what you wrote -- the content itself, or at minimum a
summary plus the two `Path`s `write_document` returned -- and stop there.
Do not:

- mark the application submitted or change its status (that's a manual
  action in the Swamp TUI, entirely outside this skill's scope)
- move on to the next posting from the list without being asked
- treat a draft as finished before the user has actually seen it

This is what "interactive-first" means in practice: the workflow produces
a draft for a human to react to, not a finished output to hand off.
