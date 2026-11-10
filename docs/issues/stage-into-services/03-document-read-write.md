---
title: "[Stage into services 3] Document reads and writes go through DocumentService"
labels: [stage-into-services]
depends_on: [2]
---

## Problem

`stage.WriteDocument` and `stage.ReadDocument` held rules that belong to documents:

- **The application check.** A document may be touched only for an application that exists and isn't deleted, since folders are keyed by ID and a write for an unused ID would land in the next application's folder.
- **The version check.** A write carrying a `replacesVersion` happened only if the document was still that version, and `NoDocumentYet` meant it must still be absent. Without one, the write overwrote whatever was there.
- **Write then record.** The file was written, then `RecordWrite` recorded it as `SourceWriteDocument`.
- **The read.** It went straight to `os.ReadFile`.

The errors lived in `stage` (`ErrApplicationNotFound`, `ApplicationDeletedError`, `ErrDocumentNotWritten`, `ErrEmptyReplacesVersion`, `NoDocumentYet`), so `mcpserver` matched errors from a package that wasn't the one deciding them.

## Change

- `DocumentService.Write(ctx, applicationID, documentType, content, version, source)` and `DocumentService.Read(ctx, applicationID, documentType)`, with the application check (`requireApplication`), the version check and the record after the write.
- A write always names the version the caller read (`NoDocumentYet` for none), and a document changed since is refused with `documents.ErrChanged`. There's no unconditional write: its only use was the user asking to overwrite, which a caller does by reading first. `documents.Store.Write` takes the version too, and `WriteIfUnchanged` goes.
- The errors and `NoDocumentYet` move to `internal/service`; `ErrEmptyReplacesVersion` becomes `ErrNoVersion`. `mcpserver.documentToolError` matches them, with the same messages.
- `write_document`'s `ReplacesVersion` is required, a plain string. The apply-to-posting skill says to read first to overwrite on the user's say-so.
- `stage.WriteDocument` and `stage.ReadDocument` call through, until step 10 removes them.

## Acceptance criteria

- [x] Service tests, each watched fail first: a write is made and recorded with its source; an unknown application and a deleted one (naming its posting) are refused for writes and reads; the version read, another version, `NoDocumentYet` with and without a document, and an empty version; a read returns the document and its path, and `ErrDocumentNotWritten` for none.
- [x] `stage/document.go` holds no rule of its own.
- [x] `write_document` and `read_document` errors in `mcpserver_test.go` unchanged, except that omitting `ReplacesVersion` is now refused (`TestWriteDocument_OmittedReplacesVersion_IsRefused`).
- [x] Verification checklist clean.
