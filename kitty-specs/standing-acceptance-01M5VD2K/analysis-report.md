---
schema_version: 1
artifact_type: spec-kitty.analysis-report
command: /spec-kitty.analyze
mission_slug: standing-acceptance-01M5VD2K
mission_id: 01M5VD2K9G554Y2CVBMGPN30MQ
generated_at: '2026-10-26T09:21:18.330592+00:00'
analyzer_agent: gpt-6.1-sol
input_artifacts:
  spec.md:
    path: kitty-specs/standing-acceptance-01M5VD2K/spec.md
    sha256: ee2335d84a338b76e80977ec1f5e7b7448860ccea35046c9801d23e04efdb1c4
  plan.md:
    path: kitty-specs/standing-acceptance-01M5VD2K/plan.md
    sha256: f8c55a3fcca004a6bbc447a0d53ab24fd3963f74613cfb83f00f57343014f859
  tasks.md:
    path: kitty-specs/standing-acceptance-01M5VD2K/tasks.md
    sha256: 3cbbee4642fdf8c08a171b6fb7af3bb59d27358f66448fbcb0d8a5e470df8d39
  charter:
    path: .kittify/charter/charter.yaml
    sha256: 404c65fe9e64919858d009c763644aefbfd369884d573d1986e3db7fa1f14bc9
verdict: blocked
issue_counts:
  high: 4
  low: 0
  medium: 2
  critical: 2
  info: 0
findings:
- id: C1
  severity: critical
  category: charter
  summary: Independently landable parser and admission WPs expose inconsistent validation of rule-bearing facts.
- id: C2
  severity: critical
  category: charter
  summary: The unconditional rule-key scan invalidates previously valid extension-bearing signed history.
- id: I1
  severity: high
  category: inconsistency
  summary: The unconditional older-client rejection guarantee is false for rule-bearing maintainer acceptance facts.
- id: I2
  severity: high
  category: inconsistency
  summary: The frozen-test audit already fails against the old base after the reviewed sync-speedup merge.
- id: I3
  severity: high
  category: inconsistency
  summary: Remote interruption recovery after recording is blocked before the promised sync and idempotent branch.
- id: I4
  severity: high
  category: inconsistency
  summary: Keyless preview eligibility cannot equal actor-authorized server verdicts for every advancement fixture.
- id: U1
  severity: medium
  category: underspecification
  summary: The process-wide older-binary cache is specified with a shorter test-scoped temporary-directory lifetime.
- id: U2
  severity: medium
  category: underspecification
  summary: The shallow semantic defect matrix can pass through unrelated dependency gaps without exercising derivation.
---

## Specification Analysis Report

**Mission:** `standing-acceptance-01M5VD2K`  
**Audience:** the maintainer and the agent refining the implementation artifacts.  
**Verdict:** **NOT READY** for implementation. Two CRITICAL and four HIGH findings require resolution.

### Scope and evidence

Independent static cross-check of `spec.md`, `plan.md`, `tasks.md`, all 18 WP prompts, the associated contracts, data model and walkthrough, and security-sensitive source at `d881d9bcd0ade4fa1bce91061954a8582e8d9318` on `feat/standing-acceptance`. Charter authority is `.kittify/charter/charter.yaml`, with its human-readable companion `.kittify/charter/charter.md`; this repository does not place it at `/charter/charter.md`.

Prerequisites succeeded with tasks available and matching target/base branch. Spec Kitty 3.2.7 reported no available upgrade. The starting checkout was clean. `7a47e6952a427ad7e3a3766c221fd334d9331950` remains an ancestor of HEAD and a valid older-client reference. The reviewed sync-speedup merge `d881d9b` is preserved.

Source checks covered `automerge.go`, `policy.go` evaluation and merge validation, `protected_checks.go`, `quarantine.go` dependency/admission and object borrowing, `shallow.go` closure, `store.go` relationships, `governance.go` merging, `serve_client.go` advancement/recovery, `serve_protected.go` verdicts, and `serve_verdict_facts.go` actor-chain admission. References below use 1-based line numbers at that HEAD. `WPnn` means the unique `tasks/WPnn-*.md` prompt in this mission.

