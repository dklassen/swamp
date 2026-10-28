---
title: "[RFC 0013 step 19] Posting detail is a stacked screen that owns its posting and application"
labels: [rfc-0013, rfc0013-step-19, rfc-0012]
rfc: docs/rfcs/0013-tui-change-messages.md
related_rfc: docs/rfcs/0012-tui-screen-router.md
rfc_sections: ["Adopting: Still in tui", "Adopting: Navigation without the model"]
depends_on: [13, 14, 17, 18]
---

RFC 0013, step 5 (posting detail). Related: #284, #285.

## Problem

Posting detail is opened from three screens by `enterPostingDetailMsg`, each seeding it differently (`tui/app.go:1568`, `:1600`). `App` loads its application, reviews and scores, moves it between siblings on `navigatePostingMsg`, reloads it on `refreshPostingDetailMsg`, and rebuilds it on each application write.

## Change

- `Nav[PostingDetail]` (the posting, its application if any, and the sibling posting IDs in list order, empty when opened outside the posting list) pushes it. It loads in `Init`: `Read[PostingApplication]`, `Read[ReviewList]`, `Read[PostingScores]`.
- Previous/next send `Replace` with the sibling's snapshot. `message.Replace` is new, beside `Push` and `Pop`, and the stack's `replace` pops and pushes in one step.
- It routes `Changed[Markup]` and `Changed[Application]` `About` its posting or application and patches itself.
- Its own `enter…` requests (status, notes, review select) stay `App`'s until steps 20 and 25.
- Remove `enterPostingDetailMsg`, `navigatePostingMsg`, `backToPostingListMsg`, `refreshPostingDetailMsg` and `postingReloadedMsg`'s detail path.

## Acceptance criteria

- [ ] Opening from the posting list, all postings and application detail shows the same posting; previous/next only from the posting list.
- [ ] A status change or archive made while it's covered shows when it's back on top.
- [ ] Manual check against real data from each entry point.
- [ ] Verification checklist clean.
