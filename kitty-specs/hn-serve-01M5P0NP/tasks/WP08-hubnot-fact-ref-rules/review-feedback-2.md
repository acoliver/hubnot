# WP08 independent review, cycle 2: changes required

Reviewer: llxprt. Mission: hn-serve-01M5P0NP.
Reviewed lane HEAD: `3722b0fa52fd598dea3a5562dd8bd709e45d43c8`.
Implementation commits: `010d99f`, `732a8a9`.
Dependency-specific source base: `3984eb9fc57ed69215c1ba7637c2284a3aa2057f`.
Workspace: `/home/acoliver/projects/appaday/gitium/hubnot/.worktrees/hn-serve-01M5P0NP-lane-a`.

## Blocking finding: valid at-limit pushes are rejected when a log reuses a hosted signature blob

**Requirements: NFR-002, T048.3, the range/helper parity Definition of Done.**

Locations: `serve_verdict_facts.go:566-570,611-630`, used by the live actor rule at `serve_verdict_facts.go:354-363`.

The incremental measurer discounts hosted commits, event payload blobs and attachment blobs, but omits hosted signature blob IDs. The comment that signature blobs cannot recur in a valid chain overlooks reuse as a later `log.txt`: a valid signed run result can contain the bytes of an earlier event's encoded signature as its log. The log digest and every signature remain valid.

The independent probe creates two signed issues and a proposal, syncs them, then appends a signed run request/result whose log equals the first issue's signature blob. The reused signature predates the hosted tip. The unchanged replication helper and the server range helper return:

```text
                           Events  Objects  LargestObject  LargestAttachment  TotalBytes
replication helper              5        8            738                 86        2264
server range helper             5        9            738                 86        2350
```

Real pushes through the generated forced-command SSH shim reject the valid chain at both correct limits:

```text
code=over-budget budget=max-objects limit=8 measured=9
code=over-budget budget=max-total-bytes limit=2264 measured=2350
```

Both refusals leave all hosted refs unchanged. Restoring default budgets accepts the identical signed chain, confirming that the failure is budget accounting rather than invalid evidence. This violates the explicit requirement that the range variant return the same totals as `measureQuarantinedSelectionUnderBudgets` and accept a ref at its limit.

### Required correction

1. Discount every previously reachable blob that can recur as a new attachment, including hosted signature blobs. Preserve the batched hosted read and avoid per-event hosted-prefix loading. A batched signature object-ID query can accompany the existing exact signature-size accounting; deriving IDs solely from decoded signatures must also account for accepted whitespace encodings.
2. Preserve the corrected budget precedence, inclusive boundaries and ordinary-client helper behavior.
3. Add regression coverage comparing full and range measurements for this cross-role blob reuse. Exercise real pushes at the correct `max-objects` and `max-total-bytes` limits, and verify acceptance. Include a padded signature variant so accounting uses the actual hosted blob.

No implementation or pre-existing test was modified by this reviewer. The reproduction is a scratch-only Go overlay.

## Previous blockers

Both previous findings are addressed by the revised implementation for the exercised cases:

- `TestServeFactsAdmittedRunLogsStayReadable` passes with 8 MiB plus one byte and exactly 16 MiB logs, each followed by a successful ordinary append; 16 MiB plus one byte is refused. Explicit repository-wide ceilings are documented and checked before admission. `TestServeFactsHostedFactCeilings` proves exact event/byte boundaries and readback with padded signatures using lowered limits on the real verdict context.
- `TestServeFactsBudgetPriorityWithSeveralExceeded` passes through real actor, proposal and memory pushes. Largest-object and attachment limits precede total bytes, and object count precedes byte dimensions.

The blocking finding above is a separate completeness defect in previously reachable object discounting.

## Criterion and requirement coverage

`facts` below means `serve_verdict_facts.go`. Complete substep analysis is retained in the evidence directory's `criterion-coverage.md`.

