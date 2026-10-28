---
affected_files: []
cycle_number: 1
mission_slug: standing-acceptance-01M5VD2K
reproduction_command:
reviewed_at: '2026-10-28T10:30:40Z'
reviewer_agent: user
wp_id: WP18
---

# WP18 review: changes requested

Reviewed implementation commit `ea01d3f1189a37b957f0e6ca511f41fd8d79ab40` on integrated lane HEAD `f51c82d3d1952fad2096932e656c3bd4a03d6dd9`. Only WP18 is reviewed or transitioned by this verdict.

## Blocking issue 1: distinguish local execution from remote synchronization and publication

**Location:** `docs/threat-model.md:248-261`, in "Optional protected checks and native automerge".

The threat model still states: "Neither preview nor execution fetches, starts a daemon, executes a workflow, synchronizes or publishes". This is false for the documented standing-rule command `hn automerge PROPOSAL --rule NAME --execute --remote REMOTE`. `automergeRemote` calls `cmdSync` at `automerge.go:894`, which imports hosted facts and publishes local facts before the second eligibility check. It then calls `mergeRemoteBound`, which requests hosted advancement and records and publishes the merge fact. The publication-only rerun also publishes an existing fact. `docs/serve-v0.md`, "Automerge rules" and "Rerunning a rule advancement", correctly describe these effects, including publication before a later refusal.

This is a trust-boundary and operator-side-effect contradiction, covered explicitly by T089 step 1's required consistency pass and C-006/FR-023. The nearby blanket description of execution advancing a local target under a signed-base lease likewise needs a local-execution qualifier, since remote execution advances the hosted branch to the signed head without creating a local merge commit.

**Required fix:** scope the no-fetch/no-sync/no-publication guarantee and the local merge/lease description to preview and execution without `--remote`. Add a concise explanation that `--remote` identifies and synchronizes with `hn serve`, can publish local facts even when subsequent rule evaluation refuses advancement, requests the hosted fast-forward, and publishes the resulting fact. Mention or link the authorized publication-only rerun. Keep the correction inside the existing owned threat-model section. Preserve every existing test assertion.

## Independently executed quality gates

All gates passed on the reviewed integrated HEAD, using the `go` on PATH and its shared heavy-test slots. No timeout, assertion failure or data race occurred.

| Check | Result |
|----|----|
| `git diff --name-only d881d9b HEAD -- '*.go'` piped to `gofmt -l`, and `gofmt -l .` | PASS, no output |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `go test -race -run 'TestStandingQuality' ./...` | PASS, root package 1195.476s; elapsed 19m59.554s; includes the NFR-006 p95 ratio assertion |
| `go test -race -count=1 -timeout 150m -skip '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery$' ./...` | PASS, root package 4669.593s; elapsed 77m50.223s; all other packages passed |
| `go test -count=1 -run '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery$' .` | PASS, 11.154s; no base comparison needed |
| `git diff --check` | PASS |

Versions: `go version go1.26.8 linux/amd64`; `git version 2.53.0`.

Independent logs are under `/home/acoliver/projects/appaday/gitium/.scratch/wp/standing-acceptance-01M5VD2K/WP18/`: `reviewer-gates.log` and `reviewer-traceability.log`.

The required traceability negative control was run in a detached scratch worktree at the reviewed HEAD. `TestStandingQualityTraceability` first passed with all 79 IDs. Removing exactly the FR-001 documentation row made the actual test fail with `FR-001 has 0 rows, want 1`. The scratch mutation was restored and the temporary worktree removed.

## Requirements and scope audit

- C-001: PASS. `git diff d881d9b HEAD -- go.mod go.sum` is empty. WP18 adds only a root `package main` Go test file and uses standard-library imports.
- C-003 and NFR-004: PASS. `git diff --name-status d881d9b HEAD -- '*_test.go'` lists only ten additions. Enumerating baseline test files with `git ls-tree -r --name-only d881d9b | grep '_test\.go$'` and diffing each produces no changes. The full race suite includes `TestStandingCompatPreExistingTestsFrozen`. WP18 changes no pre-existing test file.
- C-006 and FR-023: FAIL for blocking issue 1. The new fact example is checked against a genuinely recorded CLI merge fact. All documented-example guards passed.
- C-010: PASS. Scope searches found no introduction of model-family metadata, issue requirement classes, review-round caps, release publishing or GitHub publishing.
- C-011: PASS for this review. Nothing was pushed. The reviewed lane targets `feat/standing-acceptance` as the authoritative prompt specifies.
- NFR-006: PASS. The test builds the 1,000-event verifier once, creates separate 20-fact rule and acceptance remotes, restores verifier copies, alternates 20 runs per variant, checks actual chain promotion, and enforces p95(rule) <= 1.5 * p95(acceptance).
- NFR-007: PASS. Every prescribed independent gate passed.
- T087 / SC-006 traceability: PASS for the required ID/test mapping and its executable negative control.
- T088: PASS for the required authority transfer, minimum independent review, modified-client residual risk, protected `hn serve` recommendation, case-variant paths, server checks, union validity and preserved legacy history.
- T089 consistency: FAIL for blocking issue 1; audits and gates pass.

## Anti-pattern checklist

| Item | Verdict | Evidence |
|----|----|----|
| 1. Dead code | N/A | WP18 adds no production module or public production function. New helpers belong to executable quality tests. Timing invokes live `hn sync`; fact and maintainers-mode execution tests invoke live automerge paths. |
| 2. Synthetic-fixture tests | PASS | Timing imports real signed chains; the protocol example is compared to a fact recorded by real CLI execution. Parser/style unit fixtures exercise the actual validators they claim to test. |
| 3. Silent empty return | PASS | JSON/example/trace parsers return explicit errors. Missing specification skips with a visible reason as required. Empty validation-problem lists indicate success. No unexplained swallowed failure was found. |
| 4. FR coverage | PASS | FR-023 has actual recorded-fact, executable-example, reason-documentation and style assertions. The remaining defect is the documented behavioral contradiction identified above. |
| 5. Frozen surface | PASS | No WP18 commit changes an existing test; baseline audit is empty for every pre-existing test file. Dependencies are unchanged. |
| 6. Locked decision | PASS | No WP18 code contradicts the excluded-scope or immutable-history constraints. Fact validity remains distinct from current execution vetoes. |
| 7. Shared-file ownership | PASS | Coordination note: WP18 is the sequential final documentation editor of lane-b. Its changes are limited to its explicitly owned `standing_quality_test.go`, `docs/governance-v0.md`, `docs/protocol-v0.md`, and `docs/threat-model.md`. It adds the assigned traceability/example/threat sections and minimal consistency fixes; WP17's "Compatibility and recovery" text is unchanged by WP18. Earlier owners' tests and documentation examples remain intact. |
| 8. Production fragility | N/A | WP18 introduces no production error path. Test failures are explicit assertions with diagnostics. |

The single blocking issue is documentation correctness under the required consistency pass. No implementation or test-gate regression was found.
