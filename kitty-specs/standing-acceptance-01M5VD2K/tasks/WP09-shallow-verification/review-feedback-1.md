# WP09 review: changes required

Implementation reviewed: `3ac781ee014bd25026ef3583a8a5fd8b2f2435a4`.
Gate workspace HEAD at review start: `725d86d` in lane-b.
Reviewer: llxprt, independent review, invocation `739ae5b28edd4dadb1c8c19cf958f2f1`.

## Blocking issue 1: recovery falsely succeeds when the head exists but the proposal code ref is absent

**Locations:** `shallow.go:1417-1421`, `shallow.go:1454-1458`, and the new call from `policy.go:572`. The interaction is with `recoverSelectedShallow` at `shallow.go:2018-2028` and `verifyRecordedShallowGap` at `shallow.go:2189-2196`.

**Requirements:** FR-014 and T040/T041 require recoverable shallow object dependencies and usable recovery guidance.

Reproduced through the real CLI with a valid rule merge fact and complete evidence, base, head, policy and pipeline objects. Only the candidate's accepted proposal code ref was omitted. An exact `--proposal <candidate>` selection was saved before verification.

1. `hn log` exits 1 with `operation=rule merge verification; kind=proposal code ref`, naming the candidate and `refs/hn/proposals/<candidate>`, and recommending `hn sync origin --recover-shallow`.
2. That recovery command exits 0, prints nothing and does not restore the missing ref.
3. `hn log` still exits 1 with the identical gap.

`cmdLog` calls `collectEvents` after `prepareShallowVerification` has restored the previous scope. The new rule guard therefore records this gap without a replay scope. Recovery falls back to `verifyRecordedShallowGap`, which only probes `MissingID`, the already-present signed head commit. It treats that probe as success and clears the durable gap without checking or fetching the required proposal ref.

**Required fix:** preserve a replayable, candidate-aware verification scope for this new rule-path gap, or make recovery recheck the required ref and its equality to the signed head before declaring the gap resolved. Recovery must import the selected supplier when the ref is absent and must reverify the complete original rule-fact operation before clearing the gap. Keep maintainer-path behavior unchanged.

Add a black-box regression in `standing_fact_test.go` that constructs `standingFactShallowArrangement{SkipProposals: []string{candidate.ID}}`, saves the exact proposal selection, invokes `hn log`, invokes the recommended recovery command, and requires a subsequent `hn log` to succeed with the fact present. Checking only the diagnostic string does not cover recovery.

Independent reproduction: `/home/acoliver/projects/appaday/gitium/.scratch/wp/standing-acceptance-01M5VD2K/WP09/review-recovery.log`. Scratch-only overlay test `TestWP09ReviewLogCodeRefRecovery` failed in 9.74 seconds with recovery exit 0 followed by log exit 1. No tracked test or production file was changed to run it.

## Blocking issue 2: maintained protocol documentation still describes the removed activation boundary

**Location:** `docs/protocol-v0.md:310-314`.

The current documentation says every shallow clone refuses every rule-only merge fact and quotes `cannot be verified in a shallow clone by this build; verify it in a full clone`. WP09 removes that runtime refusal and enables shallow path (b), but its commit changes no documentation. This contradicts the delivered CLI behavior and the mission's C-006/HN-005 requirement that maintained documentation move with behavior.

**Required fix:** replace this interim paragraph with the enabled shallow behavior: complete closure allows rule derivation; listed evidence and answered requests must be selected; base/head/code ref, policy blob and pipeline definitions are guarded with named dependency gaps and recovery guidance; missing objects never become an empty change list; maintainer merges retain their previous behavior. Record the documentation ownership coordination described below before editing this shared file.

## Requirement and acceptance audit

