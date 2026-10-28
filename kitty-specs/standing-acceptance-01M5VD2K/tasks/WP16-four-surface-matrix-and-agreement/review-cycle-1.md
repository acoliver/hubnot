---
affected_files: []
cycle_number: 1
mission_slug: standing-acceptance-01M5VD2K
reproduction_command:
reviewed_at: '2026-10-28T04:07:53Z'
reviewer_agent: user
wp_id: WP16
---

# WP16 review: changes requested

Reviewed implementation commits `a2f975b36a220076eba4c71885b4ec743008acff` and `65684a9bd59b1ca20c1739b96f75a61d32f65bca` at lane head `dd2356feda41bc0a097cbde6d54a8320df1c6006`.

The required independent quality gate passes. Approval is blocked by three missing security-evidence assertions in this test-only WP.

## Blocking 1: Exercise shallow sync admission in the matrix

Location: `standing_fact_test.go:1716-1722`, `standingFactShallowSurface.Judge`.

T076 step 1 specifies a shallow verifier that runs `hn sync` and `hn proposal status`; step 2 requires zero promotion on refusal. This implementation calls `standingFactShallowVerifierWith`, which fetches the actor chains directly into accepted refs with raw Git, then calls only `proposal status` and `log`. No `hn sync` runs in this column. Its unchanged-ref assertion starts after the raw fetch has installed the judged invalid chain. Consequently the reported shallow cells establish projection refusal, while shallow import/promotion behavior is unexercised.

Add a real shallow `hn sync origin` judgment for every matrix fixture, preserving the complete closure and matched controls for F01-F20. Arrange the signer's accepted chain before sync so that the judged fact has not already been installed as accepted. Assert the refused chain's accepted ref is unchanged and no fact from that refused selection is promoted. Retain the independently hydrated projection judgment where necessary to prove that each semantic row reaches its intended derivation check instead of a dependency gap. Keep F21/F22 separate with their expected dependency diagnostics. Apply the same shallow admission check to the bound and execution-to-admission sets.

## Blocking 2: Add four-surface FR-015 immutability cases

Location: `standing_fact_test.go:2409-2432`, `2527-2582`; fixture preparation in `standing_harness_test.go:1176-1202`.

FR-015 is in this WP's requirement_refs and its definition of done requires evidence that events outside the listed evidence cannot invalidate a fact. The matrix never publishes a current maintainer rejection, later trusted change request, or later trusted failed result outside the evidence and then requires all four surfaces to accept the unchanged valid fact. F07 and F08 put the hostile event inside the defective fact's evidence; their matched controls use a later approval/passing result, which supersedes the earlier hostile event. They do not establish immutable validity under later/current vetoes.

Add explicit positive matrix rows that retain the valid fact's original qualifying evidence, publish later vetoes outside it, and require acceptance on local, import, shallow, and serve surfaces. Cover a maintainer rejection, trusted change request/approval withdrawal, and trusted pipeline failure, with full shallow closure. Assert the signed fact and evidence IDs remain unchanged. Where practical, prove the same current event set blocks a fresh live rule evaluation, so the fixture distinguishes fact validation from execution/advancement. `TestRuleAcceptanceFactImmutable` in `rule_acceptance_test.go:1007` provides unit coverage of the shared validator; WP16 needs the public-CLI four-surface evidence to catch surface-specific use of current events.

## Blocking 3: Assert zero promotion from a refused actor chain

Location: `standing_fact_test.go:1699-1701`, `standingFactImportSurface.Judge`.

The helper returns a successful refusal whenever the accepted ref is empty OR its history lacks the rejected fact commit. A regression that promotes a prefix of the refused signer's chain therefore still produces a passing matrix cell, despite T076 step 2 and NFR-001 requiring zero promotion from the refused chain. This matters especially when the judged fact follows previously valid events, and for the new oversized-path row, which has no earlier per-surface counterpart.

