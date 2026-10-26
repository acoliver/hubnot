---
work_package_id: WP09
title: Shallow verification of rule merge facts
dependencies:
- WP08
requirement_refs:
- FR-012
- FR-013
- FR-014
- NFR-001
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T040
- T041
- T042
phase: Phase 3 - Fact admission
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: shallow.go
create_intent: []
execution_mode: code_change
owned_files:
- shallow.go
- standing_fact_test.go
role: implementer
tags: []
tracker_refs: []
---

# WP09: Shallow verification of rule merge facts

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Make shallow verification, the third fact-admission surface, require everything a rule merge fact's derivation reads before local projection evaluates it (FR-012, FR-013, FR-014, US3-4):

- each listed evidence ID as a `selected event fact` (today every merge evidence ID is classified as a `decision`, `shallow.go` lines 1012 to 1020);
- for each listed `run.result`, the run request it answers (the existing `run.result` rule already requires it);
- the candidate's base commit, head commit (proposal code ref), base policy blob and every base-policy pipeline definition at the head.

When any of these is unavailable, the command refuses with the existing shallow-dependency message and recovery guidance and never evaluates the fact, so no empty or partial change list is ever judged. Acceptance merge facts keep classifying evidence as `decision` dependencies.

## Context

- Read spec US3-4 and FR-013 and FR-014; [contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Inputs and object access per surface" (shallow row); [research.md](../research.md) finding 11.
- Start with `spec-kitty agent action implement WP09 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code in `shallow.go`: `guardShallowEventClosure` (line 958), the dependency kinds (`shallowSelectedFact`, `shallowDecision`, `shallowBaseCommit`, `shallowProposalCodeRef`, `shallowPolicyBlob`, `shallowPipelineDefinition`, `shallowRunRequest`), `classifyShallowDependency`, `exactDependency`, `guardProposalEvaluationDependencies` (line 1381), `guardProposalDependencies`, `guardBasePolicy`, `guardPipelineDefinition`, `prepareShallowVerification`. Existing shallow tests: `operational_acceptance_test.go` (`TestOperationalSelfHostingAlphaShallowRecoveryCLI`) and the shallow tests in `replication_test.go`; read one to learn how a shallow clone is created and how recovery is printed.
- From WP06 to WP08: harness, fact builders, raw publishing of actor refs, `standing_fact_test.go` helpers.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `shallow.go` | CHANGE. The `proposal.merged` arm of `guardShallowEventClosure`, plus a small helper for rule-fact code dependencies. |
| `standing_fact_test.go` | CHANGE. Add `TestStandingFactShallow...`. |

## Subtasks

### Subtask T040: Rule-fact closure in `guardShallowEventClosure`

**Steps**:

1. In the `proposal.merged` case, keep the subject requirement (`shallowCandidateEvent`) first. Then branch on `event.Rule`:
   - empty: the existing loop, each evidence ID as `shallowDecision`. Unchanged.
   - non-empty: each evidence ID as `shallowSelectedFact`. A missing ID is reported with the existing message `signed event <fact> (proposal.merged) requires unavailable fact <id>` and the classification's recovery.
2. Still for a rule fact, once the subject is present: require the candidate's code dependencies with the existing guards, in this order, using the candidate as owner (`replicationProposal`, candidate ID): base commit, proposal code ref equal to the signed head, base policy blob (`guardBasePolicy`), and each base-policy pipeline definition at the head (`guardPipelineDefinition`). Reuse `guardProposalEvaluationDependencies(operation, candidate, events)` if its checks and order match; it already guards base policy and pipeline definitions after `guardProposalDependencies`. Build the `StoredEvent` slice it needs from the closure's facts.
3. Evidence items of kind `run.result` already require their request through the existing `run.result` arm when the result itself is in the closure; confirm by test that a listed result whose request is absent is refused with `shallowRunRequest`.
4. Run the rule-fact checks only for facts in the closure; do not change `shallowAcceptedFacts`.

### Subtask T041: Shallow tests for US3-4 with recovery guidance

**Steps** (tests `TestStandingFactShallow...`):

1. Create a shallow verifier clone the way existing shallow tests do (`git clone --depth 1` of the bare remote plus the replication selection that existing shallow tests use), selecting E's chain.
2. **Complete closure**: with the valid rule merge fact and every evidence chain selected, and the candidate's code available, `hn proposal status <candidate>` succeeds and reports merged.
3. **Missing review**: deselect R's chain (or never fetch it); `hn proposal status <candidate>` refuses with a shallow dependency message naming the approval ID as `selected event fact` and printing recovery guidance (the existing `hn sync --recover-shallow` or selection advice that `classifyShallowDependency` produces).
4. **Missing run result** and **missing run request**: same, naming the result (as `selected event fact`) or the request (as `run request`).
5. **Missing base commit, missing head or code ref, missing policy blob, missing pipeline definition**: arrange each with the shallow depth or selection, and assert the matching dependency kind (`base commit`, `proposal code ref`, `policy blob`, `pipeline definition`).
6. **Acceptance fact unchanged**: an acceptance merge fact whose decision is missing still reports `decision`.
7. For each refusal, assert no ref in the shallow clone changed.

If a case cannot be constructed through public commands (for example a shallow clone that holds the head but not the base policy blob), say so in the Activity Log and cover it with a direct call to `guardShallowEventClosure` from the test inside the shallow clone (`t.Chdir`), which still exercises the production function.

### Subtask T042: Missing commits never become an empty change list

**Purpose**: FR-014 across the local, shallow and import paths, in one place a reviewer can read.

**Steps**:

1. `TestStandingFactShallowNoEmptyDiff`: for a rule merge fact whose candidate head is unavailable, assert that every surface reached through public commands (local projection in a full clone after deleting the code object from a copy, shallow verification, `hn sync` import) refuses or reports a missing dependency, and that none reports `changed-paths` with "no changed paths" (which would mean the empty list was evaluated).
2. Read `changedPathsAt`: confirm that `git diff-tree` with an absent object exits non-zero, so the error path is taken. Record the Git version and the observed message in the Activity Log.

## Test Strategy

Black-box tests with real shallow clones. Run `go test -race -run 'TestStandingFactShallow|Shallow' ./...`, then the full gate.

## Definition of Done

- [ ] T040 to T042 complete; full gate passes.
- [ ] **FR-013 (shallow)**: each listed review, run result and answered run request is a required selected fact.
- [ ] **FR-014**: base commit, code ref, policy blob and pipeline definitions are required; refusals carry recovery guidance; no empty change list is ever evaluated.
- [ ] **NFR-001 (shallow surface)**: every defect that reaches a shallow clone is refused there or by local projection after the guard.
- [ ] Acceptance merge facts keep their `decision` classification; existing shallow tests pass unchanged.

## Risks

- Constructing each missing-object case through public commands is fiddly; budget time for it and prefer one shallow fixture with several selections.

## Reviewer Guidance

- Diff the `proposal.merged` arm: the empty-rule branch must be byte-identical.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
