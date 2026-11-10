---
title: "[Stage into services 11] A posting's application form is fetched when the posting lands"
labels: [stage-into-services]
depends_on: [4]
rfc: 0002
rfc_sections: ["Phase 2: Match what each form asks for", "Wave D — match what each form asks (phase 2)"]
---

## Problem

A Greenhouse posting's form is fetched only when a posting is prepared and has no stored form (`PostingService.CurrentForm`, step 4), and never again. Until then Swamp doesn't know what the posting asks for, and a form saved by hand in the TUI first (`f` on application detail, which any source can open) stops the board's form from ever being fetched.

RFC 0002 put the fetch at prepare time to avoid "one for every posting on every sync". Sync already knows when a posting lands (`Ingest` reports `Created`), so fetching then costs one request per new posting, not one per sync.

## Change

- When sync ingests a new Greenhouse posting, it fetches and stores the posting's form through `PostingService`, which owns the fetchers.
- A failed form fetch doesn't fail the posting's ingest, and is reported in sync's result.
- `CurrentForm` still fetches a posting with no stored form, covering a failed fetch at sync and postings ingested before this step.
- RFC 0002's phase 2 item 1 and task D2 say the form is fetched when the posting lands, and why.

## Decide before starting

- **Where the fetches run.** Inside the company's sync lease (5 minutes, with each fetch allowed 30 seconds) one after another, with bounded concurrency, or after the postings are written, outside the lease. A large board's first sync can bring hundreds of new postings at once. Bob's host-side counts of open Greenhouse postings, in total and at the largest company, size this.
- **An updated posting.** Refetch its form when `Ingest` reports `Updated`, tying the form to the posting's content with a check sync already makes, at the cost of overwriting a form edited by hand. Or leave a stored form alone.
- **Retrying a failed fetch.** Each sync fetches forms for its company's open postings that have none, or only `CurrentForm` does, at prepare.
- **Backfill.** Open postings stored before this step have no form. The retry above fetches them on the next sync, a one-off burst; otherwise they keep being fetched at prepare.

## Acceptance criteria

- [ ] Service and sync tests, each watched fail first: a new posting's form is fetched and stored; a failed form fetch still ingests the posting and is reported; a source without a fetcher makes no request; plus whatever the decisions above add.
- [ ] RFC 0002 updated in the same PR.
- [ ] Manual check: a sync of one Greenhouse company on a copy of the real database stores forms for its new postings, within the lease.
- [ ] Verification checklist clean.
