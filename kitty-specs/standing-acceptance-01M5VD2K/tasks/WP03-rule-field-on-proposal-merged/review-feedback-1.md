# WP03 review: changes requested

Reviewed lane revision: `0e420fe49d2815f58ebbf206ad8bf6b90ebb28ee`.
WP03 implementation commit: `cef2dec`.

## Blocking issue: required full race gate failed

The independently executed required command exited 1:

```sh
go test -race -count=1 -timeout 60m -skip '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery$' ./...
```

The only failing test was:

```text
--- FAIL: TestServeQualityPushVerdictTimeNFR001 (278.03s)
    serve_quality_test.go:280: .../hn sync origin timed out: context deadline exceeded
FAIL hubnot 2283.544s
```

The failure happened while publishing the padded actor chains during fixture setup, before the timed push samples. No data-race report appeared. This failure is absent from the supplied baseline failure list. The required full gate has therefore not passed, which blocks approval under the review instructions.

Action: investigate the sync timeout, then rerun the exact complete gate from the lane workspace and record a successful result before resubmitting. If host load is suspected, run on an adequately available host; the reviewer observed shared host load and cannot attribute this timeout to the implementation. Compare a reproduction against the unchanged base if needed to distinguish environment sensitivity from a regression. Preserve every frozen test and its timeout/assertions. Do not weaken, skip, or replace this test to obtain a passing gate. No source-level defect is asserted by this review.

Evidence: `/home/acoliver/projects/appaday/gitium/.scratch/wp/standing-acceptance-01M5VD2K/WP03/review-race.log` and `review-race.exit`.

## Checks that passed

- `gofmt -l` on every changed Go file printed nothing.
- `go vet ./...`, `go build ./...`, and `git diff --check` passed.
- The separately required command `go test -count=1 -run '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery$' .` passed in 44.682s. No base-worktree exception was needed.
- Independent scratch-overlay tests compared the real verifier against the verifier and validator from `d881d9b`: signed unknown-rule shapes, every non-resource kind, canonical claims, malformed content fields, invalid signatures, and the strict-resource negative control retained legacy verdicts, IDs, and decoded fields after ignoring Rule.
- Additional parser tests covered escaped keys, duplicate folded keys, claim positions, nested values, lone surrogate values in unrelated keys, and large numeric unknown values.
- Independent benchmarks: claim walk 446.2 ns/op without a claim and 671.5 ns/op with one; verification 56,073 ns/op and 54,378 ns/op respectively. Walk overhead is approximately 0.8% and 1.2%, below 10%.
- FR-010 shape, FR-024 claim recognition, FR-022 event compatibility, and C-004 immutable-history behavior have substantive production-path assertions in `standing_event_test.go`.
- WP03 changes only `event.go` and the new `standing_event_test.go`. The existing tests are unchanged against the frozen snapshot `d881d9b`; the older sync changes visible against the mission ref predate that snapshot.
- Admission remains unchanged. Documentation and later rule-claim consumers are explicitly assigned to WP07 and the dependent work packages in `tasks.md`, per tasks decision 12.

## Required anti-pattern checklist

| Item | Verdict | Evidence |
| --- | --- | --- |
| Dead code | PASS | `appendEventWithAttachments` calls `encodeAndSign`/`marshalEvent`; `loadStoredEventAt` calls `verifyEvent`/`parseMergeRuleClaim`; all new parser helpers have production callers. Later rule-policy interpretation and recording have named WPs. |
| Synthetic-fixture test | PASS | Behavioral assertions invoke real signing and verification with actual Ed25519 signatures. Parser tests invoke the production parser. |
| Silent empty return | PASS | Empty parser returns explicitly mean no canonical claim under the contract and never reject signed legacy bytes. |
| FR coverage | PASS | Canonical round trip, no-claim matrix, signing refusals, strict-resource control, byte identity, and legacy audit cover the WP-owned requirement portions. |
| Frozen surface | PASS | No pre-existing test file or signed fixture changed in WP03; frozen-baseline test diff and history are empty. |
| Locked decision | PASS | No event kind or admission change; `Rule` is `json:"-"`; verification retains original bytes and all prior checks. |
| Shared-file ownership | PASS | WP03's implementation files are exclusively assigned to WP03. Planning/status synchronization is tool-managed and outside the implementation scope. |
| Production fragility | PASS | New signing errors are the contract's explicit fail-loud validation; recognition failures return no claim. No transient-race refusal was added. |

## Dependency notification

WP03 is returning to planned because its required gate did not pass. Agents for WP05, WP06, WP13, and WP17 must account for this review block and rebase on the corrected/revalidated WP03 revision before consuming it. This notice is also included in the WP03 status transition note.