No feature code or planning artifacts were changed. Counterexamples below follow the specified tasks and current source; they are not claims of executed feature tests or evidence that hostile historical payloads exist in this checkout. The regression suite, race tests, build and lint gates were not run for this documentation-only analysis.

### Findings

| ID | Category | Severity | Location(s) | Summary | Recommendation |
|----|----------|----------|-------------|---------|----------------|
| C1 | Charter alignment | CRITICAL | `tasks.md:L25-48,L61-65`; WP03:L88-126; WP07:L100-104,L133; WP08:L110-116; WP13:L94-112; `policy.go:L547-567`; `quarantine.go:L1347-1367` | Parser support lands before all admission paths understand `rule`. A maintainer-signed fact with sufficient valid acceptance decisions and `rule=nonexistent` still passes the old semantic checks. After WP07 local projection refuses it while import and server actor-chain admission remain permissive until WP08. No refusal guard or atomic activation boundary is specified. | Define fail-closed activation across all consumers. Add downgrade-shaped rule-plus-decision fixtures at each independently landable boundary, and make intermediate consumers refuse unsupported semantics. Apply the same boundary discipline to WP13 parsing `hn-rule` before WP14 interprets it. |
| C2 | Charter alignment | CRITICAL | `spec.md:L223,L245`; `plan.md:L38`; WP03:L100-116,L134-157; `event.go:L100-109`; `.kittify/charter/charter.yaml:L69-74` | A valid signed legacy `review.submit` with an ignored `rule` extension is accepted by the current permissive decoder. WP03's unconditional scan rejects the identical authenticated bytes. Its own risk section acknowledges this possibility; checking only Hubnot-generated fixtures cannot establish historical compatibility for users' repositories. This conflicts with the immutable-history guarantee and HN-002. | Specify a verifiable legacy/feature activation boundary that preserves previously valid history while enforcing new semantics. Add signed legacy extension-bearing regression fixtures across admission paths. Keep the charter unchanged. |
| I1 | Inconsistency | HIGH | `spec.md:L150-160,L223`; WP17:L65,L116-134; WP07:L114,L133; `event.go:L100-109`; `policy.go:L547-567` | Older clients ignore `rule`, so they can admit a maintainer-signed merge with enough acceptance-decision evidence even when it names an absent or inappropriate rule. WP17 tests only approval/result evidence and then promises rejection of every rule merge fact. It misses the counterexample. | Limit the guarantee to a realizable boundary, such as correctly encoded standing facts and policies carrying new fields. Add the legacy-policy, maintainer-signed rule-plus-decisions counterexample and document the observed old/new outcomes. Changing this mission cannot retrofit an old executable. |
| I2 | Inconsistency | HIGH | `spec.md:L233,L244`; WP17:L95; WP18:L145-146 | The required empty pre-existing-test diff against `7a47e69` already fails at analysis HEAD: `replication_borrow_test.go` existed there and now has 129 additions and 12 deletions from the reviewed sync-speedup merge. A correct implementation preserving that merge cannot satisfy this audit as written. | Separate the immutable older-binary reference from the implementation-start audit snapshot. Freeze pre-existing tests at `d881d9b` or an explicitly recorded later start snapshot; audit only mission edits against it. Preserve the reviewed merge and retain `7a47e69` for compatibility tests. |
| I3 | Inconsistency | HIGH | WP15:L100-101,L145-167; `contracts/automerge-cli.md:L113-119`; `automerge.go:L55-80,L233-237,L654-659`; `serve_client.go:L171-178,L335-348` | The remote interruption table promises that rerunning after `record` syncs the unpublished fact and reaches the already-merged branch. Both the command's initial eligibility check and the post-sync inspection reject merged proposals first. WP15 changes only `automergeRemote`; WP12 repairs only missing facts. The recorded-but-unpublished recovery route is unassigned and unreachable under the prescribed changes. | Specify an authorized, publication-only/idempotent remote rerun path before both merged blockers. Assign its command-level changes and tests. Verify `record` and `republish` interruptions finish without another fact or advancement. Keep ordinary local already-merged refusal. |
| I4 | Inconsistency | HIGH | `spec.md:L209,L231`; WP04:L102-111; WP15:L166; `contracts/serve-rule-advancement.md:L48`; `contracts/rule-merge-fact.md:L141-143`; `automerge.go:L55-71` | NFR-002 requires every advancement verdict to equal keyless preview eligibility, and WP15 explicitly includes executor fixtures. An unrelated actor X can obtain an eligible keyless preview of E's satisfied rule, while X's push must fail `advance-not-rule-executor`. The preview has no actor input and deliberately loads no identity. | Separate shared rule-satisfaction agreement under identical inputs from requester authorization and transport checks, or add an explicit actor-aware comparison surface. Define matching branch, policy, code and fact inputs, and assign executable tests for each comparison. |
| U1 | Underspecification | MEDIUM | WP17:L101-106; WP17:L125-136 | The old binary is built in the caller's `t.TempDir()` and its path cached process-wide with `sync.Once`. The first top-level test's cleanup deletes the binary before later consumers unless all consumers share a longer-lived parent test. No lifetime arrangement is specified. | Group consumers under one owning parent test, give a suite-owned cache explicit cleanup after all consumers, or avoid process-wide caching. Require a full run with multiple compatibility consumers. |
| U2 | Underspecification | MEDIUM | `spec.md:L230-231`; WP09:L100-106; WP16:L103-105,L121-123 | Every shallow semantic-negative row may count an earlier dependency gap as success. This can produce a 100% rejection column without testing that semantic defects reach rule derivation. The separate complete-closure positive control does not require the negative fixtures to have equivalent closure. | Hydrate code, policy and evidence for semantic F01-F18 cases and assert the intended derivation check. Keep missing-dependency cases separate. Include valid controls with the same selection/object arrangement. |

