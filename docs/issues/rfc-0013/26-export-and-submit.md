---
title: "[RFC 0013 step 26] Export reports a Ran; submit writes its own status"
labels: [rfc-0013, rfc0013-step-26]
rfc: docs/rfcs/0013-tui-change-messages.md
rfc_sections: ["Adopting: Still in tui (Jobs outside the database)"]
depends_on: [18, 22, 23]
---

RFC 0013, step 5 (export and submit).

## Problem

Export reports `applicationExportedMsg` to `App`, which keeps the export directory and hands it to submit. Submit sends `confirmApplicationSubmitMsg` for `App` to set the status, and both screens' opens and cancels go through `App`.

## Change

- `Nav`s push both with the application and `App`'s export directory (RFC 0012, section 7).
- Export sends `Ran[ExportReport]` (the directory, the paths written and the documents skipped) or `Error`; `App` routes it to remember the directory.
- Submit sets the status itself; step 18's `Changed[Application]` closes it.
- Remove `enterApplicationExportMsg`, `enterApplicationSubmitMsg`, `cancelApplicationExportMsg`, `cancelApplicationSubmitMsg`, `applicationExportedMsg` and `confirmApplicationSubmitMsg`.

## Acceptance criteria

- [ ] Export, then submit, uses the directory just exported to.
- [ ] Submit closes with the status changed; the home screen shows it.
- [ ] Manual check against real data.
- [ ] Verification checklist clean.
