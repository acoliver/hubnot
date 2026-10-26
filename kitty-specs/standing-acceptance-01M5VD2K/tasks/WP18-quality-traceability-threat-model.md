---
work_package_id: WP18
title: Import timing, traceability, threat model and regression gate
dependencies:
- WP12
- WP15
- WP16
- WP17
requirement_refs:
- C-001
- C-003
- C-006
- C-010
- C-011
- FR-023
- NFR-004
- NFR-006
- NFR-007
planning_base_branch: feat/standing-acceptance
merge_target_branch: feat/standing-acceptance
branch_strategy: Planning artifacts for this mission were generated on feat/standing-acceptance. During /spec-kitty.implement this WP may branch from a dependency-specific base, but completed changes must merge back into feat/standing-acceptance unless the human explicitly redirects the landing branch.
subtasks:
- T085
- T086
- T087
- T088
- T089
phase: Phase 6 - Evidence, compatibility and quality
history:
- !!omap
  - timestamp: '2026-10-26T06:54:14Z'
  - agent: system
  - action: Prompt generated via /spec-kitty.tasks
agent_profile: implementer-ivan
authoritative_surface: standing_quality_test.go
create_intent:
- standing_quality_test.go
execution_mode: code_change
owned_files:
- standing_quality_test.go
- docs/threat-model.md
- docs/governance-v0.md
- docs/protocol-v0.md
role: implementer
tags: []
tracker_refs: []
---

# WP18: Import timing, traceability, threat model and regression gate

## ⚡ Do This First: Load Agent Profile

Use the `/ad-hoc-profile-load` skill to load the agent profile specified in the frontmatter, and behave according to its guidance before parsing the rest of this prompt.

- **Profile**: `implementer-ivan`
- **Role**: `implementer`
- **Agent/tool**: `codex`

If no profile is specified, run `spec-kitty agent profile list` and select the best match for this work package's `task_type` and `authoritative_surface`.

---

## Objective

Close the mission with measured and checked evidence:

- **NFR-006**: importing an actor chain with 20 rule merge facts into a repository holding 1,000 events takes at most 1.5 times as long as importing the same chain shape with 20 acceptance merge facts, at the 95th percentile over 20 runs.
- **FR-023**: the `standing-rule-merge-fact` example in `docs/protocol-v0.md`, checked against a freshly recorded fact.
- **SC-006**: a "Standing acceptance traceability" section in `docs/governance-v0.md` that maps every FR, NFR, SC and user-story scenario of this mission to tests, with a test that keeps it honest.
- **C-006 / OQ-11**: the trust-boundary text of `docs/threat-model.md` updated, including the accepted residual risk for modified clients on ordinary remotes.
- **NFR-007**: the full gate passes on the integrated candidate, and no pre-existing test assertion changed (C-003, NFR-004).

## Context

