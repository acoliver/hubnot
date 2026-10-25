---
schema_version: 1
artifact_type: spec-kitty.analysis-report
command: /spec-kitty.analyze
mission_slug: hn-serve-01M5P0NP
mission_id: 01M5P0NPCQXWXNN63MXCBQSRVP
generated_at: '2026-10-25T03:50:05.366465+00:00'
analyzer_agent: llxprt
input_artifacts:
  spec.md:
    path: kitty-specs/hn-serve-01M5P0NP/spec.md
    sha256: 7538d2cd472301db895798890cd6009dc72128740388611432350b3d1d6f7954
  plan.md:
    path: kitty-specs/hn-serve-01M5P0NP/plan.md
    sha256: cc899738ef404cd342ac35c6c8020ecd2256163822a1a221b5f7ac3a9009931d
  tasks.md:
    path: kitty-specs/hn-serve-01M5P0NP/tasks.md
    sha256: 4abd37ab21c36bed7d2c669be5653b1d54962858713ce3c219cecadf1d02690d
  charter:
    path: .kittify/charter/charter.yaml
    sha256: 404c65fe9e64919858d009c763644aefbfd369884d573d1986e3db7fa1f14bc9
verdict: unknown
issue_counts:
  medium:
  high:
  info:
  low:
  critical:
findings: []
---

---
verdict: ready
issue_counts:
  low: 0
  high: 0
  critical: 0
  medium: 1
  info: 0
findings:
- id: GOV-001
  severity: medium
  category: ambiguity
  summary: Charter CLI timeout applicability to the hosted push-verdict workload is undefined.
---


## Specification Analysis Report

### Scope and evidence

- **Mission:** `hn-serve-01M5P0NP`.
- **Reviewed revision:** `9836a52`, branch `mission/hn-serve`. Untracked review-cycle files under `tasks/WP01-*/` and `tasks/WP03-*/` were present and are not analysis inputs.
- **Previous analysis:** `792955e` (reviewed `7e0f4f4`). This is a delta re-analysis: the only analysis-input change since `792955e` is `tasks.md` (commit `7495ff3`). `spec.md`, `plan.md` and the charter hashes are unchanged.
- **Artifacts reviewed:** complete specification, plan, task index, all 14 WP prompts and 91 subtasks, data model, research, quickstart, six contracts, and charter YAML with its Markdown companion.
- **Prerequisites:** valid; required spec, plan and tasks exist. Available documents: data-model.md, plan.md, quickstart.md, research.md, spec.md, tasks.md. Target and base branch: `mission/hn-serve`.
- **Evidence boundary:** planning-artifact consistency and planned verification. Implementation tests, builds and runtime measurements were not run. Coverage indicates assigned work, not completed delivery.
- **Paths:** `spec.md`, `plan.md`, `tasks.md`, and `tasks/` below are relative to `kitty-specs/hn-serve-01M5P0NP/`. Charter paths are repository-relative.

### Delta since previous analysis

| Change | Location | Assessment |
|--------|----------|------------|
| `commands.go` ownership split from `WP11` to `WP03, WP11` with a scope note | `tasks.md:67-68` (File ownership table) | Consistent. `tasks/WP03-rejection-protocol-and-marker.md` lists `commands.go` in `owned_files` and limits WP03 to routing the three `git push` calls in `publishLocalFacts` through `servePushRef`. `tasks/WP11-all-trusted-selection.md` keeps `commands.go` and declares `dependencies: [WP03, WP04]`. In `lanes.json`, lane-b (WP03) and lane-c (WP04, WP11) both carry `commands.go` in `write_scope`, and lane-c depends on lane-b, so the two owners never run concurrently. The tasks.md rule that concurrently runnable WPs never share a file still holds. Lane-a (WP01 and others) does not own `commands.go`. |

No new finding results from this change. It touches no requirement, success criterion, story scenario or subtask, so the coverage tables below are unchanged.

### Active findings

| ID | Category | Severity | Location(s) | Summary | Recommendation |
|----|----------|----------|-------------|---------|----------------|
| GOV-001 | Ambiguity | MEDIUM | `.kittify/charter/charter.yaml:14-15`; `spec.md:197`; `plan.md:22` | Charter governance sets `cli_timeout_seconds: 2.0`; NFR-001 measures a hosted push-verdict workload at p95 under 5 seconds over 20 samples. The charter does not state whether its timeout includes server-side verdict execution. This leaves the applicable acceptance boundary unclear. | Clarify the timeout's scope and measurement boundary through a separate, explicit charter clarification, then align the mission's performance interpretation. Preserve the specified workload and measurement details unless deliberately revised. |

