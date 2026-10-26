---
work_package_id: WP07
title: Local projection admits only valid rule merge facts
dependencies:
- WP05
- WP06
requirement_refs:
- FR-012
- NFR-001
- NFR-005
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T031
- T032
- T033
- T034
phase: Phase 3 - Fact admission
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: policy.go
create_intent:
- standing_fact_test.go
execution_mode: code_change
owned_files:
- policy.go
- store.go
- standing_fact_test.go
role: implementer
tags: []
tracker_refs: []
---

# WP07: Local projection admits only valid rule merge facts

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Make local event projection, the first of the four fact-admission surfaces (FR-012), accept a rule merge fact only after `validateRuleMergeFact`, with local object access and one memo per projection pass (NFR-005). A fact without `rule` keeps today's acceptance-fact check byte for byte. Because every command projects events, an invalid rule merge fact in a local store makes every command refuse, exactly as an invalid acceptance merge fact does today (US3-3).

This WP must land before any client records a rule merge fact (WP11): without it, the executing actor's own clone would reject its own fact.

## Context

- Read [contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Inputs and object access per surface" (the local projection row) and "Memoization"; spec US3-3; plan "Changes to existing code" rows for `policy.go` and `store.go`.
- Start with `spec-kitty agent action implement WP07 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code: `validateEventRelationships` (`store.go` line 306; the `proposal.merged` case at lines 357 to 367 calls `validateProposalMergeBinding` then `validateMergeEvent`); `validateMergeEvent` (`policy.go` lines 547 to 568); `loadPolicy`, `loadPipeline` (`ci.go`), `changedPathsAt("", base, head)` (`protected_checks.go`). `validateEventRelationships` is also called from `issue_read.go` line 104.
- From WP05: `ruleObjectAccess`, `newRuleValidationMemo`, `validateRuleMergeFactWith`, `ruleFactError`.
- From WP06: `newStandingHarness`, `ReadyCandidate`, `SignFact`, `standingHarnessDefects`, `Run`, `Refs`.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `policy.go` | CHANGE. `validateMergeEvent` gains a rule branch and a memo-taking variant. WP11 later adds the status line in `cmdProposalStatus`; do not touch it here. |
| `store.go` | CHANGE. `validateEventRelationships` creates one memo per pass and passes it on. |
| `standing_fact_test.go` | NEW. `TestStandingFactLocal...`. WP08, WP09 and WP16 add tests later. |

## Subtasks

### Subtask T031: Rule branch in `validateMergeEvent` with local object access

**Steps**:

1. Add `func localRuleObjectAccess() ruleObjectAccess` in `policy.go`:
   - `PolicyAt`: `loadPolicy(commit)` returning the policy and digest.
   - `ChangedPaths`: `changedPathsAt("", base, head)`.
   - `PipelineDigest`: the digest returned by `loadPipeline(head, name)`.
2. Add `func validateMergeEventWith(memo *ruleValidationMemo, merge, proposal StoredEvent, byID map[string]StoredEvent) error`:
   - `merge.Event.Rule == ""`: run today's body of `validateMergeEvent` unchanged (same messages, same `shortID` use).
   - otherwise: `validateRuleMergeFactWith(memo, merge, proposal, byID)`. Wrap a non-`ruleFactError` error (policy unavailable, changed paths unavailable) as `merge %s: %w` with the full fact ID, and return a `ruleFactError` unwrapped so its message reads `rule merge <full ID> fails <check>: <detail>` on every surface.
3. Keep `validateMergeEvent(merge, proposal, byID)` as a wrapper that creates a fresh memo with `localRuleObjectAccess()`. Existing tests that call it keep working.
4. Missing objects (FR-014, local part): when `ChangedPaths` fails because a commit is absent, the error propagates and projection refuses; it never evaluates an empty list. A full clone always holds both commits after import; a shallow clone is guarded earlier by WP09.

### Subtask T032: Per-pass memo wiring in `store.go`

**Steps**:

1. In `validateEventRelationships`, create `memo := newRuleValidationMemo(localRuleObjectAccess())` once before the loop over events, and call `validateMergeEventWith(memo, stored, proposal, byID)` in the `proposal.merged` case. Leave the binding check (`validateProposalMergeBinding`) first, as today.
2. Do not create the memo lazily inside the loop; one per pass is the contract.
3. Do not change any other case of the switch.

### Subtask T033: Local-store tests for F01 to F18 and the valid fact

**Steps** (tests `TestStandingFactLocal...` in `standing_fact_test.go`, black-box through the built binary, facts signed with the WP06 builders):

1. **Valid fact**: `ReadyCandidate`, then `SignFact("E", nil)` for it. In E's clone, `hn proposal status <candidate>` exits 0 and reports `Status:   merged` (the rule-naming line arrives in WP11; do not assert it here). `hn log` or another projecting command also succeeds.
2. **Defects**: for each semantic defect F01 to F18 of `standingHarnessDefects`, in a fresh harness or a fresh copy of the signer's clone, sign the defective fact into the signer's chain and run `hn proposal status <candidate>`: non-zero exit; output contains `rule merge <full fact ID> fails <expected check>`; `h.Refs(role)` is unchanged by the command. Run a second projecting command (for example `hn issue list` or `hn log`) and assert it refuses the same way (US3-3: "any command").
3. **Cost**: share one harness across defects when the defect does not need a special candidate, by signing each defect in its own throwaway clone made with `git clone --no-local` of the signer's clone plus a copy of its identity, or by resetting the signer's actor ref with `git update-ref` after each case. Choose the faster approach and record it.
4. **Memo (NFR-005)**: two valid rule merge facts for the same candidate (signed by E, and by a second executing actor listed in the same rule for this test) project with one changed-path computation. Observe this through a test-only counter exposed via `HN_INTERNAL_TESTING=1` only if a counter is already a pattern in the codebase; otherwise rely on WP05's unit test and skip a black-box counter. Record the decision.

### Subtask T034: Acceptance merge facts and local projection unchanged

**Steps**:

1. Run the existing tests that exercise `validateMergeEvent` and projection (`policy_test.go`, `governance_revision_test.go`, `lineage_test.go`, `automerge_test.go`, `operational_acceptance_test.go`) and record that they pass unchanged.
2. Add `TestStandingFactLocalAcceptanceFactUnchanged`: an acceptance merge fact produced by `hn decide --accept` and `hn merge` projects and reports merged, and a hand-signed acceptance fact citing no decisions is refused with today's message `merge <short ID> lacks required acceptance evidence`.
3. Add `TestStandingFactLocalRuleFieldTriggersRuleCheck`: a fact that cites valid acceptance decisions and also carries `rule` is judged as a rule merge fact and fails `evidence-item` (a decision is not rule evidence). This proves the branch is chosen by the `rule` field alone.

## Test Strategy

Black-box tests through the built binary with facts signed in-process. Run `go test -race -run 'TestStandingFactLocal' ./...`, then the full gate.

## Definition of Done

- [ ] T031 to T034 complete; full gate passes.
- [ ] **FR-012 (local projection)**: every rule merge fact is judged by `validateRuleMergeFact`; acceptance facts keep their check and messages.
- [ ] **NFR-001 (local surface)**: F01 to F18 are refused by every projecting command, with the full fact ID and check name.
- [ ] **NFR-005**: one memo per projection pass.
- [ ] No pre-existing test assertion changed.

## Risks

- `validateEventRelationships` runs on every command; a slow memo or a per-fact policy load would slow everything. Keep allocations per pass, not per event.

## Reviewer Guidance

- Diff `validateMergeEvent`: the no-rule path must be byte-identical.
- Run one defect by hand and read the full refusal.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
