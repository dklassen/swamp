---
title: "[Stage into services 14] Starting to draft reads the stored form"
labels: [stage-into-services]
depends_on: [13]
---

## Problem

After step 13, sync fetches every open posting's missing form, but `DraftingService.Prepare` still fetches one itself when none is stored, so drafting keeps a second copy of the fetchers and a fetch policy of its own.

## Change

- `Prepare` reads the stored form and never fetches. With none stored, `ApplicationForm` is null and the agent drafts both documents, as it does after a failed fetch today.
- `DraftingService` loses the fetchers and `FormConfig`, and `FormRead` goes. `stage_prepare` stops returning `ApplicationFormError`, which was `omitempty`, so the agent's JSON is otherwise unchanged. `apply-to-posting` told the agent what to do when it was set; that line goes in the same change.

## Acceptance criteria

- [x] Drafting tests, each watched fail first, or against a mutation where the old code already had the behaviour: a posting with a stored form is prepared with it; one without is prepared with none and no board request.
- [x] `NewDraftingService` takes no fetchers.
- [ ] Manual check: on a copy of the real database, `stage_prepare` on a posting synced since step 13 returns its form.
- [x] Verification checklist clean.
