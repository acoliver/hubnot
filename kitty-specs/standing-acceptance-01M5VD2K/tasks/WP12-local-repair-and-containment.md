---
work_package_id: WP12
title: Local repair and executing-actor containment
dependencies:
- WP11
requirement_refs:
- C-007
- FR-009
- FR-019
- FR-020
- FR-023
- NFR-003
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T054
- T055
- T056
- T057
- T058
phase: Phase 4 - Local standing acceptance
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: automerge.go
create_intent: []
execution_mode: code_change
owned_files:
- automerge.go
- standing_automerge_test.go
- docs/governance-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP12: Local repair and executing-actor containment

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Two things that make delegation to a rule safe in a local clone:

1. **Repair (FR-020, local)**: when the code merged but the rule merge fact was not recorded, rerunning the same `hn automerge PROPOSAL --rule NAME --execute` re-verifies the rule and records the missing fact exactly once, with `commit` set to the existing merge commit, and creates no commit and moves no ref ([contracts/automerge-cli.md](../contracts/automerge-cli.md) "Local repair"). `hn merge` repair stays maintainer-only (OQ-9).
2. **Containment (FR-019, NFR-003, C-007)**: an executing actor that is not a maintainer cannot publish a decision that counts, cannot `hn merge`, cannot execute a rule that does not list it, and cannot merge a policy amendment or protected-check change under any rule. Every attempt is refused before any countable event or ref change. Every other existing blocker of User Story 6 still refuses execution.

## Context

