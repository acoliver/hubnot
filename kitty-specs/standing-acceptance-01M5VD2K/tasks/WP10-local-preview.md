---
work_package_id: WP10
title: Keyless preview under a standing rule
dependencies:
- WP02
- WP04
- WP06
requirement_refs:
- FR-005
- FR-006
- FR-008
- FR-009
- FR-017
- FR-023
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T043
- T044
- T045
- T046
- T047
phase: Phase 4 - Local standing acceptance
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: automerge.go
create_intent:
- standing_automerge_test.go
execution_mode: code_change
owned_files:
- automerge.go
- standing_automerge_test.go
- docs/governance-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP10: Keyless preview under a standing rule

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Make `inspectAutomerge` and the keyless preview `hn automerge PROPOSAL --rule NAME` honor acceptance modes ([contracts/automerge-cli.md](../contracts/automerge-cli.md) "Preview" and "Blockers by acceptance mode"):

- Under acceptance mode `rule`, the blockers `current approval, protected check or CI evidence is not ready` and `current required maintainer acceptances are missing` are replaced by the rule's threshold blockers from `evaluateRuleAcceptance`: `rule approval threshold is unmet: N of M counted approvals` and `pipeline "P" has N of M passing trusted results` (FR-008, FR-009 eligibility part, FR-005, FR-006).
- Under acceptance mode `maintainers` with `reviewers` set, the existing blockers stay and the rule approval threshold is checked as well (US5-4).
- Every other blocker keeps its text, its position and both modes: maintainer rejection, trusted change request (any trusted reviewer), trusted failure, policy amendment, protected change, author, path, branch, target equals base, target policy equals base policy (FR-017 local), lineage, code ref, merged, clean tree, shallow, partial and unfinished operations.
- The preview prints three additive lines and still writes nothing.

`inspectAutomerge` is also what local execution re-runs inside the merge guard (WP11), so after this WP a standing rule is eligible in the preview; execution still refuses until WP11 wires the rule authority, because `mergeProposal` still requires acceptances.

## Context

- Read spec US1-4, US5 (all), US6-1 to US6-3 and US6-7, FR-005, FR-006, FR-008, FR-009, FR-017; [contracts/automerge-cli.md](../contracts/automerge-cli.md) "Preview", "Blockers by acceptance mode" and "Tested examples".
- Start with `spec-kitty agent action implement WP10 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code in `automerge.go`: `automergeInspection`, `inspectAutomerge` (blocker order is the order of its `append` calls), `printAutomergeInspection`, `cmdAutomerge`. `loadPipeline` (`ci.go`) gives the definition digest at the head.
- From WP04: `ruleInputs`, `evaluateRuleAcceptance`, `ruleAcceptance.UnmetOf`, the unmet kinds, `ruleCountedReviewers`. From WP06: the harness (`newStandingHarness`, `standingHarnessDefaultPolicy`, `ReadyCandidate`, `Checkout`, `Run`, `Refs`, `Events`).
- Existing tests that must pass unchanged: `automerge_test.go`, `serve_automerge_test.go`, `policy_rules_acceptance_test.go`.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `automerge.go` | CHANGE. `automergeInspection` gains a rule-acceptance field; `inspectAutomerge` and `printAutomergeInspection` change. Do not touch `cmdAutomerge`'s execution path or `automergeRemote` (WP11, WP12, WP15). |
| `standing_automerge_test.go` | NEW. `TestStandingAutomergePreview...`. WP11 and WP12 add tests later. |
| `docs/governance-v0.md` | CHANGE. Preview text in "Rule-based native automerge" and the `standing-preview` example. |

## Subtasks

### Subtask T043: Rule-mode blockers in `inspectAutomerge`

**Steps**:

1. Add `RuleAcceptance *ruleAcceptance` to `automergeInspection`.
2. When `result.Rule != nil`, build `ruleInputs` from what `inspectAutomerge` already has: `evaluation.Policy`, `evaluation.PolicyDigest`, `*proposal`, `ruleName`, `TargetBranch: ""` (the inspection has its own checked-out-branch blocker), `changed`, `events`, and `PipelineDefinitions` from `loadPipeline(proposal.Event.Head, name)` for each base-policy pipeline (skip a pipeline whose definition fails to load; `evaluateProposal` already refuses that case when the code matches). Call `evaluateRuleAcceptance` and store the result.
3. Replace, only when `result.Rule.Acceptance.Standing()`:
   - the `!evaluation.Ready` blocker, at its current position, with the texts of `UnmetOf(ruleUnmetApprovals, ruleUnmetPipeline)` in their order;
   - drop the `!evaluation.Accepted` blocker.
4. Keep every other `append` exactly where it is. In particular the trusted change request and trusted failure blockers stay, so vetoes are reported with today's text; the rejection blocker stays.
5. Do not add the evaluator's `author`, `path`, `manual-path` or `rule-missing` texts to the blockers; the inspection already reports them with the same text. This keeps one copy of each blocker.

### Subtask T044: Rule reviewer threshold under acceptance mode `maintainers`

**Steps**:

1. When the rule is not standing and `!result.Rule.Reviewers.IsZero()`, append the `approvals` unmet text (if any) right after the `current required maintainer acceptances are missing` position, so it reads next to the other evidence blockers.
2. A maintainers-mode rule without `reviewers` gets no new blocker: behavior identical to today (NFR-004).
3. Local execution under such a rule records an acceptance merge fact as today (US5-4); that needs no change here, because execution re-runs this inspection inside its guard.

### Subtask T045: Additive preview lines in `printAutomergeInspection`

**Steps**: after the `Permitted executing actors:` list, when `i.Rule != nil` and `i.RuleAcceptance != nil`:

```text
Acceptance: rule (standing acceptance; no maintainer acceptance required)
Counted reviewers:
  <fingerprint>
