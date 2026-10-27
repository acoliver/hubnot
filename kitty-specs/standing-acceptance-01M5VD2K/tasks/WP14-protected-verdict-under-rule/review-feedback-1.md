# WP14 review: changes requested

Implementation reviewed: `9386f9afce7718065e65540a9f52d5569dbb0ea4`.
Tested lane head: `520e513a14af753099510dbf4290be5c1b1e78cc`.
Reviewer: llxprt. Review invocation: `be6f6303ddd14f648b2f905bb4d4bec3`.

## Blocking issue: required race gate did not finish within 60 minutes

The exact required command failed:

```sh
go test -race -count=1 -timeout 60m -skip '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery$' ./...
```

It ran from 2026-10-27 05:13:18 to 06:13:21 -03:00 and returned 1. The root package reported:

```text
panic: test timed out after 1h0m0s
    running tests:
        TestStandingFactShallowDefects (37s)
        TestStandingFactShallowDefects/fact-only_rows (36s)
FAIL hubnot 3600.130s
```

No assertion failure or data race was reported before the timeout. This is a blocking validation gap under the supplied review instructions and the WP's full-gate completion criterion. It does not establish a defect in WP14's implementation. The timeout occurred in another concern's test while the whole suite exhausted its budget. No other WP's code or status was changed by this review.

**Required remediation:** obtain a successful run of the exact command above on the integrated lane head under sufficiently isolated host load. Preserve every existing test and assertion. If work is needed outside WP14's owned files, record coordination before changing it. Do not increase the required timeout, skip additional tests, or treat the agent wrapper's longer timeout as satisfying this gate. If the required gate cannot be met in this environment, obtain an authoritative gate-policy resolution before resubmitting; this reviewer cannot waive it.

The agent-side `go` wrapper replaces `-timeout 60m` with `150m`, so this review put `/usr/bin` first on PATH for the exact gate and honored both shared heavy-test locks. A separate already-running shared-lane suite passed in 5037.023 seconds with the wrapper's extended timeout. That supports a load/runtime explanation but does not satisfy the prescribed 60-minute gate.

## Successful checks

- `gofmt -l` on every changed Go file in the authoritative review range: no output.
- `go vet ./...`: passed.
- `go build ./...`: passed.
- `git diff --check 9386f9a^ 9386f9a`: passed.
- `go test -count=1 -run '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery$' .`: passed in 41.605 seconds. No base-worktree fallback was needed.
- `go test -count=1 -run 'TestStandingServeVerdict|TestStandingServeDocumentedVeto|TestStandingServeWireRuleRefusedBeforeVerdict' .`: passed in 153.720 seconds, including every WP14 verdict test, the executed documentation example and unchanged WP13 B5.
- The literal `go test -run 'TestServeProtected|TestServeAdvance|TestServeAutomerge'` hit Go's default 10-minute package timeout. Its unchanged tests passed on a fresh rerun with `-count=1 -timeout 60m`, in 586.033 seconds.

## Requirement and acceptance review

- **FR-012: PASS.** The live server verdict calls the shared `evaluateRuleAcceptance` with base policy, digest, candidate, target branch, changed paths, pipeline definitions and hosted/push facts.
- **FR-016: PASS.** Tests exercise acceptance; trusted change-request/failure vetoes; missing rule, wrong mode and target; disallowed author/path; missing approval/CI; wrong executing actor; maintainer rejection precedence; and two executing actors racing. Stable reason lines, IDs, next actions and immutable hosted refs are checked.
- **FR-017: PASS.** Step 7a rejects a differing destination digest. The fixture places the candidate head above amendment H1 with the older signed base and checks the exact diagnostic and full-tip revision action.
- **FR-018: PASS.** Direct pushes and unnamed advancements still require maintainer acceptance. The original dual-policy checks remain; the extracted step-12 guard preserves its behavior.
- **FR-023: PASS for WP14.** The server guide describes rule checks and later vetoes, links the reason table, preserves unnamed-push behavior, and executes the exact `standing-serve-veto` example. Client changes are assigned to WP15.
- **FR-025: PASS.** The interim refusal is removed in the same commit as rule evaluation. B5 remains unchanged, and the added post-verdict assertion requires `protected-rule-not-satisfied` for M's accepted candidate with `hn-rule=nosuch`.
- **T064–T069: implementation and focused tests PASS. Full-gate completion: FAIL**, solely for the blocking race-gate timeout above.

## Anti-pattern checklist

1. **Dead code: PASS.** Live path: `main` → `cmdServe` → pre-receive → `evaluateServePush` → `checkServeProtectedUpdate` → `evaluateServeProtectedUpdate` → candidate → rule acceptance/protected baseline/veto rejection helpers. No new production module or public API.
2. **Synthetic-fixture tests: PASS.** The acceptance assertions observe real public hn/Git operations through the forced-command harness.
3. **Silent empty return: PASS.** `nil` means no rejection. Empty-tip behavior preserves step 12; unavailable definitions become missing CI. Changed-path failures are explicit refusals.
4. **FR coverage: PASS.** Every assigned FR has production-path behavioral assertions, as mapped above.
5. **Frozen surface: PASS.** WP14 touches no test file present at `d881d9b`. Existing `standing_serve_test.go` tests are byte-identical apart from the added import; new helpers/tests are appended. No WP14 commit changes the frozen governed harness.
6. **Locked decisions: PASS.** No dependency, event kind, history rewrite, server signing, commit creation or manual-path bypass was introduced. Standing acceptance requires an explicit named rule.
7. **Shared-file ownership: PASS.** All four files are owned. Coordination note: `tasks.md` assigns `serve_verdict.go` refusal addition to WP13 and removal to WP14; `standing_serve_test.go` is sequentially shared by WP13/WP14/WP15/WP19; `docs/serve-v0.md` is sequentially shared by WP14/WP15/WP19, with WP14 owning the server explanation and WP15 the client flow. Prior assertions are preserved.
8. **Production fragility: PASS.** New failures are stable fail-closed protocol rejections. No new panic, exit or exception on transient races.

## Evidence and dependent notification

Evidence directory: `/home/acoliver/projects/appaday/gitium/.scratch/wp/standing-acceptance-01M5VD2K/WP14/`.

- `review-current.md`: detailed source review.
- `review-current-gate.log`: exact race failure and passing isolated performance result.
- `review-focused-current.log`: original focused-command timeout and passing WP14/B5 tests.
- `review-existing-extended.log`: successful unchanged serve-regression rerun.

Dependent WP15 implementer/reviewer notification was posted to the shared mission tracer as `tooling-friction-2d4e56ff427f`: refresh/rebase onto WP14's revised dependency after remediation and before proceeding. WP15's status was not modified.
