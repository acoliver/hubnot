---
work_package_id: WP17
title: Opt-out repositories and older clients
dependencies:
- WP03
- WP11
requirement_refs:
- C-003
- C-004
- FR-022
- FR-024
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
create_intent:
- standing_compat_test.go
execution_mode: code_change
owned_files:
- standing_compat_test.go
- docs/threat-model.md
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
2. An `hn` built from commit `7a47e6952a427ad7e3a3766c221fd334d9331950` (the older-client reference) rejects any policy that contains `acceptance` or `reviewers`, including `"acceptance": "maintainers"` (US7-2).
3. That older client rejects every fact valid only through path (b), because the signer is not a maintainer or the evidence lacks acceptance decisions, so it never mistakes standing acceptance for maintainer acceptance (US7-3). It fails closed.
4. Older and new clients agree on every fact that passes path (a), including a maintainer-signed fact with enough acceptance decisions that also carries a rule claim, where both accept (US7-4, analysis finding I1), and on every legacy event (US7-5, C-004).

Decisions: `01M5VSEHEVA82N9T64ZH1P1Q2A` (union validity), `01M5VSEHFPWQWFBNTP9ZFYP3TR` (one owning parent test for the older binary, analysis finding U1) and `01M5VSEHF9P9VRVTY68PD8DZ7D` (the pre-existing test audit snapshot is `d881d9b`, analysis finding I2; `7a47e69` stays the older-client reference).

## Context

- Read spec US7, FR-022, NFR-004; [research.md](../research.md) findings 1 and 2 and decision `01M5VF0QVPQ871M0DA8Q0SG5CX`.
- Start with `spec-kitty agent action implement WP17 --agent <name> --mission standing-acceptance-01M5VD2K`.
- WP03's claim and encoding tests live in `standing_event_test.go`; this WP creates `standing_compat_test.go`. From WP06: the legacy fixtures (`standingHarnessLegacyFixtures`) and boundary fixtures (`standingHarnessBoundaryFixtures`), and the harness, which accepts a binary path per run if you add a variant; if it does not, run the older binary directly with `exec.Command` in the clone directories the harness created, using `operationalTestEnvironment`.
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
| `standing_compat_test.go` | NEW. `TestStandingCompatOlderClient` (the single owner of the older binary, with every consumer as a subtest) and `TestStandingCompatPreExistingTestsFrozen`, helpers prefixed `standingCompat`. |
| `docs/threat-model.md` | CHANGE. "Compatibility and recovery" section only. WP18 edits two other sections later. |

## Subtasks

### Subtask T081: Opt-out repositories yield identical outcomes (US7-1, NFR-004)

**Steps**:

1. Subtest `OptOut` of `TestStandingCompatOlderClient` (T082). Scenario script, run twice on identical fresh repositories, once with the new binary and once with the older binary, both with a policy that has an automerge rule without `acceptance` or `reviewers` and actors `[M]`:
   - preview of a ready, accepted candidate; preview of a candidate missing acceptance; preview of a candidate changing a path outside the rule;
   - `hn automerge --execute` of the ready candidate by M;
   - `hn decide --accept` and `hn merge` of a second candidate;
   - `hn proposal status` of both; `hn policy show`; `hn policy check --base main --file .hn/policy.json` for an unchanged draft.
2. Compare outputs line by line after normalizing event IDs, commit IDs, timestamps and short IDs to placeholders. The only allowed differences are the additive lines this mission introduced: `Acceptance: ...`, `Counted reviewers...`, `Counted approvals: ...` in the preview; `  Acceptance: ...` and `  Counted reviewers: ...` in `hn policy show`; `automerge rule NAME acceptance: ...` and `automerge rule NAME reviewers added/removed: ...` in `hn policy check`. Keep that allow-list in one table in the test and assert nothing else differs, including exit codes.
3. Compare the recorded facts: same kinds in the same order on every chain, no `rule` key on any event, and the same evidence counts.
4. `TestStandingCompatPreExistingTestsFrozen` (C-003, NFR-004; decision `01M5VSEHF9P9VRVTY68PD8DZ7D`): when commit `d881d9bcd0ade4fa1bce91061954a8582e8d9318` is present, run `git diff --name-status d881d9b HEAD -- '*_test.go'` in the source repository and require every line to start with `A` (added files only), so no test file that exists at the snapshot is modified, deleted or renamed. Skip with a visible reason naming the commit when it is absent. Do not compare against `7a47e69`: the reviewed sync-speedup merge changed `replication_borrow_test.go` between the two commits.

### Subtask T082: Older-client build owned by one parent test

**Steps** (decision `01M5VSEHFPWQWFBNTP9ZFYP3TR`, analysis finding U1):

1. `TestStandingCompatOlderClient(t *testing.T)` is the only test that builds or uses the older binary. It builds it once into its own `t.TempDir()`, so the binary lives until the parent and every subtest finish, and then runs each consumer as a subtest: `OptOut` (T081), `PolicyFields` (T083), `RuleOnlyFacts` and `MaintainerMergeAgreement` (T084), `LegacyHistory` (T084). No other top-level test reads the path. No `sync.Once`, package variable or process-wide cache holds it.
2. `standingCompatBuildOlder(t) string`, called only from the parent:
   - skip the parent with `t.Skip("older-client check needs the full suite; run without -short")` under `testing.Short()`;
   - in the source repository, `git cat-file -e 7a47e6952a427ad7e3a3766c221fd334d9331950^{commit}`; skip with a reason naming the commit when it is absent (for example in a shallow CI checkout);
   - `git archive --format=tar 7a47e69...` into the parent's `t.TempDir()` (use Go's `archive/tar` to extract the archive output instead of a shell pipe);
   - `go build -o <dir>/hn-older .` with `GOFLAGS=` and `GOTOOLCHAIN=local` in the environment, so the build needs no network; skip with a reason when `go` is not on `PATH`.
