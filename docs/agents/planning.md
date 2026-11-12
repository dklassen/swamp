# Work orders and RFCs

## Work state lives in the repo

Notes that let work resume in a fresh session (what's done, what's next, what was decided and why) go in the repo, never in an agent's own memory or notes outside it, where Bob and other agents can't see or review them.

- Work spanning several PRs or issues: a folder `docs/issues/<series>/` holding a work order, `README.md`, and an issue file per step that needs one written up.
- A design spanning several issues: an RFC in `docs/rfcs/`, with its work order in `docs/issues/<series>/`.
- Update the work order in the PR that changes it, so it never lags `main`.

The format below is the one place it's defined; change it here, then bring the files in line.

**Work order (`docs/issues/<series>/README.md`)**, in this order:

1. `# <Series> work order`, then one paragraph: the scope, and a link to the RFC if there is one.
2. A table, one row per step: `| Step | Title | Issue | PR | State | Depends on |`. Title links to the step's issue file if it has one. Issue and PR are `#N`, or `-`. State is one of `planned`, `in progress`, `in review`, `merged`, `dropped`. Depends on is step numbers, or `none`.
3. `## Next`, optional: work not yet a step, with what's needed to start it.
4. `## Decided`: one bullet per decision, with why. A decision that holds beyond the series moves to AGENTS.md, a doc it links, or an RFC instead.
5. `## Open`: questions waiting on Bob, and what each blocks. `None.` if empty.
6. `## Follow-ups`: issues filed along the way that are outside the series, `#N: what`.

**Issue file (`docs/issues/<series>/NN-slug.md`)**, `NN` being its step:

- Front matter: `title`, `labels`, `depends_on` (step numbers), plus `rfc` and `rfc_sections` for an RFC's step.
- `## Problem`, `## Change`, `## Decide before starting` (omitted if nothing is), `## Acceptance criteria` as a checklist.

## RFCs

- Before building a value, range or rule from an RFC, read its Open questions and raise any that bear on it with Bob first.
- Update an RFC in the change that alters one of its rules while it's still being built. Once its status is Implemented it's a record: don't retroactively update it unless Bob gives permission.
- An RFC stays compact: Summary, Problem (the code as it stood), Design as one-line rules grouped by topic, Accepted costs, Open questions, Out of scope. No code, progress tables, test names or commit SHAs.
