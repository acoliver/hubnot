---
schema_version: 1
artifact_type: spec-kitty.analysis-report
command: /spec-kitty.analyze
mission_slug: standing-acceptance-01M5VD2K
mission_id: 01M5VD2K9G554Y2CVBMGPN30MQ
generated_at: '2026-10-26T10:41:44.761630+00:00'
analyzer_agent: unknown
input_artifacts:
  spec.md:
    path: kitty-specs/standing-acceptance-01M5VD2K/spec.md
    sha256: 77961a9daea9adbce59c2ed0459d319235da481d57f98579c8eb7b5404f778cc
  plan.md:
    path: kitty-specs/standing-acceptance-01M5VD2K/plan.md
    sha256: c1c6ac0a8007c9eb47d1571c9bd74fccd36f8e2a9ff9e1ef1f620d13b3c285e3
  tasks.md:
    path: kitty-specs/standing-acceptance-01M5VD2K/tasks.md
    sha256: afc6132c0a2a6dc7c5cb4e251aff39cb1f0aba0a8317cf14b19831ac64c8c96e
  charter:
    path: .kittify/charter/charter.yaml
    sha256: 404c65fe9e64919858d009c763644aefbfd369884d573d1986e3db7fa1f14bc9
verdict: ready
issue_counts:
  low: 0
  critical: 0
  high: 0
  medium: 0
  info: 0
findings: []
---

## Specification Analysis Report

**Mission:** `standing-acceptance-01M5VD2K`
**Round:** 3, final planning analysis
**Reviewed commit:** `b9cea382c15dce09a4996abb30fc0bab0c19699c`
**Verdict:** **READY** for implementation.

The maintainer and WP implementers can proceed with the approved plan. All four round-2 findings are resolved, and no new finding was confirmed in the remediation scope. Readiness here concerns planning consistency. Implementation and its required automated verification remain outstanding.

### Scope and method