### Security counterexamples and boundaries

- **C1 staged rollout:** choose a legacy base policy, a maintainer M, a candidate and enough valid M acceptance decisions. Add a syntactically valid `rule` name that does not exist and sign the merge as M. WP03's parser accepts it; the existing merge validators ignore the field. WP07 then makes the local result differ from import/server until WP08. M already holds manual merge authority, so this counterexample demonstrates invalid rule-fact admission and cross-surface disagreement; it does not claim that the interim code authorizes a non-maintainer branch update. WP13 similarly needs an explicit unsupported-semantics refusal until its evaluator lands.
- **C2 historical bytes:** start with a signed non-resource review that has an extra top-level `rule: "annotation"`. Current `verifyEvent` authenticates the original bytes while `json.Unmarshal` ignores the extra field. WP03 rejects that same signed review. Re-encoding the payload would change its signature and ID, which cannot repair immutable history. A finite fixture audit proves compatibility only for those fixtures.
- **I1 old executable:** use an old-field-only base policy so strict policy parsing does not hide the evidence-path result. M signs a merge carrying both `rule` and sufficient acceptance decisions. The old executable checks maintainer authority and decisions; the new design treats the presence of `rule` as selecting a different validator. Test and document this boundary explicitly.
- **Accepted design preserved:** live evaluation sees current vetoes; immutable fact validation sees only listed evidence and answered requests. The report does not ask admission to invalidate facts after later vetoes. The modified-client risk on ordinary remotes is already accepted; `hn serve` enforces vetoes when advancing protected branches.
- **Merged object safety preserved:** HEAD borrows accepted objects into quarantine only for complete repositories and declines borrowing for shallow, promisor and alternate-backed stores (`quarantine.go:L1939-2021`). Planned one-store tests must account for an effective Git object view that legitimately includes approved alternates. No finding calls for reverting that speedup or treating absent objects as an empty diff.

### Coverage Summary Table

Coverage below means a concrete task assignment exists. It does not mean the task is implemented, its test has run, or its acceptance contract is sound. Stable keys retain the source requirement ID plus a title-derived slug.

