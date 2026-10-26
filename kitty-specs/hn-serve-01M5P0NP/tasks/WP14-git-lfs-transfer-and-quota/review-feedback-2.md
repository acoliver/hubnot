# WP14 review cycle 2

## Verdict: changes requested

Reviewed lane head `fe71f0b974612375a0851e477bd71574c02e4ce7`. WP14 implementation commits: `0c29ad5` and `f953089`. Earlier shared-lane WP changes were excluded from this finding.

## Blocking issue: T088 declared-size-plus-one-byte upload limit

`serve_lfs.go:97-129` reads the entire pkt-line payload before `serve_lfs.go:663-676` checks it against the remaining declared upload size. A client declaring a small object can therefore make the server consume up to a full crossing packet, whose maximum payload is 65,515 bytes. T088 step 1 explicitly requires stopping after at most `N + 1` object-body bytes.

The current implementation bounds total overread by one packet, but still exceeds the specified body-byte cap. The new test at `serve_lfs_test.go:663-689` permits that extra packet, and `docs/serve-v0.md:1829-1835` documents the looser limit.

Required correction:

1. Enforce the remaining declared-size-plus-one-byte budget while reading upload packet bodies. If a packet crosses that budget, stop reading at the budget and terminate the session without interpreting unread upload payload as commands.
2. Add a strict regression assertion for a small declared size and a maximum-size crossing packet. Count consumed body bytes separately from framing and require no more than `N + 1`. Preserve all pre-existing test assertions and cover staged-file cleanup and lack of publication.
3. Update the guide to describe the enforced `N + 1` body-byte limit. A documentation change allowing an extra packet does not satisfy T088.

This is the only blocking finding.

## Quality gates: all passed

Executed from the authoritative lane workspace:

- `gofmt -l` on every changed Go file: empty output.
- `go vet ./...`: passed.
- `go build ./...`: passed.
- `go test -race -count=1 -timeout 60m -skip '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery$' ./...`: passed, `hubnot` 695.442 seconds.
- `go test -count=1 -run '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery$' .`: passed, 10.953 seconds. No base fallback needed.

Gate log: `/home/acoliver/projects/appaday/gitium/.scratch/wp/hn-serve-01M5P0NP/WP14/review-2-gate.log`.

## Acceptance and ownership assessment

- T086, T087, T089, T090 behavioral breadth, and T091: PASS.
- T088: FAIL on the exact upload read cap above; staging, verification, publication and interruption behavior otherwise covered.
- FR-020 transfer and verification: covered, with the T088 bound requiring correction.
- FR-021 quota, inclusive boundaries, default quota and unsupported locking: PASS.
- C-003 standard-library-only implementation: PASS.
- C-010 separability: PASS. WP14 stays in its five owned files; session changes are limited to planned LFS enablement and dispatch.
- Live command reachability, real Git LFS client coverage, documentation examples and traceability: PASS.
- Pre-existing test assertion preservation: PASS. No modified existing test files against `6ecca57`; `serve_session_test.go` is unchanged by WP14.
- Shared-file coordination: recorded in `review-feedback-1.md` for WP06 dispatch and WP13 documentation sections.

## Anti-pattern checklist

1. Dead code: PASS.
2. Synthetic-fixture test: PASS.
3. Silent empty return: PASS.
4. FR coverage: PASS.
5. Frozen surface: PASS.
6. Locked decision: FAIL, the T088 body-byte cap violation above.
7. Shared-file ownership: PASS.
8. Production fragility: N/A for `raise` constructs; Go error paths report explicit failures.
