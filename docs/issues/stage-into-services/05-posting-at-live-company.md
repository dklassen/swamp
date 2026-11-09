---
title: "[Stage into services 5] A posting at a deleted company is refused in one place"
labels: [stage-into-services]
depends_on: []
---

## Problem

Three callers read a posting and then its company, each its own way:

- **`stage.ReadPosting`** (`stage/read_posting.go:36`) calls `GetIncludingDeleted`, then refuses a deleted company by name ("posting 12 is at Acme, which was deleted").
- **`stage.Prepare`** (`stage/stage.go:381`) calls `CompanyService.Get`, whose query filters `deleted_at IS NULL`. A deleted company's posting fails with "stage: get company: not found", which doesn't say why.
- **`tui/application_detail.go:330`** calls `GetIncludingDeleted` and allows a deleted company, deliberately: the home screen lists an application whatever its company's state.

"Search leaves out postings at deleted companies, so reading one is refused" is a rule about postings, and it's in `stage`.

## Change

- A `PostingService` method returns a posting with its company, refusing one at a deleted company with an error that names it (a sentinel or typed error the endpoints can match).
- `ReadPosting` uses it. Step 6 makes `Prepare` use it.
- The TUI's read stays as it is: it wants the opposite rule.

## Decide before starting

- **What it returns**: the posting and `*company.Company`, or a small domain type with just what callers read (name, description).

## Acceptance criteria

- [ ] A service test: a posting at a deleted company is refused with the new error; one at a live company comes back with its company.
- [ ] `read_posting`'s message is unchanged.
- [ ] Verification checklist clean.