GOV-001 records an applicability ambiguity. A direct charter MUST conflict is unproven: the settings name general CLI timeout and hosted-verdict performance, respectively. No WP or requirement is demonstrated impossible as written. HIGH and CRITICAL are therefore unwarranted on the available evidence. Any confirmed charter MUST conflict would require mission alignment or a separate explicit charter update.

### Verification of previous findings

All five findings from `6d5865c` are resolved in the current prompts. Historical severities do not contribute to active counts.

| Prior ID | Prior severity | Disposition | Current evidence and resolution |
|----------|----------------|-------------|---------------------------------|
| AUD-004 | MEDIUM | Resolved | `tasks/WP13-quality-evidence-and-threat-model.md:129-130` removes the writable `/tmp` tmpfs, requires a read-only root with only the declared state and SSH paths writable, probes writes to `$HOME/outside`, `/tmp`, and an absolute outside path, and wraps all server-side invocations including administration and descendants. `tasks/WP14-git-lfs-transfer-and-quota.md:165` extends that boundary to LFS. |
| AUD-005 | HIGH | Resolved | `tasks/WP06-forced-command-session.md:234` puts disabled-LFS refusal in `TestServeSessionLFSSeamRefusedWhenDisabled`, gated by `serveLFSEnabled`, with an explanatory skip when enabled. `tasks/WP14-git-lfs-transfer-and-quota.md:95,116` sets the capability true and leaves `serve_session_test.go` unchanged. Both lifecycle phases can satisfy their gates. |
| AUD-006 | MEDIUM | Resolved | `tasks/WP08-hubnot-fact-ref-rules.md:107` assigns range-only budget measurement using already batched hosted-prefix events and the appended range. Totals and inclusive limits match the existing helper, equivalence coverage is required, and ordinary client replication remains unchanged. |
| AUD-007 | MEDIUM | Resolved | `tasks/WP08-hubnot-fact-ref-rules.md:107-108` sends all non-budget measurement errors to the diagnostic walk. It derives the full event ID from readable `event.json` even with an invalid signature, using the commit ID only when unreadable. T052 at line 144 explicitly covers altered-signature rejection with the payload-derived full ID. |
| AUD-008 | MEDIUM | Resolved | `tasks/WP01-server-state-foundation.md:177` requires bounded, sorted `Lstat` traversal of `state/tmp`, `lfs/incoming`, and `lfs/objects`, rejecting links, special files and unsafe modes, with a 100,000-entry cap and public-CLI fixtures. `tasks/WP14-git-lfs-transfer-and-quota.md:125-146` assigns corresponding LFS storage/use-boundary checks. |

### Coverage summary

Requirement keys combine stable spec identifiers with concise imperative labels. FR/NFR definitions are in `spec.md:169-204`; task definitions are in `tasks.md:83-173` and the corresponding WP prompts. Every row below has assigned work. Success criteria are listed separately from the 42 normative FR/NFR/constraint requirements.

