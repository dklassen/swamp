---
title: "[Stage into services 3] Document reads and writes go through DocumentService"
labels: [stage-into-services]
depends_on: [2]
---

## Problem

`stage.WriteDocument` and `stage.ReadDocument` (`stage/document.go:67`, `:98`) hold rules that belong to documents:

- **The application check.** A document may be touched only for an application that exists and isn't deleted (`application`, `:47`), since folders are keyed by ID and a write for an unused ID would land in the next application's folder.
- **The version check.** A write carrying a `replacesVersion` happens only if the document is still that version, and `NoDocumentYet` means the document must still be absent.
- **Write then record.** The file is written, then `RecordWrite` records it as `SourceWriteDocument`.
- **The read.** It goes straight to `os.ReadFile`.

The errors live in `stage` (`ErrApplicationNotFound`, `ApplicationDeletedError`, `ErrDocumentNotWritten`, `ErrEmptyReplacesVersion`, `NoDocumentYet`), so `mcpserver` maps errors from a package that isn't the one deciding them.

## Change

- `DocumentService.Write(ctx, applicationID, documentType, content, replacesVersion, source)` and `DocumentService.Read(ctx, applicationID, documentType)`, with `stage`'s application check, version check and record-after-write.
- The errors and `NoDocumentYet` move with them, to `internal/service` or `internal/domain/document`. `mcpserver.documentToolError` matches the new ones; its messages don't change.
- `stage.WriteDocument` and `stage.ReadDocument` call through, until step 10 removes them.

## Decide before starting

- **`replacesVersion` stays `*string`?** nil writes unconditionally, `NoDocumentYet` expects none, "" is refused. The pointer distinguishes "not given" from "given empty", which `ErrEmptyReplacesVersion` uses, so it earns its keep under AGENTS.md's rule. Keep it unless a caller shows otherwise.
- **Where the errors live**: `service` (beside the method) or `domain/document` (if anything outside the service needs to match them without importing it).

## Acceptance criteria

- [ ] Service tests for: an unknown application, a deleted one (the error names its posting), a stale `replacesVersion`, `NoDocumentYet` over an existing document, an empty `replacesVersion`, and a write recorded with its source. Each written and run one at a time, moved from `stage/stage_test.go`'s document tests.
- [ ] `stage/document.go` holds no rule of its own.
- [ ] `write_document` and `read_document` errors in `mcpserver_test.go` unchanged.
- [ ] Verification checklist clean.
