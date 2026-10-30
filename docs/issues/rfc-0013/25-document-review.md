---
title: "[RFC 0013 step 25] Document reviews are Event[Changed[Review]]"
labels: [rfc-0013, rfc0013-step-25]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Adopting: Still in tui", "Decisions: Payloads (How an error is carried)"]
depends_on: [19, 23]
---

RFC 0013, step 5 (the review picker and form).

## Problem

The picker is opened from posting detail and replaced by the form (`enterDocumentReviewFormMsg`), which application detail can also open. The form's result is `documentReviewCreatedMsg`, or `documentChangedDuringReviewMsg` when the document changed under it. The screens that show reviews read them again on coming back on top.

## Change

- A `Review` snapshot; the form sends `Changed[Review]`, its Subject the review's application, or `Error`. A document that changed is `Error` with a new code, `document_changed`, whose text says who changed it; the form reads the document again and shows the diff.
- `Nav[Application]` pushes the picker. It sends `Replace` to the form with the document it picked, as `Nav[DocumentReview]`: the content read when it was picked, which is what the review describes. Application detail pushes the form the same way.
- Posting detail, application detail and the home screen route `Changed[Review]` `About` their application. Posting detail checks the application in the handler, since its application can change while it's open.
- Remove `enterDocumentReviewSelectMsg`, `enterDocumentReviewFormMsg`, `cancelDocumentReviewSelectMsg`, `cancelDocumentReviewFormMsg`, `documentReviewCreatedMsg` and `documentChangedDuringReviewMsg`, and `returnStack`, which the review screens were the last to use.

## Acceptance criteria

- [ ] A review shows on every screen that shows its application.
- [ ] A review refused because the document changed says so and keeps the form open.
- [ ] Manual check against real data.
- [ ] Verification checklist clean.
