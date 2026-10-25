---
affected_files: []
cycle_number: 1
mission_slug: hn-serve-01M5P0NP
reproduction_command:
reviewed_at: '2026-10-25T07:29:43Z'
reviewer_agent: user
wp_id: WP04
---

# WP04 review: changes requested

Reviewer: llxprt, 2026-10-25.
Reviewed head: `78a59e835ac9c241c74668bafbcf6c309bb2f320`.
Authoritative review base: `f1ae940ef5a5e18755d093b6e505a76eb82075d5` (`kitty/mission-hn-serve-01M5P0NP`).
WP04 implementation commit: `512ba3632a92db4563107b555686e7f0e508dca9`.

## Blocking issue: destination acceptance threshold lacks its required test

**P2, T021 / C-004 / FR-008:** `validation_seams_test.go:330-459` tests destination decision authorization and digest filtering, but never changes `Proposals.RequiredAccepts`. The fixture policies at lines 38 and 794 both require one acceptance. T021 explicitly requires a test proving that destination evaluation applies the destination acceptance threshold. The definition of done requires destination semantics to be tested.

The production expression at `policy.go:446` correctly reads the supplied policy's threshold by inspection. The missing regression test would distinguish that behavior from a fixed one-acceptance threshold. No mutation test was run, and this finding does not claim a production defect.

Add a `TestValidationSeams...` case using one existing qualifying acceptance:

1. Evaluate under the base policy with `RequiredAccepts: 1` and assert `Accepted == true`.
2. Create a valid destination policy with at least two maintainers and `RequiredAccepts: 2`, retaining the original signer and other evidence permissions.
3. Call `evaluateProposalUnderPolicy` with that destination policy and the base policy digest.
4. Assert the destination evaluation retains exactly one qualifying decision and remains ready, while `Accepted == false`.
5. Keep every pre-existing test unchanged and rerun the required gate.

## Quality gate run by the reviewing agent

All checks passed in the authoritative lane workspace at the reviewed head:

- `gofmt -l` on every changed Go file: no output.
- `go vet ./...`: PASS.
- `go build ./...`: PASS.
- `go test -race -count=1 -timeout 60m -skip '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery' ./...`: PASS, 338.607 seconds.
- `go test -count=1 -run '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery' .`: PASS, 36.556 seconds.

The pasted test commands contained stray agent/backtick text; the named performance test was used as their selector. No base performance comparison was needed because the head passed. Gate logs and concise evidence are under `/home/acoliver/projects/appaday/gitium/.scratch/wp/hn-serve-01M5P0NP/WP04/`. No temporary worktree was created.

## Acceptance and requirement results

- T020: PASS. Extraction preserves evaluation behavior and error ordering; equivalence and supplied-code cases exercise the seams.
- T021: FAIL only for the blocking acceptance-threshold test above. Reviewer, author-approval, maintainer, runner, result-threshold, digest and destination-pipeline behavior are covered. Typed pipeline errors preserve the existing message.
- T022: PASS. Only appended commits are loaded; actor, signature, sequence, previous ID, tree and parent defects are rejected. Empty-old-head and empty-range cases are covered.
- T023: PASS. Zero options preserve strict dependency behavior. The absent-code option validates the base, checks ancestry for a present head and retains revision predecessor/author checks.
- T024: PASS. Fast-forward merge facts validate through real local collection/evaluation and replication. Merge validators remain unchanged.
- T025: PARTIAL due to T021's missing case. All 31 pre-existing test files remain unchanged, including their commit histories. The full quality gate passes.
- T026: PASS. Both governance documentation sections are present and consistent with the behavior.
- C-004 / FR-008: implementation supported by inspection; mandatory destination-threshold coverage incomplete.
- C-006: PASS. No server signing identity, signed verdict fact or authority change is introduced.
- FR-007 / NFR-001 seam: PASS. Range validation joins the hosted prefix without reloading its commits from Git.
- FR-015 assumption: PASS. `commit == head` validates locally and through replication without validation changes.
- NFR-007: PASS. Existing tests are untouched and all requested gates pass.

## Anti-pattern checklist

1. Dead code: PASS under the authorized later-WP seam exception. Existing `hn` evaluation paths reach the extracted core; ordinary replication reaches the zero-option wrapper. `tasks.md` assigns actor/dependency callers to WP08 T048/T051 and policy callers to WP09 T055/T056.
2. Synthetic-fixture tests: PASS. Tests invoke production validation/evaluation and a real signed-event replication transaction.
3. Silent empty return: PASS. Successful absent-head dependency checks and empty actor ranges are explicitly specified; errors propagate.
4. FR coverage: PASS at the per-FR minimum. FR-007, FR-008 and FR-015 have behavioral assertions. The narrower mandatory T021 test gap remains blocking.
5. Frozen surface: PASS. Pre-existing tests and frozen evidence are untouched. No private key, generated binary or dependency change is introduced.
6. Locked decisions: PASS. Existing strict replication, signed-history and authority boundaries remain intact.
7. Shared-file ownership: PASS with the coordination note below.
8. Production fragility: PASS. New errors fail closed on malformed input or invalid policy evidence; no panic or unexplained transient-race failure path is introduced.

## Scope and coordination note

WP04's implementation changes exactly its four owned files: `policy.go`, `quarantine.go`, `validation_seams_test.go` and `docs/governance-v0.md`. Inherited WP03 files are excluded from WP04 attribution. Existing production callers cover the extraction, and the remaining library seams have explicitly assigned WP08/WP09 consumers. The pending documentation link is assigned to WP09.

`quarantine.go` is shared sequentially by WP04 and WP11 in lane-c. WP04 owns the localized validation seams; WP11 owns subsequent all-trusted resolution/not-advertised behavior. No WP11 selection behavior is changed here.

**Dependency notification for WP08 and WP11 agents:** WP04 has changes requested. Rebase dependent work onto the corrected WP04 commits before continuing integration. No other WP status was changed by this review.