| Requirement Key | Has Task? | Task IDs | Notes |
|-----------------|-----------|----------|-------|
| FR-001 acceptance-mode-field | Yes | T001-T005 | Decoder and validation. |
| FR-002 non-maintainer-executing-actors | Yes | T002,T004-T005 | Mode-dependent actor authority. |
| FR-003 path-safety-for-standing-acceptance | Yes | T003-T005 | Literal `.hn` and protected-selector restrictions. |
| FR-004 minimum-independent-review | Yes | T002,T004-T005 | Positive threshold. |
| FR-005 rule-reviewers | Yes | T001-T005,T016-T019,T043-T046 | Counting narrowed in both modes; vetoes retained. |
| FR-006 author-approval-under-standing-acceptance | Yes | T016,T019,T043,T046 | Standing self-approval excluded. |
| FR-007 policy-inspection | Yes | T007-T010 | Deterministic show/check. |
| FR-008 preview | Yes | T043-T047 | Keyless and read-only; I4 affects agreement. |
| FR-009 execution-without-per-proposal-acceptance | Yes | T043,T048-T050,T052,T054-T058,T070-T074 | Other execution blockers preserved. |
| FR-010 rule-merge-fact | Yes | T011-T013,T020-T024,T049-T050,T072 | C1/C2 affect activation and legacy interpretation. |
| FR-011 rule-merge-signer | Yes | T021,T024,T048,T050,T052 | Includes a maintainer listed as executing actor. |
| FR-012 re-derivation-by-every-verifier | Yes | T020-T024,T031,T033-T039,T040-T042,T064-T068,T076-T080 | C1 staging; U2 semantic shallow coverage. |
| FR-013 evidence-availability | Yes | T036,T038,T040-T042 | Selected facts and answered requests. |
| FR-014 missing-code-objects | Yes | T006,T035-T038,T040-T042 | No empty diff on missing objects. |
| FR-015 scope-of-fact-validation | Yes | T022,T024,T076,T078 | Listed evidence only; later vetoes do not invalidate facts. |
| FR-016 hn-serve-advancement-under-a-rule | Yes | T059-T069 | Server derives standing rule and authenticates actor. |
| FR-017 target-policy-must-equal-base-policy | Yes | T043,T046,T064,T068 | Local and hosted destination policies. |
| FR-018 direct-protected-pushes-unchanged | Yes | T065-T068,T070,T074 | No-rule/direct paths retain acceptance. |
| FR-019 bounded-executing-actor-authority | Yes | T056-T057,T070,T074 | Refusal and unchanged refs. |
| FR-020 recovery-of-an-interrupted-rule-merge | Yes | T054-T055,T057,T071-T074 | I3 identifies the record/publication recovery gap. |
| FR-021 status-names-the-rule | Yes | T051-T052 | Full signer/fact identity. |
| FR-022 compatibility | Yes | T001,T004,T011-T014,T081-T084 | C2 and I1 invalidate unconditional guarantees. |
| FR-023 documentation-with-tested-examples | Yes | T010,T032,T047,T053,T058,T063,T069,T075,T086-T089 | Examples accompany their owning mission changes. |
| NFR-001 fail-closed-verification | Yes | T033-T034,T038-T042,T076-T077,T079 | C1 activation; U2 shallow negative controls. |
| NFR-002 verdict-agreement | Yes | T019,T024,T074,T076-T078,T080 | I4 requires a realizable comparison contract. |
| NFR-003 executing-actor-containment | Yes | T056-T057,T074 | Public forbidden-command checks. |
| NFR-004 opt-out-compatibility | Yes | T081-T084,T089 | I2 audit snapshot; U1 binary lifetime. |
| NFR-005 bounded-validation-work | Yes | T023-T024,T031,T035,T037,T079 | Memo and 8 MiB refusal. |
| NFR-006 import-cost | Yes | T085 | Twenty runs per variant, p95 ratio at most 1.5. |
| NFR-007 regression-gate | Yes | T089; every WP gate | Formatting, vet, build, race and diff checks. |

**Story/action inventory:** 36 numbered scenarios across US1-US7 map to tasks. US1 covers preview, local execution, absence of decisions and fresh verification; US2 covers adoption and invalid drafts; US3 covers all admission boundaries and missing evidence; US4 covers hosted advancement, veto, actor/destination checks and repair; US5 covers narrowed reviewers/self-approval; US6 covers blockers and forbidden commands; US7 covers opt-out and old-client behavior. Their concrete contradictions and evidence weaknesses are listed above rather than counted as unmapped work.

