# WP08 review: changes required

Reviewer: llxprt, independent of the implementing model.
Mission: hn-serve-01M5P0NP.
Workspace: /home/acoliver/projects/appaday/gitium/hubnot/.worktrees/hn-serve-01M5P0NP-lane-a.
Implementation: 010d99fe6ae986967e4c98d6510020de9fa0d368.
Dependency-specific base: 3984eb9fc57ed69215c1ba7637c2284a3aa2057f.
Reviewed lane HEAD: 457a5b3373a4a52effb09cdbc8990ae3ab32384d.
The later lane commits have no additional Go or serve-guide changes.

## Blocking issue 1: admitted facts can prevent all subsequent fact pushes

Locations: `serve_verdict_facts.go:189-205,227-229`; the subsequent hosted load at `serve_verdict.go:155-159,213-222,255-259`; the existing reader ceiling at `issue_read.go:38-46,346-347`.

A correctly signed run result carrying an 8,388,609-byte `log.txt` is accepted by the real hook. It fits the documented default attachment budget of 16 MiB, object budget of 64 MiB and total budget of 1 GiB. A subsequent ordinary valid actor append is rejected with `actor-chain-invalid` and:

```text
hn-serve: hosted facts could not be verified; ask the administrator.
```

The server reloads its newly accepted hosted history using `collectIssueEvents`, whose independent object limit is 8 MiB. Static review shows this failed hosted load rejects every actor, proposal, memory and protected update, including updates by unrelated actors. Immutable history prevents an ordinary corrective append from removing the triggering log. WP08 introduces the executable admission path by replacing the fail-closed actor placeholder.

Required correction:

1. Make the server's batched hosted-read limits compatible with data it admits, including configurable budgets and accumulated hosted history. Keep verification, bounded resource use and fail-closed handling of corrupt history.
2. Account for the reader's aggregate limits as well as its object limit. Raising only the 8 MiB ceiling leaves the same availability defect at aggregate boundaries. Explicitly document and consistently enforce any additional repository-wide limits before admitting data that would exceed them.
3. Add a real-push regression that accepts a valid log above 8 MiB within server budgets, then accepts a subsequent ordinary actor append. Cover relevant aggregate-limit boundaries.
4. Preserve the documented 16 MiB attachment allowance rather than silently reducing it to 8 MiB.

This blocks NFR-002 and production availability. The requirement to fail closed on genuinely unreadable hosted facts remains valid.

## Blocking issue 2: partial measurements violate required budget precedence

Locations: `serve_verdict_facts.go:448-464`, particularly the early return at lines 462-463; budget mapping at lines 120-135; related proposal and memory measurement calls at lines 585-589 and 649-653. The documented priority is at `docs/serve-v0.md:985-989`.

A real signed actor event has complete measurements:

```text
Events=1 Objects=4 LargestObjectBytes=1114 LargestAttachmentBytes=0 TotalBytes=1522
```

With `max-object-bytes=1113` and `max-total-bytes=1`, the real hook returns:

```text
over-budget budget=max-total-bytes limit=1 measured=247
```

T051 requires largest object before total bytes, so the correct result is:

```text
over-budget budget=max-object-bytes limit=1113 measured=1114
```

The measurement stops after the first commit object exceeds the total budget. The mapping helper therefore never receives the later largest-object measurement. The same phase ordering can select total bytes before measuring attachments. `TestServeFactsOverBudgetMapping` supplies complete literal measurements and misses this production-path defect.

Required correction:

1. Complete the measurements needed to select the first exceeded dimension in the mandated order: events, objects, largest object, largest attachment, total bytes.
2. Preserve bounded work and unchanged ordinary-client helper behavior, using a server-specific mode or adapter if needed.
3. Apply consistent ordering to actor, proposal and memory rules, including consumers of the existing short-circuiting selection helper.
4. Add real-push tests with simultaneous violations, including largest-object versus total and attachment versus total. Assert structured `budget`, `limit` and `measured` fields and unchanged hosted refs.

This blocks T051.3 and NFR-002. It is an observable contract defect.

## Requirement and acceptance evidence

All source locations refer to the reviewed lane workspace. `facts` below means `serve_verdict_facts.go`.