3. Assert the binary runs (`hn-older --help` or the usage error exits as expected) before the subtests start.
4. Subtests must not call `t.Parallel()` unless the parent waits for them before returning; keep them sequential, which also keeps fact signing with `t.Chdir` safe.
5. Run `go test -race -run 'TestStandingCompatOlderClient' -count=1 ./...` and confirm every subtest ran against the same binary (log its path and size once in the parent).

### Subtask T083: Older client rejects the new policy fields (US7-2)

**Steps** (subtest `PolicyFields` of `TestStandingCompatOlderClient`):

1. For each of `"acceptance": "rule"`, `"acceptance": "maintainers"`, and `"reviewers": [R]` (each alone), commit the policy in a fresh repository and run `hn-older policy show` and `hn-older proposal status <candidate>`: non-zero exit and a message containing `unknown field` and the field name.
2. The new binary accepts the same policies (`"acceptance": "maintainers"` included).

### Subtask T084: Rule-only facts, maintainer-merge agreement and legacy history across clients

**Steps** (subtests of `TestStandingCompatOlderClient`):

1. **`RuleOnlyFacts`, realistic case**: after the US1 flow with the new binary (E executes `ordinary`, publishes), an older-client verifier clone runs `hn-older sync origin`: E's rule merge fact is not promoted and `hn-older proposal status <candidate>` does not report `merged`. The refusal may come from the base policy containing the new fields; assert refusal, not its wording.
2. **`RuleOnlyFacts`, isolated case**: the base policy uses only fields the older client knows (a maintainers-mode rule), and the fact carries `rule` naming that rule and lists approvals and run results (sign it with the WP06 builder, once signed by a non-maintainer and once by a maintainer). The older client refuses with `merge ... is not authorized by exact base policy` (non-maintainer) and `merge ... lacks required acceptance evidence` (maintainer). The new client refuses both with `rule-signer` or `rule-acceptance`.
3. Confirm the older client did not fail at parse time for the isolated case (its `verifyEvent` ignores the unknown key), so the evidence check is what refuses it.
4. **`MaintainerMergeAgreement`** (US7-4, analysis counterexample I1): under a base policy with only fields the older client knows, M signs a `proposal.merged` that cites enough M accept decisions and carries `rule` naming (a) an absent rule and (b) the existing maintainers-mode rule; and a third variant whose evidence holds the decisions plus the approval. Each older and new verifier imports the chain with `sync`: both accept every variant and report the candidate merged. The new client's status marks the annotation as not verified; the older client prints no rule line. Record both outputs in the test log.
5. **`LegacyHistory`** (US7-5, C-004): the legacy fixtures L01 to L06 of WP06 under the same policy. Older and new verifiers reach the same verdict on each: L01 to L05 accepted, L06 refused as before.
6. Write the observed old and new outcomes of steps 1, 2, 4 and 5 as one table in the test log.
7. Document the observed behavior in `docs/threat-model.md`, "Compatibility and recovery": older executables reject a policy that contains `acceptance` or `reviewers` (including `"acceptance": "maintainers"`) and reject every fact valid only through the rule path, so a mixed team cannot mistake standing acceptance for maintainer acceptance; older and newer clients agree on every maintainer merge, including one that carries a rule name, and on all history signed before the mission; rolling back a binary does not remove published rule merge facts, which a newer client keeps judging against their own base policy. Plain sentences, no em dashes.

## Test Strategy

Black-box tests with two binaries. Run `go test -race -run 'TestStandingCompat' ./...` (without `-short`), then the full gate.

## Definition of Done

- [ ] T081 to T084 complete; full gate passes.
- [ ] **US7-1 / NFR-004**: opt-out outputs equal the older binary's outputs apart from the allow-listed additive lines; facts identical in kind and shape.
- [ ] **US7-2**: the older client rejects each new field, including `"acceptance": "maintainers"`.
- [ ] **US7-3**: the older client never admits a fact valid only through path (b).
- [ ] **US7-4 / US7-5 / C-004**: older and new clients agree on maintainer merges with a rule claim (both accept) and on every legacy fixture.
- [ ] **U1**: the older binary is built and used only inside `TestStandingCompatOlderClient`; every consumer is a subtest; no process-wide cache; a full run with all consumers passes.
- [ ] **C-003 / NFR-004**: `git diff --name-status d881d9b HEAD -- '*_test.go'` lists only added files.
- [ ] Skips are visible and name their reason.
- [ ] "Compatibility and recovery" in `docs/threat-model.md` describes older-client behavior for the new fields and facts.

## Risks

- The older build fails if the toolchain refuses the older `go.mod`; `go.mod` at that commit declares `go 1.26`, the same as today. Record the toolchain version used.

## Reviewer Guidance

- Read the allow-list of additive lines; nothing else may differ.
- Confirm no test outside `TestStandingCompatOlderClient` touches the older binary.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
- 2026-10-26T09:45:00Z – llxprt – Prompt revised after analysis (I1, I2, U1; decisions 01M5VSEHEVA82N9T64ZH1P1Q2A, 01M5VSEHF9P9VRVTY68PD8DZ7D and 01M5VSEHFPWQWFBNTP9ZFYP3TR): one owning parent test for the older binary, rule-plus-decisions agreement and legacy subtests, d881d9b audit