Capture the accepted actor ref before sync. On every refused selection assert exact equality afterwards; in this fresh verifier the expected ref is empty. Check that sync reports the refused selection and returns a refusal status, while continuing to allow independently valid selections to promote. Earlier `standingFactAssertImportedExcept` already demonstrates the required zero-promotion check. Integrate equivalent assertions into the new shallow-sync path from issue 1. Keep refusal and acceptance diagnostics distinct from harness failures.

## Independent verification

Executed from the printed lane workspace with `go` on PATH (`/home/acoliver/projects/appaday/gitium/bin/go`), without bypassing shared-host slots:

- `gofmt -l` on every changed Go file: PASS, no output.
- `git diff --check a2f975b^..65684a9`: PASS.
- `go vet ./...`: PASS.
- `go build ./...`: PASS.
- `go test -race -count=1 -timeout 150m -skip '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery$' ./...`: PASS, root package 3469.458 seconds; no races or assertion failures.
- `go test -count=1 -run '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery$' .`: PASS, 11.788 seconds. No base-worktree comparison was needed.

Reviewer logs are under `/home/acoliver/projects/appaday/gitium/.scratch/wp/standing-acceptance-01M5VD2K/WP16/`: `review-static.log`, `review-race.log`, `review-performance.log`.

Read the implementer's logged matrix: 68/68 agreement rows, 1/1 oversized-path row (8,844,000 bytes versus an 8,388,608-byte bound), and 6/6 client-recorded facts. The independent full race run includes all three matrix tests and the boundary test. These passing results do not fill the missing assertions above.

## Requirement and ownership audit

- C-004, FR-024, FR-025: legacy and boundary sets cover the declared verdicts, including maintainer-first union behavior.
- FR-012, NFR-001, NFR-002: existing semantic/control, missing-dependency, valid, legacy and boundary rows are present; issues 1 and 3 block completeness of the admission/state evidence.
- FR-015: issue 2 blocks four-surface coverage.
- NFR-005: the oversized-path fixture and preview/execution refusals are real CLI tests; issues 1 and 3 also apply to its admission-state evidence.
- WP16 changes only its owned `standing_fact_test.go`: 1,219 added lines, zero removed lines relative to the pre-WP16 parent. No existing assertion was changed. No production behavior, frozen payload, private key, generated binary, or maintained documentation changed in these commits. Documentation changes are unnecessary for this test-only diff.
- Shared-file coordination: `tasks.md` ownership row explicitly assigns `standing_fact_test.go` sequentially to WP07, WP08, WP09 and WP16. WP16 appends the matrix after those helpers and leaves their assertions and the already-enabled boundary table unchanged. This review requests changes only within WP16's assigned surface.
- Public CLI reachability verified: projection runs `validateMergeEventWith` through store loading; import and serve dependencies use `admitMergeFact`; shallow projecting commands run `guardShallowEventClosure`. New WP16 helpers are test-only and invoke the built production executable against real repositories and the forced-command server harness.

## Required anti-pattern checklist

1. Dead code: N/A for production additions. All additions are test-only helpers or test entry points; no new production module/API.
2. Synthetic-fixture test: PASS. Assertions exercise real `hn` and Git commands; in-process signing creates hostile input for the actual verifier.
3. Silent empty return: PASS. Empty helper results explicitly mean no admissible prefix, no diagnostic violation, or no report annotation; no swallowed production error.
4. FR coverage: FAIL. FR-015 lacks the required four-surface current/later-outside-evidence acceptance case; shallow sync admission is also absent (issues 1 and 2).
5. Frozen surface: PASS. WP16 modifies no pre-existing frozen test or signed fixture and removes no existing line.
6. Locked decision: PASS. No new production path or contradiction of the union/claim/namespace constraints; fixture coverage gaps are described above.
7. Shared-file ownership: PASS. Explicit sequential ownership is recorded in tasks.md and this coordination note.
8. Production fragility: N/A. No production change or new production failure path.

## Dependent-work notification

WP18 depends on WP16. Refresh/rebase on the corrected WP16 commits after these test-evidence fixes land, and rerun relevant integration gates. Preserve all earlier shared lane-b work. This review changes only WP16's status.