| Requirement Key | Has Task? | Task IDs | Notes |
|-----------------|-----------|----------|-------|
| FR-001 optional-hosting | Yes | T001-T007, T034-T039, T076, T085 | Optional command and ordinary-remote regression. |
| FR-002 administer-repositories | Yes | T004, T028-T032 | Create, configure, inspect, recover; no push provisioning. |
| FR-003 register-key-actor-bindings | Yes | T003, T008-T009, T027, T032 | Full fingerprints, unique bindings, private registry. |
| FR-004 manage-authorized-keys | Yes | T010-T013, T027, T032-T033 | Restricted forced commands; outside bytes preserved. |
| FR-005 restrict-forced-commands | Yes | T034-T040, T082, T087, T090 | Closed grammar, path refusal, real SSH, LFS. |
| FR-006 attribute-pushes | Yes | T035-T036, T041, T043, T045-T046 | Registered actor attribution. |
| FR-007 validate-actor-chains | Yes | T022-T023, T025, T048, T051-T052 | Append-only range, dependencies, full-ID diagnostics. |
| FR-008 enforce-protected-governance | Yes | T020-T021, T025, T054-T059 | Base and destination policy checks. |
| FR-009 advance-only-tested-commit | Yes | T054-T060, T063-T065 | Trusted CI, exact fast-forward, no server commit. |
| FR-010 enforce-other-ref-rules | Yes | T042-T044, T046, T049-T053 | Proposal, memory, tags, actor and reserved refs. |
| FR-011 enforce-branch-ownership | Yes | T004, T044-T047 | Owner, maintainer and imported unowned branches. |
| FR-012 reject-push-atomically | Yes | T043, T046, T051-T052, T057, T059 | Mixed updates, unchanged refs, quarantine discard. |
| FR-013 report-structured-rejections | Yes | T014-T019, T035, T043, T046, T052, T059, T063 | Versioned lines, full IDs, reason catalogue. |
| FR-014 request-protected-advancement | Yes | T056, T061-T065 | Publish facts, maintainer check, exact branch request. |
| FR-015 sign-client-merge-fact | Yes | T024-T026, T063-T065 | Confirmed exact head and interrupted-operation recovery. |
| FR-016 preserve-local-merge | Yes | T061, T065, T085 | Existing no-remote and no-ff behavior. |
| FR-017 advertise-accepted-refs | Yes | T017-T019, T028, T068, T071-T072, T074 | Marker and actor/proposal refs. |
| FR-018 select-all-trusted | Yes | T068-T075, T077 | Discovery, validation and memory boundary. |
| FR-019 preserve-selection-parity | Yes | T068-T072, T076 | Existing selectors and saved formats. |
| FR-020 transfer-verified-lfs-content | Yes | T086-T088, T090-T091 | Round trip and hash mismatch refusal. |
| FR-021 enforce-lfs-quota | Yes | T004, T029-T030, T087-T091 | Inclusive quota, accounting, unsupported locks. |
| FR-022 serialize-protected-updates | Yes | T005, T037, T039, T054, T059, T066 | Lock, stale tip, 50 rounds of 10 concurrent requests. |
| FR-023 maintain-tested-documentation | Yes | T007, T013, T019, T026, T033, T040, T047, T053, T060, T067, T073, T078-T079, T083-T084, T091 | Owned examples, protocol docs and traceability. |
| NFR-001 measure-verdict-latency | Yes | T022, T048, T080 | 1,000 events, 20 samples, p95 under 5 seconds; GOV-001. |
| NFR-002 enforce-input-budgets | Yes | T030, T048-T052 | Five budgets, inclusive and one-over boundary cases. |
| NFR-003 fail-closed | Yes | T007, T018, T025, T032, T039, T041-T042, T046, T052, T059 | Invalid keys, state, paths, signatures and policy. |
| NFR-004 confine-unprivileged-writes | Yes | T001-T002, T012, T032, T036, T081, T090 | Root refusal, write audit and enforced boundary. |
| NFR-005 prevent-update-races | Yes | T005, T037, T054, T059, T066 | Exactly one winner per concurrency round. |
| NFR-006 verify-client-independently | Yes | T024, T059, T065, T072, T074-T077 | Fresh verifier and forged-marker rejection. |
| NFR-007 preserve-regression-gates | Yes | T025, T065, T072, T085; every WP gate | Formatting, race, vet, build, diff check; prior assertions protected. |
| NFR-008 validate-private-state | Yes | T002-T007, T012, T027, T032, T081, T088, T090 | Strict schemas/modes and bounded subtree checks. |
| C-001 keep-service-optional | Yes | T001, T007, T061, T068-T072, T076, T085 | Existing workflows remain available. |
| C-002 use-openssh-forced-commands | Yes | T008-T013, T034-T040, T082 | Installed Git protocol and real SSH round trip. |
| C-003 prefer-standard-library | Yes | T001-T013, T085-T091 | Planned foundation and LFS implementation; no new dependency planned. |
| C-004 apply-both-policies | Yes | T020-T021, T025-T026, T054-T059 | Base and destination policies. |
| C-005 avoid-untested-server-merge | Yes | T054-T060, T063-T065 | Fast-forward to tested head. |
| C-006 avoid-new-authority | Yes | T013, T020-T026, T036, T040, T053, T056, T064-T065, T073, T084 | No server signer or evidence authority. |
| C-007 preserve-immutable-history | Yes | T022-T023, T044, T048-T053, T055, T059-T060 | Append-only facts and successor recovery. |
| C-008 update-protocol-and-docs-together | Yes | T019, T026, T040, T047, T053, T060, T067, T073, T079, T084, T091 | Documentation moves with behavior. |
| C-009 test-public-boundaries | Yes | T032-T033, T038-T039, T046, T052, T059, T065-T066, T072, T074-T077, T080-T082, T090 | Public CLIs and real temporary repositories. |
| C-010 keep-lfs-last-separable | Yes | T037, T039, T079, T083, T086-T091 | Capability-gated seam and WP14 ownership. |
| C-011 exclude-adjacent-features | Yes | T084-T085 | Threat-model/scope audit and regression gate. |
| SC-001 exercise-six-brief-examples | Yes | T046, T052, T059, T065, T075, T077, T083 | Cross-WP public-CLI examples and traceability. |
| SC-002 preserve-rejection-integrity | Yes | T018, T039, T043, T046, T052, T059, T065, T090 | Parseable reasons and unchanged refs. |
| SC-003 govern-successful-tips | Yes | T054-T059, T065 | Trusted CI and acceptance under both policies. |
| SC-004 exercise-phase-zero-flow | Yes | T077-T078 | No manual actor/proposal selector or direct main push. |
| SC-005 reproduce-evidence | Yes | T024, T065, T067, T074-T075, T077, T083 | Fresh verifier reproduces advancement evidence. |
| SC-006 trace-and-review-delivery | Yes | T079, T083-T085; independent WP reviews | Requirements, tests, docs, review and final gate. |

