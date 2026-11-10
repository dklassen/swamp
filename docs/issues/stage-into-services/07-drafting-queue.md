---
title: "[Stage into services 7] The drafting queue is one DraftingService query"
labels: [stage-into-services]
depends_on: [2, 12]
---

## Problem

`stage.List` (`stage/stage.go:276`) decides which postings still need drafting:

- interested, non-archived postings with no terminal application (`PostingService.Interested`),
- plus started applications whose posting wasn't marked interested and isn't archived (`startedNotInterested`, `:302`),
- minus any whose documents are all on disk (`allDrafted`, `:445`) with no current review flagged (`needsRework`, `:165`).

That's business logic spread over three services, and it costs queries:

- **Archived check.** `PostingService.Markup` is called once per started application.
- **Reviews.** `ApplicationService.Active` already loads each application's reviews, which `startedNotInterested` throws away. `candidate` (`:337`) then loads them again for every posting.
- **Notes.** `postings.Application` is called once per posting only to read `Notes`, which `ActiveApplication` already embeds.
- **The rebuild.** `startedNotInterested` rebuilds `posting.Interested` by hand so the two lists share a shape.

## Change

- `DraftingService.Queue`, over repositories only, returns the queue in `List`'s order (interested first, then started-not-interested), each entry with its posting, company name, application (if any) with notes, and current reviews.
- "Every document drafted and none flagged" is decided in the service; `needsRework` becomes a domain function on the review map (`internal/domain/document`).
- `stage.List` becomes a mapping to `Candidate`. `startedNotInterested`, `candidate`, `allDrafted` and `needsRework` leave `stage`.

## Decide before starting

- **Whether the archived and started filtering moves into SQL.** It can be one query (SQL may cross tables, RFC 0011), or the service can merge two queries in Go without the per-row lookups. Recommended: start in Go and remove the per-row calls; a single query is a later change if it's ever slow.

## Acceptance criteria

- [x] Service tests, one behaviour at a time, moved from `stage/stage_test.go`'s `List` tests: interested shown; archived left out; started-not-interested shown after interested; terminal left out; fully drafted and unflagged left out; fully drafted with a current flagged review shown; a flag on an earlier draft doesn't count.
- [x] No service call per queue entry in the service or in `stage`.
- [x] `list_postings`' JSON in `mcpserver_test.go` unchanged.
- [x] Verification checklist clean.
