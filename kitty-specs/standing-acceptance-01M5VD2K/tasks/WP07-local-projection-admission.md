---
work_package_id: WP07
title: Local projection admits merge facts through the union rule
dependencies:
- WP02
- WP05
- WP06
requirement_refs:
- C-004
- FR-012
- FR-023
- FR-024
- FR-025
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
- T092
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
- docs/protocol-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP07: Local projection admits merge facts through the union rule

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Make local event projection, the first of the four fact-admission surfaces (FR-012), judge every `proposal.merged` through the shared union combinator `admitMergeFact` (FR-024, decision `01M5VSEHEVA82N9T64ZH1P1Q2A`):

1. path (a): today's body of `validateMergeEvent`, extracted unchanged as `maintainerMergeCheck`, runs first for every fact, whatever its `rule` field says; success is a maintainer merge;
2. path (b): only when path (a) fails and the fact carries a canonical rule claim, local object access with one memo per projection pass (NFR-005) and `validateRuleMergeFact`.

Every fact that projects today keeps projecting with the same meaning, including maintainer merges that carry a rule claim and legacy events with rule-named keys (C-004). Because every command projects events, an invalid fact in a local store makes every command refuse, exactly as an invalid acceptance merge fact does today (US3-3).

Fail closed at this boundary (FR-025, decision `01M5VSEHF5B1FM9TB52KWKQGWZ`): in a shallow repository, path (b) refuses with `rule merge <full fact ID> cannot be verified in a shallow clone by this build; verify it in a full clone` until WP09 replaces that refusal with the shallow closure rules. Replication import and the `hn serve` actor-chain check still run path (a) only until WP08. This WP creates the boundary test that records which surfaces implement path (b).

This WP must land before any client records a rule merge fact (WP11): without it, the executing actor's own clone would reject its own fact.

It also documents merge facts in `docs/protocol-v0.md`: the `rule` field WP03 added, how a rule claim is recognized, the union rule, and the evidence semantics of path (b) (HN-005, FR-023).

## Context

