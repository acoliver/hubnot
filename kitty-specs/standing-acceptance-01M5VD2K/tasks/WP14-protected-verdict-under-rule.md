---
work_package_id: WP14
title: Protected-branch verdict under a named rule
dependencies:
- WP04
- WP06
- WP13
requirement_refs:
- FR-012
- FR-016
- FR-017
- FR-018
- FR-023
- FR-025
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T064
- T065
- T066
- T067
- T068
- T069
phase: Phase 5 - hn serve rule advancement
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: serve_protected.go
create_intent:
- standing_serve_test.go
execution_mode: code_change
owned_files:
- serve_protected.go
- serve_verdict.go
- standing_serve_test.go
- docs/serve-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP14: Protected-branch verdict under a named rule

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Let `hn serve` advance a protected branch for an executing actor that names a standing rule, after it evaluates that rule itself from the base policy and the facts it holds (FR-016, FR-017 server, OQ-6, OQ-7). With `hn-rule` present, `evaluateServeProtectedUpdate` runs ([contracts/serve-rule-advancement.md](../contracts/serve-rule-advancement.md) "Protected-branch steps with hn-rule"; decision `01M5VF0G52PH940GQ1BWX0X9TR`):

| Step | Under `hn-rule` |
|----|----|
| 1 to 7 | unchanged |
| 7a (new) | destination policy digest equals base policy digest, else `protected-destination-policy` with next `revise <candidate> against <tip>` |
| 8, 9 | unchanged (a maintainer rejection is still `protected-blocked`) |
| 10 | `evaluateRuleAcceptance` replaces readiness and acceptance: vetoes give `protected-rule-veto`, every other failure `protected-rule-not-satisfied` |
| 11 | skipped (7a makes it identical to step 10) |
| 12 | unchanged |
| 13 | requester must be an executing actor of the named rule in the base policy, else `advance-not-rule-executor` |

Without `hn-rule` nothing changes: a direct push and an `hn-advance` push without `hn-rule` still need maintainer acceptance under both policies (FR-018). The server still signs nothing and never creates a commit (C-008).

This WP also removes WP13's interim refusal of every `hn-rule` push from `evaluateServePush` (`serve_verdict.go`), in the same change that adds the rule steps, so no build ever both accepts `hn-rule` and skips its evaluation (FR-025, decision `01M5VSEHF5B1FM9TB52KWKQGWZ`). Boundary fixture B5 (WP13's `TestStandingServeWireRuleRefusedBeforeVerdict`) must still pass.

## Context

