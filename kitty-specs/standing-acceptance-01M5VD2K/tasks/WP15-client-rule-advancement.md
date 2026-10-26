---
work_package_id: WP15
title: Client rule advancement through hn serve
dependencies:
- WP08
- WP12
- WP14
requirement_refs:
- C-008
- FR-018
- FR-019
- FR-023
- NFR-002
- NFR-003
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T070
- T071
- T072
- T073
- T074
- T075
- T095
phase: Phase 5 - hn serve rule advancement
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: serve_client.go
create_intent:
- standing_serve_test.go
execution_mode: code_change
owned_files:
- serve_client.go
- automerge.go
- standing_serve_test.go
- docs/serve-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP15: Client rule advancement through hn serve

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Deliver User Story 4 from the executing actor's side ([contracts/automerge-cli.md](../contracts/automerge-cli.md) "Remote execution"):

```text
hn automerge PROPOSAL --rule ordinary --execute --remote origin
```

keeps the existing steps of `automergeRemote` (identify the remote, require a selection that imports every trusted actor, `hn sync`, inspect again, local target equals hosted tip) and then calls `mergeRemoteBound` with a rule binding. With a rule binding:

| `mergeRemoteBound` step | Change |
|----|----|
| 1 validate locally | the rule authority replaces the acceptance and maintainer checks of `serveAdvanceValidate` |
| 6 request advancement | adds `--push-option=hn-rule=NAME` |
| 7 confirm, rerun message | names `hn automerge PROPOSAL --rule NAME --execute --remote REMOTE` |
| 8 record | records a rule merge fact with `commit` = head, after the pre-sign self-check |

Reruns after an interruption (the remote repair branch and the publication-only rerun, FR-020 remote) belong to WP19, which builds on this WP (decision `01M5VSEHFCZGQHQ14CKJ3MAMP7`). `hn merge --remote` stays maintainer-only and never sends `hn-rule` (FR-018, FR-019). The server signs nothing; the client signs the fact (C-008).

