---
work_package_id: WP10
title: Advancement request and fast-forward merge fact
dependencies:
- WP09
requirement_refs:
- C-005
- FR-014
- FR-015
- FR-016
- NFR-005
- NFR-006
planning_base_branch: mission/hn-serve
merge_target_branch: mission/hn-serve
branch_strategy: Planning artifacts for this mission were generated on mission/hn-serve. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into mission/hn-serve unless the human explicitly redirects the landing branch.
subtasks:
- T061
- T062
- T063
- T064
- T065
- T066
- T067
phase: Phase 4 - Advancement
history:
- !!omap
  - at: '2026-10-25T00:09:18Z'
  - actor: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: serve_client.go
create_intent:
- serve_client.go
- serve_advance_test.go
- docs/serve-v0.md
execution_mode: code_change
owned_files:
- serve_client.go
- governance.go
- main.go
- serve_advance_test.go
- docs/serve-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP10: Advancement request and fast-forward merge fact

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Implement the client side of advancement exactly as `contracts/advancement.md` specifies:

```text
hn merge PROPOSAL --remote REMOTE [--branch BRANCH]
```

The command publishes the maintainer's facts, asks the `hn serve` remote to fast-forward the protected branch to the proposal head that CI tested, confirms the move, and records `proposal.merged` with `commit == head` (FR-014, FR-015). A rerun after any interruption records a missing merge fact exactly once. `hn merge PROPOSAL` without `--remote` keeps its current `--no-ff` behavior and repair path unchanged (FR-016). The server holds no signing identity (C-006); the maintainer's client signs.

Also prove US3 end to end, including a fresh verifier clone (NFR-006) and the NFR-005 race.

## Context

- Read `contracts/advancement.md` (all of it), `research.md` "Advancement request wire shape", spec US3 and FR-014 to FR-016.
- Start with `spec-kitty agent action implement WP10 --agent <name> --mission hn-serve-01M5P0NP`.
- Existing code: `cmdMerge`, `appendProposalMergeEvent`, `requireLineageTerminalEligibility` (`governance.go`); `requireFullEventID`, `prepareShallowVerification`, `guardShallowEventClosure`, `collectEvents`, `resolveEventDependency`, `guardProposalEvaluationDependencies`, `evaluateProposal`, `requireProposalCode`, `loadIdentity`, `publishLocalFacts` (`commands.go`), `actorListed`.
- From WP03: `serveRemoteIdentifies`, `extractServeRejections`, `servePushOptionAdvance`, `servePushOptionExpected`.
- From WP09 tests: `newServeGovernedFixture` and its methods.
- WP11 (all-trusted) is in another lane and not available here; fixtures use exact selections or `--all`.

## Branch Strategy

- **Planning/base branch at prompt creation**: `mission/hn-serve`
- **Final merge target for completed work**: `mission/hn-serve`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `governance.go` | CHANGE. Flag parsing at the top of `cmdMerge` and dispatch to `serve_client.go`. Do not alter the existing no-flag path. |
| `serve_client.go` | NEW. The nine advancement steps. |
| `main.go` | CHANGE. Add `  hn merge PROPOSAL --remote REMOTE [--branch BRANCH]` to `printUsage`, directly after `  hn merge PROPOSAL`. |
| `serve_advance_test.go` | NEW. |
| `docs/serve-v0.md` | CHANGE. "Advancing a protected branch" section only. |

## Subtasks

### Subtask T061: `hn merge PROPOSAL --remote REMOTE [--branch BRANCH]` parsing and usage

**Steps**:

1. At the top of `cmdMerge`: if `args` contains no `--remote` and no `--branch` token, fall through to the existing code with `args` untouched, so the old `len(args) != 1` check and every message stay identical. Otherwise parse with `quietFlags("merge")`: one positional `PROPOSAL` plus `--remote REMOTE` and optional `--branch BRANCH`. `--branch` without `--remote` is a usage error. Accept flags before or after the positional argument.
2. Usage error text: `usage: hn merge PROPOSAL [--remote REMOTE [--branch BRANCH]]`.
3. `BRANCH` defaults to `main`; accept a short name or `refs/heads/...`; normalize to `refs/heads/NAME` and validate it (`git check-ref-format`). `REMOTE` must pass `validReplicationRemote` and exist (`git remote get-url`).
4. Add the usage line in `main.go`.

**Files**: `governance.go`, `main.go`.

### Subtask T062: Advancement steps 1 to 4

**Steps** in `serve_client.go` (`cmdMergeRemote(proposalID, remote, branchRef string) error`); each failure names the step in its message:

