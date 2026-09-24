# Domain docs

## Layout

This repository uses a single-context layout:

- `CONTEXT.md`: domain vocabulary, relationships, and invariants.
- `docs/adr/`: architectural decision records.

## Reading rules

Before exploring the codebase, read CONTEXT.md and the ADRs relevant
to the area being changed.

If these files do not exist, proceed silently. Domain-modeling creates
them when terminology or decisions are resolved; setup does not create
empty placeholders.

## Vocabulary

Use the terms defined in CONTEXT.md when naming domain concepts in
code, tests, issues, and proposals.

If a needed concept is missing, identify the gap for domain-modeling
rather than inventing competing terminology.

## Decision conflicts

If a proposal conflicts with an ADR, identify the ADR and explain
why the decision should be reconsidered. Preserve the distinction
between an accepted decision and a proposed replacement.