| Requirement | Result and evidence |
| --- | --- |
| C-007 | PASS: actor deletion/non-ancestor checks at facts:147-161; proposal immutability at 533-544. ActorChainRewrite and ProposalRefRules exercise real pushes. |
| FR-007 | Covered actor ownership, append, signature/range, dependency and projection validation at facts:144-229. ActorKeyMismatch, AlteredEventIsInvalid, ActorChainRewrite and DependencyMissing exercise real hooks and full IDs. WP completion remains blocked by issue 1. |
| FR-010, assigned fact classes | Covered proposal candidate/head/base/immutability checks at facts:532-597 and owned append-only memory validation at 612-670. ProposalRefRules, ProposalCodeArrivesAfterCandidate and MemoryRefRules pass. Budget correctness remains blocked by issue 2. Protected implementation is explicitly assigned to WP09. |
| FR-012 | PASS for reviewed paths: the hook rejects before acceptance; the common rejection helper snapshots all refs and checks exact ordered attributed rejection pairs. RejectedActorEventsDoNotReachLaterChecks proves rejected facts cannot satisfy later proposal checks. |
| NFR-002 | FAIL: individual inclusive boundary pairs and range/helper parity pass, but issues 1 and 2 violate usable budget admission and mandatory diagnostic ordering. |
| NFR-003 | PASS for assigned invalid-signature and missing-dependency behavior. AlteredEventIsInvalid, DependencyMissing and HostedLoadFailureFailsClosed pass. The legitimate-data availability trigger is issue 1. |

| Acceptance item | Result and evidence |
| --- | --- |
| T048.1-2: rewrite before ownership | PASS: facts:147-170; ActorChainRewrite and ActorKeyMismatch cover deletion, rewind, divergence and mapped keys. |
| T048.3: range measurement, inclusive limits, helper parity | PARTIAL: facts:189-198,392-508 walks the appended range and counts the batched prefix without per-event hosted rereads. RangeMeasurementMatchesSelectionHelper and individual boundaries pass. Issues 1 and 2 block completion. |
| T048.4: prefix/range validity and full diagnostic IDs | PASS for inspected behavior: facts:180-205,306-377 reuses the range validator and obtains the failing raw payload's full digest or commit fallback. AlteredEventIsInvalid covers altered first and appended events. |
| T048.5: dependencies and absent-code exception | PASS: facts:261-298 combines hosted, previously accepted and appended events, includes pushed proposal heads and invokes the production dependency validator with AllowAbsentProposalCode=true. DependencyMissing and ProposalCodeArrivesAfterCandidate pass. |
| T048.6: closure/issue/identity projections | PASS: facts:212-225,246-254 reuses all three production validators over the combined event set. Success and dependency tests exercise the path. |
| T048.7: publish accepted facts only | PASS: facts:228 is after validation. RejectedActorEventsDoNotReachLaterChecks exercises the non-leakage guarantee. |
| T049.1-5: proposal rules | Covered at facts:532-597 in required order. Real tests cover move/delete, missing and wrong-kind candidates, wrong signed head and successful later creation. Base ancestry is explicitly enforced at 574-579. Budget precedence is blocked by issue 2. |
| T050.1-4: memory rules | Covered at facts:612-670. MemoryRefRules exercises record/sync, append, wrong owner, rewrite, deletion and invalid copied memory data. Budget precedence is blocked by issue 2. |
| T051.1-2: wiring, evaluation order and fact sharing | PASS: serve_verdict.go:33-41,296-309 and facts:228. Protected placeholder stays fail-closed for WP09/T054-T060. Mixed actor/proposal rejection test passes. |
| T051.3: budget diagnostic priority | FAIL: issue 2 reproduces the wrong production rejection despite the mapper's correct isolated ordering. |
| T051.4: hosted-read failure handling | PASS as fail-closed error handling: HostedLoadFailureFailsClosed checks all fact/protected classes, exact sentence and one hosted load. Issue 1 shows a valid-data trigger requiring correction. |
| T052.1-2: feature/proposal success and ordering | PASS: FeatureBranchAndProposal and ProposalCodeArrivesAfterCandidate use real hn/git, including separate and same-push candidate/code publication. |
| T052.3-5: actor ownership, altered signatures, rewrites | PASS: ActorKeyMismatch, AlteredEventIsInvalid and ActorChainRewrite cover all requested cases and full diagnostic IDs. |
| T052.6-8: missing dependencies, proposal and memory rules | PASS for supplied cases: DependencyMissing, ProposalRefRules and MemoryRefRules execute the live hooks and common ref snapshot checks. |
| T052.9: five budget boundary pairs | PASS for isolated fixtures: BudgetsAreInclusive checks at-limit and one-over cases through repo set and real pushes. Issues 1 and 2 need additional regressions. |
| T052.10: unchanged rejected refs and exact pairs | PASS for supplied fixtures: the common rejection helper snapshots all refs and checks exact count/order/actor/sentence; mixed actor/proposal rejection is included. |
| T053.1: guide content | PRESENT at docs/serve-v0.md:863-1034, covering each required topic. Budget claims require correction with issues 1 and 2. |
| T053.2: marked executable example | PASS: TestServeFactsDocumentedExample compares the two relayed actor-key-mismatch lines after ID substitution. |
| T053.3: writing conventions | PASS: descriptive direct text; no em dash in the WP08 documentation diff. No style preference is a rejection reason. |