This WP also proves the rule-satisfaction part of NFR-002 between the keyless preview and the `hn serve` verdict under identical inputs, and tests executor authorization and transport checks on their own (decision `01M5VSEHFH0B1BSJ1NA923FKAA`; [contracts/serve-rule-advancement.md](../contracts/serve-rule-advancement.md#agreement-nfr-002)).

## Context

- Read spec US4-1 to US4-3, US6-6 (remote parts), FR-018, FR-019, NFR-002, NFR-003, C-008; the contract sections named above; [contracts/serve-rule-advancement.md](../contracts/serve-rule-advancement.md) "Push options" (older-server message).
- Start with `spec-kitty agent action implement WP15 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code: `serveAdvanceBinding`, `mergeRemoteBound`, `serveAdvanceValidate`, `serveAdvanceRerun`, `serveAdvanceRecordAndPublish`, `serveAdvanceRecord`, `serveAdvanceInterrupt` and `serveAdvanceInterruptEnv` (`serve_client.go`); `automergeRemote`, `automergeUnselectedActors` (`automerge.go`); `extractServeRejections` (`serve_reasons.go`); `servePushOptionRule` (WP13).
- From WP11: `appendRuleMergeEvent`, `ruleMergeAuthority`. From WP10 and WP12: `automergeInspection.RuleAcceptance` and `Repair`. From WP14: `newStandingServeFixture`, `standingServeDefaultPolicy`, `StandingReady`, `RulePush`.
- Pinned behavior: `serve_replication_test.go` checks the push options `hn merge --remote` sends; that path must not send `hn-rule`. `serve_automerge_test.go` covers maintainers-mode automerge through the server and must pass unchanged.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `serve_client.go` | CHANGE. `Rule` in the binding and its effect on steps 1, 6, 7 and 8. |
| `automerge.go` | CHANGE. `automergeRemote` only: the rule binding and the comment that says the server does not evaluate automerge rules. WP19 adds the rerun paths. |
| `standing_serve_test.go` | CHANGE. `TestStandingServeAdvance...`, the agreement test and the example test. |
| `docs/serve-v0.md` | CHANGE. Client part of "Automerge rules" for rule advancement. WP19 adds the rerun rows of "Interruptions and reruns". |

## Subtasks

### Subtask T070: Rule binding in `serveAdvanceBinding` and step 1

**Steps**:

1. `type serveAdvanceBinding struct { ExpectedActor, ExpectedTip, Rule string }`.
2. Split the acceptance and maintainer checks out of `serveAdvanceValidate` into a binding-aware form: with `binding.Rule == ""`, the function and its messages are unchanged. With a rule:
   - skip `!evaluation.Accepted` and the maintainer check;
   - evaluate the rule with `evaluateRuleAcceptance` on the same events and the candidate's base policy (changed paths with `changedPathsAt("", base, head)`, pipeline digests with `loadPipeline`), require `Satisfied()`, and require the identity to be an executing actor of the rule (`identity %s is not an executing actor for automerge rule %q`);
   - keep the readiness, lineage, rejection and code checks.
   Return the `ruleAcceptance` for later steps.
3. `cmdMergeRemote` passes an empty binding, so `hn merge --remote` keeps every check.

### Subtask T071: `hn-rule` push, rerun text and older-server message

**Steps**:

1. Step 6 with a rule: append `"--push-option="+servePushOptionRule+"="+binding.Rule` after the two existing options. Without a rule, the argument list is byte-identical.
2. `serveAdvanceRerun` with a rule returns `hn automerge PROPOSAL --rule NAME --execute --remote REMOTE`; without, unchanged. Thread the binding into the places that print rerun commands.
3. Older server: when the push fails and `extractServeRejections` yields an `unsupported-command` rejection while a rule was sent, print the server lines as today and return `REMOTE does not support rule advancement (hn-rule); upgrade hn on the server`. Put the decision in a small function `serveAdvanceRuleUnsupported(rejections []serveRejection, rule string) bool` so it can be unit-tested (tasks decision 7).

### Subtask T072: Step 8 records a rule merge fact

**Steps**:

1. `serveAdvanceRecord` with a rule: re-collect, re-resolve and re-evaluate as today (lineage eligible, not merged, ready, head equals the advanced commit), then re-evaluate the rule (satisfied, identity still an executing actor), then `appendRuleMergeEvent(identity, proposal, evaluation, acceptance, head)`, so `commit` equals the head and the self-check runs before signing.
2. `serveAdvanceRecordAndPublish` prints, for a rule:

   ```text
   Recorded rule merge event 7d8e9f0a1b2c under rule ordinary with 2 evidence item(s)
   ```

   and, for the repair path that WP19 wires, `Recorded missing rule merge event ...`. Without a rule, the existing `with %d acceptance decision(s)` text is unchanged.
3. Publication (step 9) is unchanged: the fact travels in E's actor chain and the server judges it with the replication validator (WP08).

### Subtask T073: `automergeRemote` with the rule binding

**Steps**:

1. When the inspected rule is standing, call `mergeRemoteBound(..., serveAdvanceBinding{ExpectedActor: actor, ExpectedTip: hosted, Rule: rule})`. Maintainers-mode rules keep the existing call.
2. Leave the local-equals-hosted refusal and the merged blockers as they are; WP19 adds the remote repair branch and the publication-only rerun in front of them.
3. Update the comment above `automergeRemote` that says the server does not evaluate automerge rules.

### Subtask T074: Remote tests for US4-1, US4-2, US4-3 and FR-019

**Steps** (tests `TestStandingServeAdvance...`, through the standing serve fixture; E's clone has `main` checked out at the hosted tip and `hn replication select origin --all-trusted` or `--all`):

1. **US4-1**: `hn automerge <c> --rule ordinary --execute --remote origin` as E exits 0; server `main` equals the head; no new commit; E's chain holds a rule merge fact with `commit` = head; the server accepted E's chain (the actor ref on the server equals E's local ref).
2. **US4-2**: a new clone with the all-trusted selection runs `hn sync origin` and `hn proposal status <c>`: merged, `Merged by rule ordinary, signed by <E> ...`.
3. **US4-3 through the client**: S publishes a change request after E's last sync; E's command (which syncs first) is refused locally by the inspection. Then, to prove the server's veto, use the interruption seam or a test hook that runs between the client's import and its push (for example `HN_TEST_SERVE_ADVANCE_INTERRUPT_AFTER=publish` followed by S's push and a direct `RulePush`), and assert `protected-rule-veto`.
4. **Interruptions**: one case only, `publish` (`HN_INTERNAL_TESTING=1` and `serveAdvanceInterruptEnv`), followed by a full rerun, to prove the binding survives an interruption before the push. The remaining rows of the interruption table belong to WP19.
5. **FR-019 / NFR-003 remote**: E's `hn merge <c> --remote origin` is refused at step 1 (no acceptance; not a maintainer); E's direct `git push origin <head>:refs/heads/main` is refused with `protected-not-ready`; X's `hn automerge <c> --rule ordinary --execute --remote origin` is refused locally as not an executing actor. Hosted refs and E's and X's chains unchanged in each case.
6. **hn merge --remote unchanged**: M's maintainer advancement of an accepted candidate sends exactly `hn-advance` and `hn-expected-tip` (observe the push log as `serve_replication_test.go` does, without editing it).
7. **Older-server message**: unit test `serveAdvanceRuleUnsupported` with a fabricated `unsupported-command` rejection and with other codes.

### Subtask T075: Serve guide client advancement and `standing-serve-advance` example

**Steps**:

1. In `docs/serve-v0.md` "Automerge rules", update the client paragraph for standing rules: the command sends `hn-rule`, the server evaluates the rule itself, the fact is recorded by the client with `commit` equal to the head, and what an executing actor sees when the server refuses. Note the minimum server version: a server without this mission refuses `hn-rule`, and the client names the upgrade. Leave "Interruptions and reruns" to WP19.
2. Add `<!-- example: standing-serve-advance -->` with the US4-1 command and its output lines.
3. Add `TestStandingServeDocumentedAdvance` containing the exact marker string; it runs US4-1 and compares the documented output lines after substitution.
4. Style: plain direct sentences, no em dashes.

### Subtask T095: Preview and hn serve agree on rule satisfaction; authorization and transport tested on their own (NFR-002)

**Purpose**: decision `01M5VSEHFH0B1BSJ1NA923FKAA`. Compare the keyless preview with the server verdict under identical inputs, and test the checks that agreement excludes separately.

**Steps** (`TestStandingServeAdvanceAgreementPreviewServer` and `TestStandingServeAdvanceAuthorizationAndTransport` in `standing_serve_test.go`):

1. **Matched inputs** for every agreement fixture, as the contract lists them: E's checked-out `main` equals the hosted `main`, which equals the candidate's signed base; the base and target policy are the same commits on both sides; the head and proposal code ref are present locally and hosted; E runs `hn sync origin` with the all-trusted selection immediately before the preview, and the test publishes nothing between E's preview and E's push. Assert these four conditions in the test before each comparison.
2. **Fixtures**: satisfied; trusted change request; trusted failure; maintainer rejection; destination policy different from the base policy (both tips at the same amendment commit); `hn-rule` naming a missing rule; a maintainers-mode rule; a rule for another target branch; author outside; path outside; policy or protected-check change; too few counted approvals; too few passing results.
3. **Comparison**: for each fixture, record the preview outcome (eligible when it prints no blocker), then E's `RulePush` with the correct expected tip. Assert preview eligible exactly when the server accepts, and that every server refusal comes from steps 7a, 8, 9 or 10 (`protected-destination-policy`, `proposal-ref-mismatch`, `protected-blocked`, `protected-rule-veto`, `protected-rule-not-satisfied`). Log the table; agreement must be 100%.
4. **Authorization, tested on its own**: for the satisfied fixture, X's keyless preview is eligible; X's `RulePush` is refused with `advance-not-rule-executor`; X's `hn automerge <c> --rule ordinary --execute --remote origin` is refused locally as not an executing actor. Hosted refs unchanged. These are authorization refusals, not agreement failures.
5. **Transport and binding, tested on their own**: a stale `hn-expected-tip` gives `protected-stale-tip`; E's command with local `main` behind the hosted tip is refused locally (`local main is at ... but origin has ...`); a selection that omits a trusted actor is refused before import; a remote that is not an `hn serve` remote is refused. Hosted refs unchanged in each case.

## Test Strategy

Black-box tests through the forced-command harness. Run `go test -race -run 'TestStandingServeAdvance|TestServeAdvance|TestServeAutomerge|TestServeReplication' ./...`, then the full gate.

## Definition of Done

- [ ] T070 to T075 and T095 complete; full gate passes.
- [ ] **US4-1, US4-2**: the hosted branch fast-forwards to the head, the rule merge fact has `commit` = head, a fresh clone verifies it.
- [ ] **FR-018 / FR-019 / NFR-003**: `hn merge --remote` and direct pushes unchanged; every forbidden attempt leaves hosted refs and chains unchanged.
- [ ] **NFR-002**: under the matched inputs, preview eligibility and the server verdict agree on rule satisfaction for every T095 fixture; authorization and transport refusals are tested separately and logged as such.
- [ ] **C-008**: the server never signs; only E's client signs.
- [ ] Every pre-existing serve test passes unchanged; `standing-serve-advance` executed.

## Risks

- The executing actor's selection must import every trusted actor, or a veto stays invisible to the inspection; the existing `automergeUnselectedActors` check already refuses that (tasks decision 10).

## Reviewer Guidance

- Diff `mergeRemoteBound`: without a rule, every argument and message is unchanged.
- Confirm this WP adds no rerun path; WP19 owns the repair branch and the publication-only rerun.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
- 2026-10-26T09:45:00Z – llxprt – Prompt revised after analysis (I3, I4; decisions 01M5VSEHFCZGQHQ14CKJ3MAMP7 and 01M5VSEHFH0B1BSJ1NA923FKAA): remote repair and the interruption table move to WP19; T095 adds identical-input agreement with separate authorization and transport tests