### User-story coverage

All 27 scenarios have identifiable assigned acceptance work:

| Story | Scenarios | Planned task ownership | Actions covered |
|-------|-----------|------------------------|-----------------|
| US1 | 1-6 | T044-T046, T048, T052, T059 | Valid push, protected refusal, actor ownership, broken-signature full ID, rewrite refusal, ordinary branch ownership. |
| US2 | 1-5 | T027-T032, T034-T039, T082 | Provision/register/clone, unknown and removed keys, unsupported commands, unsafe paths. |
| US3 | 1-6 | T021, T025, T054-T059, T062-T067, T083 | Exact advancement and signed fact, fresh verification, stale ancestry, destination threshold, missing CI, concurrent maintainers. |
| US4 | 1-6 | T068, T070-T072, T074-T077 | Runner discovery, unpublished actors, explicit memory selection, ordinary-remote refusal, selector parity, Phase 0 flow. |
| US5 | 1-4 | T087-T090 | Byte-identical LFS round trip, hash mismatch, inclusive quota, unsupported locking. |

### Charter alignment

The requested root `charter/charter.md` is absent. The repository's authoritative runtime charter is `.kittify/charter/charter.yaml`; `.kittify/charter/charter.md` is its human-readable companion. Both were consulted.

No direct conflict with the active HN directives was established: repository-native optional service (HN-001), exact evidence (HN-002), hostile-input bounds and least authority (HN-003), immutable successor recovery (HN-004), and synchronized tests/documentation (HN-005). GOV-001 records the separate performance-setting applicability ambiguity. Resolving it must preserve charter authority; any charter change requires a separate explicit workflow.

### Detection passes

- **Duplication:** no actionable duplicate requirement or task ownership found. Contract and WP restatements serve distinct consumer layers.
- **Ambiguity:** GOV-001 is the active scope ambiguity. No other unresolved behavioral placeholder was identified. Staged refusal seams have explicit replacement owners.
- **Underspecification:** all five historical gaps are resolved by current prompt instructions and planned fixtures. No additional actionable edge-case gap was identified.
- **Charter:** no demonstrated MUST conflict; performance applicability remains MEDIUM pending clarification.
- **Coverage:** all 42 normative requirements, six success criteria and 27 story scenarios have assigned work.
- **Inconsistency:** no remaining blocking terminology, data-model, dependency or acceptance contradiction was identified after the five fixes. The `commands.go` co-ownership is sequenced by WP and lane dependencies.

### Unmapped tasks

None identified. All T001-T091 are assigned across WP01-WP14 and support a requirement, story, supporting harness/recovery, documentation, or a quality gate. T031 lock-exec recovery, T038 transport harness and T079/T083 documentation/traceability are legitimate supporting tasks. Coverage mappings list representative direct owners rather than every transitive dependency.

### Metrics

- **Total requirements:** 42 normative requirements: 23 FR, 8 NFR, 11 constraints.
- **Additional success criteria:** 6.
- **Total tasks:** 91 unique subtasks across 14 WPs.
- **Coverage:** 42/42 normative requirements, 100% assigned; 6/6 success criteria; 27/27 story scenarios.
- **Ambiguity count:** 1.
- **Duplication count:** 0.
- **Critical issues count:** 0.
- **Historical findings verified resolved:** 5.
- **Active severity counts:** CRITICAL 0, HIGH 0, MEDIUM 1, LOW 0, INFO 0.
- **Final verdict:** ready.

### Next Actions

1. Implementation may proceed with the non-blocking governance ambiguity recorded, for example `/spec-kitty.implement --mission hn-serve-01M5P0NP` when implementation is authorized.
2. Address GOV-001 through a separate explicit charter-scope clarification. If needed afterward, use `/spec-kitty.plan --mission hn-serve-01M5P0NP` to document the applicable performance boundary and rerun `/spec-kitty.analyze --mission hn-serve-01M5P0NP`.
3. During implementation/review, verify the five fixes against their assigned acceptance cases and run the prescribed gates. This report claims planned coverage only.
4. No remediation or implementation edits are authorized by this analysis. Concrete remediation suggestions can be provided separately upon request.