- Read spec "Success Criteria", NFR-006, NFR-007, C-001, C-003, C-006, C-010, C-011 and OQ-8, OQ-11; plan "Risks" (the modified-client row) and "Cross-cutting: documentation"; [research.md](../research.md) "Adversarial evidence" (rows A2 and A7) and decision `01M5VF0W7NWESJH5RAD46NSYMN`; [contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Tested example".
- Start with `spec-kitty agent action implement WP18 --agent <name> --mission standing-acceptance-01M5VD2K`.
- Existing helpers to reuse without editing: `serveQualityPercentile95` and the measurement pattern of `TestServeQualityPushVerdictTimeNFR001`; the traceability parser pattern of `serveQualityTraceRows`, `serveQualityTestName` and `serveQualityDeclaration` (`serve_quality_test.go`). `TestServeQualityTraceabilityTestsExist` refuses non-`hn serve` IDs in `docs/serve-v0.md`, which is why this mission's traceability lives in `docs/governance-v0.md`.
- From earlier WPs: the harness (WP06), the execution flow (WP11), the serve flows (WP14, WP15), the matrix (WP16), compatibility (WP17).

## Branch Strategy

- **Planning/base branch at prompt creation**: `feat/standing-acceptance`
- **Final merge target for completed work**: `feat/standing-acceptance`
- **Actual execution workspace is resolved later**: `/spec-kitty.implement` decides the lane workspace path from `lanes.json`. Each computed lane gets exactly one worktree, and sequential WPs in the same lane reuse it.
- **If the resolved workspace differs from your expectation**: trust the path printed by `spec-kitty agent action implement`.
- **If human instructions contradict these fields**: stop and resolve the intended landing branch before working.

## Ownership

| File | Change |
|----|----|
| `standing_quality_test.go` | NEW. `TestStandingQuality...`. |
| `docs/threat-model.md` | CHANGE. Two sections named in T088. WP17 already updated "Compatibility and recovery"; leave it. |
| `docs/governance-v0.md` | CHANGE. New "Standing acceptance traceability" section; consistency fixes only elsewhere. |
| `docs/protocol-v0.md` | CHANGE. The `standing-rule-merge-fact` example; consistency fixes only elsewhere. |

## Subtasks

### Subtask T085: Import timing test (NFR-006)

**Steps**:

1. Fixture, built once: a repository with 1,000 events spread over the harness participants (issues, comments, reviews; use in-process `appendEvent` per participant clone for speed, then publish with raw pushes), plus two variants of one signer chain appended on top:
   - **rule**: 20 candidates, each with one approval and one passing result, and 20 rule merge facts signed by E;
   - **acceptance**: the same 20 candidates' shape with one maintainer acceptance decision each, and 20 acceptance merge facts signed by M.
   Put each variant in its own bare remote so each run imports exactly one variant.
2. Measurement: prepare one verifier that already holds the 1,000 base events; for each of 20 runs per variant, copy the verifier directory, run `hn sync origin` against the variant's remote and time the command. Alternate variants run by run to spread host noise.
3. Assert `p95(rule) <= 1.5 * p95(acceptance)` with `serveQualityPercentile95`. Log both distributions with `t.Logf`.
4. Skip under `-short` with a visible reason. If the bound fails on the gate host, do not loosen it: record the numbers, check that the memo (WP05, WP07, WP08) computes one changed-path list per `(base, head)`, and raise a follow-up issue as the research premortem says.

### Subtask T086: `standing-rule-merge-fact` protocol example and test

**Steps**:

1. In `docs/protocol-v0.md`, "Runs and governance events", after WP07's rule merge fact paragraph, add `<!-- example: standing-rule-merge-fact -->` followed by the JSON of [contracts/rule-merge-fact.md](../contracts/rule-merge-fact.md) "Wire shape" with placeholder values.
2. `TestStandingQualityDocumentedRuleMergeFact` (its source contains the exact marker string): run the local US1 flow, read E's latest fact with `git show refs/hn/actors/<E>:event.json`, and assert its key set equals the documented key set, `rule` equals `ordinary`, `policy` is the base digest, `evidence` has two sorted full IDs (the approval and the result), and `commit` is the merge commit.

### Subtask T087: "Standing acceptance traceability" section and its checking test

**Steps**:

1. Add `## Standing acceptance traceability` at the end of `docs/governance-v0.md` with tables:
   - requirements: one row per FR-001 to FR-023 and NFR-001 to NFR-007, with the backticked names of the tests that prove it;
   - success criteria: SC-001 to SC-006;
   - user-story scenarios: US1-1 to US7-3 (every numbered acceptance scenario of the spec).
   The first cell of each data row is the ID; tests are named with backticks.
2. `TestStandingQualityTraceability`: parse the section (adapt the parser pattern of `serveQualityTraceRows` into a local `standingQualityTraceRows`); require every ID of the spec to appear exactly once; refuse unknown IDs; require every named test to be declared in some `*_test.go` file (`^func TestName(`); require at least one test per row.
3. Read the spec's requirement and scenario IDs from `kitty-specs/standing-acceptance-01M5VD2K/spec.md` at test time so the check cannot drift from the spec; skip with a reason if the file is absent (for example in a source archive without `kitty-specs/`).

### Subtask T088: Threat-model update

**Steps** in `docs/threat-model.md`:

1. "Optional protected checks and native automerge": the paragraph that says automerge permits only executing maintainers after acceptance requirements hold must describe both acceptance modes. Add plain statements: under acceptance mode `rule`, acceptance authority moves from per-proposal maintainer decisions to the maintainer-accepted policy amendment that adopted the rule; the executing actor gains authority over exactly that rule; policy amendments and protected-check changes stay manual; a standing rule cannot cover `.hn` or protected-check paths; at least one independent approval is required and the author's own approval never counts; every verifier re-derives the rule from the base policy, the candidate's code and the listed evidence.
2. Residual risk (accepted by the maintainer with the plan): a modified executing-actor client on an ordinary remote can omit a current change request or failure from its view and record a rule merge fact that verifies, because facts are judged by their own evidence (OQ-8), as acceptance merge facts are and as a malicious maintainer could today. `hn serve` enforces vetoes at advancement time; recommend `hn serve` with protected branches for repositories that adopt standing rules. Also state A7: case-variant directories such as `.HN/` are ordinary content because Git paths are case-sensitive and Hubnot reads only `.hn/...`.
3. "Governance of protected branches" (under "hn serve trust boundary"): the server now evaluates a named standing rule for an advancement request, including the vetoes it holds, requires the requester to be one of the rule's executing actors, and refuses when the destination policy differs from the base policy; direct pushes still need maintainer acceptance; the server still signs nothing.
4. Style: plain direct sentences, descriptive headings, no em dashes, and no rhetorical contrasts.

### Subtask T089: Documentation consistency pass, pre-existing test audit and full gate

**Steps**:

1. Read the four changed documents end to end and fix contradictions left by earlier WPs (for example a sentence that still says automerge needs maintainer acceptance in every mode). Keep each fix minimal and inside the owning section.
2. `TestStandingQualityDocsStyle`: the lines added by this mission to `docs/governance-v0.md`, `docs/protocol-v0.md`, `docs/serve-v0.md` and `docs/threat-model.md` contain no em dash (U+2014). Compute "added lines" as the lines of the standing sections and examples you can locate by heading and marker; if that is impractical, check the whole of the new sections only.
3. Pre-existing test audit (C-003, NFR-004): `git diff 7a47e6952a427ad7e3a3766c221fd334d9331950 -- $(git ls-tree --name-only 7a47e69 -- '*_test.go')` must be empty. Record the command and its empty output in the Activity Log.
4. C-001: `git diff 7a47e69 -- go.mod go.sum` is empty; every new file is `package main` in the repository root.
5. C-010: no code or doc introduces model-family metadata, issue requirement classes, review-round caps, releases or GitHub publishing (search the mission diff and record the result).
6. C-011: nothing was pushed; the work stays on `feat/standing-acceptance`.
7. Full gate on the integrated candidate: `gofmt -l .` prints nothing; `go vet ./...`; `go build ./...`; `go test -race ./...` (without `-short`); `git diff --check`. Record durations and the Go and Git versions.

## Test Strategy

Timing, example, traceability and style tests, then the full gate. Run `go test -race -run 'TestStandingQuality' ./...`, then the full gate.

## Definition of Done

- [ ] T085 to T089 complete; full gate passes.
- [ ] **NFR-006**: p95 ratio at most 1.5, numbers logged.
- [ ] **FR-023**: every `standing-*` example in the four documents is executed by a test that contains its marker; `TestServeQualityDocumentedExamplesAreExecuted` passes.
- [ ] **SC-006**: the traceability section covers every ID of the spec and names only existing tests.
- [ ] **C-006**: threat model updated, including the accepted residual risk.
- [ ] **NFR-004 / C-003**: the pre-existing test audit is empty.
- [ ] **NFR-007**: gate green.

## Risks

- The timing fixture is large; build it once per test and copy directories for runs instead of rebuilding.

## Reviewer Guidance

- Read the threat-model diff for the residual-risk paragraph and the `hn serve` recommendation.
- Run the traceability test after deleting one row to see it fail.

## Activity Log

- 2026-10-26T06:54:14Z – system – Prompt generated via /spec-kitty.tasks
