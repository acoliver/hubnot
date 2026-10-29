---
work_package_id: WP19
title: Remote rule merge recovery and the publication-only rerun
dependencies:
- WP15
requirement_refs:
- C-008
- FR-019
- FR-020
- FR-023
- NFR-003
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T096
- T097
- T098
- T099
- T100
phase: Phase 5 - hn serve rule advancement
history:
- !!omap
  - timestamp: '2026-10-26T09:45:00Z'
  - agent: llxprt
  - action: Prompt generated during analysis remediation (finding I3, decision 01M5VSEHFCZGQHQ14CKJ3MAMP7)
agent_profile: implementer-ivan
authoritative_surface: automerge.go
create_intent:
- standing_serve_test.go
execution_mode: code_change
owned_files:
- automerge.go
- serve_client.go
- standing_serve_test.go
- docs/serve-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP19: Remote rule merge recovery and the publication-only rerun

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Make every interruption of a remote rule merge recoverable by rerunning the same command (FR-020 remote, US4-7, US4-8; decisions `01M5VF10Q3SQX8WTK2MEN92ZSC` and `01M5VSEHFCZGQHQ14CKJ3MAMP7`; [contracts/automerge-cli.md](../contracts/automerge-cli.md) "Remote repair", "Publication-only rerun" and "Interruptions and reruns"):

```text
hn automerge PROPOSAL --rule NAME --execute --remote REMOTE
```

Two rerun paths, for standing rules only:

1. **Remote repair** (the server applied the push, no fact was recorded): `automergeRemote` skips the local-equals-hosted check when the hosted tip contains the head, the candidate has no merge fact and the local target still equals the signed base, and `mergeRemoteBound` step 5 records the missing rule merge fact once with `commit` equal to the head.
2. **Publication-only rerun** (the fact was recorded but not, or only partly, published): today both inspections stop such a rerun with `proposal is already recorded as merged` before any publication step (analysis finding I3; `automerge.go` lines 55 to 80, 233 to 237 and 654 to 659). `cmdAutomerge` takes a separate, authorized path before `inspection.eligible()`: it skips the merged blockers of both inspections and the local-equals-hosted check, publishes or republishes the identity's chain, and confirms that the hosted branch contains the fact's commit. It signs nothing, appends no event and sends no branch update.

A rerun without `--remote` keeps the ordinary `proposal is already recorded as merged` refusal, and so does a rerun by any other identity or when the candidate's merge facts are not this identity's rule merge facts. The server still signs nothing (C-008).

## Context

