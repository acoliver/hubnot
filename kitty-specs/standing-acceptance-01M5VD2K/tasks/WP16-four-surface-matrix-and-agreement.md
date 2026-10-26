---
work_package_id: WP16
title: Four-surface fixture matrix and verdict agreement
dependencies:
- WP09
- WP15
requirement_refs:
- FR-012
- FR-015
- NFR-001
- NFR-002
- NFR-005
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T076
- T077
- T078
- T079
- T080
phase: Phase 6 - Evidence, compatibility and quality
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: standing_fact_test.go
create_intent:
- standing_fact_test.go
execution_mode: code_change
owned_files:
- standing_fact_test.go
role: implementer
tags: []
tracker_refs: []
---

# WP16: Four-surface fixture matrix and verdict agreement

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Produce the security evidence the spec measures across all four fact-admission surfaces of FR-012: local projection, replication import, shallow verification and the `hn serve` actor-chain check.

- **NFR-001**: every defect fixture F01 to F22 (at least 14 signed facts, each with exactly one defect) is refused by each surface 100% of the time, with zero promoted facts and zero changed hosted refs.
- **US3-2**: a mapped actor pushing the signer's chain to an `hn serve` hosted repository is rejected with `actor-chain-invalid` naming the fact, and every hosted ref is unchanged.
- **NFR-002**: all four surfaces reach the same accept or reject outcome for every valid and invalid fixture.
- **NFR-005**: a candidate whose changed-path output exceeds the existing 8 MiB bound is refused on every surface and in execution, never truncated.
- **Property**: every fact the real client records in the valid scenarios is admitted by all four surfaces (plan risk table, first row).

This WP adds tests only. If a surface disagrees, fix the cause in the owning production file with a recorded one-line rationale (the owners are listed in `tasks.md`), and add the regression to this matrix.

## Context

- Read [contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Fixture matrix (NFR-001)" and "Agreement (NFR-002)"; spec US3 (all), NFR-001, NFR-002 and NFR-005, SC-003 and SC-004.
- Start with `spec-kitty agent action implement WP16 --agent <name> --mission standing-acceptance-01M5VD2K`.
- From WP06: `standingHarnessDefects`, `SignFact`, `SignRawFact`, `Verifier`. From WP07 to WP09: the local, import and shallow helpers in `standing_fact_test.go`. From WP14 and WP15: `newStandingServeFixture`, `RulePush`, the client advancement helpers in `standing_serve_test.go`. From existing serve tests: how `serve_facts_test.go` pushes an actor ref with the actor's key and reads `actor-chain-invalid` rejections (`f.reject`, `requireServeIDs`).

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `standing_fact_test.go` | CHANGE. `TestStandingFactMatrix...`, the four-surface runner and agreement report. |

## Subtasks

### Subtask T076: Defect matrix runner across the four surfaces

**Steps**:

1. Define a surface interface in the test file:

   ```go
   type standingFactSurface interface {
       Name() string
       // Judge publishes or places the signer's chain and returns accept or
       // reject, the observed refusal text, and an error for harness failures.
       Judge(t *testing.T, defect standingHarnessDefect) (accepted bool, detail string)
   }
   ```

   with four implementations: `local` (fact in the signer's own clone; run `hn proposal status`), `import` (raw-push the actor ref to the bare remote; a fresh verifier runs `hn sync origin`), `shallow` (a shallow verifier selecting the signer's chain runs `hn sync` and `hn proposal status`), `serve` (the signer pushes its actor ref to the hosted repository with its own key).
2. For each defect F01 to F22 and each surface: build the defect fresh, judge it, assert refusal, assert the refusal text names the full fact ID and the expected check (or the parse message, or the missing-dependency message where the contract says so), and assert zero promoted facts (import, shallow) or unchanged refs (serve: `ServerRefs` before and after; local: `Refs`).
3. Shallow surface nuance: a shallow clone may refuse earlier with a shallow dependency gap (WP09). That counts as refusal; record the observed reason per defect in the report.
4. Print a matrix with `t.Logf`: rows F01 to F22, columns the four surfaces, cells the observed outcome and check. Keep the report stable (sorted) so reviewers can diff runs.
5. Budget: the full matrix may take minutes. Share one serve harness and one bare-remote harness per test, build defects from one valid base scenario where possible, and skip the matrix under `-short` with a visible reason. The full gate runs without `-short`.

### Subtask T077: `hn serve` actor-chain rejections with unchanged hosted refs (US3-2)

**Steps**:

1. For every defect, the signer pushes `refs/hn/actors/<signer>` to the hosted repository with its own key. Expect one `hn-serve/1 reject` line with `code=actor-chain-invalid` (parse defects F19 and F20 also give `actor-chain-invalid`; F21 and F22 give `dependency-missing` naming the absent ID or commit, as the contract's hn serve row states).
2. Assert the rejection's IDs name the fact's full ID (use `extractServeRejections`), and every hosted ref is byte-identical before and after.
3. A valid rule merge fact pushed the same way is accepted.

### Subtask T078: Verdict agreement over the valid and invalid sets (NFR-002)

**Steps**:

1. Valid set: at least the US1 fact (local merge commit), the US4 fact (fast-forward, `commit` = head), a fact under rule reviewers (US5-1 after gpt approves), a fact whose result answers a listed requester's run (D1), a fact signed by a maintainer listed as executing actor (FR-011), and a fact with two pipelines.
2. For every valid and invalid fixture, collect the four verdicts and require them to be equal (all accept or all reject). Fail with the full matrix row when they differ.
3. Log the agreement rate; it must be 100%.

### Subtask T079: Changed-path bound fixture (NFR-005)

**Steps**:

1. Build a candidate whose `git diff-tree --name-only -z` output exceeds 8 MiB: one blob reused for tens of thousands of long paths under `src/` (for example 40,000 entries with 200-byte names), assembled with `git mktree` or `git fast-import` so the fixture builds in seconds.
2. Preview and `--execute` refuse with the existing bound error (`policy Git output exceeds bound` or `inspect changed paths: ...`); no merge, no fact.
3. A hand-signed rule merge fact for that candidate is refused by every surface with the bound error; none reports `changed-paths` with a truncated list.
4. Log the fixture's byte size.

### Subtask T080: Execution-to-admission property over valid fixtures

**Steps**:

1. Run each valid scenario of T078 through the real client (`hn automerge --execute`, locally or with `--remote`), not through the fact builder.
2. Publish the recorded fact and judge it on all four surfaces: all accept.
3. This proves that the pre-sign self-check (WP11) and the four verifiers agree on facts the client actually writes.

## Test Strategy

Long black-box matrix through real remotes, shallow clones and the forced-command harness. Run `go test -race -run 'TestStandingFactMatrix' ./...` (without `-short`), then the full gate.

## Definition of Done

- [ ] T076 to T080 complete; full gate passes.
- [ ] **NFR-001**: F01 to F22 refused on all four surfaces, zero promoted facts, zero changed hosted refs.
- [ ] **US3-2**: `actor-chain-invalid` (or `dependency-missing` for F21 and F22) naming the fact; hosted refs unchanged.
- [ ] **NFR-002**: 100% agreement across surfaces for valid and invalid sets.
- [ ] **NFR-005**: the 8 MiB bound refuses everywhere.
- [ ] **FR-015**: no surface reads events outside the evidence (covered by the immutability defect cases).
- [ ] Matrix report logged.

## Risks

- Run time. If the matrix exceeds the gate host's patience, split it into one test per surface; do not drop cases.

## Reviewer Guidance

- Read the logged matrix and check every cell is a refusal with the expected reason.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
