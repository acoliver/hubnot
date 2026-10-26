---
schema_version: 1
artifact_type: spec-kitty.analysis-report
command: /spec-kitty.analyze
mission_slug: standing-acceptance-01M5VD2K
mission_id: 01M5VD2K9G554Y2CVBMGPN30MQ
generated_at: '2026-10-26T10:26:12.935555+00:00'
analyzer_agent: gpt-6.1-sol
input_artifacts:
  spec.md:
    path: kitty-specs/standing-acceptance-01M5VD2K/spec.md
    sha256: 77961a9daea9adbce59c2ed0459d319235da481d57f98579c8eb7b5404f778cc
  plan.md:
    path: kitty-specs/standing-acceptance-01M5VD2K/plan.md
    sha256: c1c6ac0a8007c9eb47d1571c9bd74fccd36f8e2a9ff9e1ef1f620d13b3c285e3
  tasks.md:
    path: kitty-specs/standing-acceptance-01M5VD2K/tasks.md
    sha256: c2db894d5708d9a893499ec13667891881d661f4f4eda142b7ad4858a47ce4db
  charter:
    path: .kittify/charter/charter.yaml
    sha256: 404c65fe9e64919858d009c763644aefbfd369884d573d1986e3db7fa1f14bc9
verdict: blocked
issue_counts:
  high: 2
  medium: 2
  low: 0
  critical: 0
  info: 0
findings:
- id: I4
  severity: high
  category: inconsistency
  summary: Agreement fixtures require targets equal to the signed base while also requiring a different target policy.
- id: I5
  severity: high
  category: inconsistency
  summary: The harness requires every legacy payload to verify successfully, including the deliberately invalid L06 control.
- id: U2
  severity: medium
  category: underspecification
  summary: Shallow positive controls require the same immutable candidate even for candidate-shape defects.
- id: U3
  severity: medium
  category: underspecification
  summary: The missing-evidence recovery fixture uses an approval that the default rule does not count.
---

## Specification Analysis Report

**Mission:** `standing-acceptance-01M5VD2K`
**Round:** 2
**Verdict:** **NOT READY** for implementation. Two HIGH findings leave contradictory executable-test instructions. Two MEDIUM findings need fixture-construction refinements.

### Scope and evidence

Static analysis of `spec.md`, `plan.md`, `tasks.md`, all 19 WP prompts, four contracts, research, data model, quickstart, lane ownership, remediation decisions and security-sensitive source at `33ea0c302af2d4aaa31518a98951fa2e7400a1b8` on `feat/standing-acceptance`. The report was assembled from the incremental external draft `../.scratch/analyze-standing-acceptance-draft.md`. `WPnn` below identifies the unique `tasks/WPnn-*.md` prompt in this mission. Locations use 1-based lines at that HEAD.

The charter authority is `.kittify/charter/charter.yaml`, with human companion `.kittify/charter/charter.md`. No charter exists at `/charter/charter.md` in this repository. Prerequisites succeeded with all required artifacts available, matching base/target branch and no warnings. The session startup check reported Spec Kitty 3.2.7 with action `none`. The starting checkout was clean.

Maintainer decisions A-F are settled requirements. This analysis checks their expression in implementable artifacts. It preserves union validity, intentional mixed-version support, the evidence-only immutable-fact boundary, the reviewed object-borrowing speedup, split test snapshots, authorized publication-only reruns, and separate satisfaction/authorization comparisons.

Executed checks confirmed 100 unique task IDs T001-T100, exactly one prompt owner per task, 19 WPs, and same-lane dependency order. `git diff --exit-code d881d9b HEAD -- '*.go' go.mod` and the pre-existing-test diff were empty. The older-client reference `7a47e6952a427ad7e3a3766c221fd334d9331950` remains an ancestor. Tool versions: Git 2.53.0 and go1.26.8 linux/amd64.

