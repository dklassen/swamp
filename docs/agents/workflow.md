# Branches, PRs and issues

## Feature branch workflow

Each task/feature gets its own branch off `main`, reviewed via PR and merged on GitHub when complete. Keep PRs scoped to the minimal change needed -- one issue or one fix per PR by default (see below); don't fold in unrelated or loosely-related work just because it's convenient to be touching the code anyway.

1. Create the branch off `main` in its own worktree (`git worktree add -b <descriptive-branch-name> .claude/worktrees/<name> main`), never by switching `/workspace`'s branch: other sessions work in that checkout at the same time and share its HEAD.
2. Build via TDD ([testing.md](testing.md)), committing in small, well-documented commits as logical chunks complete — not one giant commit at the end. No `Co-Authored-By` trailer or AI-authorship footer (see [AGENTS.md](../../AGENTS.md)).
3. Run the verification checklist ([AGENTS.md](../../AGENTS.md)) before every commit.
4. For any change touching the TUI or other runtime-visible behavior, do a manual verification against real data before considering the work done.
5. Push the branch and open its PR without asking. Don't merge it: Bob merges on GitHub after review, and nothing is merged into `main` locally.
6. Once Bob says a PR is merged, clean up after it: remove its worktree, delete its local and remote branches, and sync local `main`. PRs are squash-merged, so `git branch -d` refuses; check that `main` has the PR's merge commit, then use `-D`. GitHub may already have deleted the remote branch. Touch only the merged PR's branch and worktree.
7. When a PR resolves a tracked issue, use GitHub's closing-keyword syntax in the PR body (`Fixes #N` / `Closes #N`), not just a bare `#N` reference — otherwise the issue doesn't auto-close on merge and has to be closed by hand afterward.

## Issue-driven task workflow

When asked to work through a labeled group of issues:

1. Fetch the issues: `gh issue list --label "<tag>" --state open --json number,title,body,labels,url`
2. Build a task list from the results, one item per issue (number + title).
3. Work through the list in order:
   - `gh issue view <number> --comments` to pull full context before starting
   - Implement the change, on its own branch -- **one PR per issue by default**, even when several issues were filed or picked up together (e.g. all found during the same code review). Don't default to bundling multiple issues into one branch/PR for convenience; ask Bob first if bundling seems to make sense for a given batch.
   - Reference the issue number in the commit message (e.g. `fixes #123`)
   - `gh issue comment <number> --body "..."` to log what was done, if asked
4. Report progress after each issue rather than batching silently.
5. Push each finished branch and open its PR without asking, then go on to the next issue. Don't merge PRs or close issues by hand: the PR's `Fixes #N` closes its issue when Bob merges it.
