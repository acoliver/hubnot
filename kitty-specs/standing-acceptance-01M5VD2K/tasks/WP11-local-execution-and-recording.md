---
work_package_id: WP11
title: Local execution, rule merge fact and status
dependencies:
- WP07
- WP08
- WP09
- WP10
requirement_refs:
- FR-009
- FR-010
- FR-011
- FR-021
- FR-023
- FR-024
- NFR-002
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T048
- T049
- T050
- T051
- T052
- T053
- T094
phase: Phase 4 - Local standing acceptance
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: governance.go
create_intent:
- standing_automerge_test.go
execution_mode: code_change
owned_files:
- governance.go
- automerge.go
- policy.go
- standing_automerge_test.go
- docs/governance-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP11: Local execution, rule merge fact and status

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Deliver User Story 1 locally. An executing actor E that is not a maintainer runs:

```text
hn automerge PROPOSAL --rule ordinary --execute
```

in a clean full clone whose checked-out `main` equals the signed base. `main` advances to a native two-parent merge commit, and E's chain gains one `proposal.merged` with `rule` = `ordinary`, `policy` = base policy digest and `evidence` = the qualifying approval and passing run-result IDs (FR-009, FR-010, FR-011). No maintainer signs anything for that proposal (SC-002). Before signing, the client checks the exact fact with `validateRuleMergeFact` and refuses to sign a fact its own projection would reject. `hn proposal status` names the rule and the signer (FR-021).

The rule authority replaces only the acceptance and maintainer checks of `mergeProposal`; every other check, the expected-old ref lease, the checkout behavior and `hn merge` itself stay unchanged (plan "Domain rules and invariants" rule 4).

## Context