Source inspection covered event verification/signing and strict resource keys; local relationships and merge admission; replication dependency checks and promotion; shallow accepted-chain closure and proposal-status guards; live policy evaluation; local automerge blockers; serve actor-chain admission and protected verdict ordering; client advancement, publication and interruption recovery. No feature code or planning artifact was edited. Feature tests, the race suite, build and lint gates were not run for this documentation-only analysis. Resolution below means the planning correction is concrete and source-grounded, rather than implemented or dynamically proven.

### Findings

| ID | Category | Severity | Location(s) | Summary | Recommendation |
|----|----------|----------|-------------|---------|----------------|
| I4 | Inconsistency | HIGH | WP11:L195-204; WP15:L172-182; `contracts/serve-rule-advancement.md:L110-121`; WP14:L165; `automerge.go:L262-264,L297-302` | Decision E fixes the original authorization mismatch, but T094/T095 require every agreement fixture's target to equal the signed base while also including a target-policy-mismatch fixture. T095 explicitly requires asserting that equality before every comparison. At the same commit the exact policy digest cannot differ. The amendment-tip exception does not reconcile the universal base-equality assertion. | Define target tips per fixture and require equality **across the compared surfaces**. For the policy-mismatch case, put both targets at the amendment commit, explicitly allow target != signed base, and make the head descend from the amendment so the server reaches step 7a. Keep authorization and transport tests separate as decision E requires. |
| I5 | Inconsistency | HIGH | WP06:L166-176,L184; WP03:L127-128; `contracts/rule-merge-fact.md:L164-173`; `event.go:L100-108`; `resource_model.go:L522-534` | T030 step 7 requires every raw legacy payload to verify successfully in-process. L06 is deliberately a resource event with an unknown `rule` key and must fail the unchanged strict decoder. Following the harness self-test and the legacy contract simultaneously cannot pass WP06's gate. The legacy section also calls every fixture older-client-accepted before listing the negative control. | Assert each fixture's declared verification outcome: L01-L05 succeed, L06 fails with the existing unknown-field error. Keep L06 as a negative control and qualify the introductory legacy description. Do not relax strict resource decoding. |
| U2 | Underspecification | MEDIUM | WP09:L108-115; WP06:L163; `contracts/rule-merge-fact.md:L151-160`; WP16:L108-110 | The original unrelated-gap loophole is closed, but T041 now requires each passing control to use “the same candidate ... with the defect removed.” F15-F18 are candidate-shape defects; especially F17/F18 have a manual-path violation inherent in their signed base/head. No evidence-only repair can produce a valid path-(b) fact for that same candidate. The control-construction exception is unspecified. | Use the same candidate for fact-only mutations. For candidate-shape rows, create a corresponding valid candidate and rebind its reviews, requests, results and merge fact, preserving matched selection and hydration strategy. Require both complete closure and the intended semantic refusal; retain the ban on accepting gaps as semantic success. |
| U3 | Underspecification | MEDIUM | WP08:L129-133; WP06:L130,L144; `contracts/rule-merge-fact.md:L145` | F21's recovery case cites an unpublished approval from S and expects publishing it to permit admission. The default rule counts R only; S is the trusted-but-uncounted reviewer used by F09. Publishing S's approval fixes availability while leaving an `evidence-item` defect. The policy/evidence override needed for the promised positive recovery is absent. | Withhold an otherwise qualifying R approval, or explicitly include S in this fixture's rule reviewers. Keep all other evidence sufficient and assert missing dependency before publication, then successful admission after publication. |

### Round-1 finding status

