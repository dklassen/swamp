---
title: "[Stage into services 9] Scoring checks the profile; canonical files come from DocumentService"
labels: [stage-into-services]
depends_on: [2]
---

## Problem

A score must name the profile it was judged against, and that profile must be `profile.md` as it is now. `stage.checkProfile` (`stage/pending.go:119`) enforces that before `PendingPostings` and `RecordScores`, by reading and hashing the file itself. `ScoringService.Record` checks only that a profile hash was given, so another caller could record scores against a profile that never existed.

`mcpserver`'s `readCanonicalHandler` (`mcpserver/mcpserver.go:399`) reads and hashes the same file, and the canonical resume, straight off disk.

## Change

- `ScoringService.Pending` and `Record` refuse a profile hash that isn't `profile.md`'s now, and a missing profile, with errors the endpoint can match. `checkProfile` goes.
- A `DocumentService` method reads a canonical file (profile or canonical resume) with its path, whether it exists and its version. `read_profile` and `read_canonical_resume` use it.

## Decide before starting

- **How the profile reaches `ScoringService`**: the `documents.Store` (as step 2), or a narrower interface it declares for the profile alone.
- **Canonical files as a typed value.** Two files today, picked by method (`ProfilePath`, `CanonicalResumePath`). An `iota` kind is only worth it if code switches on it; otherwise two methods.

## Acceptance criteria

- [x] Service tests: a stale profile hash records nothing and lists nothing, a missing profile is refused, the current one works.
- [x] `read_profile`'s and `read_canonical_resume`'s outputs unchanged, `Exists: false` included.
- [x] `list_pending_postings` and `record_scores` error messages unchanged.
- [x] Verification checklist clean.