| Criterion / requirement | Result and observed evidence |
| --- | --- |
| T048.1-2, FR-007, C-007 | PASS. facts:309-335 checks rewrite before ownership; ActorChainRewrite and ActorKeyMismatch cover deletion, rewind, divergence, mapped actor rejection and successful owner append. |
| T048.3, NFR-002 | **FAIL**. facts:586-685 has incomplete hosted-object discounting. All five supplied isolated boundary fixtures pass, but the independent probe above demonstrates false rejections at two correct limits. |
| T048.4, FR-007 | PASS for inspected cases. facts:377-382,480-555 reuses the range validator and identifies altered initial/appended payloads by full event ID, with commit fallback. The diagnostic walk runs only on failure. Hosted prefix comes from the batched reader and is never loaded event by event. |
| T048.5 | PASS. facts:440-478 indexes hosted, accepted-push and appended events; combines hosted and newly pushed code heads; calls the production dependency validator with the allowed absent-code exception. DependencyMissing verifies the full missing ID; separate and combined publication tests pass. |
| T048.6-7 | PASS. facts:389-408,425-433 runs all three mandated projection validators before appending PushFacts. RejectedActorEventsDoNotReachLaterChecks proves rejected events cannot satisfy the later proposal check. |
| T049.1-5, FR-010 fact classes | PASS for proposal behavior and supplied budget cases. facts:728-793 enforces immutability, candidate availability/kind, signed head, base ancestry and whole-code-graph budgets in order. ProposalRefRules and ProposalCodeArrivesAfterCandidate exercise real hooks. |
| T050.1-4, FR-010 fact classes | PASS. facts:808-867 enforces owned append-only streams, budgets and loadMemoryStreamAt with the same source as replication. MemoryRefRules covers signed record/sync, append, wrong owner, rewrite, deletion and invalid stream data. |
| T051.1-2 | PASS. serve_verdict.go:36-44,216-249,307-340 retains actor/memory/proposal/other/protected order. Only accepted actor events join PushFacts. Protected implementation is explicitly assigned to WP09/T054-T060 in tasks.md. |
| T051.3 | PASS for diagnostic precedence. facts:262-301,586-705 completes byte measurements before choosing the first exceeded dimension; real simultaneous-violation tests pass for actor/proposal/memory. The independent accounting blocker remains T048.3. |
| T051.4, NFR-003 | PASS. verdict:157-184,224-235,254-273 fails every fact/protected update closed with the exact required sentence if the one hosted load fails; HostedLoadFailureFailsClosed verifies this behavior. |
| T052.1-2 | PASS. FeatureBranchAndProposal and ProposalCodeArrivesAfterCandidate verify branch, actor and code publication, code-before-candidate refusal, separate later code publication and combined actor/code publication. |
| T052.3-5 | PASS. ActorKeyMismatch, AlteredEventIsInvalid and ActorChainRewrite cover all requested actor failure cases and full diagnostic IDs. |
| T052.6-8 | PASS. DependencyMissing, ProposalRefRules and MemoryRefRules exercise real production paths and expected rejection fields. |
| T052.9, NFR-002 | Supplied tests PASS; completion **blocked** by the additional valid at-limit reuse case. BudgetsAreInclusive covers every one-over/at-limit pair, and the supplied parity fixture covers repeated logs but omits reuse of an older signature as a log. |
| T052.10, FR-012, NFR-003 | PASS for exercised refusals. The common helper at serve_push_test.go:110-145 snapshots all hosted refs and checks one ordered, attributed, parseable line pair per failing ref. Mixed actor/proposal refusal is covered. Both independent budget refusals also preserve the full ref snapshot. |
| T053.1-3 | PASS for required guide content and example. docs/serve-v0.md:863-1077 covers ownership, validation, immutable history, revision path, memory, the code-ref exception and protected safety, defaults and inclusive limits, repo set, code-graph risk, rotation and rejection links. DocumentedExample executes both relayed lines after ID substitution. No em dash in the changed section. The general budget claims require the accounting fix above. |
| C-007 | PASS. No admitted path deletes or rewrites hosted actor/proposal history. |
| FR-007 / FR-010 / FR-012 / NFR-003 | Assigned ownership, signature, relationship, immutability, memory, atomic-refusal and fail-closed behaviors are covered as above. Overall WP approval is blocked by NFR-002. |

