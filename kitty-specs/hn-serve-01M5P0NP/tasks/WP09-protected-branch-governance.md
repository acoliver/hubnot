---
work_package_id: WP09
title: Protected-branch governance under base and destination policy
dependencies:
- WP08
requirement_refs:
- C-004
- C-005
- C-006
- FR-008
- FR-009
- FR-012
- FR-013
- FR-022
planning_base_branch: mission/hn-serve
merge_target_branch: mission/hn-serve
branch_strategy: Planning artifacts for this mission were generated on mission/hn-serve. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into mission/hn-serve unless the human explicitly redirects the landing branch.
subtasks:
- T054
- T055
- T056
- T057
- T058
- T059
- T060
phase: Phase 3 - Push verdict
history:
- !!omap
  - at: '2026-10-25T00:09:18Z'
  - actor: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: serve_protected.go
create_intent:
- serve_protected.go
- serve_verdict.go
- serve_protected_test.go
- docs/serve-v0.md
execution_mode: code_change
owned_files:
- serve_protected.go
- serve_verdict.go
- serve_protected_test.go
- docs/serve-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP09: Protected-branch governance under base and destination policy

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Enforce the mission invariant: after any successful push, every protected branch tip is the signed head of a proposal that is accepted and ready under both its signed base policy and the destination policy as it stood immediately before the update, and that head carries the required trusted CI evidence (spec, "Invariant").

Replace WP07's protected placeholder with the twelve-step check from `contracts/push-verdict.md` ("Protected branch"), so that:

- the branch moves only by fast-forward to a proposal head (FR-008, FR-009, C-005);
- base and destination policies both govern (C-004);
- a missing or malformed policy, a creation by push and a deletion are refused (OQ-11);
- a request whose expected tip is stale is refused, so the loser of a race gets `protected-stale-tip` (FR-022);
- an `hn-advance` request comes from a maintainer under both policies (FR-014 server side).

The server never creates a commit and never signs anything (C-005, C-006).

## Context

- Read `contracts/push-verdict.md` ("Protected branch" table and the paragraphs after it), the plan's rule 1 ("Destination evaluation reuses signed facts"), `research.md` ("Destination policy evaluation") and spec US1-2 and US3-3 to US3-5 plus the edge cases.
- Start with `spec-kitty agent action implement WP09 --agent <name> --mission hn-serve-01M5P0NP`.
- From WP04 (`policy.go`): `evaluateProposalUnderPolicy`, `evaluateProposalWithCode` with `proposalCodeState`, and `pipelineDefinitionError`.
- From WP07 and WP08: `serveVerdictContext` (hosted events, in-push facts, maintainers), the placeholder `checkServeProtectedUpdate`, accepted in-push proposal refs.
- Existing code: `evaluateProposal`, `loadReplicationPolicyAt`, `buildLineageIndex` and lineage state (`lineage.go`), `actorListed`, `isProposalKind`, `sortStoredEvents`.
- `operational_acceptance_test.go`'s `TestOperationalSelfHostingAlpha` already drives a full proposal, review, CI, decision and merge flow against an ordinary remote with `writeOperationalPolicy` and `writeOperationalPipeline`; base the fixture in T058 on it.
- WP11 (all-trusted selection) runs in another lane and is not available here. Fixtures import facts with exact selections or `--all`.

## Branch Strategy

- **Planning/base branch at prompt creation**: `mission/hn-serve`
- **Final merge target for completed work**: `mission/hn-serve`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `serve_protected.go` | NEW. The twelve-step evaluation and its rejection details. |
| `serve_verdict.go` | CHANGE. Replace only `checkServeProtectedUpdate`'s placeholder with a call into `serve_protected.go`. |
| `serve_protected_test.go` | NEW. Includes the governed-proposal fixture builder that WP10, WP12 and WP13 reuse. |
| `docs/serve-v0.md` | CHANGE. "Protected branches" and "Recovering a protected branch" sections only. |

## Subtasks

### Subtask T054: Protected checks 1 to 5

**Steps**: in `serve_protected.go`, `T` is `OLD`, `H` is `NEW`. Report the first failure:

1. `NEW` zero: `protected-deletion`, next `protected branches cannot be deleted`.
2. `OLD` zero (creation by push): `protected-policy-invalid`, sentence explaining that protected branches are created by the administrator (OQ-11).
3. `options.ExpectedTip` set and different from `T`: `protected-stale-tip`, `ids=<expected>,<T>`, next `fetch, then retry or revise`. This must be checked before anything slower so the race loser gets this code (contract worked example `reject-stale-advance`).
4. No valid `.hn/policy.json` at `T` (`loadReplicationPolicyAt(gitDir, gitDir, T)`): `protected-policy-invalid`, next `ask the administrator; see recovery in the serve guide`. Keep the parsed policy and digest as the destination policy.
5. `T` not an ancestor of `H`: `protected-not-fast-forward`, `ids=<T>,<H>`, next `revise the proposal onto <T> with hn proposal revise`.

**Files**: `serve_protected.go`.

### Subtask T055: Candidate lookup, base policy, code ref and blocked checks (6 to 9)

**Steps**:

6. Candidates: every proposal-kind event (`proposal.open`, `proposal.revise`) in hosted events plus in-push accepted events whose signed `Head` equals `H`. When `options.Advance` is set, the candidate set is that one event, and it must exist and have head `H`. Empty set: `protected-no-proposal`, `ids=<H>`, `missing=proposal,review,ci,acceptance`, next `open a proposal for <H>`.
7. For each candidate: valid policy at its `Base` (`loadReplicationPolicyAt`), otherwise `protected-policy-invalid`.
8. Code: the candidate's hosted `refs/hn/proposals/<hex>` value, or the `NEW` value of a proposal ref accepted earlier in this push, must exist and equal `H`; otherwise `proposal-ref-mismatch`, `missing=proposal`.
9. Blocked, from the base evaluation (`evaluateProposalWithCode` with the base policy and base digest and the code state from step 8): `Lineage.Superseded`, `Lineage.LineageClosed`, `Lineage.MergeConflict`, `Merged`, or `Rejected` (a current maintainer rejection under base policy): `protected-blocked`, `ids` naming the blocking events (superseding revision, merge facts, rejecting decisions), next `run hn proposal status <candidate>`.

Use `events = hosted events + in-push accepted events` for every evaluation.

**Files**: `serve_protected.go`.

### Subtask T056: Base and destination readiness, missing kinds, maintainer check, multi-candidate rule (10 to 12)

**Steps**:

10. Base: `!Ready || !Accepted` gives `protected-not-ready` with `missing` built as: `review` when approvals are below `RequiredApprovals`; `ci` when any pipeline has fewer passed results than required; `acceptance` when not accepted.
11. Destination: `evaluateProposalWithCode(candidate, events, destPolicy, baseDigest, code)`. A `pipelineDefinitionError` (a pipeline required only by the destination policy has no valid definition at `H`) gives `protected-destination-policy` with `missing=ci`. Otherwise `!Ready || !Accepted` gives `protected-destination-policy` with the unmet kinds and a sentence naming the unmet threshold, for example `destination policy requires 2 approvals; 1 trusted approval found` (US3-4).
12. When `options.Advance` is set and `ctx.Actor` is not in both the base policy's and the destination policy's `Maintainers`: `advance-not-maintainer`, next `ask a maintainer to run hn merge --remote`. Direct pushes without `hn-advance` skip this step (OQ-4).

Multi-candidate rule (no `hn-advance`): accept if any candidate passes steps 7 to 11. If none passes, report the candidate whose first failing step is latest, ties broken by lowest event ID, with `ids` listing every candidate with head `H`.

Return a structured result so WP10 tests can assert codes and missing kinds, and so the rejection text names full IDs.

**Files**: `serve_protected.go`.

### Subtask T057: Wire the protected arm into the verdict

**Steps**:

1. Replace the placeholder body of `checkServeProtectedUpdate` in `serve_verdict.go` with a call to the new evaluator. Protected updates are evaluated last (WP07 order), after actor and proposal refs in the same push.
2. Confirm the protected update sees in-push facts: a push carrying the maintainer's acceptance on `refs/hn/actors/<M>` plus the protected update in one atomic push must be evaluated with that acceptance (contract, "Inputs").
3. No change to other arms.