- Read the contract in full; spec US4-3 to US4-6 and FR-016 to FR-018; [research.md](../research.md) findings 13 and 14 and adversarial row A8.
- Start with `spec-kitty agent action implement WP14 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code in `serve_protected.go`: the step constants (lines 19 to 33), `evaluateServeProtectedUpdate` (steps 1 to 6 and 13, lines 68 to 194), `serveProtectedUpdate`, `candidate` (steps 7 to 12), `evaluate`, `serveProtectedBlocking`, `serveProtectedUnmet`, `serveProtectedIDs`, `serveShortCommit`, `serveDiagnostic`. Object access inside the hook: `loadReplicationPolicyAt(ctx.GitDir, ctx.GitDir, commit)`, `replicationPipelineDigestAt(ctx.GitDir, ctx.GitDir, head, name)`, `changedPathsAt(ctx.GitDir, base, head)`; the hook runs inside Git's quarantine environment, so objects of the current push are visible.
- From WP13: `options.Rule`, the three reason constants. From WP04: `ruleInputs`, `evaluateRuleAcceptance`, `ruleAcceptance.UnmetOf`, the unmet kinds and `ruleVeto`.
- Serve test infrastructure to reuse without editing: `newServeHarness`, `servePushFixture`, `serveHarnessAddKey`, `serveHarnessClient`, `serveHarnessCloneURL`, `serveHarnessWriteFile`, `serveHarnessCommit`, and every method of `*serveGovernedFixture` (`Commit`, `Host`, `OpenProposal`, `Approve`, `RequestCI`, `RunCI`, `Accept`, `Reject`, `ServerTip`, `ServerRefs`, `PushMain`, `RejectMain`, `AcceptMain`, `SetMainForTest`) in `serve_protected_test.go`; `serveGovernedPipelineJSON`.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `serve_protected.go` | CHANGE. Rule steps only, each behind `options.Rule != ""`. |
| `serve_verdict.go` | CHANGE. Remove WP13's interim `hn-rule` refusal from `evaluateServePush` and nothing else. |
| `standing_serve_test.go` | CHANGE. Standing serve fixture and `TestStandingServeVerdict...`. |
| `docs/serve-v0.md` | CHANGE. Rewrite "Automerge rules" under "Advancing a protected branch" for the server's part; WP15 adds the client's part. |

## Subtasks

### Subtask T064: Step 7a: destination digest equals base digest

**Steps**:

0. Remove the interim refusal that WP13 added at the top of `evaluateServePush` (`if options.Rule != "" { return rejectServeEveryUpdate(...) }`), and only it. Do this in the same commit as the rule steps of T064 to T066.
1. Keep the destination digest from step 4 (`destPolicy, destDigest, err := loadReplicationPolicyAt(...)`) and store it in `serveProtectedUpdate`. Store `options` there too.
2. In `candidate`, right after step 7 succeeds and only when `check.options.Rule != ""`: `destDigest != baseDigest` fails with code `protected-destination-policy`, `IDs` = candidate, `Missing` = `[proposal]`, `Next` = `revise <candidate> against <tip>`, and the contract sentence:

   ```text
   <ref> at <tip8> has policy <dest digest> and candidate <candidate> was based on policy <base digest>; rule <name> applies only under its own policy.
   ```

3. Step constants: no existing test reads `Step` values, but do not renumber the existing constants. Add `serveProtectedStepRuleDestPolicy`, `serveProtectedStepRuleAcceptance` and `serveProtectedStepRuleExecutor` with values after `serveProtectedStepAdvanceAllowed`, and comment that with `hn-advance` the candidate set has exactly one candidate, so the multi-candidate "latest failing step" ranking never compares rule steps.

### Subtask T065: Step 10 under a rule: `evaluateRuleAcceptance` with vetoes

**Steps**: in `candidate`, after step 9, when `check.options.Rule != ""`, replace the step 10 readiness and acceptance check with:

1. Inputs: `basePolicy`, `baseDigest`, `*candidate`, `options.Rule`, `TargetBranch` = the pushed ref without `refs/heads/`, `Changed` = `changedPathsAt(check.ctx.GitDir, base, head)` (an error fails with `protected-rule-not-satisfied` and the diagnostic, never an empty list), `PipelineDefinitions` from `replicationPipelineDigestAt(check.ctx.GitDir, check.ctx.GitDir, head, name)` per base-policy pipeline (skip a pipeline whose definition fails), `Events` = `check.events` (hosted facts plus facts accepted earlier in the same push).
2. Report in the contract order of "Step 10 under a rule":
   - a, b, c: `UnmetOf(rule-missing, rule-mode, target-branch)` non-empty: `protected-rule-not-satisfied`, `IDs` = candidate, sentence `candidate <c> does not satisfy rule <name>: <unmet texts joined with "; ">.`;
   - d: vetoes of kind `change-request` or `failure`: `protected-rule-veto`, `IDs` = candidate then each blocking event ID sorted, `Next` = `resolve the named events, then retry`, sentence `candidate <c> is blocked by rule <name>: trusted change request <id>.` (one clause per veto, "trusted change request" or "trusted failure"). A `rejection` veto cannot reach this point because step 9 reports it as `protected-blocked`; keep that order;
   - e: `UnmetOf(author, manual-path, path)`: `protected-rule-not-satisfied`, sentence as in a;
   - f: `UnmetOf(approvals, pipeline)`: `protected-rule-not-satisfied` with `Missing` = `review` and/or `ci` in emission order, sentence as in a.
3. Substitute `<candidate>` and `<rule>` in the catalogue's next-action template for `protected-rule-not-satisfied` (`run hn automerge <candidate> --rule <rule>`).
4. A pipeline-definition error from `check.evaluate` (`baseDefinitionErr`) under a rule is already reflected by the absent digest and becomes the `pipeline` unmet condition; do not also report the step 10 not-ready text.
5. Keep the result's `basePolicy`; step 13 reads it.

### Subtask T066: Step 11 skip and step 13 executing-actor check

**Steps**:

1. Under a rule, skip step 11 entirely (no destination evaluation).
2. Step 12 (`guardTargetProtectedPolicy`) runs unchanged.
3. In `evaluateServeProtectedUpdate`, step 13 under a rule: the requester `ctx.Actor` must be in `rule.Actors` of the base policy (`ruleByName(passed.basePolicy, options.Rule)`); otherwise `advance-not-rule-executor`, `IDs` = candidate, sentence:

   ```text
   actor <actor> requested advancement of <ref> under rule <name> and is not one of its executing actors in the base policy of <candidate>.
   ```

   The maintainer requirement of step 13 does not apply under a rule. Without a rule, step 13 is byte-identical.

### Subtask T067: Standing serve fixture

**Steps** in `standing_serve_test.go`:

1. `newStandingServeFixture(t, policy func(actors map[string]string) string) *serveGovernedFixture`: mirror the body of `newServeGovernedFixture` (read it; do not edit it) with participants `A` (author), `M` (maintainer), `R` (counted reviewer), `S` (second trusted reviewer), `U` (runner), `E` (executing actor of `ordinary`, not a maintainer) and `X` (executing actor of `other` only). Set the struct's `Author`, `Reviewer`, `Runner`, `Maintainer` and `CIFailFlag` the same way, so the existing methods work. Write the policy returned by `policy(actors)` and `serveGovernedPipelineJSON` into the source repository, create the hosted repository with `hn serve repo create r --from <source>`, bind a key for every participant, and select `--all` in each clone, as the governed fixture does. Confirm how the governed fixture makes `main` protected and do the same.
2. `standingServeDefaultPolicy(actors)`: maintainers `[M]`, trusted reviewers `[R, S]`, `requiredApprovals` 1, `requiredAccepts` 1, pipeline `test` with runner `[U]`, rule `ordinary` (`acceptance` `rule`, actors `[E]`, authors `[A]`, reviewers `[R]`, paths `src/`, `tests/`, `docs/`), rule `other` (`acceptance` `rule`, actors `[X]`, authors `[A]`, paths `src/`, `tests/`, `docs/`), rule `maint` (no `acceptance`, actors `[M]`, authors `[A]`, paths `src/`), rule `release` (`acceptance` `rule`, target branch `release`, actors `[E]`, authors `[A]`, paths `src/`).
3. `(g) StandingReady(files)`: author commit on base, `OpenProposal`, R approves, M requests CI, U runs it. No acceptance.
4. `(g) RulePush(who, candidate, head, rule string) (output string, rejections []serveRejection, ok bool)`: `PushMain(who, head, "hn-advance="+candidate, "hn-expected-tip="+tip, "hn-rule="+rule)`; check how `PushMain` passes push options and use the same form.

### Subtask T068: Verdict tests through direct pushes

**Steps** (tests `TestStandingServeVerdict...`); after every rejection assert `g.ServerRefs()` is byte-identical to before (C-005):

1. **Accept**: E's rule push for a ready candidate is accepted; server `main` equals the head; no new commit (the number of commits reachable from `main` equals that from the head).
2. **US4-3 veto**: S publishes a change request after the candidate is ready; E's rule push is refused with `protected-rule-veto` whose IDs are the candidate then the change request; `main` unchanged. Same for a trusted failure (`CIFailFlag`, rerun).
3. **US4-4**: E pushes `main` directly to the head without push options: `protected-not-ready` with `missing=acceptance`, as today.
4. **FR-018**: E pushes with `hn-advance` and `hn-expected-tip` but no `hn-rule`: refused (`protected-not-ready` with `missing=acceptance` at step 10, as today).
5. **US4-5**: X requests advancement naming `ordinary`: `advance-not-rule-executor`.
6. **US4-6 / FR-017**: M lands a policy amendment on `main` whose head H1 the candidate's head descends from while the candidate's declared base is the older commit (A builds the candidate on top of H1 but opens it with `--base` the old base); E's rule push reaches step 7a and is refused with `protected-destination-policy`, the documented sentence and next `revise <candidate> against <tip>`.
7. **Not satisfied**: `hn-rule=nosuch` (rule missing); `hn-rule=maint` (acceptance mode `maintainers`); `hn-rule=release` pushed to `main` (A8, target branch); author outside; path outside (`other/x` only); approval missing (`missing=review`); CI missing (`missing=ci`). Each `protected-rule-not-satisfied`.
8. **Rejection still blocked**: a maintainer rejection gives `protected-blocked`, step 9, before any rule step.
9. **Race**: two executing actors (E and a second actor added to `ordinary` for this test) push rule advancements for two ready candidates with the same expected tip; exactly one is accepted, the other gets `protected-stale-tip` or `protected-not-fast-forward`.
10. **Existing behavior**: run `go test -run 'TestServeProtected|TestServeAdvance|TestServeAutomerge'` and record that it passes unchanged.
11. **Boundary B5 after the verdict lands**: run WP13's `TestStandingServeWireRuleRefusedBeforeVerdict` unchanged, and add an exact assertion in this WP's test that M's maintainer advancement with `hn-rule=nosuch` is now refused with `protected-rule-not-satisfied` and unchanged hosted refs.

### Subtask T069: Serve guide "Automerge rules" rewrite and `standing-serve-veto` example

**Steps**:

1. In `docs/serve-v0.md`, rewrite "Automerge rules" under "Advancing a protected branch": remove the statement that the server does not evaluate automerge rules; explain in plain sentences what the server checks for an advancement request with `hn-rule` (steps 7a, 10 and 13 and why step 11 is skipped), that vetoes arriving after the client's import are seen at step 10, that direct pushes and requests without `hn-rule` still need maintainer acceptance, and the three new reason codes with links to the protocol table. Leave the client paragraph for WP15 to update, but make sure no sentence in it contradicts the new behavior.
2. Add `<!-- example: standing-serve-veto -->` with the two rejection lines of the US4-3 case (the `hn-serve/1 reject` line and its `hn-serve:` sentence), placeholders in angle brackets as the contract shows.
3. Add `TestStandingServeDocumentedVeto` containing the exact marker string; it runs US4-3, substitutes real IDs and compares both lines with the documented ones.
4. Update the "Protected branches" check list of the guide ("The checks for a protected update") with one sentence pointing to the rule steps, if that section enumerates the steps.
5. Style: plain direct sentences, no em dashes.

## Test Strategy

Black-box tests through the forced-command harness. Run `go test -race -run 'TestStandingServeVerdict|TestServeProtected|TestServeAdvance|TestServeAutomerge' ./...`, then the full gate.

## Definition of Done

- [ ] T064 to T069 complete; full gate passes.
- [ ] **FR-016**: rule advancement evaluated by the server with the three reason codes, in contract order.
- [ ] **FR-017 (server)**: a differing destination digest refuses at step 7a.
- [ ] **FR-018**: direct pushes and requests without `hn-rule` unchanged.
- [ ] **FR-012 (server verdict)**: the same derivation as the client (`evaluateRuleAcceptance`).
- [ ] Hosted refs unchanged after every refusal; every pre-existing serve test passes unchanged; `standing-serve-veto` executed.
- [ ] **FR-025**: the interim `hn-rule` refusal is removed in the same change as the rule steps; boundary fixture B5 passes before and after.

## Risks

- Facts accepted earlier in the same push must count at step 10 (they already form `check.events`); a test where S's change request and E's advancement come in separate pushes covers the real flow.

## Reviewer Guidance

- Every new branch in `serve_protected.go` must be inside `options.Rule != ""`.
- Read one rejection line of each new code end to end through `extractServeRejections`.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
- 2026-10-26T09:45:00Z – llxprt – Prompt revised after analysis (C1; decision 01M5VSEHF5B1FM9TB52KWKQGWZ): removes WP13's interim hn-rule refusal with the rule steps (serve_verdict.go added to ownership); B5 assertion
