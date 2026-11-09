---
title: "[Stage into services 2] DocumentService owns documents; reviews come back current"
labels: [stage-into-services]
depends_on: []
---

## Problem

A review counts only while it matches the file on disk. Since the services can't see the files, every caller filters reviews itself: `DocumentService.LatestReviews`, then `documents.Current(status, reviews, document.Review.IsCurrent)`. There are five copies:

- `stage/stage.go:341` (`candidate`) and `:404` (`Prepare`)
- `tui/writes.go:85` (`currentReviews`)
- `tui/active_applications.go:147` (filtering `ApplicationService.Active`'s reviews)
- `cmd/swamp/main.go:409` (`runExport`)

`ApplicationService.Active` also builds the latest-review map inline (`internal/service/application.go:83`), a second copy of `DocumentService.LatestReviews` that skips the on-disk check.

## Change

- `NewDocumentService` and `NewApplicationService` take the `*documents.Store`. `cmd/swamp` and `testutil.NewTestApp` pass it in.
- `DocumentService.LatestReviews` returns only reviews that are still current: a review of an earlier draft, or of a deleted file, is left out, the same as no review. Its five callers drop their `documents.Current` call.
- `ApplicationService.Active` returns current reviews the same way, and the TUI drops its filter.
- `internal/service/doc.go` says the services own the documents on disk as well as their transactions.

## Acceptance criteria

- [x] `documents.Current(…, document.Review.IsCurrent)` appears only inside `internal/service`.
- [x] A service test: a review of a document rewritten afterwards isn't returned; one of the document as it is, is.
- [x] The same, through `ApplicationService.Active`.
- [x] Existing stage, TUI and export tests pass, changed only to share the test app's documents store, to write the file a review is of, and to tighten one assertion that the new rule made vacuous.
- [x] Verification checklist clean.
