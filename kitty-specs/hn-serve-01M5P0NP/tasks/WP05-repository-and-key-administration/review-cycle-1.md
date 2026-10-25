---
affected_files: []
cycle_number: 1
mission_slug: hn-serve-01M5P0NP
reproduction_command:
reviewed_at: '2026-10-25T07:45:34Z'
reviewer_agent: user
wp_id: WP05
---

# WP05 review: changes requested

## Blocking: unsafe SSH state does not stop administration

`serve_admin.go:107-113` discards every error returned by `writeServeAuthorizedKeys` in `repairAuthorizedKeys`. The callers in `serve_commands.go:134-144` and `159-161` consequently allow `repo list`, `repo show`, and `key list` to succeed when `.ssh` is group/other-writable, `authorized_keys` is unsafe or symlinked, or the managed block has duplicate markers. `lock-exec` can also start its child after these errors.

This violates the WP05 fail-closed objective, T027's common administration/regeneration preamble, NFR-008, and the unsafe-SSH-state refusal requirements in `contracts/server-state.md`. The new `TestServeAdminAuthorizedKeysFailureHandling` at `serve_admin_test.go:691-701` explicitly expects success after changing `.ssh` to mode 0777. Passing that assertion demonstrates the contract violation.

Required changes:

1. Propagate SSH safety and managed-block regeneration errors through the administration route. The command must return a nonzero status and identify the offending path instead of warning and continuing.
2. Ensure `lock-exec` refuses unsafe SSH state before starting its child, while retaining the requirement to release the administration lock before a long-running recovery command.
3. Correct the newly added failure-handling test to require refusal. Add CLI assertions for unsafe/symlinked `authorized_keys` and malformed or duplicate managed-block markers, including proof that a refused `lock-exec` does not start its child.
4. Update `docs/serve-v0.md:523-530` to remove the warning-and-continue behavior and describe the corrected refusal behavior.
5. Keep every pre-existing test and assertion unchanged. Accommodating synthetic key fixtures does not authorize weakening production safety checks.

No other blocking issue was identified in the delegated code audit.

## Acceptance and scope assessment

The isolated implementation diff is `3a5ae95fdc77b5fd834d989fbf72858cd27160d1..4d66428229ab9bec670315442e0352825af2ccef`. Reviewed lane HEAD is `cb7f02266c097591fe43f4f981ae278269c03d03`; subsequent changes through that HEAD are planning/coordination-only.

| Requirement or criterion | Result |
| --- | --- |
| FR-002; T028-T030 | PASS: explicit provisioning, allowlisted branch/tag import, marker and HEAD, protected-tip policy checks, rollback/refusals, listing/showing, quota and budget changes have production-path CLI coverage. |
| FR-003; key-binding portion of T027 | PASS: full actors/fingerprints, duplicate refusal, multiple keys for one actor, bounded file/stdin input, listing and removal are covered. |
| FR-004; US2-1 and US2-5 administration portions | PASS for successful key mutations: generated forced commands and removal are checked, with CRLF and missing-final-newline outside content preserved byte-for-byte. Shared regeneration error handling is blocked above. Session behavior belongs to WP06. |
| NFR-008; common preamble in T027 | FAIL: unsafe SSH state is downgraded to a warning. Other unsafe registry/layout refusal cases have CLI coverage. |
| T031 | Locking, separator, inherited streams/environment, exit-status passthrough and administration-lock release have coverage. Unsafe-state admission remains blocked above. |
| T032 | Binary harness and required administration scenarios are present. Correct the new unsafe-SSH success assertion. |
| FR-023 administration portion; T033 | Guide, README link and both extracted, executed documentation examples are present. Correct the error-handling documentation with the implementation. |
| Live reachability | PASS: main dispatch reaches administration routes and handlers; added persistence/helpers have production callers. WP06/T038 explicitly extends the test harness. |
| Ownership and coordination | PASS: owned files plus the prompt-authorized `serve_state.go` writers; the recorded WP05 handoff explains shared dispatcher/docs changes. |
| Pre-existing tests and frozen surfaces | PASS: WP05 adds two test files and modifies no pre-existing test file or assertion. Frozen signed/history surfaces remain untouched. |

## Anti-pattern checklist

| Check | Result |
| --- | --- |
| Dead code | PASS |
| Synthetic-fixture test | PASS: requirement tests invoke production CLI paths. |
| Silent empty return | FAIL: suppressed regeneration errors, the blocking issue above. |
| FR coverage | PASS: referenced FR behaviors have assertions; the NFR-008 unsafe-SSH assertion has the wrong expectation. |
| Frozen surface | PASS |
| Locked decision | FAIL: fail-closed SSH-state requirement, the same blocking issue. |
| Shared-file ownership | PASS |
| Production fragility | PASS: explicit errors, bounded locks, rollback and partial-write messages, and lock release before exit. |

## Quality gates

All gates were run by the reviewer in the printed lane workspace at the reviewed HEAD. No source or test file was modified during review.

| Gate | Result |
| --- | --- |
| `gofmt -l` on all 15 changed Go files from the supplied mission-base comparison | PASS: no filenames printed. |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `go test -race -count=1 -timeout 60m -skip '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery' ./...` | PASS: hubnot 484.071s; example package has no tests. |
| `go test -count=1 -run '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery' .` | PASS: hubnot 48.893s. |

The performance test passed on HEAD, so no base comparison or temporary worktree was needed. Passing gates do not resolve the unsafe-state success expectation in the new test. Logs and the exact changed-file list are in `/home/acoliver/projects/appaday/gitium/.scratch/wp/hn-serve-01M5P0NP/WP05/gate-summary.txt` and its numbered gate logs.

## WP06 dependency handoff

WP06 depends on WP05. Rebase onto the corrected WP05 implementation before extending the harness or wiring SSH sessions. This review changes only WP05's status.
