# WP14 review: changes requested

Reviewed WP14 implementation commit `0c29ad5132a7e48edf6d91a463a0b5a44eff1dd0` against its parent, at lane HEAD `1d16ce0fba4eb6340efccc12f104902d7394953b`.

## Blocking issue 1: oversized uploads consume unbounded input

**Location:** `serve_lfs.go:712-730`, `streamStaged`.

**Requirement:** T088 step 1 requires upload processing to stop reading past the declared size plus one byte.

The implementation stops writing and hashing after `size+1` bytes but continues reading every data packet until flush. A mapped client can declare a tiny object and send an arbitrarily large body after the server already knows the upload cannot match. This leaves connection time and input consumption unbounded. The passing tests do not cover this limit.

**Required fix:** Stop consuming an oversized body at the prescribed bound, remove the staged file, send a bounded mismatch refusal when possible, and terminate the session. Do not parse another command from the unconsumed object body. Add a regression test using an instrumented reader that detects reads beyond the bound and assert the response, failure exit, empty staging, and absence of a downloadable object. Preserve exact-size and one-byte-over coverage and all pre-existing test assertions.

## Acceptance review

- T086: PASS, framing checks precede payload allocation/read and writer chunking is bounded.
- T087: PASS, live session dispatch, version negotiation, operation checks, locking refusal, and staging initialization/cleanup.
- T088: FAIL, unbounded oversized-body reads described above. Other staged verification, storage, verify, and download checks passed review.
- T089: PASS, inclusive quota checks at batch, before upload, and under `lfs.lock` before publication; staging excluded.
- T090: PASS for the specified existing behavioral cases; add the missing bound regression for T088.
- T091: PASS, maintained guide, threat model, executed example, and traceability updated.
- C-003: PASS, no dependencies added.
- C-010: PASS, five owned files only; session edit is the planned enablement/dispatch seam.
- FR-020: existing transfer and verification behavior passes, with approval blocked by the T088 defect.
- FR-021: PASS, inclusive quota and unsupported locking behavior covered.
- Pre-existing test assertions: unchanged.

## Anti-pattern checklist

1. Dead code: PASS. Live `serveSession.run` dispatch reaches the implementation.
2. Synthetic-fixture test: PASS. Tests invoke production sessions and the Git LFS client.
3. Silent empty return: PASS. Reviewed returns have normal completion or explicit rejection/error semantics.
4. FR coverage: PASS. FR-020 and FR-021 have behavioral assertions and traceability.
5. Frozen surface: PASS. No frozen files or pre-existing tests changed by WP14.
6. Locked decision: PASS for reviewed contract prohibitions; the separate T088 bound fails as detailed above.
7. Shared-file ownership: PASS. Coordination note: WP14 intentionally changes the WP06-owned `serve_session.go` enablement flag and LFS refusal arm, exactly as prescribed by WP14 T087; it also fills WP13's explicitly reserved LFS documentation/traceability sections. No unrelated shared-file changes.
8. Production fragility: N/A for bare exception raises; Go storage/protocol errors propagate explicitly.

## Quality gates run independently

All mandatory gates passed at the reviewed lane HEAD:

- `gofmt -l` on every changed Go file: no output.
- `go vet ./...`: passed.
- `go build ./...`: passed.
- `go test -race -count=1 -timeout 60m -skip '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery$' ./...`: passed, package hubnot 691.421s.
- `go test -count=1 -run '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery$' .`: passed, 10.949s. Base comparison was unnecessary.

The delegated reviewer additionally ran a bounded WP14-specific behavioral suite, which passed in 5.513s. Full evidence and gate logs are under `/home/acoliver/projects/appaday/gitium/.scratch/wp/hn-serve-01M5P0NP/WP14/`.