| Requirement or task | Result |
|---|---|
| T040 evidence classification | PASS. Candidate remains first; claimed facts use selected event facts; the no-claim decision loop is byte-identical. |
| T040 union ordering | PASS. The new object guard is reached from the rule closure of `admitMergeFact`, after the unchanged maintainer check fails. |
| T040 object availability | PASS for missing-object detection and classification. Base, signed head/ref equality, policy blob and pipeline blob guards are present. Recovery is blocked by issue 1. |
| T041 / FR-012 / NFR-001 semantic coverage | Tests exercise real CLI projection for F01-F20 with accepted matched controls, complete closure and named derivation checks. F19/F20 retain their maintainer-path refusal. |
| T041 / FR-013 | Tests cover missing review/result as selected facts and a missing answered request as run request; F21 is refused before publication and accepted after publication. |
| T041 / C-004 | Maintainer check, no-claim evidence loop and `shallowAcceptedFacts` are byte-identical. Tests cover L01-L05, the L06 strict-resource negative control, B2/B3 and maintainer acceptance without the proposal code ref. |
| T042 / FR-014 | Missing-head tests cover local, shallow and import paths without evaluating an empty diff. Recovery remains FAIL under issue 1. Git 2.53.0 independently returns nonzero for a missing diff revision. |
| FR-024 | PASS. One shared union combinator retains path (a) first and unchanged. |
| FR-025 | PASS. The interim production refusal is removed and only the shallow entry is enabled in the boundary table. |
| Documentation with behavior | FAIL under issue 2. |
| Frozen assertions | PASS. No test file present at `d881d9b` is modified by WP09. The only removed test line flips the explicitly owned shallow activation entry. |

## Required anti-pattern verdicts

| Check | Verdict | Evidence |
|---|---|---|
| Dead code | PASS | Live CLI projection reaches `collectEvents` -> `validateEventRelationships` -> `validateMergeEventWith` -> `localRuleMergeCheck` -> both new shallow helpers. |
| Synthetic-fixture test | PASS | New tests build signed facts in real repositories and invoke the built `hn` binary. |
| Silent empty return | PASS | The guard's nil return in a complete repository is intentional and documented; actual Git inspection failures propagate. |
| FR coverage | FAIL | FR-014 recovery assertions only check guidance text. The independently exercised recovery behavior fails; add the regression in issue 1. |
| Frozen surface | PASS | WP09 modifies only `shallow.go`, the rule-path portion of `policy.go`, and `standing_fact_test.go`; no frozen signed history, existing baseline test, key or binary changes. |
| Locked decision | PASS | No rule guard runs before path (a), no missing object is converted to an empty diff, and no signed payload is re-encoded. |
| Shared-file ownership | PASS | WP09 stays within its assigned portions. Explicit coordination and the documentation handoff are recorded below. |
| Production fragility | PASS | New failures are fail-closed dependency or invalid-binding errors; no transient process panic or unhandled exception was added. |

## Ownership coordination and dependent-agent notification

`tasks.md` orders `policy.go` as WP07 -> WP09 -> WP11 and `standing_fact_test.go` as WP07 -> WP08 -> WP09 -> WP16. WP09 modifies only its assigned rule-path guard, its shallow activation entry and its added test section. `shallow.go` belongs to WP09.

The required protocol paragraph correction is a narrow WP09 behavior-sync handoff in the shared `docs/protocol-v0.md` surface, otherwise owned by WP07/WP13/WP18. This review records that coordination need explicitly: keep those owners' rule-fact, push-option, reason-table and example work intact and record the correction in WP09's implementation note.

WP11 and WP16 agents must refresh/rebase their dependent work after the corrected WP09 commits land and rerun their relevant gates. They share lane-b, so already-current shared-lane work must not be reset or discarded. Only WP09's status is changed by this review.

## Quality gate evidence

- `gofmt -l` prints nothing for all changed Go files in the authoritative lane diff.
- `go vet ./...` passes.
- `go build ./...` passes.
- `git show --format= --check 3ac781e` passes.
- `go test -race -count=1 -timeout 60m -skip '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery$' ./...` passes: `hubnot 5037.023s`, `internal/forge 1.026s`, `internal/githubapi 16.261s`, exit 0. The host's existing Go wrapper reported that it extended the effective timeout to 150 minutes; the requested command was invoked exactly.
- `go test -count=1 -run '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery$' .` passes: `hubnot 45.756s`, exit 0. It waited for the host's existing heavy-test locks, then ran without race instrumentation. No base comparison was needed.
- The independent added scratch-only regression fails as described in issue 1, despite the existing full suite passing.
- WP09's three reviewed files remain unchanged from gate-start HEAD `725d86d`. Other agents' coordination commits in the shared lane were preserved and were outside this review.
- Owned background gate processes exited. No temporary worktree was created. The disposable overlay JSON was removed; the small reproducer source and failure log are retained as useful review evidence.

Gate logs are scoped to `/home/acoliver/projects/appaday/gitium/.scratch/wp/standing-acceptance-01M5VD2K/WP09/` with prefix `review-739ae5b2-`. Final verdict: request changes for the two blocking issues above.