1. **Validate locally**: the same sequence `cmdMerge` uses up to the maintainer check (full ID, shallow guards, collect, resolve, dependency guard, evaluate, lineage eligibility, no rejection, accepted, code available, maintainer under base policy), plus `evaluation.Ready`. Call the same functions; do not refactor `cmdMerge`. When `evaluation.Merged` is true, do not fail yet: step 5 handles it.
2. **Identify the remote**: `serveRemoteIdentifies(remote)`; on false: `remote REMOTE is not an hn serve remote; use hn merge PROPOSAL in a checkout of the target branch`.
3. **Publish facts**: `publishLocalFacts(remote, nil)`. A failure prints any `hn-serve/1` lines from the error output and stops. If `publishLocalFacts` does not expose Git's stderr, capture it by running the same pushes through a small local helper only if necessary; prefer surfacing the existing error text. Record what you chose in the Activity Log.
4. **Read the tip**: `git ls-remote REMOTE BRANCHREF` gives `T` (refuse when absent). If `T` is not present locally, `git fetch REMOTE BRANCHREF:refs/remotes/REMOTE/NAME`.

**Files**: `serve_client.go`.

### Subtask T063: Advancement steps 5 to 7

**Steps**:

5. **Repair if already advanced**: when `H` equals `T` or `git merge-base --is-ancestor H T` succeeds, skip to step 8. If the candidate is already recorded as merged under its base policy, print `Proposal ID is already advanced on REMOTE BRANCH and recorded as merged` and exit 0.
6. **Request advancement**:

   ```text
   git push --porcelain --push-option=hn-advance=PROPOSAL --push-option=hn-expected-tip=T REMOTE +H:BRANCHREF
   ```

   Capture stdout and stderr. On failure, print every line returned by `extractServeRejections(stderr)` (the `hn-serve/1` line and its sentence, with `remote: ` and padding removed) to stderr and exit 1. If no `hn-serve/1` line is present, print Git's stderr. The branch is unchanged.
7. **Confirm**: `git ls-remote REMOTE BRANCHREF` must equal `H`; otherwise exit 1 with `server accepted the push but BRANCH is at X; rerun hn merge PROPOSAL --remote REMOTE`.

**Files**: `serve_client.go`.

### Subtask T064: Advancement steps 8 and 9

**Steps**:

8. **Record the merge fact**: re-collect events, re-resolve and re-evaluate the candidate (mirroring the repair block in `cmdMerge`): lineage eligible, not merged, accepted, ready, identity still a maintainer under the base policy. Then `appendProposalMergeEvent(identity, candidate, evaluation, H)`, which sets `commit = head = H`, `policy` = base digest and `evidence` = sorted accepting decision IDs. Never call `findProposalMergeCommit` in this path.
9. **Publish again**: `publishLocalFacts(remote, nil)`. On failure print `merge fact recorded locally; run hn sync REMOTE to publish it` and exit 1.

Output on success, exactly:

```text
Advanced REMOTE refs/heads/main to 9b2e4c6a8d0f for proposal 1c2d3e4f5a6b
Recorded merge event 7d8e9f0a1b2c with 1 acceptance decision(s)
```

After the repair path:

```text
REMOTE refs/heads/main already contains 9b2e4c6a8d0f for proposal 1c2d3e4f5a6b
Recorded missing merge event 7d8e9f0a1b2c with 1 acceptance decision(s)
```

Use the existing `shortOID` and `shortID` helpers for the short forms.

Test-only interruption points, gated like `runReplicationProcessHook`: when `HN_INTERNAL_TESTING=1` and `HN_TEST_SERVE_ADVANCE_INTERRUPT_AFTER` equals `publish`, `push`, `record` or `republish`, exit 1 right after that step with `hn: interrupted for testing after <step>`.

**Files**: `serve_client.go`.

### Subtask T065: Advancement black-box tests and interruption table

**Steps** (tests `TestServeAdvance...` in `serve_advance_test.go`, built on `newServeGovernedFixture`):