This round verified I4, I5, U2 and U3 against the complete operative diff of `b9cea38`, the [specification](spec.md), [plan](plan.md), [task index](tasks.md), governing contracts and affected consumer WP instructions. The four accepted refinements are recorded in [tasks.md, Analysis remediation](tasks.md#analysis-remediation). Round-1 resolutions were carried forward and rechecked where the remediation touched matched-input, legacy-history or shallow-control instructions. This round does not claim a new exhaustive implementation audit.

The startup check reported Spec Kitty 3.2.7 with action `none`. The single prerequisite check succeeded without errors or warnings. Current, target and base branches were `feat/standing-acceptance`; all required artifacts were available, and the starting checkout was clean.

There is no `charter/charter.md` in this repository. The project designates [`.kittify/charter/charter.yaml`](../../.kittify/charter/charter.yaml) as its runtime charter, with [`.kittify/charter/charter.md`](../../.kittify/charter/charter.md) as its human-readable companion. Those authorities were used without changing or weakening any principle.

`WPnn` below names the unique `tasks/WPnn-*.md` file in this mission. Locations are 1-based lines at the reviewed commit. The incremental evidence draft is retained at the requested external path, `../.scratch/analyze-standing-acceptance-draft.md`.

### Findings

| ID | Category | Severity | Location(s) | Summary | Recommendation |
|----|----------|----------|-------------|---------|----------------|

No unresolved findings. No overflow.

### Round-2 finding status

| ID | Previous severity | Status | Verification |
|----|-------------------|--------|--------------|
| I4 | HIGH | Resolved | [WP11:L201-206](tasks/WP11-local-execution-and-recording.md#subtask-t094-preview-and-local-execution-agree-on-rule-satisfaction-nfr-002), [WP15:L178-182](tasks/WP15-client-rule-advancement.md#subtask-t095-preview-and-hn-serve-agree-on-rule-satisfaction-authorization-and-transport-tested-on-their-own-nfr-002) and [serve contract:L110-117](contracts/serve-rule-advancement.md#matched-inputs) require per-fixture declared tips equal across the compared surfaces. The FR-017 case explicitly uses amendment H1 instead of the signed base and builds the head on H1. This follows WP14 T068 step 6 and lets server step 5 pass before step 7a refuses the changed policy digest. Local preview must report the existing target-policy blocker. Authorization and transport remain separately tested. |
| I5 | HIGH | Resolved | [WP06:L177,L185](tasks/WP06-standing-test-harness.md#subtask-t030-harness-self-test), [WP03:L128](tasks/WP03-rule-field-on-proposal-merged.md#subtask-t013-claim-legacy-and-encoding-tests) and [rule-fact contract:L166-175](contracts/rule-merge-fact.md#legacy-history-fixtures-c-004-fr-022) assert declared verification outcomes: L01-L05 succeed; L06 fails with `invalid resource event JSON: unknown field "rule"`. WP08 T038, WP16 T078 and WP17 T084 already require that negative outcome. Strict resource decoding is preserved. |
| U2 | MEDIUM | Resolved | [WP06:L164](tasks/WP06-standing-test-harness.md#subtask-t029-signed-fact-builder-for-valid-and-defective-rule-merge-facts) supplies a `Control` builder per semantic row. [WP09:L110-114](tasks/WP09-shallow-verification.md#subtask-t041-shallow-tests-for-us3-4-the-semantic-matrix-and-recovery-guidance), [WP16:L109-110](tasks/WP16-four-surface-matrix-and-agreement.md#subtask-t076-defect-matrix-runner-across-the-four-surfaces) and [rule-fact contract:L160-162](contracts/rule-merge-fact.md#fixture-matrix-nfr-001) use the same candidate for fact-only defects and a corresponding valid candidate with rebound approval, request, result and merge fact for F15-F18. Both sides require complete closure and the same selection/hydration strategy. Semantic rows must still name the intended check; dependency gaps cannot satisfy them. |
| U3 | MEDIUM | Resolved | [WP06:L130,L142,L164](tasks/WP06-standing-test-harness.md#subtask-t029-signed-fact-builder-for-valid-and-defective-rule-merge-facts) uses the default rule's counted reviewer R, adds `ApproveUnpublished` and `Publish`, gives F21 its own candidate and keeps all other evidence published and sufficient. It prohibits intervening R helpers that would publish the withheld approval. [WP08:L132](tasks/WP08-replication-import-admission.md#subtask-t038-hn-sync-import-tests-for-f01-to-f22) asserts the missing approval and recovery guidance before publication, then promotion of E's chain and merged status after publication. |

### Cross-artifact checks

**Agreement fixture feasibility.** The FR-017 example is intentionally refused. Putting both compared targets at H1 satisfies the identical-input requirement of NFR-002 without requiring H1 to equal the signed base. The candidate head descends from H1, satisfying the server's earlier fast-forward check. Local moved-target and manual-path blockers can coexist with the required target-policy blocker; local preview and execution see the same inputs and blockers. The recipe grants no new execution authority and changes no eligibility rule. See [FR-017/NFR-002](spec.md#requirements), [server step ordering](contracts/serve-rule-advancement.md#protected-branch-steps-with-hn-rule), and the existing local checks in `automerge.go:L262-264,L297-302`.

**Legacy decoding and activation.** L06 remains a deliberately invalid resource payload. `event.go:L100-108` supplies the specified error wrapper, and `resource_model.go:L522-534` rejects unknown keys while excluding `json:"-"` fields from allowed resource keys. The changed self-test distinguishes payload verification from relationship admission: B1-B4 can verify their signatures before path (b) admission exists. No unconditional rule-key refusal, relaxed resource decoder or bypass of the path-(a)-first union was introduced. See [claim recognition and union admission](contracts/rule-merge-fact.md#rule-claim-recognition) and [WP06 self-test](tasks/WP06-standing-test-harness.md#subtask-t030-harness-self-test).

**Candidate controls and evidence binding.** F15 changes the control's author to an allowed actor; F16 removes the disallowed-path defect; F17/F18 retain the allowed `src/` change while removing the manual-path file from the control. Each replacement has its own candidate-bound evidence. The negative F17/F18 rows still reach `manual-path` before `changed-paths`, as the [ordered derivation](contracts/rule-merge-fact.md#check-names) requires. The round-1 protection against shallow tests passing through unrelated gaps remains explicit.

**Missing-evidence recovery.** R is a counted non-author reviewer, M is an authorized requester and T is a trusted runner in the [default harness policy](tasks/WP06-standing-test-harness.md#subtask-t026-standing-policy-builder-and-adoption-through-a-maintainer-amendment). Publishing the withheld R approval therefore removes the sole availability defect. The before/after assertions agree with FR-013 and the [qualifying-evidence rules](data-model.md#qualifying-evidence). The contract's generic F21 absent-evidence row imposes no conflicting reviewer choice.

**Ordering and ownership.** Read-only checks found 100 unique task IDs T001-T100, exactly one prompt owner per task and a matching task index. Every dependency is ordered within its lane or declared across lanes, and shared owned files are confined to one lane. All eight changed WP frontmatters are byte-identical to the parent commit, so task lists, dependencies and ownership were preserved. `git diff --exit-code b9cea38^ b9cea38 -- '*.go' go.mod go.sum` was empty; spec, plan and lanes were also unchanged. `git diff --check` passed. Sources: [task index and maps](tasks.md#subtask-index), WP frontmatters, [lanes.json](lanes.json), and the reviewed commit diff.

### Coverage Summary Table

Coverage means assigned implementation/test work. The unchanged assignment map from round 2 was reconciled with the current requirement inventory, task index and prompt ownership. All 25 FR and 7 NFR requirements retain assigned tasks. Stable keys use the requirement ID and title-derived slug.

| Requirement Key | Has Task? | Task IDs | Notes |
|-----------------|-----------|----------|-------|
| FR-001 acceptance-mode-field | Yes | T001-T005 | Decoding and validation. |
| FR-002 non-maintainer-executing-actors | Yes | T002,T004-T005 | Mode-dependent authority. |
| FR-003 path-safety-for-standing-acceptance | Yes | T003-T005 | Policy/protected-path overlap. |
| FR-004 minimum-independent-review | Yes | T002,T004-T005 | Positive approval threshold. |
| FR-005 rule-reviewers | Yes | T001-T005,T016-T019,T043-T046 | Narrowed counting; trusted vetoes retained. |
| FR-006 author-approval-under-standing-acceptance | Yes | T016,T019,T043,T046 | Self-approval excluded. |
| FR-007 policy-inspection | Yes | T007-T010 | Deterministic policy output. |
| FR-008 preview | Yes | T043-T047,T094 | Keyless, side-effect-free comparison. |
| FR-009 execution-without-per-proposal-acceptance | Yes | T043,T048-T052,T054-T057,T070-T074 | Existing blockers retained. |
| FR-010 rule-merge-fact | Yes | T011-T013,T020-T024,T049-T050,T072 | Claim, exact evidence and recording. |
| FR-011 rule-merge-signer | Yes | T021,T024,T048-T052 | Rule actors, including listed maintainers. |
| FR-012 re-derivation-by-every-verifier | Yes | T020-T024,T031,T033-T042,T064-T068,T076-T080 | U2 controls now constructive. |
| FR-013 evidence-availability | Yes | T036,T038,T040-T042 | U3 recovery now uses a qualifying approval. |
| FR-014 missing-code-objects | Yes | T006,T035-T038,T040-T042 | No empty-diff fallback. |
| FR-015 scope-of-fact-validation | Yes | T022,T024,T076,T078 | Listed evidence and answered requests. |
| FR-016 hn-serve-advancement-under-a-rule | Yes | T059-T069,T093 | Rule verdict and interim refusal. |
| FR-017 target-policy-must-equal-base-policy | Yes | T043,T046,T064,T068,T094-T095 | I4 target-tip contradiction removed. |
| FR-018 direct-protected-pushes-unchanged | Yes | T065-T068,T070,T074 | Maintainer path preserved. |
| FR-019 bounded-executing-actor-authority | Yes | T056-T057,T070,T074,T096-T099 | Containment and authorized reruns. |
| FR-020 recovery-of-an-interrupted-rule-merge | Yes | T054-T058,T096-T100 | Local repair and remote reruns. |
| FR-021 status-names-the-rule | Yes | T051-T052 | Rule signer and unverified annotation. |
| FR-022 compatibility | Yes | T001,T004,T011-T014,T081-T084,T091 | I5 declared legacy outcomes aligned. |
| FR-023 documentation-with-tested-examples | Yes | T010,T032,T047,T053,T058,T063,T069,T075,T086-T089,T100 | Corresponding guides and examples. |
| FR-024 union-validity-of-merge-facts | Yes | T012-T014,T031,T034,T037-T041,T049-T051,T078,T084,T090-T092 | Unchanged path (a) runs first. |
| FR-025 fail-closed-activation | Yes | T031,T037,T040-T041,T064,T078,T090-T093 | Boundary fixtures and interim guards. |
| NFR-001 fail-closed-verification | Yes | T033-T034,T038-T042,T076-T079 | Semantic and dependency tables separated. |
| NFR-002 verdict-agreement | Yes | T019,T024,T076-T078,T080,T094-T095 | Matched inputs and feasible controls. |
| NFR-003 executing-actor-containment | Yes | T056-T057,T074,T095-T099 | Authorization remains separately tested. |
| NFR-004 opt-out-compatibility | Yes | T014,T081-T084,T089 | Frozen tests and older-client checks. |
| NFR-005 bounded-validation-work | Yes | T023-T024,T031,T035,T037,T079 | Memo and 8 MiB refusal. |
| NFR-006 import-cost | Yes | T085 | Twenty-run p95 ratio at most 1.5. |
| NFR-007 regression-gate | Yes | T089; every WP gate | Formatting, vet, build, race and diff checks. |

The [story-to-WP map](tasks.md#requirement-to-work-package-map) still covers US1 preview/execution, US2 policy adoption, US3 verification, US4 hosted advancement/recovery, US5 reviewer counting, US6 containment and US7 compatibility. T025-T030/T091 support those actions through shared fixtures. All 11 constraints retain owners in the same map.

### Charter Alignment Issues

None confirmed in this round's scope. The refinements preserve the existing plan's charter commitments:

| Charter principle | Verification in the changed scope |
|-------------------|-----------------------------------|
| HN-001 Repository-Native Protocol Boundary | Fixture and test instructions add no service, database or dependency. |
| HN-002 Evidence Integrity | Candidate-shape controls rebind their evidence; legacy outcomes and signed-byte semantics remain unchanged. |
| HN-003 Hostile Input and Least Authority | L06 remains refused, missing evidence blocks admission, and semantic failures require their intended check. |
| HN-004 Immutable Conflict Recovery | Replacement controls are separate signed candidates; no instruction rewrites signed history. |
| HN-005 Protocol and Documentation Move Together | Both changed contracts agree with their WP consumers; implementation documentation and test assignments remain in the mission. |

The [plan's charter check and rollout gates](plan.md#charter-check) still require independent WP review and the Go quality gate. This analysis does not waive them.

### Unmapped Tasks

None identified. All 100 indexed tasks retain one WP owner and map to requirements, constraints, or shared fixture/documentation support. No dependency inversion or new write-scope collision was found.

### Metrics

The counts below derive from the current [requirement tables](spec.md#requirements), [subtask index](tasks.md#subtask-index), prompt declarations and this round's findings carrier.

- Total requirements: **32** (25 FR + 7 NFR).
- Total tasks: **100**, in **19** WPs.
- Assignment coverage: **32/32 (100%)**.
- Round-2 findings resolved: **4/4**.
- Ambiguity count: **0**.
- Duplication count: **0**.
- Critical issues count: **0**.
- Unresolved issues: **0 CRITICAL, 0 HIGH, 0 MEDIUM, 0 LOW**.

No feature test suite, race suite, build, vet or performance gate was run during this static planning analysis. No spec, plan, task, WP, contract or source remediation was applied. Only the analysis report is persisted in the repository through `record-analysis`; the requested external draft retains incremental evidence.

### Next Actions

Proceed with `/spec-kitty.implement --mission standing-acceptance-01M5VD2K` in the computed execution lanes. Implement the declared fixture controls and before/after assertions, then run each WP's required tests, full quality gate and independent review. No additional planning remediation is required by this report.

**Final verdict: READY.**
