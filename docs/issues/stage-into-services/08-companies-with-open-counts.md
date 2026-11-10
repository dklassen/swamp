---
title: "[Stage into services 8] Companies with their open counts are CompanyService's"
labels: [stage-into-services]
depends_on: []
---

## Problem

`stage.Companies` (`stage/companies.go:23`) joins `CompanyService.ListActive` with `PostingService.CountOpenByCompany` in Go. It's two reads, so a sync between them can make a count disagree with the list. The question is keyed by company, so by AGENTS.md it's `CompanyService`'s, as a company's posting list became in app-simplification step 6a.

## Change

- `CompanyService` returns active companies, each with its open, unarchived posting count that its filters let through, in one read or one transaction.
- `stage.Companies` maps it to `CompanySummary`.

## Decide before starting

- **Whether the TUI's company list should use it too.** It makes the same two reads (`tui/company_list.go:111`, `:115`). Recommended: yes, in this PR, since that's the second copy of one question.

## Acceptance criteria

- [x] A service test: a company's count is its open, unarchived, filter-passing postings, and a deleted company is left out.
- [x] `list_companies`' JSON unchanged.
- [x] Verification checklist clean.