- Read spec US2-2, US2-3, US6 (all), FR-019, FR-020; [contracts/automerge-cli.md](../contracts/automerge-cli.md) "Local repair" and "Commands an executing actor cannot use"; decision `01M5VF06S28D7WBPBE9K7BCK18`.
- Start with `spec-kitty agent action implement WP12 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code: `inspectAutomerge`, `cmdAutomerge`, `advanceAutomerge` and its process seam `automergeProcessHook` (`automerge.go`); `findProposalMergeCommit`, `mergeProposal` (refuses contained heads whenever a guard is present, `governance.go` line 216), `cmdDecide`.
- From WP11: `appendRuleMergeEvent`, `ruleMergeAuthority`. From WP10: `automergeInspection.RuleAcceptance`. From WP06: the harness.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `automerge.go` | CHANGE. Repair state in `inspectAutomerge`, repair branch in `cmdAutomerge`, rerun hints in `advanceAutomerge` messages under acceptance mode `rule`. `automergeRemote` belongs to WP15. |
| `standing_automerge_test.go` | CHANGE. Add `TestStandingAutomergeRepair...` and `TestStandingAutomergeContainment...`. |
| `docs/governance-v0.md` | CHANGE. Limits and repair text for standing rules. |

`governance.go` should not need changes. If it does, keep the change inside a `guard.Rule != nil` branch and record why.

## Subtasks

### Subtask T054: Repair state in `inspectAutomerge` under acceptance mode `rule`

**Steps**:

1. Add `Repair string` (the merge commit) to `automergeInspection`.
2. Only when the rule exists and is standing, and `current != proposal.Event.Base`, check the repair conditions of the contract:
   - the candidate has no merge fact under its base policy (`!evaluation.Merged`);
   - `findProposalMergeCommit(proposal, current)` returns exactly one first-parent merge commit naming the head as a parent (its errors mean "not a repair state", never a hard error of the preview);
   - the current target policy digest equals the base policy digest;
   - every other rule check passes on current evidence (no other blocker remains).
3. When all hold, do not append the blocker `target no longer equals the signed base; revise the candidate (use hn merge for a missing merge-event repair)`; set `Repair`. Compute the decision after all other blockers are known, so a repair state is reported only when it is the sole difference.
4. Under acceptance mode `maintainers`, nothing changes: the blocker and its text, which directs to `hn merge`, stay.
5. `printAutomergeInspection`: when `Repair != ""`, print `Repair: code merged at <shortOID>; --execute records the missing rule merge fact` before the `Eligible:` line.

### Subtask T055: Repair branch of `hn automerge --execute`

**Steps**:

1. In `cmdAutomerge`, after the identity and executing-actor checks and the existing upstream check, when `inspection.Repair != ""`: re-run `inspectAutomerge` and require the same repair commit, base, head and policy digest (refuse with the existing "automerge candidate, policy or target changed before merge" otherwise); then `appendRuleMergeEvent(identity, proposal, evaluation, next.RuleAcceptance, next.Repair)` and print:

   ```text
   Recorded missing rule merge event 7d8e9f0a1b2c under rule ordinary with 2 evidence item(s)
   ```

   Do not call `mergeProposal`; it refuses contained heads with a guard by design.
2. A second rerun reports the existing blocker `proposal is already recorded as merged` and changes nothing.
3. Rerun hints: under acceptance mode `rule`, the messages of `advanceAutomerge` that today say `run 'hn merge <id>'` (uncertain ref outcome, interrupted recording) name `hn automerge <id> --rule <name> --execute` instead. Pass the hint into `advanceAutomerge` from `cmdAutomerge`; under acceptance mode `maintainers` the existing text stays byte-identical (existing tests may read it).
4. Recording failure after a successful merge (`mergeProposal`'s `code merged at <commit> but recording failed`) is followed by the same rerun hint under acceptance mode `rule`.

### Subtask T056: Local repair tests (FR-020)

**Steps** (tests `TestStandingAutomergeRepair...`):

1. **Interrupted for real (in-process)**: in E's clone (`t.Chdir`), set `automergeProcessHook` to fail at `merge-committed`, call `cmdAutomerge` for a ready candidate: it returns the interruption error naming the `hn automerge` rerun, `main` is the merge commit, and E's chain has no new event. Clear the hook (the existing tests reset it in `t.Cleanup`; do the same).
2. **Rerun through the binary**: `hn automerge <c> --rule ordinary` (preview) prints the `Repair:` line and no blocker; `--execute` prints `Recorded missing rule merge event ...`; `main` is unchanged by the rerun; E's chain gained exactly one `proposal.merged` with `rule` `ordinary` and `commit` equal to the merge commit.
3. **Second rerun**: blocked with `proposal is already recorded as merged`; no change.
4. **Constructed state**: build the same state without the seam (create the two-parent merge commit with `git commit-tree` and move `main` with `git update-ref`), so the black-box path is covered with no in-process step.
5. **Repair refused**: when a trusted change request arrives before the rerun, the preview shows the change-request blocker and the target blocker, and `--execute` records nothing. When the target moved again with another policy-changing merge, the repair is refused.
6. **Maintainers mode unchanged**: the same contained state under a maintainers-mode rule keeps the existing blocker that directs to `hn merge`, and `hn merge` (as M) repairs as today.
7. **hn merge stays maintainer-only**: E running `hn merge <c>` in the contained state is refused with `identity <E short> is not a maintainer under proposal policy`.

### Subtask T057: Containment and manual-path tests (US2-2, US2-3, US6-4 to US6-6)

For every case: snapshot all refs and every chain's event count in the acting clone (and the bare remote where relevant) before, and assert no countable event and no ref change after (NFR-003).

1. **US2-2**: a policy amendment adding a standing rule is opened as a proposal; E and X each run `hn automerge <amendment> --rule ordinary --execute` and `--rule other --execute`: refused with `policy amendments require manual merge`. Then R approves, M accepts and `hn merge` lands it as today (reuse `h.AdoptThroughAmendment` pieces).
2. **US2-3**: a proposal whose head adds rule `selfgrant` listing its author A and a new actor as executing actors; A and that actor run `hn automerge <p> --rule selfgrant --execute`: refused with `automerge rule "selfgrant" does not exist in the signed base policy`.
3. **US6-4**: with `ProtectedPaths: ["scripts/lint.sh"]`, a candidate changes `scripts/lint.sh` and `src/x`, with every review, CI result, designated approval and maintainer acceptance present; E executes under `ordinary`: refused with `protected check changes require manual merge`. Same for a candidate that changes `.hn/policy.json` together with `src/x`.
4. **US6-5**: target moved after the base; dirty worktree; an unfinished Git operation (`git merge` with a conflict left in place, or a `MERGE_HEAD` created by a real conflicting merge); a shallow clone; a partial clone (`git clone --filter=blob:none`): each refused with today's message.
5. **US6-6** as E with its own identity: `hn decide <c> --accept` refused with `is not a maintainer under policy`; `hn merge <c>` refused; `hn merge <c> --remote origin` against the ordinary bare remote refused at step 1; `hn automerge <c> --rule other --execute` where rule `other` has the same paths and authors but lists only X: refused with `identity <E> is not an executing actor for automerge rule "other"`.
6. **US6-1 to US6-3 at execution**: a maintainer rejection, a trusted change request, a trusted failure, a missing passing result, an author outside the rule and a path outside the rule each refuse `--execute` with `automerge blocked:` naming the blocker.

### Subtask T058: Governance guide limits and repair section

**Steps**: in `docs/governance-v0.md` under "Commit path, publication and recovery" (or a new subsection "Limits of standing acceptance" next to it), state in plain sentences:

- what an executing actor can and cannot do (the table of the contract, in prose or a table);
- that policy amendments and protected-check changes always take the manual path;
- that every existing blocker applies;
- how to repair an interrupted rule merge (rerun the same command; what the preview prints; that it records once and moves nothing), and that `hn merge` repair stays maintainer-only;
- that the rerun hint in error messages names the command to run.

No new example marker is required here; if you add one, add its executing test in `standing_automerge_test.go` in this WP. Style: no em dashes.

## Test Strategy

Black-box tests plus one in-process interruption test. Run `go test -race -run 'TestStandingAutomerge|TestAutomerge' ./...`, then the full gate.

## Definition of Done

- [ ] T054 to T058 complete; full gate passes.
- [ ] **FR-020 (local)**: the same command records the missing rule merge fact exactly once with `commit` = the merge commit, and no second merge.
- [ ] **FR-019 / NFR-003 (local)**: each forbidden command refused with zero countable events and zero ref changes.
- [ ] **FR-009 / C-007**: every listed blocker refuses execution; policy amendments and protected changes never merge by rule.
- [ ] Maintainers-mode repair text and behavior unchanged; every pre-existing test passes unchanged.

## Risks

- `findProposalMergeCommit` errors when the head appears in several merge commits; treat that as "not a repair state" and keep the target blocker, so the user sees a refusal.

## Reviewer Guidance

- Confirm the repair branch never calls `mergeProposal` and never writes a ref.
- Check that the maintainers-mode blocker text is unchanged.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
