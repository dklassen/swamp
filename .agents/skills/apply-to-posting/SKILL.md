---
name: apply-to-posting
description: Draft a tailored cover letter and resume for a job posting tracked in Swamp (the job-search tool in this repo), using the `swamp` MCP server's tools and the user's PROFILE_REFERENCE.md background file. Use this skill whenever the user asks to work on job applications, wants to draft a cover letter or resume for a posting, wants to work through their "interested" postings queue, or asks what to apply to next -- even if they don't mention "stage" or MCP by name. Always confirm which posting to work on and show drafts for review before finishing; never submit an application or advance its status.
---

# Apply to a posting

This skill turns a posting the user has marked "interested" in Swamp into a
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

Each result may carry `ApplicationNotes` (the user's free-text notes) and
`LatestReviews`, the most recent human review per document type. **A
posting with a `"flagged"` review stays in the list even when both
documents exist** -- that's the signal to revise, not draft fresh; the
review's `Notes` say what needs fixing (see step 2).

Show the user the list -- title, company, location, and whether anything's
flagged is usually enough -- and ask which one to work on. Don't pick for
them: committing to a posting is the user's call, not an inference you
make from the queue.

## 2. Commit to the posting

Once the user names a posting, call `stage_prepare` for it. This is the
one mutating step before drafting.

Treat the document paths it returns as informational -- they're locations
on the host running the server, not something to open or write yourself.
If a document already exists, check `LatestReviews` first:

- A flagged review with `Notes` set: this is a **revision**, not a fresh
  draft. Read the existing draft with `read_document`, then write a
  version that addresses the notes -- don't silently start from a blank
  page.
- No review yet, or the only review passed: tell the user and ask before
  overwriting it -- don't silently clobber drafted work you can't see.

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

Save each one with `write_document`.

## 5. Review checkpoint

Show the user what you wrote -- the content itself, or at minimum a
summary plus where each document was saved -- and stop there. Do not:

- mark the application submitted or change its status (that's a manual
  action in the Swamp TUI, entirely outside this skill's scope)
- move on to the next posting from the list without being asked
- treat a draft as finished before the user has actually seen it

This is what "interactive-first" means in practice: the workflow produces
a draft for a human to react to, not a finished output to hand off.