- Read [contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Rule claim recognition", "Union admission", "Inputs and object access per surface" (the local projection row), "Memoization", "Legacy history fixtures" and "Activation boundary fixtures"; spec US3-3, US3-6 and US3-7; plan "Changes to existing code" rows for `policy.go` and `store.go` and "Activation boundaries".
- WP02 is a dependency only because both WPs edit `docs/protocol-v0.md` (different sections); it orders the edits.
- Start with `spec-kitty agent action implement WP07 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code: `validateEventRelationships` (`store.go` line 306; the `proposal.merged` case at lines 357 to 367 calls `validateProposalMergeBinding` then `validateMergeEvent`); `validateMergeEvent` (`policy.go` lines 547 to 568); `loadPolicy`, `loadPipeline` (`ci.go`), `changedPathsAt("", base, head)` (`protected_checks.go`), `repositoryIsShallow` (`shallow.go`, read only). `validateEventRelationships` is also called from `issue_read.go` line 104.
- From WP05: `ruleObjectAccess`, `newRuleValidationMemo`, `validateRuleMergeFactWith`, `ruleFactError`, `admitMergeFact`, `mergeAuthority`.
- From WP06: `newStandingHarness`, `ReadyCandidate`, `SignFact`, `SignRaw`, `standingHarnessDefects`, `standingHarnessLegacyFixtures`, `standingHarnessBoundaryFixtures`, `Run`, `Refs`.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `policy.go` | CHANGE. `maintainerMergeCheck` (today's body, unchanged), `localRuleObjectAccess`, `validateMergeEventWith` through `admitMergeFact`, and the interim shallow refusal of path (b). WP09 replaces the interim refusal; WP11 later adds the status lines in `cmdProposalStatus`; do not touch it here. |
| `store.go` | CHANGE. `validateEventRelationships` creates one memo per pass and passes it on. |
| `standing_fact_test.go` | NEW. `TestStandingFactLocal...` and `TestStandingFactBoundary...`. WP08, WP09 and WP16 add tests later. |
| `docs/protocol-v0.md` | CHANGE. "Runs and governance events" only: the merge fact paragraphs. WP02 edits "Optional policy rules"; do not touch it. |

## Subtasks

### Subtask T031: Union admission in `validateMergeEvent` and the per-pass memo in `store.go`

**Steps**:

1. Move today's body of `validateMergeEvent` into `func maintainerMergeCheck(merge, proposal StoredEvent, byID map[string]StoredEvent) error`, byte for byte, with the same messages and the same `shortID` use. It is path (a) of the spec invariant; WP11 reuses it for status.
2. Add `func localRuleObjectAccess() ruleObjectAccess` in `policy.go`:
   - `PolicyAt`: `loadPolicy(commit)` returning the policy and digest.
   - `ChangedPaths`: `changedPathsAt("", base, head)`.
   - `PipelineDigest`: the digest returned by `loadPipeline(head, name)`.
3. Add `func validateMergeEventWith(memo *ruleValidationMemo, merge, proposal StoredEvent, byID map[string]StoredEvent) (mergeAuthority, error)` that returns `admitMergeFact(merge, maintainerPath, rulePath)` with:
   - `maintainerPath`: `maintainerMergeCheck(merge, proposal, byID)`;
   - `rulePath`: when `repositoryIsShallow()` reports true (or fails), return the interim refusal `rule merge <full fact ID> cannot be verified in a shallow clone by this build; verify it in a full clone`; otherwise `validateRuleMergeFactWith(memo, merge, proposal, byID)`, wrapping a non-`ruleFactError` error (policy unavailable, changed paths unavailable) as `merge %s: %w` with the full fact ID and returning a `ruleFactError` unwrapped so its message reads `rule merge <full ID> fails <check>: <detail>` on every surface.
4. Keep `validateMergeEvent(merge, proposal, byID) error` as a wrapper that creates a fresh memo with `localRuleObjectAccess()` and drops the authority. Existing tests that call it keep working, and for every fact without a rule claim it returns exactly today's error.
5. Per-pass memo in `store.go`: in `validateEventRelationships`, create `memo := newRuleValidationMemo(localRuleObjectAccess())` once before the loop over events, and call `validateMergeEventWith(memo, stored, proposal, byID)` in the `proposal.merged` case. Leave the binding check (`validateProposalMergeBinding`) first, as today. Do not create the memo inside the loop; one per pass is the contract (NFR-005). Do not change any other case of the switch.
6. Missing objects (FR-014, local part): path (b) only. When `ChangedPaths` fails because a commit is absent, the error propagates and projection refuses; it never evaluates an empty list. A fact that passes path (a) never reads the commits.

### Subtask T032: Protocol guide for merge facts

**Steps** in `docs/protocol-v0.md`, "Runs and governance events", after the existing `proposal.merged` paragraph:

1. A paragraph on the union rule: a `proposal.merged` fact is valid when it passes the existing maintainer check, whatever else it carries, or, failing that, when it carries a rule claim and passes the rule check; a fact that passes neither is invalid. A maintainer merge that also carries a rule claim is displayed as a maintainer merge with the rule name marked as not verified.
2. A paragraph on rule merge facts: the optional `rule` field names an automerge rule with acceptance mode `rule` in the candidate's base policy; the signer must be one of that rule's executing actors; `evidence` lists exactly the qualifying approval and passing run-result IDs, at most one per counting reviewer and one per trusted runner per pipeline, and any other item, including a `proposal.decision`, fails the rule check; `policy` is the base policy digest; `commit` is either a two-parent merge commit or the head itself after an `hn serve` fast-forward.
3. The rule claim recognition rules of [contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Rule claim recognition", as a short table, and the sentence that no event is refused because of a `rule` key: other kinds ignore it as before, and resource kinds refuse it as before.
4. One sentence on verification: every verifier re-derives the rule from the base policy, the candidate's base and head and the listed evidence, and judges the fact by that evidence alone; vetoes outside the evidence block execution and server advancement, never fact validity (FR-015). Name the stable check names of the contract and the rendered refusal form `rule merge <full fact ID> fails <check>: <detail>`.
5. One sentence on older clients: they ignore the key and run the maintainer check, so they reject every fact that only the rule check admits and agree on every maintainer merge.
6. Keep the existing paragraph on missing-event repair accurate: under acceptance mode `maintainers` repair still uses `hn merge`; add that under acceptance mode `rule` the executing actor reruns the same `hn automerge` command (FR-020, implemented in WP12 and WP19).
7. Do not add the `standing-rule-merge-fact` example; WP18 adds it with a recorded fact. Style: plain direct sentences, no em dashes.

### Subtask T033: Local-store tests for F01 to F20 and the valid fact

**Steps** (tests `TestStandingFactLocal...` in `standing_fact_test.go`, black-box through the built binary, facts signed with the WP06 builders):

1. **Valid fact**: `ReadyCandidate`, then `SignFact("E", nil)` for it. In E's clone, `hn proposal status <candidate>` exits 0 and reports `Status:   merged` (the rule-naming line arrives in WP11; do not assert it here). `hn log` or another projecting command also succeeds.
2. **Defects**: for each defect F01 to F20 of `standingHarnessDefects`, in a fresh harness or a fresh copy of the signer's clone, sign the defective fact into the signer's chain and run `hn proposal status <candidate>`: non-zero exit; output contains `rule merge <full fact ID> fails <expected check>` for F01 to F18, and the maintainer-path refusal `merge <short ID> is not authorized by the proposal policy` for F19 and F20; `h.Refs(role)` is unchanged by the command. Run a second projecting command (for example `hn issue list` or `hn log`) and assert it refuses the same way (US3-3: "any command").
3. **Cost**: share one harness across defects when the defect does not need a special candidate, by signing each defect in its own throwaway clone made with `git clone --no-local` of the signer's clone plus a copy of its identity, or by resetting the signer's actor ref with `git update-ref` after each case. Choose the faster approach and record it.
4. **Memo (NFR-005)**: two valid rule merge facts for the same candidate (signed by E, and by a second executing actor listed in the same rule for this test) project with one changed-path computation. Observe this through a test-only counter exposed via `HN_INTERNAL_TESTING=1` only if a counter is already a pattern in the codebase; otherwise rely on WP05's unit test and skip a black-box counter. Record the decision.

### Subtask T034: Maintainer merges and legacy history unchanged

**Steps**:

1. Run the existing tests that exercise `validateMergeEvent` and projection (`policy_test.go`, `governance_revision_test.go`, `lineage_test.go`, `automerge_test.go`, `operational_acceptance_test.go`) and record that they pass unchanged.
2. `TestStandingFactLocalAcceptanceFactUnchanged`: an acceptance merge fact produced by `hn decide --accept` and `hn merge` projects and reports merged, and a hand-signed acceptance fact citing no decisions is refused with today's message `merge <short ID> lacks required acceptance evidence`.
3. `TestStandingFactLocalRuleClaimOnMaintainerMerge` (US3-6, analysis counterexample C1): M signs a fact with enough M accept decisions and `rule` set to `nosuch`, and another with `rule` set to `ordinary`; both project and report merged. This proves path (a) runs first and that a rule claim never makes a maintainer merge invalid.
4. `TestStandingFactLocalLegacyHistory` (US3-7, C-004): each legacy fixture L01 to L06 of `standingHarnessLegacyFixtures` placed in a local store gives its expected verdict: L01 to L05 project as before, L06 is refused as before.

### Subtask T092: Activation boundary test

**Steps** (`TestStandingFactBoundary` in `standing_fact_test.go`, decision `01M5VSEHF5B1FM9TB52KWKQGWZ`):

1. Declare one table, `standingFactRulePathSurfaces`, mapping each surface name (`local`, `import`, `shallow`) to whether it implements path (b). This WP sets `local: true`, `import: false`, `shallow: false`. WP08 and WP09 change only their own entry; the `hn serve` actor-chain surface follows `import` (same code) and is exercised end to end in WP16.
2. For each boundary fixture B1 to B4 of `standingHarnessBoundaryFixtures` and each surface: local projection in a full clone; `hn sync origin` in a fresh full verifier after the signer's actor ref is raw-pushed; `hn proposal status` in a shallow verifier that selects the signer's chain with the complete closure. Assert: B1 is accepted exactly where the table says path (b) is implemented and refused elsewhere; B2 and B3 are accepted everywhere as maintainer merges; B4 is refused everywhere.
3. Assert the shallow refusal of B1 at this boundary carries the interim message, so WP09 can see what it replaces. If the shallow verifier can obtain the signer's chain only through `hn sync`, import refuses B1 first at this boundary; then record the observed refusal and assert the interim message with an in-process call to `validateMergeEventWith` from inside the shallow clone (`t.Chdir`), which still exercises the production path.
4. Keep the table as the only place the expected boundary state lives, so a later WP that forgets to update it fails this test.

## Test Strategy

Black-box tests through the built binary with facts signed in-process. Run `go test -race -run 'TestStandingFactLocal|TestStandingFactBoundary' ./...`, then the full gate.

## Definition of Done

- [ ] T031 to T034 and T092 complete; full gate passes.
- [ ] **FR-024 (local projection)**: every merge fact is judged by `admitMergeFact`, path (a) first; `maintainerMergeCheck` is byte-identical to today's body.
- [ ] **FR-012 (local projection)**: every fact that needs path (b) is judged by `validateRuleMergeFact`.
- [ ] **NFR-001 (local surface)**: F01 to F20 are refused by every projecting command with the expected check or maintainer-path refusal.
- [ ] **C-004**: legacy fixtures L01 to L06 give their expected verdicts; maintainer merges with a rule claim project.
- [ ] **FR-025**: the boundary table and test exist; shallow path (b) refuses with the interim message.
- [ ] **NFR-005**: one memo per projection pass.
- [ ] **FR-023 (merge facts)**: protocol guide paragraphs and the claim table written.
- [ ] No pre-existing test assertion changed.

## Risks

- `validateEventRelationships` runs on every command; a slow memo or a per-fact policy load would slow everything. Keep allocations per pass, not per event, and never compute path (b) inputs for a fact that passes path (a).

## Reviewer Guidance

- Diff `validateMergeEvent` and `maintainerMergeCheck`: path (a) must be byte-identical to today's body.
- Confirm no code in `policy.go` or `store.go` orders the two paths itself; only `admitMergeFact` does.
- Run one defect and one legacy fixture by hand and read the full output.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
- 2026-10-26T09:45:00Z – llxprt – Prompt revised after analysis (C1, C2, I1; decisions 01M5VSEHEVA82N9T64ZH1P1Q2A and 01M5VSEHF5B1FM9TB52KWKQGWZ): union admission through admitMergeFact, interim shallow refusal, legacy tests, T092 boundary test