| Round-1 ID | Status | Verification and remaining limitation |
|------------|--------|---------------------------------------|
| C1 | Resolved | Shared `admitMergeFact` runs the unchanged maintainer path first (WP05 T090, WP07 T031, WP08 T037). WP07's shallow interim refusal is replaced only in WP09. WP13's `hn-rule` interim refusal is removed atomically with WP14's verdict. B1-B5 and the activation table define supported versus unsupported consumers at independently landable boundaries. Intentional upgraded-only rule support is accepted. |
| C2 | Resolved | `Event.Rule` uses `json:"-"`; merged-only canonical recognition never introduces a new verification refusal. Noncanonical keys mean no claim. L01-L05 cover ignored legacy extensions and L06 preserves strict resource rejection. HEAD `resourceJSONKeys` excludes `json:"-"` fields. I5 concerns contradictory test instructions, rather than an instruction to change legacy decoding. |
| I1 | Resolved | Spec FR-022/FR-024, the union contract and WP17 T084 distinguish rule-only refusals from readable-policy maintainer merges. Older/newer agreement explicitly includes absent-rule and existing-rule annotations plus decisions and extra approvals. The current maintainer checks confirm these counterexamples. |
| I2 | Resolved | WP17 T081/T082 and WP18 T089 freeze pre-existing tests at `d881d9b` and build the older executable from `7a47e69`. Git confirms no Go/test edits since the frozen snapshot and preserves the reviewed speedup merge. |
| I3 | Resolved | WP19 T096 branches before command eligibility and bypasses the normal post-sync merged gate. It requires an executing actor's own already-admitted path-(b) fact with matching proposal, rule and policy. T097 publishes and confirms containment of the fact's commit without signing, appending or advancing. T099 covers record, republish, completed reruns and authorization/containment refusals. |
| I4 | Partially resolved | Authorization is correctly separated from satisfaction, and T094/T095 assign executable comparisons. The contradictory matched-tip instructions in finding I4 remain. The accepted decision E needs no change. |
| U1 | Resolved | WP17 T082 gives `TestStandingCompatOlderClient` sole ownership of the binary in its parent `t.TempDir()`. All consumers are sequential subtests; no `sync.Once`, package-level path or shorter-lived cache remains. |
| U2 | Partially resolved | WP09 T041 and WP16 T076 require complete closure, exact semantic checks and per-row passing controls; dependency-gap cases are separate. The same-candidate restriction for immutable candidate-shape defects needs the refinement described in U2. |

### Coverage Summary Table

Coverage records concrete task assignments, not completed implementation or passing tests. Stable keys retain the requirement ID plus a title-derived slug. Every FR/NFR has at least one assigned task; the findings concern the soundness of several assignments.