## Mandatory anti-pattern checklist

| Item | Result |
| --- | --- |
| Dead code | PASS for public rule entry points and module. Live hn route: main.go:56 -> serve_commands.go:38 -> serve_hooks.go:744-749 -> evaluateServePush -> three fact rules. New helpers support these paths. The unexported measurement-only wrapper is a direct test adapter around the live range implementation. WP09 owns the protected placeholder. |
| Synthetic-fixture tests | PASS. FR acceptance tests execute production validators with real signed facts and real hn/git clients. Literal mapper tests supplement the behavioral tests. |
| Silent empty return | PASS. Nil rejection denotes successful validation; missing lookups carry found=false; diagnostic fallbacks still reject; missing optional logs reach mandatory tree validation. |
| FR coverage | PASS for assigned FR-007, FR-010 and FR-012 behavior. NFR-002 fails as recorded above. |
| Frozen surface | PASS. No existing tests, signed fixtures, private keys, generated binaries or dependency files changed. git log 3984eb9..HEAD for existing core readers and replication/memory/operational tests is empty. |
| Locked decision | PASS for inspected constraints. No signing authority, commit creation, mandatory service dependency or protected bypass added. |
| Shared-file ownership | PASS. Both implementation commits modify exactly the four owned files; guide edits are confined to its assigned section. Shared sequential ownership and the range helper placement are recorded in tasks.md and WP08 status notes at 7e55d0c and 19a58e4. |
| Production fragility | PASS for inspected error paths and corrected hosted-read admission. The remaining blocker is incorrect budget accounting, rather than an unexplained transient exception. |

## Independently executed verification

All commands ran from the authoritative lane workspace as uid 1000. Temporary files and logs were isolated under the WP08 scratch directory. No parallel duplicate test runs occurred.

| Command | Actual result |
| --- | --- |
| gofmt -l on all 27 changed Go files from the authoritative mission-base diff | PASS, empty output. The manifest is snapshot.txt; includes every changed source and test Go file. |
| `go vet ./...` | PASS, exit 0. |
| `go build ./...` | PASS, exit 0. |
| `go test -race -count=1 -timeout 60m -skip '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery' ./...` | PASS, hubnot 285.055s; example package has no tests. |
| `go test -count=1 -run '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery' .` | PASS, 11.058s. No base comparison was required. |
| `git diff --check` | PASS. |
| `go test -race -count=1 -v -run '^TestServeFacts' .` | PASS, all 17 tests, 20.407s, no skips. |
| Independent scratch overlay probe | FAIL as intended by its acceptance assertions, 1.304s: incorrect object/byte totals and both real at-limit refusals; identical signed chain accepted with default budgets. |

Evidence directory:

```text
/home/acoliver/projects/appaday/gitium/.scratch/wp/hn-serve-01M5P0NP/WP08/review-2
```

Retained evidence: `criterion-coverage.md`, `snapshot.txt`, `gate-status.txt`, `gofmt.log`, `vet.log`, `build.log`, `race.log`, `performance.log`, `diff-check.log`, `focused-facts.log`, `probe-status.txt`, `review-probe.log`, and the concise reproducible `review_probe_test.go` / `overlay.json`.

Reproduce the blocker from the lane workspace:

```sh
mkdir -p /home/acoliver/projects/appaday/gitium/.scratch/wp/hn-serve-01M5P0NP/WP08/review-2/tmp
TMPDIR=/home/acoliver/projects/appaday/gitium/.scratch/wp/hn-serve-01M5P0NP/WP08/review-2/tmp go test -overlay /home/acoliver/projects/appaday/gitium/.scratch/wp/hn-serve-01M5P0NP/WP08/review-2/overlay.json -count=1 -v -run '^TestServeFactsReviewReusedHostedSignatureBlob$' .
```

## WP09 dependency notice and coordination

WP08 requires the budget accounting correction above. WP09 must wait for corrected WP08 and rebase any existing work onto the corrected lane head before continuing. This notice requests no WP09 status change. Preserve WP09's protected-rule ownership. The reviewer will record the same notice through the supported task history mechanism.
