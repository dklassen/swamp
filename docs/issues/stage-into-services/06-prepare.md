---
title: "[Stage into services 6] Starting to draft a posting is one DraftingService call"
labels: [stage-into-services]
depends_on: [2, 3, 4, 5, 12]
---

## Problem

`stage.Prepare` (`stage/stage.go:356`) commits to drafting a posting by stitching together five services and the filesystem:

- **Get or create the application.** It calls `postings.Application`, then `ApplyTo` on `store.ErrNotFound`: two calls with no transaction around them. Two `stage_prepare` calls at once can both find no application, and the second `ApplyTo` fails.
- **The posting and company.** Two reads, `postings.Get` then `companies.Get`, so a posting at a deleted company fails with a bare "get company: not found" instead of step 5's `CompanyDeletedError`.
- **The document folder and versions.** It calls `EnsureDir`, then `SHA256` for each type, with `NoDocumentYet` standing in for a missing file.
- **Current reviews.** `DocumentService.LatestReviews` reads each document to decide which reviews are current, then `stage` reads and hashes each again for its version. An edit between the two reads can report a review as current against a version it didn't see.
- **The form.** `PostingService.CurrentForm`, whose only caller this is.

"Start or resume drafting this posting" is one step of the drafting process, and it's in `stage`.

## Change

- A new `DraftingService`, taking the database, the `documents.Store`, the form fetchers and `FormConfig`. It calls repositories only, never another service.
- `DraftingService.Prepare`, in one transaction: reads the posting with its company (`PostingRepository.GetWithCompany`), refusing a deleted company with `CompanyDeletedError`; returns the posting's application, or creates it with its `application_started` history row, refusing a closed posting with `ErrPostingClosed`.
- Then it ensures the folder, reads each document once for its path, existence and version, keeps the latest reviews (step 12) that match those versions, and reads the form.
- `CurrentForm`, the form fetchers and `FormConfig` move from `PostingService` to `DraftingService`; `NewPostingService` takes only the database again.
- `stage.Prepare` becomes a mapping from that result to `Prepared`'s JSON.

## Acceptance criteria

- [ ] Service tests, one at a time: a first call creates the application and folder, a second reuses both, a closed posting is refused with `ErrPostingClosed`, a deleted company's posting is refused with `CompanyDeletedError`, versions name `NoDocumentYet` for a missing document and the hash for a present one, a review of an earlier draft isn't returned.
- [ ] A test that a posting whose application already exists returns it, not an error.
- [ ] `CurrentForm`'s tests move to `DraftingService` unchanged in what they check.
- [ ] `stage_prepare`'s JSON in `mcpserver_test.go` unchanged.
- [ ] Verification checklist clean.