| Requirement Key | Has Task? | Task IDs | Notes |
|-----------------|-----------|----------|-------|
| FR-001 acceptance-mode-field | Yes | T001-T005 | Presence/raw decoding and validation. |
| FR-002 non-maintainer-executing-actors | Yes | T002,T004-T005 | Mode-dependent actor authority. |
| FR-003 path-safety-for-standing-acceptance | Yes | T003-T005 | `.hn` and protected-selector overlap. |
| FR-004 minimum-independent-review | Yes | T002,T004-T005 | Positive approval threshold. |
| FR-005 rule-reviewers | Yes | T001-T005,T016-T019,T043-T046 | Narrowed counting in both modes; vetoes retained. |
| FR-006 author-approval-under-standing-acceptance | Yes | T016,T019,T043,T046 | Self-approval excluded for standing rules. |
| FR-007 policy-inspection | Yes | T007-T010 | Deterministic show/check output. |
| FR-008 preview | Yes | T043-T047,T094 | Keyless and side-effect free. |
| FR-009 execution-without-per-proposal-acceptance | Yes | T043,T048-T052,T054-T057,T070-T074 | Other blockers remain. |
| FR-010 rule-merge-fact | Yes | T011-T013,T020-T024,T049-T050,T072 | Canonical claim and exact evidence. |
| FR-011 rule-merge-signer | Yes | T021,T024,T048-T052 | Includes maintainer executing actors. |
| FR-012 re-derivation-by-every-verifier | Yes | T020-T024,T031,T033-T042,T064-T068,T076-T080 | U2 control refinement. |
| FR-013 evidence-availability | Yes | T036,T038,T040-T042 | U3 recovery fixture refinement. |
| FR-014 missing-code-objects | Yes | T006,T035-T038,T040-T042 | One effective object store; no empty diff. |
| FR-015 scope-of-fact-validation | Yes | T022,T024,T076,T078 | Listed evidence and answered requests only. |
| FR-016 hn-serve-advancement-under-a-rule | Yes | T059-T069,T093 | Rule verdict and fail-closed staging. |
| FR-017 target-policy-must-equal-base-policy | Yes | T043,T046,T064,T068,T094-T095 | I4 blocks the agreement fixture recipe. |
| FR-018 direct-protected-pushes-unchanged | Yes | T065-T068,T070,T074 | No-rule paths retain acceptance. |
| FR-019 bounded-executing-actor-authority | Yes | T056-T057,T070,T074,T096-T099 | Forbidden commands and narrow reruns. |
| FR-020 recovery-of-an-interrupted-rule-merge | Yes | T054-T058,T096-T100 | Local repair, remote repair, publication-only rerun. |
| FR-021 status-names-the-rule | Yes | T051-T052 | Full signer and unverified maintainer annotation. |
| FR-022 compatibility | Yes | T001,T004,T011-T014,T081-T084,T091 | I5 contradicts the L06 self-test. |
| FR-023 documentation-with-tested-examples | Yes | T010,T032,T047,T053,T058,T063,T069,T075,T086-T089,T100 | Guides and threat model in mission. |
| FR-024 union-validity-of-merge-facts | Yes | T012-T014,T031,T034,T037-T041,T049-T051,T078,T084,T090-T092 | Path (a) first, unchanged. |
| FR-025 fail-closed-activation | Yes | T031,T037,T040-T041,T064,T078,T090-T093 | B1-B5 plus interim shallow/wire guards. |
| NFR-001 fail-closed-verification | Yes | T033-T034,T038-T042,T076-T079 | U2/U3 fixture refinements. |
| NFR-002 verdict-agreement | Yes | T019,T024,T076-T078,T080,T094-T095 | I4 prevents the specified comparison setup. |
| NFR-003 executing-actor-containment | Yes | T056-T057,T074,T095-T099 | Authorization tested separately. |
| NFR-004 opt-out-compatibility | Yes | T014,T081-T084,T089 | Separate snapshots and owning parent. |
| NFR-005 bounded-validation-work | Yes | T023-T024,T031,T035,T037,T079 | Per-pass memo and 8 MiB refusal. |
| NFR-006 import-cost | Yes | T085 | Twenty runs, p95 ratio at most 1.5. |
| NFR-007 regression-gate | Yes | T089; every WP gate | Formatting, vet, build, race and diff. |

**Story/action inventory:** US1 covers preview, local execution and fresh verification; US2 covers manual adoption and unsafe drafts; US3 covers four admission surfaces and legacy/dependency cases; US4 covers hosted advancement, late vetoes, actor/destination checks and interruption recovery; US5 covers narrowed review and self-approval; US6 covers blockers and forbidden authority; US7 covers opt-out and older-client agreement/refusal. T091-T100 supply the amended activation, agreement and remote-rerun actions. No story lacks an assigned implementation/test concern.

**Additional criteria:** All 11 constraints have explicit WP owners in `tasks.md:L143-153`. SC-001/SC-003-SC-005 map to the CLI and matrix suites; SC-006 to T087/T089 and independent review. SC-002 has a specific zero-maintainer-candidate-event local case in T052 with `Requesters: [A]`. The default hosted fixture instead requests CI as M; reuse the A-requested variant and explicitly assert zero M-signed candidate events for the hosted SC-002 evidence as well. This remains a suggested coverage strengthening, rather than an additional finding.

### Charter Alignment Issues

No unresolved charter MUST conflict was confirmed. The former C1/C2 conflicts are addressed by the settled union/activation/legacy design. The remaining findings require artifact corrections; none calls for weakening the charter.

