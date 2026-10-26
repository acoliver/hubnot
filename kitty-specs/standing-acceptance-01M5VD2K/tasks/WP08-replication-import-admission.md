---
work_package_id: WP08
title: Replication import and the hn serve actor-chain check
dependencies:
- WP07
requirement_refs:
- C-005
- FR-012
- FR-013
- FR-014
- NFR-001
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T035
- T036
- T037
- T038
- T039
phase: Phase 3 - Fact admission
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: quarantine.go
create_intent:
- standing_fact_test.go
execution_mode: code_change
owned_files:
- quarantine.go
- standing_fact_test.go
role: implementer
tags: []
tracker_refs: []
---

# WP08: Replication import and the hn serve actor-chain check

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Make replication import, the second fact-admission surface, accept a rule merge fact only after `validateRuleMergeFact`, with object access that reads base and head from one complete store (quarantine, else accepted store, never a mix). `checkServeActorDependencies` in `serve_verdict_facts.go` calls `replicationEventDependencyWithOptions` for every appended event, so this WP also covers the `hn serve` actor-chain check (research finding 4); WP16 proves that surface end to end.

Before evaluation, report recoverable gaps as missing dependencies (FR-013, FR-014):

- an absent evidence ID: the existing message, `missing dependency <id>: evidence is absent; recovery: select the full actor history supplying <id>`;
- an absent base or head commit: a missing dependency naming the commit, with recovery `add exact proposal selection --proposal <candidate>`.

A chain with an invalid rule merge fact is refused as a whole, the refusal names the fact's full event ID and the failed check, and nothing from that chain is promoted (US3-1).

## Context

