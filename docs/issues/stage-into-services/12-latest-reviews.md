---
title: "[Stage into services 12] The latest review of each document type is a repository read"
labels: [stage-into-services]
depends_on: []
---

## Problem

"An application's latest review of each document type" is a read of `document_reviews`, but `DocumentReviewRepository` has no primitive for it. Services build it by hand: loop over `document.Types()`, `List` each type's reviews, keep the first.

- `DocumentService.LatestReviews` (`internal/service/document.go:128`), through `LatestReview`.
- `ApplicationService.Active` (`internal/service/application.go:89`), once per active application.
- `DraftingService` would be the third copy, in steps 6 and 7.

Each loads every review of every type to keep one per type.

## Change

- `DocumentReviewRepository.Latest(ctx, r, applicationID)` returns the latest review of each document type that has one, as a `map[document.Type]document.Review`, from one SQL query returning the latest row per type.
- `DocumentService.LatestReviews` and `ApplicationService.Active` call it. Keeping only the reviews still current on disk (`documents.Current`) stays in the services, since it's a rule.

## Acceptance criteria

- [ ] The existing `LatestReviews` and `Active` service tests pass unchanged; a new behaviour gets a service test first.
- [ ] No service loops over `document.Types()` to find latest reviews.
- [ ] Verification checklist clean.
