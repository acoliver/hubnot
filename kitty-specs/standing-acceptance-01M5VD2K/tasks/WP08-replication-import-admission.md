---
work_package_id: WP08
title: Replication import and the hn serve actor-chain check
dependencies:
- WP07
requirement_refs:
- C-004
- C-005
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

Make replication import, the second fact-admission surface, judge every `proposal.merged` through the shared union combinator `admitMergeFact` (FR-024): today's `validateReplicationMergeAt` runs first and unchanged as path (a), and only a fact that fails it and carries a canonical rule claim reaches path (b), which reads base and head from one complete store (quarantine, else accepted store, never a mix) and calls `validateRuleMergeFact`. Every fact and legacy event that imports today keeps importing with the same verdict (C-004). `checkServeActorDependencies` in `serve_verdict_facts.go` calls `replicationEventDependencyWithOptions` for every appended event, so this WP also covers the `hn serve` actor-chain check (research finding 4); WP16 proves that surface end to end.

Report recoverable gaps as missing dependencies (FR-013, FR-014):

- an absent evidence ID, before either path, exactly as today: `missing dependency <id>: evidence is absent; recovery: select the full actor history supplying <id>`;
- an absent base or head commit, inside path (b) only: a missing dependency naming the commit, with recovery `add exact proposal selection --proposal <candidate>`. A fact that passes path (a) never waits for its commits.

At this boundary, local projection (WP07) and replication import with the `hn serve` actor-chain check implement path (b); shallow verification still refuses rule-only facts until WP09 (FR-025). Update the boundary table of WP07's `TestStandingFactBoundary` for `import` and nothing else.

A chain with an invalid rule merge fact is refused as a whole, the refusal names the fact's full event ID and the failed check, and nothing from that chain is promoted (US3-1).

## Context