**Files**: `serve_verdict.go`.

### Subtask T058: Governed proposal fixture builder

**Purpose**: one reusable way to build real, signed governance evidence on a hosted repository.

**Steps** (in `serve_protected_test.go`, helpers prefixed `serveGoverned`):

1. `newServeGovernedFixture(t, options)` creates a server harness and a hosted repository whose `main` policy requires, by default, one trusted approval, one passing result of pipeline `test` from a trusted runner, and one maintainer acceptance (`writeOperationalPolicy` and `writeOperationalPipeline`, or equivalent). It creates client clones for author A, reviewer R, runner U and maintainer M, runs `hn init` in each, binds each actor's SSH key with `hn serve key add`, and records full actor IDs.
2. Methods to drive the public CLIs, each followed by `hn sync origin` with exact selections (or `--all`) so participants see each other's facts: `OpenProposal(base, change)`, `Approve(id)`, `RequestCI(id)`, `RunCI(id)` (use `hn run execute ... --backend host --allow-unsafe-host-execution` or `hn runner once` with the same flags, as the operational tests do), `Accept(id)`, `Reject(id)`, `Revise(id, ...)`.
3. `ServerTip(branch)`, `ServerRefs()` (for byte-identical snapshots), and `PushMain(clone, commit, pushOptions...)` returning stderr and the parsed rejections.
4. `SetMainForTest(commit)` using `hn serve repo lock-exec NAME -- git update-ref refs/heads/main NEW OLD` to set up destination-policy scenarios without the governed path. Use it only to arrange preconditions, never to assert behavior.
5. Keep the API small and documented; WP10, WP12 and WP13 reuse it.

**Files**: `serve_protected_test.go`.

### Subtask T059: Protected-branch black-box tests

**Steps** (tests `TestServeProtected...`). After every rejection assert server refs are byte-identical and exactly one parseable line exists per rejected ref (SC-002).

