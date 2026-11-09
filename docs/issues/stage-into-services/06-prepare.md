---
title: "[Stage into services 6] Preparing a posting is one service call"
labels: [stage-into-services]
depends_on: [2, 3, 4, 5]
---

## Problem

`stage.Prepare` (`stage/stage.go:375`) commits to drafting a posting by stitching together five services and the filesystem:

- **Get or create the application.** It calls `postings.Application`, then `ApplyTo` on `store.ErrNotFound`: two calls with no transaction around them. Two `stage_prepare` calls at once can both find no application, and the second `ApplyTo` fails.
- **The document folder and versions.** It calls `EnsureDir`, then `SHA256` for each type, with `NoDocumentYet` standing in for a missing file.
- **The rest.** Current reviews (step 2), the application form (step 4), and the posting and company (step 5).

"Start or resume drafting this posting" is one use case, and it's in `stage`.

## Change

- A service method does it: gets or creates the application in one transaction (`ApplyTo`'s closed-posting check included), ensures its folder, and returns the posting, company, application, each document's path, existence and version, the current reviews, and the form with any fetch error.
- `stage.Prepare` becomes a mapping from that result to `Prepared`'s JSON.

## Decide before starting

- **Which service.** It's keyed by a posting, so `PostingService` by AGENTS.md's rule ("the service whose question it answers"). It reads documents and reviews, so it needs the `documents.Store` (step 2) and the review repository, as `ApplicationService.Active` already uses it.
- **Its name**: `Prepare`, `StartDrafting`, `Draft`…

## Acceptance criteria

- [ ] Service tests, one at a time: a first call creates the application and folder, a second reuses both, a closed posting is refused with `ErrPostingClosed`, a deleted company's posting is refused with step 5's error, versions name `NoDocumentYet` for a missing document and the hash for a present one.
- [ ] A test that a get-or-create on a posting whose application already exists returns it, not an error.
- [ ] `stage_prepare`'s JSON in `mcpserver_test.go` unchanged.
- [ ] Verification checklist clean.
