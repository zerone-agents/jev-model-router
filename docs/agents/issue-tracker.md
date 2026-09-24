# Issue tracker: GitHub

Track work in GitHub Issues for `zerone-agents/jev-model-router`.
Use the `gh` CLI. Follow the authorization and execution rules in AGENTS.md.

Research and design notes under `docs/research/` and `docs/superpowers/`
remain local and are ignored by Git. Put requirements needed by other
contributors in the issue body or in tracked documentation. Issue links
must point to shared, accessible documents.

## Operations

- Read: `gh issue view <number> --comments`.
- List: `gh issue list` with appropriate state and label filters.
- Create: `gh issue create --title "..." --body-file <file>`.
- Comment: `gh issue comment <number> --body-file <file>`.
- Edit body: `gh issue edit <number> --body-file <file>`.
- Apply/remove labels: `gh issue edit <number> --add-label "..."`
  or `--remove-label "..."`.
- Close: `gh issue close <number>` after recording the outcome.

When a skill says "publish to the issue tracker", create a GitHub issue.
When it says "fetch the relevant ticket", read the issue and its comments.

## Pull requests as a triage surface

**PRs as a request surface: no.**

## Wayfinding

- Keep the map in one issue labelled `wayfinder:map`.
- Link child tickets as sub-issues, with `wayfinder:<type>` labels:
  research, prototype, grilling, or task.
- If sub-issues are unavailable, use a task list in the map and
  `Part of #<map>` in each child.
- Record blockers using native issue dependencies. If unavailable,
  use `Blocked by: #<number>` in the child issue.
- Select the first open, unassigned child in map order whose blockers
  are all closed.
- Claim by assigning the ticket to the driving developer.
- Resolve by recording the answer, closing the child, and adding a
  summary and link to the map's Decisions-so-far.
