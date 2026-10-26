---
work_package_id: WP05
title: Rule merge fact validator
dependencies:
- WP03
- WP04
requirement_refs:
- FR-010
- FR-011
- FR-012
- FR-015
- NFR-002
- NFR-005
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T020
- T021
- T022
- T023
- T024
phase: Phase 2 - Shared derivation and test harness
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: rule_acceptance.go
create_intent: []
execution_mode: code_change
owned_files:
- rule_acceptance.go
- rule_acceptance_test.go
role: implementer
tags: []
tracker_refs: []
---

# WP05: Rule merge fact validator

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Add the second entry point of the rule derivation, `validateRuleMergeFact`, to `rule_acceptance.go`. It judges an immutable rule merge fact against only its base policy, the candidate's changed paths, the pipeline definition digests at the head and the fact's listed evidence (FR-012, FR-015), and returns the first failed check by a stable name ([contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Derivation"). Every fact-admission surface calls it: local projection (WP07), replication import and the `hn serve` actor-chain check (WP08), and shallow clones after their dependency guard (WP09). The executing actor's client also calls it before signing (WP11, WP15).

Also add the per-pass memo and object-access type that callers use, so one validation pass computes changed paths at most once per `(base, head)` and pipeline digests at most once per `(head, pipeline)` (NFR-005, tasks decision 5).

## Context

- Read [contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) in full (check table, rendered message, "Inputs and object access per surface", "Memoization", fixture matrix F01 to F18); [data-model.md](../data-model.md) "Qualifying evidence" and "Rule fact verdict"; spec FR-010 to FR-012 and FR-015; decision `01M5VEZXQ7GD8JJ4XNDZD6B2XD`.
- Start with `spec-kitty agent action implement WP05 --agent <name> --mission standing-acceptance-01M5VD2K`.
- From WP04: `ruleInputs`, `ruleByName`, `ruleApprovalCounts`, `ruleRequestQualifies`, `ruleResultQualifies`, `ruleAuthorAllowed`, `ruleManualPathChanges`, `ruleDisallowedPaths`, `evaluateRuleAcceptance`, the test world builder in `rule_acceptance_test.go`.
- From WP03: `Event.Rule`.
- Callers already run `validateProposalMergeBinding` (`store.go`), which requires `merge.head` to equal the candidate head, before the merge validator. This validator does not repeat it.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `rule_acceptance.go` | CHANGE. Add check names, `ruleFactError`, `validateRuleMergeFact`, memo and object access. Do not change WP04's helpers' behavior; if a helper needs a parameter, update both call sites and WP04's tests. |
| `rule_acceptance_test.go` | CHANGE. Add `TestRuleAcceptanceFact...` tests. |

## Subtasks

### Subtask T020: Check names, `ruleFactError` and the rendered message

**Steps**:

1. Constants, in contract order:

   ```go
   const (
       ruleCheckPolicyDigest      = "policy-digest"
       ruleCheckRuleExists        = "rule-exists"
       ruleCheckRuleAcceptance    = "rule-acceptance"
       ruleCheckRuleSigner        = "rule-signer"
       ruleCheckRuleAuthor        = "rule-author"
       ruleCheckManualPath        = "manual-path"
       ruleCheckChangedPaths      = "changed-paths"
       ruleCheckEvidenceItem      = "evidence-item"
       ruleCheckEvidenceDuplicate = "evidence-duplicate"
       ruleCheckApprovals         = "approval-threshold"
       ruleCheckPipelines         = "pipeline-threshold"
   )
   ```

2. Error type:

   ```go
   type ruleFactError struct {
       FactID  string // full event ID of the rule merge fact
       Check   string
       EventID string // the evidence item, when the check is about one
       Detail  string
   }
   func (e *ruleFactError) Error() string {
       return fmt.Sprintf("rule merge %s fails %s: %s", e.FactID, e.Check, e.Detail)
   }
   ```

   The full fact ID is required: US3-1 asks import to name the fact's full event ID and the failed check. `Detail` passes through `safeText` when it contains event-supplied text.

### Subtask T021: Fact checks 1 to 7

**Signature**: `func validateRuleMergeFact(inputs ruleInputs, merge StoredEvent, byID map[string]StoredEvent) error`. `inputs.RuleName` is `merge.Event.Rule`; `inputs.Events` is unused. Return `nil` or a `*ruleFactError`.

| Order | Check | Fails when | Detail |
|----|----|----|----|
| 1 | `policy-digest` | `merge.Event.Policy != inputs.PolicyDigest` | `policy <fact policy> is not the base policy digest <digest>` |
| 2 | `rule-exists` | `ruleByName` is nil (including no `automerge` section) | `rule "<name>" does not exist in the base policy` |
| 3 | `rule-acceptance` | rule is not standing | `rule "<name>" has acceptance "maintainers"` |
| 4 | `rule-signer` | `merge.Event.Actor` not in `rule.Actors` | `signer <actor> is not an executing actor of rule "<name>"` |
| 5 | `rule-author` | `!ruleAuthorAllowed` | `author <actor> is not allowed by rule "<name>"` |
| 6 | `manual-path` | `ruleManualPathChanges` reports a policy amendment or protected change | `changed path <path> requires manual merge` (name the first matching path) |
| 7 | `changed-paths` | `inputs.Changed` is empty, or a path is disallowed | `the candidate has no changed paths` or `changed path <path> is outside rule "<name>"` |

In F17 and F18 the candidate changes a manual-path file together with an allowed `src/` path, so check 6 must report before check 7.

### Subtask T022: Fact checks 8 to 11

**Steps**:

1. **Check 8, `evidence-item`**: for each ID in `merge.Event.Evidence` in listed order:
   - absent from `byID`: fail with `EventID` set and detail `evidence <id> is unavailable`. (Import and shallow surfaces report absence as a missing dependency before calling the validator; this branch keeps the local surface fail-closed.)
   - `review.submit`: must satisfy `ruleApprovalCounts(policy, rule, proposal, item)`. Otherwise detail names the reason: `review <id> is not a counting approval of rule "<name>"`.
   - `run.result`: must have outcome `passed`, its `Subject` must be present in `byID` as a `run.request`, and `ruleResultQualifies(policy, proposal, inputs.PipelineDefinitions, item, request)` must hold. Otherwise detail names the first failing reason (failed outcome, request unavailable, request not authorized, wrong pipeline digest, untrusted runner, other proposal).
   - any other kind, including `proposal.decision`: `evidence <id> has kind <kind>, which a rule merge fact cannot cite`.
2. **Check 9, `evidence-duplicate`**: after all items qualified, fail if two approvals share a reviewer, or two results share `(pipeline, runner)`. Detail names both IDs.
3. **Check 10, `approval-threshold`**: distinct counting reviewers `< proposals.requiredApprovals`: detail `N of M counted approvals`.
4. **Check 11, `pipeline-threshold`**: for each base-policy pipeline in sorted order, distinct passing runners `< requiredResults`: detail `pipeline "<name>" has N of M passing trusted results`.

Validity never depends on events outside the evidence (FR-015): do not read `byID` for anything except the listed items and the requests their results answer.

### Subtask T023: Per-pass memo and object access for callers

**Steps**:

1. Object access, supplied by each surface:

   ```go
   type ruleObjectAccess struct {
       PolicyAt       func(commit string) (PolicyDocument, string, error)
       ChangedPaths   func(base, head string) ([]string, error)
       PipelineDigest func(head, pipeline string) (string, error)
   }
   ```

2. Memo: `type ruleValidationMemo struct { ... }` with `newRuleValidationMemo(access ruleObjectAccess) *ruleValidationMemo`. It caches the base policy per commit, changed paths per `(base, head)` and pipeline digests per `(head, pipeline)`, including cached errors, so a failure is not retried within the pass.
3. `func (m *ruleValidationMemo) inputsForFact(merge, proposal StoredEvent) (ruleInputs, error)`: loads the base policy at `proposal.Event.Base`, the changed paths, and the digest of every base-policy pipeline at the head. A pipeline digest error leaves that pipeline absent from `PipelineDefinitions` (its results then fail check 8 and the threshold fails check 11). A policy or changed-path error is returned to the caller, which wraps it in its own style; it is never treated as an empty change list (FR-014).
4. `func validateRuleMergeFactWith(memo *ruleValidationMemo, merge, proposal StoredEvent, byID map[string]StoredEvent) error`: `inputsForFact` then `validateRuleMergeFact`.
5. Add a counter field (for example `changedPathCalls int`) that tests read to prove one computation per `(base, head)` per pass.

### Subtask T024: Validator unit tests and the live-to-fact property test

**Steps** (tests `TestRuleAcceptanceFact...` in `rule_acceptance_test.go`, using WP04's world builder):

1. **Valid fact**: build the US1 world, call `evaluateRuleAcceptance`, then a fact with `Rule`, `Policy` = digest, `Actor` = executing actor and `Evidence` = the result's evidence. `validateRuleMergeFact` returns nil.
2. **Defects F01 to F18**: one subtest each, built from the valid fact with exactly one change, asserting the `Check` name of the contract table (F01 `rule-exists`, F02 `rule-acceptance`, F03 `rule-signer`, F04 `policy-digest`, F05 to F11 `evidence-item`, F12 `evidence-duplicate`, F13 `approval-threshold`, F14 `pipeline-threshold`, F15 `rule-author`, F16 `changed-paths`, F17 and F18 `manual-path`), and that `Error()` contains the full fact ID and the check name.
3. **Immutability (FR-015)**: add a later withdrawal (a newer `request-changes` by the same reviewer) and a later maintainer rejection to `byID`; the fact still validates.
4. **Property test**: for at least 50 generated worlds (fixed seed) that vary reviewer sets, rule reviewers, `allowAuthorApproval`, runner counts, requesters and result order, whenever `evaluateRuleAcceptance(...).Satisfied()` holds, the fact built from its evidence validates; whenever an unmet condition of kind `approvals` or `pipeline` exists, the fact built from its evidence fails with the matching threshold check.
5. **Memo**: a fake `ruleObjectAccess` counts calls; validating three facts for the same candidate in one memo calls `ChangedPaths` once and `PipelineDigest` once per pipeline; a returned error is not retried.

## Test Strategy

Pure unit tests. Run `go test -race -run 'TestRuleAcceptance' ./...`, then the full gate.

## Definition of Done

- [ ] T020 to T024 complete; full gate passes.
- [ ] **FR-010 / FR-011 / FR-012**: all eleven checks in contract order with stable names; signer must be an executing actor of a standing rule.
- [ ] **FR-015**: validation reads only listed evidence and the requests their results answer.
- [ ] **NFR-005**: the memo proves one changed-path computation per `(base, head)` per pass.
- [ ] **NFR-002 (preparation)**: the property test passes; both entry points share the WP04 helpers.

## Risks

- Re-implementing a qualification rule inside the validator instead of calling the WP04 helper is the main way the two entry points drift. Reviewers should reject any duplicated predicate.

## Reviewer Guidance

- Check the order of checks against the contract table, row by row.
- Confirm a `proposal.decision` in evidence fails check 8, never silently ignored as today's acceptance-fact check does.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
