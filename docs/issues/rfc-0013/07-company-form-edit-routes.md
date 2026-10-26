---
title: "[RFC 0013 step 7] The company form and edit register their own results with the router"
labels: [rfc-0013, rfc0013-step-7, rfc-0012]
rfc: docs/rfcs/0013-tui-change-messages.md
related_rfc: docs/rfcs/0012-tui-screen-router.md
rfc_sections: ["Steps: 4", "RFC 0012 sections 1, 5"]
depends_on: [4]
---

RFC 0013, step 4 prerequisite. See `docs/rfcs/0013-tui-change-messages.md` and `docs/rfcs/0012-tui-screen-router.md`.

## Problem

RFC 0013 step 4 says the company list, delete *and edit* register `Event[Changed[Company]]` and `Event[Error]`. But the company edit and form aren't on the router. `App.update` handles their results for them: `CompanyCreatedMsg` (`tui/app.go:1084`) calls `companyForm.checkResolved()` and switches screen, and `CompanyNameUpdatedMsg` (`:1090`) calls `companyEdit.saveResolved()`. The edit's esc is `cancelCompanyEditMsg`, an unexported type in `tui/company_edit.go:40`. The RFC lists it as `CancelCompanyEditMsg` under "Already in `tui/message`", but it isn't there yet.

Moving these handlers is RFC 0012 work. It comes first so step 8 changes the routes' message type, not who handles it.

## Change

- `companyFormModel` and `companyEditModel` implement `Routes()`. Each registers its own result `Always` and its keys and pastes `WhenFocused`, as the delete confirmation does. Each is built with its instance (step 4).
- `App` registers each when it opens and deregisters it when it closes, as with `OpenCompanyDeleteMsg`/`CloseCompanyDeleteMsg`. `App.update` loses the two result cases. Closing on success is a close request the model sends.
- The edit's cancel moves to `tui/message` as a request with an exported name.
- The models check that a result is their own opening's: by instance, until step 8 gives them `From`.

## Acceptance criteria

- [ ] Adding and renaming a company behave as before from the user's side (existing App tests).
- [ ] A save result that arrives after the form was closed and reopened doesn't change the new form (test).
- [ ] Keys reach the form or edit only while it's on top.
- [ ] RFC 0012 kept in sync if any rule changes.
- [ ] Manual check against real data: add, rename, cancel.
- [ ] Verification checklist clean.
