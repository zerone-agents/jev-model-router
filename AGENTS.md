# Repository instructions

## Product architecture

Before changing module boundaries, public capabilities, or CLI/UI behavior,
read `docs/architecture.md` and `contracts/README.md`.
When changing a CLI workflow, keep `skills/jev-router/SKILL.md` aligned with
the implemented capability contract. Mark planned capabilities explicitly.

## GitHub CLI execution

Run `gh` commands outside the sandbox with the required escalation when
network or credential access is restricted. Avoid repeated sandbox retries.

Remote writes require user authorization. Use the minimum necessary
permissions. For multiline issue and PR bodies, use `--body-file`.

## Agent skills

### Issue tracker

Track work in GitHub Issues for `zerone-agents/jev-model-router`.
Before reading, creating, or updating tickets, read
`docs/agents/issue-tracker.md`.

### Triage labels

Use the five canonical triage labels.
Before triaging or changing issue state, read
`docs/agents/triage-labels.md`.

### Domain docs

Use a single-context layout: root `CONTEXT.md` and `docs/adr/`.
Before exploring domain concepts or architectural decisions, read
`docs/agents/domain.md`.

## Frontend standards and review

Before implementing or reviewing UI changes, read
`docs/agents/frontend-standards.md`. For UI reviews, also apply
`docs/agents/frontend-review.md`.