- Read spec US3-1 and US3-5, FR-012 to FR-014, C-005; [contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Inputs and object access per surface" (replication and hn serve rows) and fixture rows F21 and F22; [research.md](../research.md) findings 3, 4 and 12 and decision `01M5VF02KFTE4BX3G7THDHRGCV`.
- Start with `spec-kitty agent action implement WP08 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code in `quarantine.go`: `replicationEventDependencyWithOptions` (lines 1125 to 1236; the `proposal.decision`/`proposal.merged` case at 1207 to 1224), `replicationDependencyOptions`, `replicationCommitPresent`, `loadReplicationPolicyAt`, `replicationPipelineDigestAt`, `validateReplicationMergeAt` (1346 to 1367). From WP01: `changedPathsInOneStore` (`protected_checks.go`), the one-complete-store lookup extracted from `validateProtectedDecisionAt`; read it, do not edit that file. In `serve_verdict_facts.go`: `checkServeActorDependencies` (read only).
- From WP05: `ruleObjectAccess`, `newRuleValidationMemo`, `validateRuleMergeFactWith`, `admitMergeFact`. From WP07: the `standing_fact_test.go` helpers and the boundary table. From WP06: the harness, the defect table and the legacy and boundary fixtures.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `quarantine.go` | CHANGE. The `proposal.merged` arm calls `admitMergeFact` with today's `validateReplicationMergeAt` (unchanged) as path (a) and a rule path that reports commit dependencies and validates; a memo option. |
| `standing_fact_test.go` | CHANGE. Add `TestStandingFactImport...`; set `import: true` in the boundary table. |

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

### Subtask T036: Missing base or head commit as a replication dependency of path (b)

**Steps**: in the `proposal.merged` arm of `replicationEventDependencyWithOptions`, keep the existing subject `require`, the evidence-absence loop and `validateProposalMergeBinding` first and unchanged. Write the following as part of the rule path closure of T037, so it runs only after path (a) failed and only when `e.Rule != ""`:

1. If neither store holds both `proposal.Event.Base` and `proposal.Event.Head` as commits, return a `replicationDependency` with `Missing` set to the first absent commit (base before head), `Recovery: "add exact proposal selection --proposal " + candidateID`, `Key: codeOwners[candidateID]`, and `Reason: "rule merge base commit is absent"` or `"rule merge head commit is absent"`. Use `replicationCommitPresent` for presence, and require both commits in the same store for the "present" answer (check the quarantine pair, then the accepted pair).
2. For each evidence item that is a `run.result`, the request it answers must be present in `byID`; if absent, report it as a missing dependency with the existing evidence recovery text. (A result already admitted always has its request; this keeps the rule fail-closed for results arriving in the same import.)
3. Return a dependency from the closure as a typed error (for example `*ruleMergeDependency` wrapping the `replicationDependency`), which the arm unwraps with `errors.As` and returns as today's dependency result.
4. Facts that pass path (a), and facts without a rule claim, never reach this block.

### Subtask T037: Union admission in the `proposal.merged` arm

**Steps**:

1. The replication import loop that validates a batch creates one memo per import pass with `replicationRuleObjectAccess` (T035). Find the import loop that calls `replicationEventDependency` for the selected chains and pass the memo through `replicationEventDependencyWithOptions`. When the option is nil, `ruleMemo` creates a memo for this call.
2. Replace the arm's final `return nil, validateReplicationMergeAt(...)` with `admitMergeFact(event, maintainerPath, rulePath)`, where `maintainerPath` is today's `validateReplicationMergeAt(gitDir, acceptedGitDir, event, byID[e.Subject], byID)` and `rulePath` runs T036 and then `validateRuleMergeFactWith(memo, event, proposal, byID)`. Do not edit the body of `validateReplicationMergeAt`.
3. The arm returns `(nil, nil)` for either authority, the dependency for a typed dependency error, and `(nil, err)` otherwise. For a fact without a rule claim, the returned error is exactly today's.
4. Make sure the import reports the error the way it reports an invalid acceptance merge fact today (the chain is refused and the message includes the error text). The `ruleFactError` text already carries the full fact ID and the check.

### Subtask T038: `hn sync` import tests for F01 to F22

**Steps** (tests `TestStandingFactImport...`):

1. Setup per defect: the harness's bare remote holds the base, the candidate's code and every honest fact. The signer signs the defective fact with WP06's builders and publishes its actor ref with raw Git, `git push origin refs/hn/actors/<actor>:refs/hn/actors/<actor>`, because `hn sync` in the signer's clone projects events first and would refuse the defective fact locally (WP07). Between defects, reset the signer's actor ref to its honest head with `git update-ref` and force-push it (`git push -f`), which a plain bare remote allows, so each defect is the only defect on the chain.
2. A verifier clone (`h.Verifier()`, selection `--all`) runs `hn sync origin`.
3. For F01 to F20: the sync output names the fact's full event ID and the expected check for F01 to F18, and today's maintainer-path refusal `merge <full ID> is not authorized by exact base policy <digest>` for F19 and F20 (no rule claim); `git for-each-ref refs/hn/actors/<signer>` in the verifier shows nothing promoted from that chain, or only the chain head that existed before the defect if the verifier had synced earlier; every other actor's chain is imported normally; `hn proposal status <candidate>` in the verifier does not report `merged`.
4. **F21 (US3-5)**: the fact cites an approval by S that S signed locally and never published. Sync reports `missing dependency <approval ID>: evidence is absent; recovery: select the full actor history supplying <approval ID>`. After S publishes and the verifier syncs again, the fact is admitted if it is otherwise valid.
5. **F22 (FR-014)**: base or head commit absent from every store. Build it with an exact selection that imports the actor chains but not the proposal code ref, or a candidate whose head was never pushed. Assert a missing dependency naming the commit with recovery `add exact proposal selection --proposal <candidate>`. If the importer reports the proposal's own absent code ref first, assert that instead and record it in the Activity Log: both mean nothing was evaluated against an empty change list.
6. Keep the matrix fast: one harness, and one fresh verifier clone per defect (a clone is cheap; a refused chain leaves the verifier unchanged, so a verifier could be reused, but a fresh one keeps each case independent). Target under two minutes for the whole test on the gate host; log the time with `t.Logf`.

7. **Maintainer merges with a rule claim and legacy history** (US3-6, US3-7, C-004): the boundary fixtures B2 and B3 and the legacy fixtures L01 to L05 of WP06, raw-pushed the same way, import into a fresh verifier and give their expected verdicts (B2, B3, L03 to L05: `hn proposal status` reports merged; L01 and L02: the events are promoted); L06 is refused as before. Set `import: true` in `standingFactRulePathSurfaces` and run `TestStandingFactBoundary`.

### Subtask T039: Import of a valid rule merge fact into a fresh clone

**Steps**:

1. `ReadyCandidate`, `SignFact("E", nil)`, raw-push E's actor ref, then a fresh verifier clone with no identity runs `hn sync origin` and `hn proposal status <candidate>`: every event verifies, status reports `merged`.
2. Re-run `hn sync origin`: no new outcome, no error (idempotent).
3. An acceptance merge fact imported in the same sync keeps today's behavior (sanity check against `replication_test.go` patterns).

## Test Strategy

Black-box tests through real temporary bare remotes. Run `go test -race -run 'TestStandingFactImport|TestReplication|TestProtected' ./...`, then the full gate.

## Definition of Done

- [ ] T035 to T039 complete; full gate passes.
- [ ] **FR-024 (replication and serve actor chain)**: the arm calls `admitMergeFact`; `validateReplicationMergeAt` is unchanged and runs first.
- [ ] **FR-012 (replication and serve actor chain)**: facts that need path (b) are admitted only after `validateRuleMergeFact` with one-complete-store object access.
- [ ] **C-004 / FR-025**: legacy fixtures and maintainer merges with a rule claim import as before; the boundary table marks `import` and the boundary test passes.
- [ ] **FR-013**: absent evidence is a missing dependency with the existing message and recovery.
- [ ] **FR-014**: in path (b), absent base or head is a missing dependency naming the commit; no empty change list is ever evaluated.
- [ ] **NFR-001 (import surface)**: F01 to F22 refused, zero facts promoted from the defective chain.
- [ ] **C-005**: every refusal leaves the verifier's refs unchanged apart from honest chains.
- [ ] `protected_checks.go` is not edited by this WP.

## Risks

- The quarantine may hold the head while the accepted store holds the base. The one-store rule refuses that pair; the test for F22 must not accidentally pass by mixing stores.
- Replication outcome text is read by existing tests; add, do not reword.

## Reviewer Guidance

- Read the diff of `replicationEventDependencyWithOptions` and confirm the acceptance-fact path is untouched and runs first, and that commit presence is checked only inside the rule path.
- Run F21 by hand and read the recovery line.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
- 2026-10-26T09:45:00Z – llxprt – Prompt revised after analysis (C1, C2; decisions 01M5VSEHEVA82N9T64ZH1P1Q2A and 01M5VSEHF5B1FM9TB52KWKQGWZ): union admission, commit dependencies only in path (b), legacy and boundary fixtures on import