1. **US1-2**: A pushes `main` one commit ahead with no proposal: `protected-no-proposal`, `missing=proposal,review,ci,acceptance`. Compare the relayed lines field for field with the `reject-direct-main` example in `contracts/push-verdict.md` (and the copy in the serve guide) after replacing illustrative IDs with fixture IDs.
2. **Accepted direct push**: a ready, accepted, CI-tested proposal head pushed to `main` by any mapped actor without push options is accepted; server `main` equals the head; no new commit exists (`git rev-list --count` unchanged apart from the head's own history).
3. **US3-5**: ready except CI: `protected-not-ready`, `missing=ci`.
4. **US3-4**: destination policy requires two approvals while the base policy requires one: use a commit `Y` that changes the policy, between the proposal's base `X` and its head `H`, set `main` to `Y` with `SetMainForTest`, then push `H`: `protected-destination-policy`, `missing=review`, sentence names the threshold.
5. **Removed reviewer**: destination policy at `Y` drops R from trusted reviewers: R's approval does not count: `protected-destination-policy`, `missing=review`.
6. **Destination-only pipeline**: destination policy requires pipeline `lint` whose definition is absent at `H`: `protected-destination-policy`, `missing=ci`.
7. **Blocked**: superseded candidate's head, rejected candidate, already merged candidate: each `protected-blocked`.
8. **Policy invalid**: `main` set to a commit with malformed policy: every update refused with `protected-policy-invalid`; base commit without policy: same code.
9. **Creation and deletion**: delete a protected branch's ref with `lock-exec`, then push it: `protected-policy-invalid`; push a deletion of `main`: `protected-deletion`.
10. **Not fast-forward**: `main` advanced past the proposal's base on another line: `protected-not-fast-forward` naming the current tip.
11. **Stale tip**: `-o hn-expected-tip=<old tip>` after the tip moved: `protected-stale-tip`, `ids=<expected>,<current>`.
12. **Code missing**: candidate whose proposal ref was never pushed: `proposal-ref-mismatch`, `missing=proposal`; same candidate with the proposal ref in the same push as the `main` update: accepted.
13. **advance-not-maintainer**: `-o hn-advance=<candidate>` from A (not a maintainer): refused; same from M: accepted.
14. **Multiple candidates**: two candidates share head `H`, one rejected and one accepted and ready: accepted; both failing: the report names the latest failing step and `ids` lists both.
15. **SC-003 check**: after every accepted protected update in this file, a fresh verifier clone runs `hn sync` with exact selections and `hn proposal status <candidate>`, and must report accepted with the required CI evidence.

**Files**: `serve_protected_test.go`.

### Subtask T060: Serve guide protected-branch and recovery sections

**Steps**:

1. "Protected branches" in `docs/serve-v0.md`: what protects a branch (server configuration, default `refs/heads/main`, never read from pushed content, OQ-5); the invariant in plain words; the twelve checks as a short ordered list with their codes; base versus destination policy (link to `docs/governance-v0.md#destination-policy`); why the server never merges or rebases and how a stale proposal is revised (HN-004); the multi-candidate rule; that direct pushes by any mapped actor pass under the same gates and that `hn-advance` additionally requires a maintainer. Include the marked example `<!-- example: reject-direct-main -->` copied from the contract, and add `TestServeProtectedDocumentedExample` that compares it with real output after ID substitution.
2. "Recovering a protected branch": when `protected-policy-invalid` blocks every update, how the administrator prepares a repaired commit (a commit descending from the current tip that restores a valid `.hn/policy.json`, reviewed out of band), and runs `hn serve repo lock-exec NAME -- git update-ref refs/heads/main <repaired> <current>`; why the old value is required (it fails if the tip moved); that this bypasses governance by design and is recorded only in the administrator's own records, since the server signs nothing.
3. Writing style: plain direct sentences, descriptive headings, no em dashes.

**Files**: `docs/serve-v0.md`, `serve_protected_test.go`.

## Test Strategy

Real governance evidence through the public CLIs on hosted repositories, with administrator shortcuts only for preconditions. Run `go test -race -run TestServeProtected ./...`, then the full gate. These tests are slow; keep fixtures shared within a test function where steps do not interfere, and use subtests.

## Definition of Done

- [ ] T054 to T060 complete; full gate passes.
- [ ] **FR-008**: every check in the table is implemented in order and tested.
- [ ] **FR-009 / C-005**: no protected tip moves without the required trusted CI; the server creates no commit.
- [ ] **C-004**: destination policy thresholds, lists and destination-only pipelines are enforced with base-digest decisions.
- [ ] **FR-012 / FR-013**: every protected rejection leaves refs byte-identical and emits one parseable line naming full IDs and missing kinds.
- [ ] **FR-022**: stale expected tips are refused with `protected-stale-tip`.
- [ ] **C-006**: the server neither signs nor records verdicts as evidence.
- [ ] Guide sections written; the `reject-direct-main` example is executed.

## Risks

- Fixture cost: each governed proposal needs several clones and syncs. Build once per test function and reuse.
- The destination-policy scenario relies on a policy-changing commit between base and head; document the construction in the test so reviewers can follow it.

## Reviewer Guidance

- Walk the contract table and find each step and its test.
- Confirm decisions are matched by base digest in the destination evaluation.
- Confirm `hn-expected-tip` is checked before policy loading.

## Activity Log

- 2026-10-25T00:09:18Z – system – Prompt generated via /spec-kitty.tasks
- 2026-10-25T11:27:14Z – llxprt – WP08 independent review requests changes: admitted within-budget logs can poison subsequent hosted-fact reads, and partial measurements violate required budget rejection priority. See tasks/WP08-hubnot-fact-ref-rules/review-feedback-1.md. Wait for corrected WP08 and rebase any existing work onto the corrected lane head before continuing. This handoff does not request a WP09 status change.
- 2026-10-25T11:58:07Z – llxprt – WP08 independent review cycle 2 returned WP08 to planned: NFR-002/T048.3 range measurement double-counts an older hosted signature blob reused as a valid run log, causing valid at-limit max-objects and max-total-bytes pushes to fail. See tasks/WP08-hubnot-fact-ref-rules/review-feedback-2.md. Wait for corrected WP08 and rebase any existing work onto the corrected lane head before continuing. No WP09 status change requested; preserve the protected-rule ownership.