Counted approvals: 1/1 (author approvals do not count)
```

For acceptance mode `maintainers`: `Acceptance: maintainers (maintainer acceptance required)`. When `reviewers` is absent: the single line `Counted reviewers: all trusted reviewers`. `Counted approvals: N/M` uses the evaluator's distinct counting approvals and `proposals.requiredApprovals`; the parenthetical appears only under acceptance mode `rule`. Fingerprints are full and sorted. Lines printed today keep their text and order; the `Changed paths:`, `Eligible:` and `Blocked:` blocks follow unchanged. Use `safeText` for any policy-supplied string.

### Subtask T046: Black-box preview tests

**Steps** (tests `TestStandingAutomergePreview...` in `standing_automerge_test.go`, through the WP06 harness; E's clone has `main` checked out at the candidate base). Snapshot `h.Refs("E")` and E's event count before each preview and assert both unchanged after it.

1. **US1-4**: default policy, `ReadyCandidate` with `src/` and `tests/` changes, zero decisions. E's preview exits 0 and prints `Acceptance: rule (standing acceptance; no maintainer acceptance required)`, `Counted reviewers:` with R, `Counted approvals: 1/1 (author approvals do not count)`, and `Eligible:`; no `Blocked:` block.
2. **US1-3 (preview part)**: same candidate and evidence under a policy identical except that `acceptance` is absent and `actors` is `[M]`; M's preview lists `current required maintainer acceptances are missing`.
3. **US5-1**: trusted reviewers R and S, rule reviewers `[R]`; only S approves: `rule approval threshold is unmet: 0 of 1 counted approvals`. Then R approves: eligible.
4. **US5-2**: S requests changes while R approves: `proposal has a current trusted change request`.
5. **US5-3**: `allowAuthorApproval` true, A listed in trusted reviewers and rule reviewers, only A approves: threshold unmet.
6. **US5-4 (preview)**: maintainers mode with `reviewers` `[R]`, only S approves, M accepted: blocked by the rule approval threshold only.
7. **US6-1 / US6-2 / US6-3 (preview)**: maintainer rejection; trusted change request; trusted failure (`h.FailCI()` and rerun); a required pipeline without a passing result; author outside the rule; changed path `other/x` outside the rule. Each preview names its blocker.
8. **US6-7**: the only passing result answers a run request signed by an actor that is neither a maintainer nor a listed requester: `pipeline "test" has 0 of 1 passing trusted results`. With `Requesters: [A]` and A requesting, eligible.
9. **US2-3**: a candidate whose head adds rule `newrule` to `.hn/policy.json`; preview under `--rule newrule` lists `automerge rule "newrule" does not exist in the signed base policy` and `policy amendments require manual merge`.
10. **FR-017 (local)**: M amends the policy on `main` after the candidate's base and E's `main` is at the new tip: blocked with `target no longer equals the signed base` and `current target policy differs from the signed base policy`.
11. **Opt-out**: a maintainers-mode rule without `reviewers` prints the existing lines plus the three additive lines, and its blockers equal those of today (compare with the expected list from `automerge_test.go` style cases).

### Subtask T047: Governance guide preview section and `standing-preview` example

**Steps**:

1. In `docs/governance-v0.md`, extend "Rule-based native automerge" with a paragraph on previewing a standing rule: what the three new lines mean, which blockers change under acceptance mode `rule`, and that vetoes from any trusted reviewer, failures and maintainer rejections still block.
2. Add `<!-- example: standing-preview -->` with the command and the full preview output for the US1-4 fixture, with placeholder IDs.
3. Add `TestStandingAutomergeDocumentedPreview` whose source contains the exact marker string; it runs the US1-4 fixture, substitutes real IDs and fingerprints for the placeholders, and requires every documented line to appear in the output in the documented order.
4. Style: plain direct sentences, no em dashes.

## Test Strategy

Black-box tests through the harness plus the unchanged automerge suites. Run `go test -race -run 'TestStandingAutomergePreview|TestAutomerge|TestServeAutomerge' ./...`, then the full gate.

## Definition of Done

- [ ] T043 to T047 complete; full gate passes.
- [ ] **FR-008**: the preview prints acceptance mode, counted reviewers and whether maintainer acceptance is required, and writes nothing.
- [ ] **FR-005 / FR-006**: approval counting follows rule reviewers in both modes and excludes the author under acceptance mode `rule`.
- [ ] **FR-009 (eligibility)**: every blocker except the acceptance blocker applies under acceptance mode `rule`.
- [ ] **FR-017 (local)**: a differing target policy blocks.
- [ ] Every pre-existing automerge test passes with no assertion change; `standing-preview` executed.

## Risks

- `inspectAutomerge` is called inside the merge guard during execution; it must stay free of side effects.

## Reviewer Guidance

- Diff the blocker sequence in `inspectAutomerge` and check that a maintainers-mode rule without `reviewers` produces exactly today's list.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