## Mandatory review checklist

| Check | Result |
| --- | --- |
| Dead code | PASS. Live path: main.go cmdServe -> hook dispatcher -> serve_hooks.go:744-749 -> evaluateServePush -> fact rules. All new production helpers have production callers. WP09 explicitly owns the remaining protected placeholder. |
| Synthetic-fixture tests | PASS for FR acceptance cases: real signed facts and real hn/git hook paths. Pure mapper coverage is supplemented by the production failure probe for issue 2. |
| Silent empty return | PASS. Nil rejection means acceptance; missing lookups carry found=false; diagnostic fallbacks still reject; optional missing attachments proceed to mandatory tree validation. |
| FR coverage | PASS for assigned FR-007/FR-010/FR-012 paths, with tests identified above. |
| Frozen surface | PASS. No signed evidence, fixtures, keys, generated binaries, dependency files or existing test files changed. |
| Locked decisions | PASS for inspected MUST-NOT boundaries. No signing, commit creation, registry-derived policy authority, service dependency or protected-rule bypass was added. |
| Shared-file ownership | PASS. Exactly the four owned files changed. The recorded WP08 coordination note in 7e55d0c describes the verdict placeholder replacement, preserved WP09 arm, owned-module range helper and guide section. tasks.md declares sequential shared ownership. Corrected WP07 commit b7409b4 is an ancestor. |
| Production fragility | FAIL: issue 1 allows admitted valid data to cause persistent global fact/protected refusal. |

## Scope and test preservation

The exact implementation diff changes only `serve_verdict_facts.go`, `serve_verdict.go`, new `serve_facts_test.go`, and the owned fact/budget section in `docs/serve-v0.md`. No pre-existing test assertion or existing test file changes. Ordinary client replication helpers remain unchanged. Documentation and its executed example accompany the behavior. No lane source or task status was edited by the delegated reviewer.

## Verification evidence

Scratch evidence is confined to:

```text
/home/acoliver/projects/appaday/gitium/.scratch/wp/hn-serve-01M5P0NP/WP08
```

The delegated reviewer ran all 14 `TestServeFacts` tests successfully. It also reproduced both blockers through real Git pushes using a scratch-only Go overlay. The probe source, overlay and `review-regressions.log` are retained there as concise reproduction evidence. The virtual test filename is not written into the lane workspace.

Reproduction command, from the lane workspace, with `TMPDIR` set to an existing `tmp` directory under the scratch path:

```sh
go test -overlay /home/acoliver/projects/appaday/gitium/.scratch/wp/hn-serve-01M5P0NP/WP08/overlay.json -count=1 -v -run '^TestServeFactsReview(MultipleBudgetPriority|AcceptedLogRemainsReadable)$' .
```

The reviewing agent independently executed the full required gate from the printed lane workspace:

| Command | Result |
| --- | --- |
| `gofmt -l serve_facts_test.go serve_verdict.go serve_verdict_facts.go` | PASS, empty output. |
| `gofmt -l .` | PASS, empty output. |
| `go vet ./...` | PASS. |
| `go build ./...` | PASS. |
| `git diff --check` | PASS. |
| `go test -race -count=1 -timeout 60m -skip '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery' ./...` | PASS, hubnot 280.127s; example package has no tests. |
| `go test -count=1 -run '^TestMemoryIndexTenThousandRecordsPerformanceAndRecovery' .` | PASS without race, 11.157s. |

The stray backticks and `--agent llxprt` text in the user's test-pattern formatting were omitted from the intended Go test regex. An initial foreground invocation was terminated by the shell tool's 120-second inactivity watchdog before a result; the complete race and performance commands were rerun sequentially as a managed job and both exited zero. Logs are `reviewer-race.log`, `reviewer-performance.log` and `reviewer-gate-status.txt` under the scratch directory. The non-race performance gate passed, so no base-commit scratch worktree was needed. No temporary worktree was created. The prompt's pre-existing missing-JUnit-artifact baseline is unrelated to either blocker.

All existing tests pass. The two additional failing behavioral probes establish the blocking defects described above. No pre-existing test was modified to produce either result.

## WP09 dependency notice

WP08 is returned for corrections. WP09 must wait for the corrected WP08 implementation and rebase any existing work onto the corrected lane head before continuing. This notice requests no WP09 status change. Preserve WP09's protected-rule ownership and coordinate any correction that touches additional shared files.