- Read spec US3-1 and US3-5, FR-012 to FR-014, C-005; [contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Inputs and object access per surface" (replication and hn serve rows) and fixture rows F21 and F22; [research.md](../research.md) findings 3, 4 and 12 and decision `01M5VF02KFTE4BX3G7THDHRGCV`.
- Start with `spec-kitty agent action implement WP08 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code in `quarantine.go`: `replicationEventDependencyWithOptions` (lines 1125 to 1236; the `proposal.decision`/`proposal.merged` case at 1207 to 1224), `replicationDependencyOptions`, `replicationCommitPresent`, `loadReplicationPolicyAt`, `replicationPipelineDigestAt`, `validateReplicationMergeAt` (1346 to 1367). From WP01: `changedPathsInOneStore` (`protected_checks.go`), the one-complete-store lookup extracted from `validateProtectedDecisionAt`; read it, do not edit that file. In `serve_verdict_facts.go`: `checkServeActorDependencies` (read only).
- From WP05: `ruleObjectAccess`, `newRuleValidationMemo`, `validateRuleMergeFactWith`. From WP07: the `standing_fact_test.go` helpers. From WP06: the harness and defect table.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `quarantine.go` | CHANGE. Rule-fact dependency checks and the rule branch of `validateReplicationMergeAt`; a memo option. |
| `standing_fact_test.go` | CHANGE. Add `TestStandingFactImport...`. |

`serve_verdict_facts.go` is not changed. If threading the memo option into `checkServeActorDependencies` turns out to be necessary for NFR-006, make that one-line change with a recorded rationale; otherwise a nil memo option creates a fresh memo per event there, which is correct.

## Subtasks

### Subtask T035: Replication object access for rule facts

**Steps** in `quarantine.go`:

1. Add `func replicationRuleObjectAccess(gitDir, acceptedGitDir string) ruleObjectAccess` with:
   - `PolicyAt`: `loadReplicationPolicyAt(gitDir, acceptedGitDir, commit)`;
   - `ChangedPaths`: `changedPathsInOneStore(gitDir, acceptedGitDir, base, head)` (WP01);
   - `PipelineDigest`: `replicationPipelineDigestAt(gitDir, acceptedGitDir, head, name)`.
2. Add `RuleMemo *ruleValidationMemo` to `replicationDependencyOptions`, and a helper `func (o replicationDependencyOptions) ruleMemo(gitDir, acceptedGitDir string) *ruleValidationMemo` that returns the option's memo or a fresh one built from `replicationRuleObjectAccess`.
3. Unit test (`TestStandingFactImportObjectAccess`): with two temporary repositories, a rule fact whose base and head live only in the accepted store gets its changed paths from there; one whose base is only in the quarantine and head only in the accepted store gets an error, never a mixed or empty list.

### Subtask T036: Missing base or head commit as a replication dependency

**Steps**: in the `proposal.merged` arm of `replicationEventDependencyWithOptions`, keep the existing subject `require` and the evidence-absence loop first and unchanged. Then, only when `e.Rule != ""`:

1. If neither store holds both `proposal.Event.Base` and `proposal.Event.Head` as commits, return a `replicationDependency` with `Missing` set to the first absent commit (base before head), `Recovery: "add exact proposal selection --proposal " + candidateID`, `Key: codeOwners[candidateID]`, and `Reason: "rule merge base commit is absent"` or `"rule merge head commit is absent"`. Use `replicationCommitPresent` for presence, and require both commits in the same store for the "present" answer (check the quarantine pair, then the accepted pair).
2. For each evidence item that is a `run.result`, the request it answers must be present in `byID`; if absent, report it as a missing dependency with the existing evidence recovery text. (A result already admitted always has its request; this keeps the rule fail-closed for results arriving in the same import.)
3. Acceptance merge facts (no `rule`) skip this block entirely.

### Subtask T037: Rule branch in `validateReplicationMergeAt`

**Steps**:

1. The replication import loop that validates a batch creates one memo per import pass with `replicationRuleObjectAccess` (T035). Find the import loop that calls `replicationEventDependency` for the selected chains and pass the memo through `replicationEventDependencyWithOptions`. When the option is nil, `ruleMemo` creates a memo for this call.
2. In `validateReplicationMergeAt` (or at its call site), when `merge.Event.Rule != ""`, call `validateRuleMergeFactWith(memo, merge, proposal, byID)` and return its error. The binding check `validateProposalMergeBinding` still runs first, as today.
3. When `rule` is absent, the function body is unchanged.
4. Make sure the import reports the error the way it reports an invalid acceptance merge fact today (the chain is refused and the message includes the error text). The `ruleFactError` text already carries the full fact ID and the check.

### Subtask T038: `hn sync` import tests for F01 to F22

**Steps** (tests `TestStandingFactImport...`):

1. Setup per defect: the harness's bare remote holds the base, the candidate's code and every honest fact. The signer signs the defective fact with WP06's builders and publishes its actor ref with raw Git, `git push origin refs/hn/actors/<actor>:refs/hn/actors/<actor>`, because `hn sync` in the signer's clone projects events first and would refuse the defective fact locally (WP07). Between defects, reset the signer's actor ref to its honest head with `git update-ref` and force-push it (`git push -f`), which a plain bare remote allows, so each defect is the only defect on the chain.
2. A verifier clone (`h.Verifier()`, selection `--all`) runs `hn sync origin`.
3. For F01 to F20: the sync output names the fact's full event ID and the expected check (or the parse message for F19 and F20); `git for-each-ref refs/hn/actors/<signer>` in the verifier shows nothing promoted from that chain, or only the chain head that existed before the defect if the verifier had synced earlier; every other actor's chain is imported normally; `hn proposal status <candidate>` in the verifier does not report `merged`.
4. **F21 (US3-5)**: the fact cites an approval by S that S signed locally and never published. Sync reports `missing dependency <approval ID>: evidence is absent; recovery: select the full actor history supplying <approval ID>`. After S publishes and the verifier syncs again, the fact is admitted if it is otherwise valid.
5. **F22 (FR-014)**: base or head commit absent from every store. Build it with an exact selection that imports the actor chains but not the proposal code ref, or a candidate whose head was never pushed. Assert a missing dependency naming the commit with recovery `add exact proposal selection --proposal <candidate>`. If the importer reports the proposal's own absent code ref first, assert that instead and record it in the Activity Log: both mean nothing was evaluated against an empty change list.
6. Keep the matrix fast: one harness, and one fresh verifier clone per defect (a clone is cheap; a refused chain leaves the verifier unchanged, so a verifier could be reused, but a fresh one keeps each case independent). Target under two minutes for the whole test on the gate host; log the time with `t.Logf`.

### Subtask T039: Import of a valid rule merge fact into a fresh clone

**Steps**:

1. `ReadyCandidate`, `SignFact("E", nil)`, raw-push E's actor ref, then a fresh verifier clone with no identity runs `hn sync origin` and `hn proposal status <candidate>`: every event verifies, status reports `merged`.
2. Re-run `hn sync origin`: no new outcome, no error (idempotent).
3. An acceptance merge fact imported in the same sync keeps today's behavior (sanity check against `replication_test.go` patterns).

## Test Strategy

Black-box tests through real temporary bare remotes. Run `go test -race -run 'TestStandingFactImport|TestReplication|TestProtected' ./...`, then the full gate.

## Definition of Done

- [ ] T035 to T039 complete; full gate passes.
- [ ] **FR-012 (replication and serve actor chain)**: rule merge facts are admitted only after `validateRuleMergeFact` with one-complete-store object access.
- [ ] **FR-013**: absent evidence is a missing dependency with the existing message and recovery.
- [ ] **FR-014**: absent base or head is a missing dependency naming the commit; no empty change list is ever evaluated.
- [ ] **NFR-001 (import surface)**: F01 to F22 refused, zero facts promoted from the defective chain.
- [ ] **C-005**: every refusal leaves the verifier's refs unchanged apart from honest chains.
- [ ] `protected_checks.go` is not edited by this WP.

## Risks

- The quarantine may hold the head while the accepted store holds the base. The one-store rule refuses that pair; the test for F22 must not accidentally pass by mixing stores.
- Replication outcome text is read by existing tests; add, do not reword.

## Reviewer Guidance

- Read the diff of `replicationEventDependencyWithOptions` and confirm the acceptance-fact path is untouched.
- Run F21 by hand and read the recovery line.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