- **HN-001:** Git-native state and transport, optional `hn serve`, standard-library Go and flat `package main`; no new database, service or dependency.
- **HN-002:** Legacy signed bytes remain unchanged. Path (a) retains its semantics; path (b) binds exact candidate, policy, code and qualifying evidence. Strict resource decoding remains intact. I5 must be repaired in the test instructions.
- **HN-003:** All consumers either run full path-(b) derivation or refuse unsupported semantics. The shallow and `hn-rule` interim guards prevent permissive activation windows. Missing objects never become empty diffs; changed-path output remains bounded. An upgraded consumer intentionally accepting a valid rule-only fact while an older consumer refuses it is part of the approved rollout.
- **HN-004:** Revision and repair retain signed history; reruns create at most the missing fact. Publication-only reruns create no fact or advancement.
- **HN-005 and gates:** Documentation/examples and relevant tests are assigned in the same mission. Each WP specifies formatting, vet, build, race tests and diff checks, with independent review. The analysis does not claim those implementation gates have run.

### Security checks and accepted boundaries

1. **Admission ordering:** `admitMergeFact` is the only chooser. Unchanged path (a) succeeds before any rule-only object dependency is read; malformed annotations never cause a new legacy parse error. Defective rule-only facts have no fallback authority.
2. **Intermediate WP order:** parser-only states preserve the existing maintainer validator. WP07 explicitly refuses rule-only projection in shallow repositories until WP09. WP13 refuses every parsed `hn-rule` request until WP14's evaluator lands. `lanes.json` and prompt dependencies place WP09 before local signing in WP11 and WP19 before the final matrix. No new permissive staged window was identified.
3. **Older clients:** the planned parent-owned executable tests exercise unknown policy fields, rule-only refusal, readable-policy maintainer facts with annotations, and legacy history including strict-resource refusal. HEAD confirms permissive non-resource decoding plus maintainer/digest/decision checks. The old executable itself was not built during this analysis.
4. **Remote recovery:** T096 recognizes only the current identity's own admitted rule fact, matching candidate, rule and base digest, and requires rule actor membership and serve identification. T097 checks containment of the fact's commit, never performs an advancement or signs a replacement. `publishLocalFacts` publishes all local collaboration refs, as the WP19 risk section acknowledges; it does not push branch refs. The read-tip helper may fetch remote-tracking state. These existing helper effects do not confer merge authority.
5. **Immutable evidence boundary:** later or omitted vetoes outside listed evidence do not invalidate a rule fact. A modified executing-actor client on an ordinary remote can ignore a veto; that residual risk is explicitly accepted and assigned to the threat model. `hn serve` enforces current vetoes when advancing protected branches.
6. **Object safety:** HEAD's accepted-object borrowing remains unchanged. The one-store rule means a complete effective Git object view, including approved borrowed objects; the mission must preserve the reviewed speedup. No missing object may be interpreted as an empty changed-path list.

### Unmapped Tasks

None. All 100 unique subtasks belong to exactly one of 19 WP prompts and map to FR/NFR requirements, explicit constraints, or shared test/documentation support. T025-T030/T091 provide fixture infrastructure. No duplicate task IDs, missing task owners or same-lane dependency inversions were found. The computed roots, evaluator and harness lanes have disjoint write scopes; shared source/test/document files are serialized in the main lane.

### Metrics

- Total requirements: **32** (25 FR + 7 NFR).
- Total tasks: **100**, in **19** WPs.
- Requirements with at least one task: **32/32 (100%)** assignment coverage.
- Ambiguity findings: **0**.
- Duplication findings: **0**.
- Inconsistency findings: **2**.
- Underspecification findings: **2**.
- Critical issues: **0**.
- Issue counts: **0 CRITICAL, 2 HIGH, 2 MEDIUM, 0 LOW**.
- Round-1 dispositions: **6 resolved, 2 partially resolved**.

### Next Actions

Resolve I4 and I5 before `/spec-kitty.implement`; also refine U2/U3 while correcting the fixture instructions. Keep decisions A-F unchanged. Any remediation requires a separate explicitly authorized editing step. This analysis applies no edits to those targets.

Suggested follow-up: use `/spec-kitty.tasks --mission standing-acceptance-01M5VD2K` with the specific prompt/contract refinements above; update the agreement contract consistently with `/spec-kitty.plan --mission standing-acceptance-01M5VD2K` if needed. Then rerun `/spec-kitty.analyze --mission standing-acceptance-01M5VD2K` and require a fresh recorded verdict.

**Final verdict: NOT READY.**
