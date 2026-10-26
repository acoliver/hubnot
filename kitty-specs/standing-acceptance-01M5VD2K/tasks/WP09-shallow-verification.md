---
work_package_id: WP09
title: Shallow verification of rule merge facts
dependencies:
- WP08
requirement_refs:
- C-004
- FR-012
- FR-013
- FR-014
- FR-024
- FR-025
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
create_intent:
- standing_fact_test.go
execution_mode: code_change
owned_files:
- shallow.go
- policy.go
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

Make shallow verification, the third fact-admission surface, implement path (b) and replace the interim shallow refusal that WP07 put in the rule path of local projection (FR-012, FR-013, FR-014, FR-025, US3-4). Path (a) stays exactly as today in a shallow clone: a fact that passes the maintainer check never waits for rule-path inputs, and every legacy verdict is unchanged (C-004).

For a fact that carries a canonical rule claim and fails path (a), shallow verification requires everything the derivation reads before it evaluates the fact, and otherwise refuses with the existing shallow-dependency message and recovery guidance, so no empty or partial change list is ever judged:

- each listed evidence ID as a `selected event fact` (today every merge evidence ID is classified as a `decision`, `shallow.go` lines 1012 to 1020);
- for each listed `run.result`, the run request it answers (the existing `run.result` rule already requires it);
- the candidate's base commit, head commit (proposal code ref), base policy blob and every base-policy pipeline definition at the head.

Merge facts without a rule claim keep classifying evidence as `decision` dependencies. The defect matrix rows of this surface prove that semantic defects reach rule derivation (decision `01M5VSEHFV2K03JJY0NN6DFNAT`, analysis finding U2): every semantic row runs with a complete closure, has a matched valid control, and must name its derivation check.

## Context