- Read spec US4-7, US4-8 and FR-020; the contract sections named above; [data-model.md](../data-model.md) "Lifecycle of a candidate under a standing rule" (`Recorded` state); [research.md](../research.md) finding 22.
- Start with `spec-kitty agent action implement WP19 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing code: `cmdAutomerge`, `inspectAutomerge` (the merged blocker at lines 233 to 237), `automergeRemote` (`automerge.go`); `mergeRemoteBound` steps 3 to 9, `publishLocalFacts`, `serveAdvanceReadTip`, `serveAdvanceIsAncestor`, `serveAdvanceRecordAndPublish`, `serveAdvanceInterrupt` and its seams `publish`, `push`, `record`, `republish` (`serve_client.go`); `serveRemoteIdentifies`.
- From WP07: `maintainerMergeCheck` and `validateMergeEventWith` (read only); a projected `proposal.merged` with `Rule` set whose maintainer check fails was admitted through path (b). From WP11: `appendRuleMergeEvent`. From WP15: the rule binding of `mergeRemoteBound`, `newStandingServeFixture`, `StandingReady`, `RulePush` and the `TestStandingServeAdvance...` helpers.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `automerge.go` | CHANGE. The publication-only branch in `cmdAutomerge` before `inspection.eligible()`, its helper that finds the identity's own rule merge fact, and the remote repair branch in `automergeRemote`. Local execution and `inspectAutomerge` stay as WP10 to WP12 left them. |
| `serve_client.go` | CHANGE. `serveAdvancePublishOnly`, which publishes and confirms; step 5's repair recording for a rule binding if WP15 left it incomplete. `mergeRemoteBound` without a rule stays byte-identical. |
| `standing_serve_test.go` | CHANGE. `TestStandingServeRecovery...`. |
| `docs/serve-v0.md` | CHANGE. The rule advancement rows of "Interruptions and reruns". |

## Subtasks

### Subtask T096: Publication-only rerun: authorization and selection

**Steps**:

1. Add `func automergeOwnRuleMergeFact(events []StoredEvent, proposal StoredEvent, rule, actor, policyDigest string) (*StoredEvent, error)` in `automerge.go`. It returns the `proposal.merged` events (sorted by ID, first wins) whose `Subject` is the candidate, whose `Rule` equals `rule`, whose signer is `actor`, whose `Policy` equals the base policy digest, and for which `maintainerMergeCheck` fails, so projection admitted them through path (b). Projection has already validated every listed fact; do not re-run path (b).
2. In `cmdAutomerge`, after `printAutomergeInspection` and before `inspection.eligible()`, when `*execute && *remote != ""` and the inspection's rule exists and is standing: load the identity; require `actorListed(identity.Actor, inspection.Rule.Actors)` (authorization; otherwise fall through to the existing path, which refuses as today); collect events and call `automergeOwnRuleMergeFact`. When it returns a fact and `serveRemoteIdentifies(*remote)` is true, print `Publication-only rerun: rule merge event <short ID> is already recorded` and call `serveAdvancePublishOnly` (T097); otherwise continue with the existing code unchanged.
3. The path must not run for acceptance mode `maintainers`, without `--remote`, for another identity, or when the only merge facts are acceptance merge facts or another actor's rule merge facts. In each of those cases the existing refusals, including `proposal is already recorded as merged`, apply unchanged.

### Subtask T097: Publication-only rerun: publish and confirm the hosted branch

**Steps**:

1. Add `func serveAdvancePublishOnly(remote, branchRef string, fact *StoredEvent, proposal *StoredEvent) error` in `serve_client.go`:
   - `publishLocalFacts(remote, nil)`; on error return `merge fact recorded locally; run hn sync <remote> to publish it` as step 9 does today;
   - `serveAdvanceInterrupt("republish")` after publishing, so the existing seam also covers this path;
   - `serveAdvanceReadTip(remote, branchRef)`; require the tip to equal `fact.Event.Commit` or descend from it (`serveAdvanceIsAncestor`);
   - on success print `<remote> <branchRef> contains <short head> for proposal <short ID>` and `Rule merge event <short ID> under rule <name> is published on <remote>`;
   - otherwise return `rule merge event <short ID> is recorded but <remote> <branchRef> does not contain <commit>; a publication-only rerun never advances the branch`.
2. It never calls `appendEvent`, `appendRuleMergeEvent` or a branch push. Add a comment that says so and why (FR-020: no second fact and no second advancement).

### Subtask T098: Remote repair branch in `automergeRemote`

**Steps**:

1. After `cmdSync` and the second inspection, standing rules only: when the hosted tip equals the head or descends from it (`serveAdvanceIsAncestor(head, hosted)`), the inspection reports no merge fact, and the local target still equals the signed base, skip the "local is at X but remote has Y" refusal and call `mergeRemoteBound` with the rule binding and `ExpectedTip: hosted`.
2. Its step 5 sees the head contained and records the missing fact once with `commit` equal to the head, printing:

   ```text
   origin refs/heads/main already contains 9b2e4c6a8d0f for proposal 1c2d3e4f5a6b
   Recorded missing rule merge event 7d8e9f0a1b2c under rule ordinary with 2 evidence item(s)
   ```

3. Maintainers-mode rules keep the existing behavior and messages.

### Subtask T099: Interruption and rerun tests

**Steps** (tests `TestStandingServeRecovery...` in `standing_serve_test.go`, through the standing serve fixture; E's clone has `main` checked out at the hosted tip and the all-trusted selection). For each row, interrupt with `HN_INTERNAL_TESTING=1` and `serveAdvanceInterruptEnv`, then rerun the same command without the variable:

1. **publish** (facts published, no push yet): the rerun is a full run; exactly one rule merge fact and one advancement. A push the server never applied leaves the same state and needs no separate seam.
2. **push** (the `push` seam fires after the server applied the push): the rerun takes the remote repair branch and prints the repair lines; hosted `main` is unchanged by the rerun; exactly one rule merge fact with `commit` equal to the head.
3. **record** (fact recorded, not published): the rerun prints `Publication-only rerun: ...`, publishes the fact, and confirms the branch; afterwards the server's `refs/hn/actors/<E>` equals E's local actor ref; E's chain gained no event during the rerun; the server push log shows no `hn-advance` push during the rerun; hosted `main` is unchanged.
4. **republish** (fact published, interrupted after publication): the rerun takes the publication-only path, publishes nothing new, confirms the branch, and creates no event and no advancement.
5. **completed run**: rerunning after a successful run gives the publication-only output again and changes nothing (idempotent); a third run is identical.
6. **Refusals that stay**: the same rerun without `--remote` is refused with `proposal is already recorded as merged`; X running the same command is refused (not an executing actor) and publishes nothing; M running it with no rule merge fact of M's own is refused with `proposal is already recorded as merged`; a candidate merged only by an acceptance merge fact is refused the same way for E.
7. **Hosted branch does not contain the commit**: E signs a valid rule merge fact for a ready candidate with the WP06 builder whose `commit` is a commit the server does not hold on `main`; the rerun publishes, then exits 1 with the `never advances the branch` message; hosted `main` is unchanged and E's chain gained no event.
8. After every case, assert the count of rule merge facts for the candidate across all chains and the hosted refs against the expected values.

### Subtask T100: Serve guide reruns for rule advancement

**Steps**:

1. In `docs/serve-v0.md` "Interruptions and reruns", add the rule advancement rows of the contract table: full run, remote repair, publication-only rerun after recording, after publication and after a completed run. Say who may use the publication-only rerun (the executing actor that signed the fact) and that it never signs, records or advances anything.
2. State that a rerun without `--remote` refuses an already-merged candidate as before, and that `hn merge` repair remains maintainer-only.
3. Style: plain direct sentences, no em dashes.

## Test Strategy

Black-box tests through the forced-command harness. Run `go test -race -run 'TestStandingServeRecovery|TestStandingServeAdvance|TestServeAdvance|TestServeAutomerge' ./...`, then the full gate.

## Definition of Done

- [ ] T096 to T100 complete; full gate passes.
- [ ] **FR-020 (remote)**: every row of the interruption table ends with exactly one rule merge fact and one advancement; the record and republish rows finish through the publication-only path.
- [ ] **FR-019 / NFR-003**: only the executing actor that signed the fact can take the publication-only path; every other rerun keeps today's refusal and changes no ref or chain.
- [ ] **C-008**: the server signs nothing; the publication-only path signs nothing either.
- [ ] The local already-merged refusal is unchanged; `hn merge --remote` and maintainers-mode automerge are unchanged; every pre-existing serve test passes unchanged.

## Risks

- The publication-only branch sits before `inspection.eligible()`; a mistake in its conditions could let a rerun skip blockers that matter. Keep the conditions in one function with a test for each refusal row of T099 step 6.
- `publishLocalFacts` pushes every local fact; a refusal of an unrelated fact by the server shows up here. Report it as `publishLocalFacts` does today.

## Reviewer Guidance

- Confirm the publication-only path calls no signing function and no branch push.
- Read the T099 log of fact counts and hosted refs for every row.

## Activity Log

- 2026-10-26T09:45:00Z – llxprt – Prompt generated during analysis remediation (finding I3, decision 01M5VSEHFCZGQHQ14CKJ3MAMP7); the remote repair branch and interruption rows move here from WP15