1. **US3-1**: proposal ready and accepted under both policies; M runs `hn merge <id> --remote origin`: server `main` equals the head; the number of commits reachable from server `main` equals that reachable from the head (no new commit); M's clone holds a `proposal.merged` event with `commit == head`; output matches the success lines.
2. **US3-2 / NFR-006**: a fresh clone with no Hubnot identity: `git clone`, `hn replication select origin --all`, `hn sync origin`, `hn proposal status <full id>`: status `merged`, and the same approval, CI result, acceptance and merge event IDs as M's clone.
3. **US3-3**: `main` advanced by another proposal after this one was opened: the command fails with the `protected-not-fast-forward` line naming the current tip and directing `hn proposal revise`; `main` unchanged.
4. **US3-4 through the command**: destination policy needs two approvals: the command prints `protected-destination-policy` and exits 1.
5. **advance-not-maintainer**: an author who is not a maintainer is stopped at step 1 locally; to prove the server check, push with `hn-advance` directly from a non-maintainer key and assert `advance-not-maintainer`.
6. **Ordinary remote**: against an ordinary bare remote, the command refuses with the step 2 message and writes nothing.
7. **Interruption table** (contract): for each of `publish`, `push`, `record`, `republish`, interrupt, then rerun the same command without the variable. Assert the server branch, the local merge fact count (exactly one `proposal.merged` for the candidate in M's clone after the rerun) and the published state, row by row as in the table. Include the "interrupted after the server applied the update" row: interrupt after `push`; the rerun takes the repair path and prints the repair lines.
8. **Second maintainer**: after M advanced, a second maintainer M2 runs the command: takes the step 5 path; if M's fact has not reached M2's clone, M2 records its own fact; `hn proposal status` in a verifier shows `merged` and no `merge conflict`.
9. **US3-6**: M and M2 run the command for two different ready proposals concurrently: exactly one advances `main`; the other exits 1 with `protected-stale-tip` (or `protected-not-fast-forward` if its push read the new tip; both leave `main` at the winner's head, and the test accepts either code while asserting exactly one success).
10. **FR-016**: `hn merge <id> --branch main` without `--remote` is a usage error; `hn merge <id>` in a checkout whose `origin` is an `hn serve` remote performs the existing local `--no-ff` merge and does not contact the server (assert the server's push log has no new entry).

**Files**: `serve_advance_test.go`.

### Subtask T066: NFR-005 race test

**Steps**:

1. Build ten ready, accepted, CI-tested proposals on the same base `T` once (reuse the fixture; one maintainer key is enough).
2. Repeat 50 times: reset server `main` to `T` with `hn serve repo lock-exec NAME -- git update-ref refs/heads/main T <current>` (administrator precondition); then start ten `git push --porcelain -o hn-advance=Pi -o hn-expected-tip=T origin +Hi:refs/heads/main` processes concurrently from one maintainer clone and wait for all.
3. Assert per repetition: exactly one push exited 0; server `main` equals that push's head; the other nine exited non-zero with `protected-stale-tip` lines; the push log gained exactly ten entries (one accepted, nine rejected); no other ref changed.
4. Keep the test under a few minutes on the gate host: the stale-tip check runs before any policy or event loading (WP09 step 3 and WP07's lazy loading). Log total time with `t.Logf`.

**Files**: `serve_advance_test.go`.

### Subtask T067: Serve guide advancement section with tested example

**Steps**:

1. "Advancing a protected branch" in `docs/serve-v0.md`: who may advance, what the command does step by step in plain words, why facts are published in their own push (so they survive a refused advancement), the forced refspec and why the server's checks replace Git's client-side fast-forward refusal, what the merge fact records (`commit == head`, link to `docs/governance-v0.md#fast-forward-merge-facts`), the interruption table and the rerun rule, the second-maintainer case, the race outcome (`protected-stale-tip`), and that `hn merge PROPOSAL` without `--remote` is unchanged.
2. Include the marked example `<!-- example: advance -->` from the quickstart (command plus output block) and add `TestServeAdvanceDocumentedExample` that runs it on the fixture after ID substitution and compares output line by line.
3. Include the marked example `<!-- example: reject-stale-advance -->` from `contracts/push-verdict.md` and compare it with the losing output of the US3-6 test.
4. Writing style: plain direct sentences, descriptive headings, no em dashes.

**Files**: `docs/serve-v0.md`, `serve_advance_test.go`.

## Test Strategy

Black-box tests with real maintainers, reviewers and runners through the shim. Run `go test -race -run TestServeAdvance ./...`, then the full gate.

## Definition of Done

- [ ] T061 to T067 complete; full gate passes.
- [ ] **FR-014**: advancement publishes facts first, then requests the fast-forward with `hn-advance` and `hn-expected-tip`; refusals are printed as structured lines and the branch does not move.
- [ ] **FR-015**: the merge fact has `commit == head`; every interruption row ends with exactly one merge fact after rerun.
- [ ] **FR-016**: the existing merge path is untouched (diff shows only the early dispatch in `cmdMerge`) and existing tests pass unchanged.
- [ ] **NFR-005**: 50 repetitions of 10 concurrent pushes, exactly one advance each, no partial updates.
- [ ] **NFR-006**: a fresh identity-less clone reproduces review, CI, acceptance and merge evidence.
- [ ] **C-005**: no commit is created on the server or client by the advancement path.
- [ ] Guide section written; both examples executed.

## Risks

- `publishLocalFacts` may hide Git's stderr; step 3 failures must still show the server's reason. Decide early (T062) and test it.
- Concurrent tests can be flaky on slow hosts; assert outcomes, not timings, except for the logged total.

## Reviewer Guidance

- Diff `governance.go` and confirm the no-flag path is byte-identical below the dispatch.
- Run the interruption rows by hand once with the environment variable.
- Confirm the client never calls `findProposalMergeCommit` in remote mode.

## Activity Log

- 2026-10-25T00:09:18Z – system – Prompt generated via /spec-kitty.tasks