- Read spec US3-4 and FR-013 and FR-014; [contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Union admission", "Inputs and object access per surface" (shallow row) and "Fixture matrix"; [research.md](../research.md) finding 11; plan "Activation boundaries".
- Start with `spec-kitty agent action implement WP09 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code in `shallow.go`: `guardShallowEventClosure` (line 958), the dependency kinds (`shallowSelectedFact`, `shallowDecision`, `shallowBaseCommit`, `shallowProposalCodeRef`, `shallowPolicyBlob`, `shallowPipelineDefinition`, `shallowRunRequest`), `classifyShallowDependency`, `exactDependency`, `guardProposalEvaluationDependencies` (line 1381), `guardProposalDependencies`, `guardBasePolicy`, `guardPipelineDefinition`, `prepareShallowVerification`. Existing shallow tests: `operational_acceptance_test.go` (`TestOperationalSelfHostingAlphaShallowRecoveryCLI`) and the shallow tests in `replication_test.go`; read one to learn how a shallow clone is created and how recovery is printed.
- From WP07: `validateMergeEventWith`, `localRuleObjectAccess` and the interim shallow refusal inside its rule path (`policy.go`), and the boundary table `standingFactRulePathSurfaces`. From WP05: `admitMergeFact`. From WP06 to WP08: harness, fact builders, legacy and boundary fixtures, raw publishing of actor refs, `standing_fact_test.go` helpers.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `shallow.go` | CHANGE. The `proposal.merged` arm of `guardShallowEventClosure` (evidence classification for facts with a rule claim) and `shallowRuleObjectGuard`, the path (b) dependency guard. |
| `policy.go` | CHANGE. In the rule path of `validateMergeEventWith` only: replace WP07's interim shallow refusal with a call to `shallowRuleObjectGuard` before `validateRuleMergeFactWith`. Nothing else in `policy.go` changes. |
| `standing_fact_test.go` | CHANGE. Add `TestStandingFactShallow...`; set `shallow: true` in the boundary table. |

## Subtasks

### Subtask T040: Rule-fact closure in shallow verification

**Steps**:

1. In the `proposal.merged` case of `guardShallowEventClosure`, keep the subject requirement (`shallowCandidateEvent`) first. Then branch on `event.Rule`:
   - empty: the existing loop, each evidence ID as `shallowDecision`. Unchanged.
   - non-empty: each evidence ID as `shallowSelectedFact`. A missing ID is reported with the existing message `signed event <fact> (proposal.merged) requires unavailable fact <id>` and the classification's recovery. The verdict for a maintainer merge with a rule claim and a missing decision is still a refusal; only the label changes.
2. Add `func shallowRuleObjectGuard(operation string, proposal StoredEvent, events []StoredEvent) error` in `shallow.go`. It returns nil in a complete repository. In a shallow one it requires the candidate's code dependencies with the existing guards, in this order, using the candidate as owner (`replicationProposal`, candidate ID): base commit, proposal code ref equal to the signed head, base policy blob (`guardBasePolicy`), and each base-policy pipeline definition at the head (`guardPipelineDefinition`). Reuse `guardProposalEvaluationDependencies(operation, candidate, events)` if its checks and order match.
3. In `policy.go`, replace the interim refusal in the rule path of `validateMergeEventWith` with `shallowRuleObjectGuard(...)` followed by `validateRuleMergeFactWith`. The guard runs only inside the rule path, so only after path (a) failed for a fact with a rule claim.
4. Evidence items of kind `run.result` already require their request through the existing `run.result` arm when the result itself is in the closure; confirm by test that a listed result whose request is absent is refused with `shallowRunRequest`.
5. Do not change `shallowAcceptedFacts`.

### Subtask T041: Shallow tests for US3-4, the semantic matrix and recovery guidance

**Steps** (tests `TestStandingFactShallow...`):

1. Create a shallow verifier clone the way existing shallow tests do (`git clone --depth 1` of the bare remote plus the replication selection that existing shallow tests use), selecting E's chain.
2. **Complete closure control**: with the valid rule merge fact, every evidence chain selected, the answered requests present, and the base commit, head and code ref, base policy blob and pipeline definitions available, `hn proposal status <candidate>` succeeds and reports merged. Record this selection and object arrangement as the shallow fixture of the matrix.
3. **Semantic rows (decision `01M5VSEHFV2K03JJY0NN6DFNAT`)**: for each defect F01 to F20, build the defect with the same selection and object arrangement as its valid control (hydrate any extra candidate's code, policy blob and pipeline definitions, and select every chain its evidence names). Assert the refusal names the expected derivation check (`rule merge <full fact ID> fails <check>`) for F01 to F18, or the maintainer-path refusal for F19 and F20. A shallow dependency gap on a semantic row fails the test. Before each semantic row, assert that its own valid control (WP06's `Control` for the row) is accepted with the same selection and hydration strategy as the row:
   - **Fact-only rows** (F01 to F14, F19, F20): the control is the valid rule merge fact for the same candidate and arrangement, with the defect removed.
   - **Candidate-shape rows** (F15 to F18): the defect lies in the signed candidate itself, so no change to the fact can remove it. The control is a corresponding valid candidate, built from the same base and files with the defect removed (an allowed author for F15; only allowlisted paths for F16; the `src/` change without the manual-path file for F17 and F18), with its own reviews, CI requests, run results and merge fact rebound to it.
   - Both the control and the row need a complete closure. The row must still name its intended check; a refusal through a gap never counts as semantic success.
4. **Missing-dependency rows, a separate table**: missing review (deselect R's chain): refuses naming the approval ID as `selected event fact`, with the recovery guidance that `classifyShallowDependency` produces; missing run result (as `selected event fact`) and missing run request (as `run request`); missing base commit, missing head or code ref, missing policy blob, missing pipeline definition: the matching kind (`base commit`, `proposal code ref`, `policy blob`, `pipeline definition`). F21 and F22 belong here too.
5. **Maintainer merges and legacy history unchanged**: an acceptance merge fact whose decision is missing still reports `decision`; boundary fixtures B2 and B3 and legacy fixtures L01 to L05 with their closure verify as before, and a maintainer merge with a rule claim whose candidate code is absent still verifies, because path (a) never reads it.
6. For each refusal, assert no ref in the shallow clone changed. Set `shallow: true` in `standingFactRulePathSurfaces` and run `TestStandingFactBoundary`.

If a case cannot be constructed through public commands (for example a shallow clone that holds the head but not the base policy blob), say so in the Activity Log and cover it with a direct call to `guardShallowEventClosure` or `shallowRuleObjectGuard` from the test inside the shallow clone (`t.Chdir`), which still exercises the production function.

### Subtask T042: Missing commits never become an empty change list

**Purpose**: FR-014 across the local, shallow and import paths, in one place a reviewer can read.

**Steps**:

1. `TestStandingFactShallowNoEmptyDiff`: for a rule merge fact whose candidate head is unavailable, assert that every surface reached through public commands (local projection in a full clone after deleting the code object from a copy, shallow verification, `hn sync` import) refuses or reports a missing dependency, and that none reports `changed-paths` with "no changed paths" (which would mean the empty list was evaluated).
2. Read `changedPathsAt`: confirm that `git diff-tree` with an absent object exits non-zero, so the error path is taken. Record the Git version and the observed message in the Activity Log.

## Test Strategy

Black-box tests with real shallow clones. Run `go test -race -run 'TestStandingFactShallow|TestStandingFactBoundary|Shallow' ./...`, then the full gate.

## Definition of Done

- [ ] T040 to T042 complete; full gate passes.
- [ ] **FR-013 (shallow)**: each listed review, run result and answered run request of a fact with a rule claim is a required selected fact.
- [ ] **FR-014**: base commit, code ref, policy blob and pipeline definitions are required by path (b) only; refusals carry recovery guidance; no empty change list is ever evaluated.
- [ ] **NFR-001 (shallow surface)**: every semantic row F01 to F20 runs with a complete closure and a passing matched control, and names its derivation check; missing-dependency rows are a separate table.
- [ ] **FR-025**: the interim shallow refusal is gone; the boundary table marks `shallow`; the boundary test passes.
- [ ] **C-004**: maintainer merges and legacy fixtures verify in a shallow clone as before; merge facts without a claim keep their `decision` classification; existing shallow tests pass unchanged.

## Risks

- Constructing each missing-object case through public commands is fiddly; budget time for it and prefer one shallow fixture with several selections.
- A semantic row that refuses through a gap would make the shallow column look complete without testing derivation; the matched control is what catches it.

## Reviewer Guidance

- Diff the `proposal.merged` arm: the no-claim branch must be byte-identical.
- Confirm `shallowRuleObjectGuard` is called only from the rule path, never before path (a).
- Read the logged semantic table and check every row names a derivation check, not a gap.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
- 2026-10-26T09:45:00Z – llxprt – Prompt revised after analysis (C1, U2; decisions 01M5VSEHF5B1FM9TB52KWKQGWZ and 01M5VSEHFV2K03JJY0NN6DFNAT): path (b) only after path (a), replaces WP07's interim shallow refusal (policy.go added to ownership), semantic matrix with complete closure and matched controls
- 2026-10-26T10:35:12Z – llxprt – Prompt revised after analysis round 2 (U2; decision 01M5VWE2Q3AVA8NPGRCSKBFW5M): matched controls use the same candidate for fact-only rows and a corresponding valid candidate for F15 to F18
