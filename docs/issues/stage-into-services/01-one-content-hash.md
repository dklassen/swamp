---
title: "[Stage into services 1] A document's hash has one home"
labels: [stage-into-services]
depends_on: []
---

## Problem

`documents.ContentSHA256` (`documents/documents.go:178`) is a copy of `document.ContentSHA256` (`internal/domain/document/hash.go:10`), down to its comment. Reviews, exports and writes are hashed with the domain one (in `internal/repository`). Files on disk are hashed with the copy, in `documents`, `stage`, `mcpserver` and `tui`. The two agree only because nobody has changed either.

## Change

No behaviour change.

- Every caller of `documents.ContentSHA256` calls `document.ContentSHA256`: `documents.Store.SHA256`, `stage/pending.go`, `mcpserver/mcpserver.go` (twice), `tui/editor.go`, `tui/document_review_form.go`.
- `documents.ContentSHA256` goes.

## Acceptance criteria

- [ ] `grep -rn 'documents.ContentSHA256'` finds nothing.
- [ ] No test changes beyond the renamed call.
- [ ] Verification checklist clean.