- Read spec US1 (all four scenarios), FR-009 to FR-011 and FR-021, SC-002; [contracts/automerge-cli.md](../contracts/automerge-cli.md) "Local execution"; [contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Recording a rule merge fact" and "hn proposal status"; [research.md](../research.md) finding 6 and decision `01M5VF06S28D7WBPBE9K7BCK18`.
- Start with `spec-kitty agent action implement WP11 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code: `proposalMergeGuard`, `mergeProposal`, `appendProposalMergeEvent`, `cmdMerge` (`governance.go`; the acceptance and maintainer checks are at lines 172, 185, 241, 247, 285 and 291); `cmdAutomerge`, `advanceAutomerge` (`automerge.go`); `cmdProposalStatus` (`policy.go`); `nextEvent`, `encodeAndSign`, `eventID` (`event.go`); `appendEvent` (`store.go`); `collectEvents`.
- From WP05: `validateRuleMergeFactWith`, `newRuleValidationMemo`, `admitMergeFact`, `mergeAuthority`. From WP07: `localRuleObjectAccess`, `maintainerMergeCheck`, `validateMergeEventWith`. From WP10: `automergeInspection.RuleAcceptance`. From WP06: the harness and the boundary fixtures B2 and B3.
- WP09 is a dependency only because both WPs edit `policy.go` (WP09 the rule path of `validateMergeEventWith`, this WP `cmdProposalStatus`); it orders the edits.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `governance.go` | CHANGE. Rule authority in `proposalMergeGuard`, the four reachable checks in `mergeProposal`, `appendRuleMergeEvent`, the recording branch. `cmdMerge` and `cmdDecide` unchanged. |
| `automerge.go` | CHANGE. `cmdAutomerge` builds the rule authority. Do not touch `inspectAutomerge` beyond what WP10 did, and do not touch `automergeRemote` (WP15). |
| `policy.go` | CHANGE. `cmdProposalStatus` status lines only. |
| `standing_automerge_test.go` | CHANGE. Add `TestStandingAutomergeExecute...`. |
| `docs/governance-v0.md` | CHANGE. Execution text and the `standing-execute` example. |

## Subtasks

### Subtask T048: Rule authority in `proposalMergeGuard` and `mergeProposal`

**Steps**:

1. Add to `governance.go`:

   ```go
   // ruleMergeAuthority lets hn automerge stand in a standing rule for the
   // per-proposal acceptance and maintainer checks of mergeProposal. Only
   // cmdAutomerge builds one; hn merge never does.
   type ruleMergeAuthority struct {
       Name   string
       Latest *ruleAcceptance // refreshed by the guard's Check on every re-inspection
   }
   ```

   and a field `Rule *ruleMergeAuthority` on `proposalMergeGuard`.
2. Add `func (g *proposalMergeGuard) authorize(identity *Identity, evaluation *ProposalEvaluation) error`: with a rule authority, require `g.Rule.Latest != nil`, `g.Rule.Latest.Satisfied()`, and `actorListed(identity.Actor, rule.Actors)` for the rule named `g.Rule.Name` in `evaluation.Policy` (the base policy); refusal `identity %s is not an executing actor for automerge rule %q` (existing text). Without a rule authority it returns the existing maintainer check result with the existing message.
3. In `mergeProposal`, with a rule authority only:
   - line 172 `!evaluation.Accepted`: skipped;
   - line 185 maintainer check: replaced by `guard.authorize(identity, evaluation)`;
   - line 285: keep `Merged`, `Rejected` and `!Ready` in the condition, drop `!Accepted`; keep the message;
   - line 291 maintainer check: replaced by `guard.authorize`.
   Lines 241 and 247 sit in the contained-merge repair block, which already refuses whenever a guard is present (line 216), so they stay unchanged; local rule repair lives in `hn automerge` (WP12).
4. Recording at the end of `mergeProposal`: with a rule authority, call `appendRuleMergeEvent(identity, proposal, evaluation, guard.Rule.Latest, mergeCommit)` and print, after the existing `Merged proposal ...` line:

   ```text
   Recorded rule merge event 7d8e9f0a1b2c under rule ordinary with 2 evidence item(s)
   ```

   Otherwise the existing `appendProposalMergeEvent` and output.
5. With `guard == nil` (`hn merge`) or a guard without a rule authority (automerge under acceptance mode `maintainers`), every line of `mergeProposal` behaves as before. Write the changes as `if guard != nil && guard.Rule != nil { ... } else { existing }` so the diff shows the existing code untouched.

### Subtask T049: `appendRuleMergeEvent` with the pre-sign self-check

**Steps**:

1. Signature: `func appendRuleMergeEvent(identity *Identity, proposal *StoredEvent, evaluation *ProposalEvaluation, acceptance *ruleAcceptance, resultCommit string) (*StoredEvent, error)`.
2. Build with `nextEvent(identity, "proposal.merged")`: `Subject` = candidate ID, `Policy` = `evaluation.PolicyDigest`, `Head` = candidate head, `Commit` = `resultCommit`, `Evidence` = `acceptance.Evidence`, `Rule` = `acceptance.Rule.Name`.
3. Self-check before writing: `encodeAndSign(event, identity)`, `id := eventID(payload)`, `fact := StoredEvent{ID: id, Event: event, Payload: payload}`; `collectEvents()` and an index `byID` plus `fact`; `validateProposalMergeBinding(fact, *proposal)` and `validateMergeEventWith(newRuleValidationMemo(localRuleObjectAccess()), fact, *proposal, byID)`, which runs the same `admitMergeFact` order as every verifier. Unless it returns `mergeByRule` with no error, return `refusing to sign a rule merge fact that verifiers would reject: <message>` and write nothing. Requiring `mergeByRule` keeps a standing-rule merge from being recorded as anything but a rule merge.
4. Then `appendEvent(event, identity)`. ed25519 signatures are deterministic and the event value is identical, so the stored ID equals `id`; assert it and return an internal error if it differs.
5. The caller already merged the code when this runs. On refusal, `mergeProposal` returns `code merged at <commit> but recording failed: ...` as it does today; WP12's repair path records the fact once the cause is resolved.

### Subtask T050: `cmdAutomerge` wiring for acceptance mode `rule`

**Steps**:

1. After the existing identity, executing-actor and upstream checks, when `inspection.Rule.Acceptance.Standing()`, set `guard.Rule = &ruleMergeAuthority{Name: *rule, Latest: inspection.RuleAcceptance}`.
2. In `guard.Check` (which re-runs `inspectAutomerge` and refuses on any change), also refresh `guard.Rule.Latest = next.RuleAcceptance` when the authority is present, and refuse with the existing "automerge candidate, policy or target changed before merge" text if the refreshed acceptance is not satisfied. The comment "The common merge path signs this exact latest acceptance snapshot" now also covers the rule evidence.
3. Acceptance mode `maintainers`: no authority; behavior unchanged, including US5-4, where the inspection's rule threshold (WP10) is part of eligibility and the recorded fact is an acceptance merge fact.
4. A maintainer listed in a standing rule's `actors` also gets the authority and records a rule merge fact (FR-011, spec edge case).

### Subtask T051: `hn proposal status` lines for rule claims

**Steps**: in `cmdProposalStatus`, after `Status:   ...`, for each merge event in `evaluation.MergeEvents` with a non-empty `Rule`, sorted by event ID, classify it with `maintainerMergeCheck` (projection already admitted it, so a failing maintainer check means it was admitted through path (b)) and print:

```text
Merged by rule ordinary, signed by <full executor fingerprint> in fact sha256:<full fact ID>
```

for a rule merge, or, for a maintainer merge that carries a rule claim (FR-021, US3-6):

```text
Merged by maintainers in fact sha256:<full fact ID>; its rule annotation "ordinary" is not verified
```

Pass the rule name through `safeText`. Merge facts without a rule claim print no new line. No other line changes.

### Subtask T052: Black-box execution tests for US1-1, US1-2 and US1-3

**Steps** (tests `TestStandingAutomergeExecute...`):

1. **US1-1**: default policy; `ReadyCandidate` changing `src/` and `tests/`; E's `main` at the base and clean. `hn automerge <c> --rule ordinary --execute` exits 0. `main` is a commit with exactly two parents, base then head, and the head's tree. E's chain gained exactly one event: `proposal.merged` with `rule` `ordinary`, `policy` the base digest, `evidence` exactly the sorted approval and run-result IDs, `commit` the merge commit. No `proposal.decision` exists for the candidate on any chain. Output contains the two documented lines.
2. **SC-002**: adopt rule `ordinary` with `h.AdoptThroughAmendment` (with `Requesters: [A]` on pipeline `test`, so A requests CI), then A opens a candidate on the new base, R approves, A requests CI, T runs it, E executes. Assert no event whose subject is the candidate is signed by M.
3. **US1-2**: E publishes (`git push origin main`, `hn sync origin`). A verifier clone without identity runs `hn sync origin` and `hn proposal status <c>`: exit 0, `Status:   merged` and `Merged by rule ordinary, signed by <E> in fact <full ID>`.
4. **US1-3**: the same candidate and evidence under a policy whose rule has no `acceptance` and `actors` `[M]`: M's `--execute` exits non-zero with `automerge blocked:` and `current required maintainer acceptances are missing`; refs and every chain unchanged.
5. **FR-011**: a standing rule that lists M among its executing actors; M executes; the recorded fact carries `rule`.
6. **Two rules**: the candidate satisfies `ordinary` and a second standing rule `docsonly` that also lists E; E executes `--rule docsonly`; the fact names `docsonly`.
7. **Self-check**: an in-process test (`t.Chdir` into E's clone) calls `appendRuleMergeEvent` with an acceptance whose evidence omits the approval: returns the "refusing to sign" error and E's actor ref is unchanged.
8. **Status of acceptance merges unchanged**: a candidate merged with `hn decide` and `hn merge` prints no `Merged by rule` line.
9. **Status of a maintainer merge with a rule claim**: with boundary fixture B3 in the store, `hn proposal status` prints `Status:   merged` and the `Merged by maintainers ... is not verified` line, and no `Merged by rule` line.

### Subtask T053: Governance guide execution section and `standing-execute` example

**Steps**:

1. In "Rule-based native automerge" of `docs/governance-v0.md`, add "Executing a standing rule": who may execute, what changes compared with acceptance mode `maintainers` (no maintainer decision; the fact names the rule and lists approvals and results), what the client checks before signing, and what `hn proposal status` prints.
2. Add `<!-- example: standing-execute -->`: the local User Story 1 flow as commands (open, review, run request, runner, preview, execute, status) with the expected output lines.
3. Add `TestStandingAutomergeDocumentedExecute` containing the exact marker string; it replays the flow on the harness and compares each documented output line after substitution.
4. Style: plain direct sentences, no em dashes.

### Subtask T094: Preview and local execution agree on rule satisfaction (NFR-002)

**Purpose**: decision `01M5VSEHFH0B1BSJ1NA923FKAA`. The preview and local execution share every input (same clone, branch tip, base and target policy, candidate code and fact set), so they must reach the same rule-satisfaction outcome; executor authorization is tested separately.

**Steps** (`TestStandingAutomergeAgreementPreviewExecute` in `standing_automerge_test.go`):

1. Fixtures, each in its own harness or a reset copy, with E's clone clean: satisfied; a trusted change request (counted reviewer, and uncounted reviewer S); a trusted failure; a maintainer rejection; too few counted approvals (only S approved); too few passing results; author outside the rule; a path outside the rule; a `.hn/policy.json` change; a protected-check change; a rule with acceptance mode `maintainers` named with `--rule`; a rule that does not exist; a target policy that differs from the base policy.
   - **Target tip per fixture**: each fixture declares the tip of `main` in E's clone, and the preview and the execution both run against that tip in the same clone, with nothing synced or published between them. Before each comparison, assert that E's `main` equals the fixture's declared tip. The declared tip is the candidate's signed base for every fixture except the target-policy fixture.
   - **Target-policy fixture**: use the recipe of WP14 T068 step 6. M lands a policy amendment H1 on `main` that keeps rule `ordinary`; A builds the candidate's head on top of H1 and opens it with `--base` the older commit; E's `main` is at H1. The target differs from the signed base by construction, so do not assert target equals base for this fixture. Its preview must list `current target policy differs from the signed base policy`.
2. For each fixture: run the keyless preview and record `eligible` (no blocker) and the blocker lines; then run `hn automerge <c> --rule NAME --execute` as E. Assert execution merges exactly when the preview was eligible, and when it refuses, its `automerge blocked:` text names the same blockers as the preview, in the same order.
3. Log a table of fixture, preview outcome and execution outcome; agreement must be 100%.
4. Authorization is not part of this comparison: one extra case runs the satisfied fixture as X, whose preview is eligible and whose execution is refused with `identity <X> is not an executing actor for automerge rule "ordinary"`; record it in the log as an authorization refusal, not an agreement failure. WP12 T057 covers the rest of the containment set.

## Test Strategy

Black-box tests through the harness, one in-process self-check test, and the unchanged `automerge_test.go`, `governance_revision_test.go` and `policy_test.go`. Run `go test -race -run 'TestStandingAutomerge|TestAutomerge|TestMerge' ./...`, then the full gate.

## Definition of Done

- [ ] T048 to T053 and T094 complete; full gate passes.
- [ ] **NFR-002 (preview and execution)**: 100% rule-satisfaction agreement over the T094 fixtures; authorization logged separately.
- [ ] **FR-024 (pre-sign)**: the self-check uses `validateMergeEventWith` and requires a rule merge.
- [ ] **FR-009**: a non-maintainer executing actor merges a standing-rule candidate locally with no maintainer decision; all other blockers still apply.
- [ ] **FR-010 / FR-011**: the recorded fact has `rule`, base `policy` and exactly the qualifying evidence; its signer is an executing actor; a maintainer executing a standing rule also records a rule merge fact.
- [ ] **FR-021**: status names the rule, the full signer fingerprint and the full fact ID for a rule merge, and marks a rule claim on a maintainer merge as not verified.
- [ ] Pre-sign self-check proven by test.
- [ ] `hn merge` and maintainers-mode automerge unchanged; every pre-existing test passes with no assertion change; `standing-execute` executed.

## Risks

- The authority must never be set by `cmdMerge`, `cmdMergeRemote` or `cmdDecide`. Search for every `proposalMergeGuard{` literal after the change.
- If `appendEvent` produced a different ID from the self-checked payload, the self-check would have judged a different fact. Step 4 of T049 guards it.

## Reviewer Guidance

- Diff `mergeProposal` and confirm each changed condition is inside a `guard.Rule != nil` branch.
- Run US1-1 by hand and read E's new event with `git show refs/hn/actors/<E>:event.json`.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
- 2026-10-26T09:45:00Z – llxprt – Prompt revised after analysis (C1, I4; decisions 01M5VSEHEVA82N9T64ZH1P1Q2A and 01M5VSEHFH0B1BSJ1NA923FKAA): self-check through validateMergeEventWith, status lines by authority, T094 preview-to-execution agreement, WP09 added as a dependency to order policy.go edits
- 2026-10-26T10:35:12Z – llxprt – Prompt revised after analysis round 2 (I4; decision 01M5VWE2PV2R1N81SCMR46JQZ0): T094 declares a target tip per fixture; the target-policy fixture puts main at the amendment commit