**Additional success-criterion coverage:** SC-001 and SC-003-SC-005 map to the public-CLI/matrix suites; SC-006 maps to T087/T089 and independent WP reviews. SC-002 has a concrete no-maintainer-event local case at WP11:L166 using pipeline `Requesters: [A]`. The default harness instead requests CI as M. The remote test instructions should also reuse the A-requested variant and assert the absence of M-signed candidate events to make the local-and-remote SC-002 evidence explicit; no additional finding is counted for this refinement.

### Charter Alignment Issues

- **C1, HN-003:** an independently landable validator transition must validate hostile claims before use. Route all consumers through refusal or complete validation before enabling new semantics. The lane graph alone does not provide an atomic protocol activation barrier.
- **C2, HN-002 and immutable-history guarantees:** the new parser must distinguish newly governed semantics without rejecting previously valid authenticated history solely because an ignored extension key now has meaning. The WP03 fixture audit does not discharge this obligation.
- **Other checked constraints:** the plan uses Go's standard library, root `package main`, repository-native Git storage, real temporary repositories/remotes, public CLI acceptance boundaries and a keyless server. No new service/database/dependency is planned. C-003 explicitly prohibits edits to existing assertions; I2 corrects the audit boundary without permitting such edits. Documentation and executable examples are assigned within the same mission, as HN-005 requires. Quality gates and independent reviews are stated for every WP.

### Unmapped Tasks

None. All 89 unique subtasks T001-T089 belong to the 18 WPs and map to functional requirements, non-functional requirements, constraints, or test/documentation support. T025-T030 provide the shared public-boundary harness; T014 supplies parsing cost/history audit evidence. C-001-C-011 have explicit ownership in `tasks.md:L138-148`. There are no duplicate task IDs or unexplained extra implementation tasks.

### Metrics

- Total functional and non-functional requirements: **30** (23 FR, 7 NFR).
- Additional constraints: **11**; success criteria: **6**; numbered story scenarios: **36**.
- Total tasks: **89 unique subtasks**, grouped into **18 work packages**.
- Assigned requirement coverage: **100%** (30/30 have at least one task); demonstrated implementation coverage is unmeasured.
- Requirements with zero task assignments: **0**.
- Ambiguity count: **0** unresolved vague-threshold findings; **2** separate underspecification findings.
- Duplication count: **0** actionable duplicate-requirement findings.
- Findings: **8**. CRITICAL **2**, HIGH **4**, MEDIUM **2**, LOW **0**.
- Critical issues count: **2**.
- Overflow findings: **0**.

### Verification evidence

- Prerequisite check returned valid mission context, spec, plan and tasks.
- `git merge-base --is-ancestor 7a47e69 HEAD` succeeded.
- `git cat-file -e 7a47e69:replication_borrow_test.go` succeeded, confirming the file is pre-existing.
- `git diff --numstat 7a47e69 HEAD -- '*_test.go'` reported `129 12 replication_borrow_test.go`; the narrowed patch shows changes from the reviewed merge. This is the reproducible I2 audit failure, not a feature regression test failure.
- The subtask index contains exactly 89 unique IDs, T001-T089, and matches all 18 WP prompt ranges.
- No implementation gate results are claimed. The source-derived C1/C2/I1 counterexamples require executable regression fixtures during approved refinement/implementation.

### Next Actions

1. Resolve **C1 and C2 before `/implement`**, keeping the charter authoritative. Refine the protocol activation/legacy-history boundary in `/spec-kitty.specify --mission standing-acceptance-01M5VD2K` and align the plan and contracts through `/spec-kitty.plan --mission standing-acceptance-01M5VD2K`.
2. Refine I1's older-client guarantee and I4's agreement inputs. Preserve the existing live-veto versus immutable-fact distinction.
3. Refine the task/WP ownership and dependencies through `/spec-kitty.tasks --mission standing-acceptance-01M5VD2K`: repair remote publication recovery, separate audit and old-binary snapshots, define cache lifetime, and require complete-closure semantic shallow negatives.
4. Rerun `/spec-kitty.analyze --mission standing-acceptance-01M5VD2K` against the refined artifacts before implementation.

These are proposed follow-up actions. This analysis applies no remediation and requests no maintainer decision, per the delegated run instructions.

**Final verdict: NOT READY. CRITICAL: 2; HIGH: 4; MEDIUM: 2; LOW: 0.**
