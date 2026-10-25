# Decision Moment `01M5R5CY0Z5RZ3V0J8Q2W5C49K`

- **Mission:** `hn-serve-01M5P0NP`
- **Origin flow:** `plan`
- **Slot key:** `plan.validation.reuse-boundary`
- **Input key:** `validation_reuse_boundary`
- **Status:** `resolved`
- **Created:** `2026-10-24T23:54:37.471552+00:00`
- **Resolved:** `2026-10-24T23:54:38.812768+00:00`
- **Resolved by:** `llxprt (maintainer-delegated planning)`
- **Opened by:** `llxprt`
- **Other answer:** `false`

## Question

How does the push check reuse replication validation without duplicating it?

## Options

- Call the existing quarantine validators from the pre-receive hook against the quarantined push, with one documented server exception for candidates whose code ref has not arrived
- Write a separate server validator
- Other

## Final answer

The hook reuses validateQuarantinedActor (extended with a hosted-prefix range so only appended commits are re-walked), replicationEventDependency, validateActorChains, validateExactEventReferenceClosure, validateIssueRelationships, ProjectIdentityContinuity, loadMemoryStreamAt, measureQuarantinedSelectionUnderBudgets and enforceReplicationBudgets, and reads hosted events through the existing batched verified reader collectIssueEvents. One server exception: a candidate event whose code ref is neither hosted nor in the same push is accepted without the code-ref dependency; its code ref is checked against the signed head when it arrives (FR-010).

## Rationale

Reuse satisfies FR-007 literally. The exception is required because hn sync publishes actor refs before proposal refs in separate pushes; without it, the actor push would need the proposal ref and the proposal ref would need the candidate, so neither could land. Range validation and the batched reader keep NFR-001 within 5 seconds at 1,000 events; per-commit git show of every hosted event would cost about 2,000 subprocesses.

## Change log

- `2026-10-24T23:54:37.471552+00:00` — opened
- `2026-10-24T23:54:38.812768+00:00` — resolved (final_answer="The hook reuses validateQuarantinedActor (extended with a hosted-prefix range so only appended commits are re-walked), replicationEventDependency, validateActorChains, validateExactEventReferenceClosure, validateIssueRelationships, ProjectIdentityContinuity, loadMemoryStreamAt, measureQuarantinedSelectionUnderBudgets and enforceReplicationBudgets, and reads hosted events through the existing batched verified reader collectIssueEvents. One server exception: a candidate event whose code ref is neither hosted nor in the same push is accepted without the code-ref dependency; its code ref is checked against the signed head when it arrives (FR-010).")
