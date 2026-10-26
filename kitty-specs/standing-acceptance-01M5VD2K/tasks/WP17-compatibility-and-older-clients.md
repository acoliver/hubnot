---
work_package_id: WP17
title: Opt-out repositories and older clients
dependencies:
- WP03
- WP11
requirement_refs:
- C-003
- FR-022
- NFR-004
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T081
- T082
- T083
- T084
phase: Phase 6 - Evidence, compatibility and quality
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: standing_compat_test.go
create_intent: []
execution_mode: code_change
owned_files:
- standing_compat_test.go
role: implementer
tags: []
tracker_refs: []
---

# WP17: Opt-out repositories and older clients

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Prove User Story 7 with executable tests (FR-022, NFR-004, C-003; decision `01M5VF0QVPQ871M0DA8Q0SG5CX`):

1. A repository that never adds `acceptance` or `reviewers` behaves exactly as before: same eligibility, same refusals, same recorded facts (US7-1, NFR-004).
2. An `hn` built from commit `7a47e6952a427ad7e3a3766c221fd334d9331950` (the mission's Go base) rejects any policy that contains `acceptance` or `reviewers`, including `"acceptance": "maintainers"` (US7-2).
3. That older client rejects a chain containing a rule merge fact, because the signer is not a maintainer or the evidence lacks acceptance decisions, so it never mistakes standing acceptance for maintainer acceptance (US7-3).

## Context

- Read spec US7, FR-022, NFR-004; [research.md](../research.md) findings 1 and 2 and decision `01M5VF0QVPQ871M0DA8Q0SG5CX`.
- Start with `spec-kitty agent action implement WP17 --agent <name> --mission standing-acceptance-01M5VD2K`.
- From WP03: the existing `standing_compat_test.go` (parse and encoding tests); keep them. From WP06: the harness, which accepts a binary path per run if you add a variant; if it does not, run the older binary directly with `exec.Command` in the clone directories the harness created, using `operationalTestEnvironment`.
- Older code facts: `parsePolicyBytes` uses `DisallowUnknownFields` (`policy.go` line 110); `verifyEvent` ignores unknown keys for non-resource events; `validateMergeEvent` needs `requiredAccepts` maintainer acceptance decisions and `validatePolicy` forces `requiredAccepts >= 1`.

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `standing_compat_test.go` | CHANGE. Add `TestStandingCompatOptOut...` and `TestStandingCompatOlderClient...`. Do not change WP03's tests. |

## Subtasks

### Subtask T081: Opt-out repositories yield identical outcomes (US7-1, NFR-004)

**Steps**:

1. Scenario script, run twice on identical fresh repositories, once with the new binary and once with the older binary (T082), both with a policy that has an automerge rule without `acceptance` or `reviewers` and actors `[M]`:
   - preview of a ready, accepted candidate; preview of a candidate missing acceptance; preview of a candidate changing a path outside the rule;
   - `hn automerge --execute` of the ready candidate by M;
   - `hn decide --accept` and `hn merge` of a second candidate;
   - `hn proposal status` of both; `hn policy show`; `hn policy check --base main --file .hn/policy.json` for an unchanged draft.
2. Compare outputs line by line after normalizing event IDs, commit IDs, timestamps and short IDs to placeholders. The only allowed differences are the additive lines this mission introduced: `Acceptance: ...`, `Counted reviewers...`, `Counted approvals: ...` in the preview; `  Acceptance: ...` and `  Counted reviewers: ...` in `hn policy show`; `automerge rule NAME acceptance: ...` and `automerge rule NAME reviewers added/removed: ...` in `hn policy check`. Keep that allow-list in one table in the test and assert nothing else differs, including exit codes.
3. Compare the recorded facts: same kinds in the same order on every chain, no `rule` key on any event, and the same evidence counts.
4. Record in the Activity Log that the full pre-existing suite passed with no assertion edits (run `git diff <mission base> -- '*_test.go'` restricted to pre-existing files and confirm it is empty).

### Subtask T082: Older-client build from commit `7a47e69`

**Steps**:

1. `standingCompatOlderBinary(t) string`:
   - skip with `t.Skip("older-client check needs the full suite; run without -short")` under `testing.Short()`;
   - in the source repository, `git cat-file -e 7a47e6952a427ad7e3a3766c221fd334d9331950^{commit}`; skip with a reason naming the commit when it is absent (for example in a shallow CI checkout);
   - `git archive --format=tar 7a47e69... | tar -x -C <t.TempDir()>` (use Go's `archive/tar` to extract the archive output instead of a shell pipe);
   - `go build -o <dir>/hn-older .` with `GOFLAGS=` and `GOTOOLCHAIN=local` in the environment, so the build needs no network; skip with a reason when `go` is not on `PATH`.
2. Cache the binary with `sync.Once` per test process; the build takes seconds.
3. Assert the binary runs (`hn-older --help` or the usage error exits as expected).

### Subtask T083: Older client rejects the new policy fields (US7-2)

**Steps**:

1. For each of `"acceptance": "rule"`, `"acceptance": "maintainers"`, and `"reviewers": [R]` (each alone), commit the policy in a fresh repository and run `hn-older policy show` and `hn-older proposal status <candidate>`: non-zero exit and a message containing `unknown field` and the field name.
2. The new binary accepts the same policies (`"acceptance": "maintainers"` included).

### Subtask T084: Older client rejects rule merge facts (US7-3)

**Steps**:

1. **Realistic case**: after the US1 flow with the new binary (E executes `ordinary`, publishes), an older-client verifier clone runs `hn-older sync origin`: E's rule merge fact is not promoted and `hn-older proposal status <candidate>` does not report `merged`. The refusal may come from the base policy containing the new fields; assert refusal, not its wording.
2. **Isolated case**: the base policy uses only fields the older client knows (a maintainers-mode rule), and the fact carries `rule` naming that rule and lists approvals and run results (sign it with the WP06 builder, once signed by a non-maintainer and once by a maintainer). The older client refuses with `merge ... is not authorized by exact base policy` (non-maintainer) and `merge ... lacks required acceptance evidence` (maintainer). The new client refuses both with `rule-signer` or `rule-acceptance`.
3. Confirm the older client did not fail at parse time for the isolated case (its `verifyEvent` ignores the unknown key), so the evidence check is what refuses it.

## Test Strategy

Black-box tests with two binaries. Run `go test -race -run 'TestStandingCompat' ./...` (without `-short`), then the full gate.

## Definition of Done

- [ ] T081 to T084 complete; full gate passes.
- [ ] **US7-1 / NFR-004**: opt-out outputs equal the older binary's outputs apart from the allow-listed additive lines; facts identical in kind and shape.
- [ ] **US7-2**: the older client rejects each new field, including `"acceptance": "maintainers"`.
- [ ] **US7-3**: the older client never admits a rule merge fact.
- [ ] **C-003**: no pre-existing test file changed in the mission diff.
- [ ] Skips are visible and name their reason.

## Risks

- The older build fails if the toolchain refuses the older `go.mod`; `go.mod` at that commit declares `go 1.26`, the same as today. Record the toolchain version used.

## Reviewer Guidance

- Read the allow-list of additive lines; nothing else may differ.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
